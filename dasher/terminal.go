// Package dasher implements the screen model and control-code decoder of a
// Data General Dasher D200 display terminal.
//
// The D200 does not use ANSI escape sequences. It uses single-byte control
// codes (shown here in octal, as DG documented them), a few multi-byte
// sequences introduced by 020 (write window address) and 036 (command
// prefix), and it answers two host queries.
package dasher

import "sync"

const (
	Cols = 80
	Rows = 24
)

// Attribute bits for a character cell.
type Attr uint8

const (
	AttrBlink Attr = 1 << iota
	AttrDim
	AttrUnderline
	AttrReverse
)

// Cell is one character position on the screen.
type Cell struct {
	Ch   byte
	Attr Attr
}

// D200 control codes (octal, as in the DG documentation).
const (
	cNUL         = 000
	cPrintForm   = 001
	cBlinkEnable = 003
	cBlinkDisabl = 004
	cReadAddr    = 005
	cBell        = 007
	cHome        = 010
	cTab         = 011
	cNL          = 012
	cEraseEOL    = 013
	cErasePage   = 014
	cCR          = 015
	cBlinkOn     = 016
	cBlinkOff    = 017
	cWriteAddr   = 020
	cPrintScreen = 021
	cRollEnable  = 022
	cRollDisable = 023
	cUnderOn     = 024
	cUnderOff    = 025
	cCursorUp    = 027
	cCursorRight = 030
	cCursorLeft  = 031
	cCursorDown  = 032
	cDimOn       = 034
	cDimOff      = 035
	cCommand     = 036
	cDEL         = 0177
)

type parseState int

const (
	stNormal parseState = iota
	stAddrCol
	stAddrRow
	stCommand
	stSkipOne // swallow one parameter byte of an unsupported 036 sequence
	stEsc     // after ESC: ANSI sequences from modern hosts are swallowed
	stCSI     // inside an ANSI CSI sequence (ESC [ ... final byte)
	stEscMid  // ESC followed by intermediate bytes, e.g. ESC ( B
	stString  // inside an OSC/DCS string (ESC ] ... BEL or ESC \)
	stStrEsc  // ESC seen inside an OSC/DCS string
)

// ModelIDResponse is what the terminal sends back when the host issues the
// "read model ID" command (036 C). The bytes after 036 o # identify the
// model, status and firmware. These values are a best-effort default and
// can be changed if a particular host program expects something else.
var ModelIDResponse = []byte{036, 'o', '#', 041, 0, 0}

// Terminal is the D200 screen state. It is safe for concurrent use: the
// serial reader feeds it while the GUI reads snapshots.
type Terminal struct {
	mu sync.Mutex

	cells [Rows][Cols]Cell
	x, y  int
	attr  Attr

	rollEnabled  bool
	blinkEnabled bool

	state   parseState
	addrCol byte

	// Reply is called with bytes the terminal must send back to the host
	// (cursor-position and model-ID reports). It may be nil.
	Reply func([]byte)
	// Bell is called when the host sends 007. It may be nil.
	Bell func()
	// Title is called when the host sets the window title with an xterm
	// OSC sequence (ESC ] 0; or 2; text BEL). Some DG emulators use this
	// to show their status. It may be nil.
	Title      func(string)
	eraseOnDEL bool

	osc      []byte // collected OSC text
	oscIsOSC bool   // true for OSC (ESC ]), false for DCS/PM/APC
	newTitle *string
}

// SetEraseOnDEL makes a received 0177 erase the previous character instead
// of being ignored (useful on a loopback plug with no host).
func (t *Terminal) SetEraseOnDEL(on bool) {
	t.mu.Lock()
	t.eraseOnDEL = on
	t.mu.Unlock()
}

// New returns a cleared terminal in its power-on state.
func New() *Terminal {
	t := &Terminal{}
	t.Reset()
	return t
}

// Reset returns the terminal to its power-on state.
func (t *Terminal) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reset()
}

func (t *Terminal) reset() {
	t.erasePage()
	t.rollEnabled = true
	t.blinkEnabled = true
	t.state = stNormal
}

// Feed processes bytes received from the host. Callers should strip the
// parity bit beforehand if the line is running 7-bit with parity.
func (t *Terminal) Feed(data []byte) {
	var replies [][]byte
	bells := 0
	t.mu.Lock()
	for _, b := range data {
		r, bell := t.feedByte(b)
		if r != nil {
			replies = append(replies, r)
		}
		if bell {
			bells++
		}
	}
	title := t.newTitle
	t.newTitle = nil
	t.mu.Unlock()
	// Callbacks run outside the lock so they may safely call back in.
	if title != nil && t.Title != nil {
		t.Title(*title)
	}
	if t.Reply != nil {
		for _, r := range replies {
			t.Reply(r)
		}
	}
	if t.Bell != nil && bells > 0 {
		t.Bell()
	}
}

func (t *Terminal) feedByte(b byte) (reply []byte, bell bool) {
	switch t.state {
	case stAddrCol:
		t.addrCol = b
		t.state = stAddrRow
		return nil, false
	case stAddrRow:
		t.state = stNormal
		// 0177 in either position means "leave this coordinate unchanged".
		if t.addrCol != 0177 {
			t.x = int(t.addrCol&0177) % Cols
		}
		if b != 0177 {
			t.y = int(b&0177) % Rows
		}
		return nil, false
	case stCommand:
		t.state = stNormal
		return t.command(b), false
	case stSkipOne:
		t.state = stNormal
		return nil, false
	case stEsc:
		// A D200 has no escape sequences. Linux hosts often send ANSI ones
		// anyway (e.g. bash's ESC [ ? 2004 h); drop them rather than print
		// them as junk.
		switch {
		case b == '[':
			t.state = stCSI
		case b == ']' || b == 'P' || b == '^' || b == '_':
			// OSC (window title etc.), DCS, PM, APC: a string up to BEL or ESC \.
			t.state = stString
			t.oscIsOSC = b == ']'
			t.osc = t.osc[:0]
		case b >= 040 && b <= 057:
			t.state = stEscMid
		default:
			t.state = stNormal
		}
		return nil, false
	case stCSI:
		if b >= 0100 && b <= 0176 {
			t.state = stNormal
		}
		return nil, false
	case stEscMid:
		if b >= 060 && b <= 0176 {
			t.state = stNormal
		}
		return nil, false
	case stString:
		switch b {
		case cBell:
			t.endString()
		case 033:
			t.state = stStrEsc
		default:
			if len(t.osc) < 256 {
				t.osc = append(t.osc, b)
			}
		}
		return nil, false
	case stStrEsc:
		if b == '\\' {
			t.endString()
		} else {
			t.state = stString
		}
		return nil, false
	}

	switch b {
	case cDEL:
		// A real D200 ignores DEL. Optionally treat it as "erase the previous
		// character", which is handy on a loopback plug with no host.
		if t.eraseOnDEL {
			if t.x > 0 {
				t.x--
			} else if t.y > 0 {
				t.x, t.y = Cols-1, t.y-1
			}
			t.cells[t.y][t.x] = Cell{Ch: ' '}
		}
	case cNUL, cPrintForm, cPrintScreen:
		// Ignored (no printer attached).
	case cBlinkEnable:
		t.blinkEnabled = true
	case cBlinkDisabl:
		t.blinkEnabled = false
	case cReadAddr:
		return []byte{037, byte(t.x), byte(t.y)}, false
	case cBell:
		return nil, true
	case cHome:
		t.x, t.y = 0, 0
	case cTab:
		t.x = (t.x/8 + 1) * 8
		if t.x >= Cols {
			t.x = 0
			t.lineFeed()
		}
	case cNL:
		t.x = 0
		t.lineFeed()
	case cEraseEOL:
		for i := t.x; i < Cols; i++ {
			t.cells[t.y][i] = Cell{Ch: ' '}
		}
	case cErasePage:
		t.erasePage()
	case cCR:
		t.x = 0
	case cBlinkOn:
		t.attr |= AttrBlink
	case cBlinkOff:
		t.attr &^= AttrBlink
	case cWriteAddr:
		t.state = stAddrCol
	case cRollEnable:
		t.rollEnabled = true
	case cRollDisable:
		t.rollEnabled = false
	case cUnderOn:
		t.attr |= AttrUnderline
	case cUnderOff:
		t.attr &^= AttrUnderline
	case cCursorUp:
		t.y = (t.y + Rows - 1) % Rows
	case cCursorDown:
		t.y = (t.y + 1) % Rows
	case cCursorRight:
		t.x++
		if t.x >= Cols {
			t.x = 0
			t.y = (t.y + 1) % Rows
		}
	case cCursorLeft:
		t.x--
		if t.x < 0 {
			t.x = Cols - 1
			t.y = (t.y + Rows - 1) % Rows
		}
	case cDimOn:
		t.attr |= AttrDim
	case cDimOff:
		t.attr &^= AttrDim
	case cCommand:
		t.state = stCommand
	case 033:
		t.state = stEsc
	default:
		if b >= 040 && b < 0177 {
			t.put(b)
		}
		// Other control codes are not used by the D200 and are ignored.
	}
	return nil, false
}

// command handles the byte following the 036 command prefix.
func (t *Terminal) command(b byte) []byte {
	switch b {
	case 'C': // read model ID
		return append([]byte(nil), ModelIDResponse...)
	case 'D': // reverse video on (D200 with reverse-video option; D210/D211)
		t.attr |= AttrReverse
	case 'E': // reverse video off
		t.attr &^= AttrReverse
	case 'F':
		// Extended D210/D211 command group (not part of the D200). Swallow
		// the sub-command byte so it is not displayed as text.
		t.state = stSkipOne
	}
	return nil
}

func (t *Terminal) put(b byte) {
	t.cells[t.y][t.x] = Cell{Ch: b, Attr: t.attr}
	t.x++
	if t.x >= Cols {
		t.x = 0
		t.lineFeed()
	}
}

// lineFeed moves down one row. With roll enabled the screen scrolls at the
// bottom; with roll disabled the cursor wraps to the top row.
func (t *Terminal) lineFeed() {
	if t.y < Rows-1 {
		t.y++
		return
	}
	if !t.rollEnabled {
		t.y = 0
		return
	}
	copy(t.cells[:Rows-1], t.cells[1:])
	for i := range t.cells[Rows-1] {
		t.cells[Rows-1][i] = Cell{Ch: ' '}
	}
}

func (t *Terminal) erasePage() {
	for y := range t.cells {
		for x := range t.cells[y] {
			t.cells[y][x] = Cell{Ch: ' '}
		}
	}
	t.x, t.y = 0, 0
	t.attr = 0
}

// Snapshot is a consistent copy of the screen for rendering.
type Snapshot struct {
	Cells        [Rows][Cols]Cell
	X, Y         int
	BlinkEnabled bool
	RollEnabled  bool
}

// Snapshot copies the current screen state.
func (t *Terminal) Snapshot(s *Snapshot) {
	t.mu.Lock()
	s.Cells = t.cells
	s.X, s.Y = t.x, t.y
	s.BlinkEnabled = t.blinkEnabled
	s.RollEnabled = t.rollEnabled
	t.mu.Unlock()
}

// Text returns row y as a plain string (for tests and copy-to-clipboard).
func (s *Snapshot) Text(y int) string {
	b := make([]byte, Cols)
	for x := range b {
		b[x] = s.Cells[y][x].Ch
	}
	return string(b)
}

// endString finishes an OSC/DCS string. "0;text" and "2;text" set the
// window title.
func (t *Terminal) endString() {
	t.state = stNormal
	if !t.oscIsOSC {
		return
	}
	str := string(t.osc)
	if len(str) >= 2 && (str[:2] == "0;" || str[:2] == "2;") {
		title := str[2:]
		t.newTitle = &title
	}
}
