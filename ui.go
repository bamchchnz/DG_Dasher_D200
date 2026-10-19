package main

import (
	"bytes"
	"fmt"
	"image/color"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/gomono"

	"github.com/bamchchnz/dasher200/dasher"
	"github.com/bamchchnz/dasher200/link"
)

type mode int

const (
	modeTerminal mode = iota
	modeSetup
	modeHelp
)

const (
	hexRows      = 9 // title + 8 lines of traffic
	traceKeep    = 400
	bytesPerLine = 9
)

var altName = func() string {
	if runtime.GOOS == "darwin" {
		return "Option"
	}
	return "Alt"
}()

// traceLine is one line of the diagnostic panel.
type traceLine struct {
	tx   bool
	data []byte
}

// Game is the Ebitengine application.
type Game struct {
	cfg  *Config
	prof Profile // active settings

	term *dasher.Terminal
	snap dasher.Snapshot

	mu          sync.Mutex
	line        link.Conn
	lineErr     string
	connecting  bool
	backToSetup bool // connection failed while connecting from setup

	prompt  *promptState
	recall  *dasher.Recall // Up/Down arrow line recall
	mouse   mouseState     // selection, links, clipboard
	pasteCh chan []byte    // clipboard text waiting to be sent

	hostTitle  string // window title set by the host (e.g. emulator status)
	titleShown string
	trace      []traceLine
	bellUtil   time.Time

	mode     mode
	showHex  bool
	hexOctal bool
	ticks    int

	statusMsg   string
	statusUntil time.Time

	setup setupState

	faceSrc  *text.GoTextFaceSource
	face     *text.GoTextFace
	faceSize float64
	cellW    float64
	cellH    float64
	textTop  float64
	scale    float64
	quit     bool
}

func newGame(cfg *Config, prof Profile) (*Game, error) {
	src, err := text.NewGoTextFaceSource(bytes.NewReader(gomono.TTF))
	if err != nil {
		return nil, err
	}
	g := &Game{cfg: cfg, prof: prof, term: dasher.New(), faceSrc: src, hexOctal: true, scale: 1,
		recall: dasher.NewRecall(recallLines), pasteCh: make(chan []byte, 4)}
	g.term.Reply = func(b []byte) { g.send(b, false) }
	g.term.Title = func(t string) {
		g.mu.Lock()
		g.hostTitle = strings.TrimSpace(t)
		g.mu.Unlock()
	}
	g.term.Bell = func() {
		g.mu.Lock()
		g.bellUtil = time.Now().Add(150 * time.Millisecond)
		g.mu.Unlock()
	}
	return g, nil
}

// ---------------------------------------------------------------------------
// Connection

// connect opens the connection described by the active profile. Network
// connections can take a while and may ask for a password, so it runs in
// the background; the status bar shows progress.
func (g *Game) connect() {
	g.disconnect()
	prof := g.prof
	g.mu.Lock()
	g.connecting = true
	g.lineErr = ""
	g.mu.Unlock()
	if prof.Kind != "serial" && prof.Kind != "" {
		g.term.Reset()
	}
	g.recall.Reset()
	h := link.Handlers{
		OnData: func(b []byte) {
			g.recall.HostOutput()
			g.term.Feed(b)
		},
		OnTrace: g.addTrace,
		OnError: func(err error) {
			g.mu.Lock()
			g.line = nil
			g.lineErr = err.Error()
			g.mu.Unlock()
		},
		Prompt:    g.ask,
		StripHigh: prof.StripHighBit,
	}
	go func() {
		c, err := openConn(prof, h)
		g.mu.Lock()
		defer g.mu.Unlock()
		g.connecting = false
		if err != nil {
			g.lineErr = err.Error()
			g.line = nil
			g.backToSetup = true
			return
		}
		g.line = c
		g.lineErr = ""
		g.cfg.Last = prof
		_ = g.cfg.save()
	}()
}

// openConn dials the right kind of connection for a profile.
func openConn(p Profile, h link.Handlers) (link.Conn, error) {
	term := p.TermType
	if term == "" {
		term = "d200"
	}
	switch p.Kind {
	case "telnet":
		return link.DialTelnet(p.Host, p.TCPPort, strings.ToUpper(term), h)
	case "ssh":
		return link.DialSSH(p.Host, p.TCPPort, p.User, term, h)
	case "program":
		return link.StartProgram(p.Command, p.WorkDir, term, h)
	default:
		return link.Open(p.Settings, h.OnData, h.OnTrace, h.OnError)
	}
}

func (g *Game) disconnect() {
	g.mu.Lock()
	g.hostTitle = ""
	l := g.line
	g.line = nil
	g.mu.Unlock()
	if l != nil {
		l.Close()
	}
}

func (g *Game) currentLine() link.Conn {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.line
}

// send transmits keyboard bytes; echo applies local echo for typed input.
func (g *Game) send(b []byte, echo bool) {
	if len(b) == 0 {
		return
	}
	if echo {
		g.mouse.hasSel = false // typing clears the highlight
	}
	if l := g.currentLine(); l != nil {
		l.Send(b)
		if echo {
			g.recall.Typed(b, g.prof.LocalEcho)
		}
	} else if echo {
		g.notConnected()
	}
	if echo && g.prof.LocalEcho {
		g.term.Feed(b)
	}
}

// backspaceCode is what the Backspace key (Mac "delete") sends.
func (g *Game) backspaceCode() []byte {
	switch g.prof.Backspace {
	case "BS":
		return []byte{010}
	case "LEFT":
		return []byte{031}
	}
	return []byte{0177}
}

// sendErase sends an erase key. With local echo on it also erases the
// previous character on screen (cursor left, space, cursor left), since the
// D200 itself takes no action on RUBOUT.
func (g *Game) sendErase(code []byte) {
	g.send(code, false)
	if g.currentLine() == nil {
		g.notConnected()
	} else {
		g.recall.Erased()
	}
	if g.prof.LocalEcho {
		g.term.Feed([]byte{031, 040, 031})
	}
}

func (g *Game) notConnected() {
	g.mu.Lock()
	busy := g.connecting
	g.mu.Unlock()
	if !busy {
		g.flash("Not connected - press " + altName + "+S for setup")
	}
}

func (g *Game) sendBreak() {
	l := g.currentLine()
	if l == nil {
		g.flash("Not connected")
		return
	}
	go func() {
		if err := l.Break(250 * time.Millisecond); err != nil {
			g.flash("BREAK failed: " + err.Error())
			return
		}
		g.addTrace(link.Traffic{TX: true, Data: nil})
		g.flash("BREAK sent (250 ms)")
	}()
}

func (g *Game) addTrace(t link.Traffic) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.Data == nil { // BREAK marker
		g.trace = append(g.trace, traceLine{tx: true, data: nil})
	}
	for _, b := range t.Data {
		n := len(g.trace)
		if n == 0 || g.trace[n-1].tx != t.TX || g.trace[n-1].data == nil || len(g.trace[n-1].data) >= bytesPerLine {
			g.trace = append(g.trace, traceLine{tx: t.TX, data: make([]byte, 0, bytesPerLine)})
			n++
		}
		g.trace[n-1].data = append(g.trace[n-1].data, b)
	}
	if len(g.trace) > traceKeep {
		g.trace = append([]traceLine(nil), g.trace[len(g.trace)-traceKeep:]...)
	}
}

func (g *Game) flash(msg string) {
	g.mu.Lock()
	g.statusMsg = msg
	g.statusUntil = time.Now().Add(3 * time.Second)
	g.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Input

// repeating reports a key press, with auto-repeat after a short delay.
func repeating(k ebiten.Key) bool {
	d := inpututil.KeyPressDuration(k)
	return d == 1 || (d >= 24 && (d-24)%3 == 0)
}

var fKeys = []ebiten.Key{ebiten.KeyF1, ebiten.KeyF2, ebiten.KeyF3, ebiten.KeyF4, ebiten.KeyF5,
	ebiten.KeyF6, ebiten.KeyF7, ebiten.KeyF8, ebiten.KeyF9, ebiten.KeyF10, ebiten.KeyF11,
	ebiten.KeyF12, ebiten.KeyF13, ebiten.KeyF14, ebiten.KeyF15}

func (g *Game) Update() error {
	g.ticks++
	g.term.SetEraseOnDEL(g.prof.RxDelErase && (g.prof.Kind == "serial" || g.prof.Kind == ""))
	if g.quit {
		return ebiten.Termination
	}
	g.mu.Lock()
	want := "Dasher D200"
	if g.hostTitle != "" && g.line != nil {
		want += "  -  " + g.hostTitle
	}
	if want != g.titleShown {
		g.titleShown = want
		ebiten.SetWindowTitle(want)
	}
	back := g.backToSetup && g.mode == modeTerminal
	g.backToSetup = false
	g.mu.Unlock()
	if back {
		g.enterSetup()
	}
	if g.updatePrompt() {
		return nil
	}
	alt := ebiten.IsKeyPressed(ebiten.KeyAlt)
	ctrl := ebiten.IsKeyPressed(ebiten.KeyControl)
	meta := ebiten.IsKeyPressed(ebiten.KeyMeta)

	if meta && inpututil.IsKeyJustPressed(ebiten.KeyQ) {
		return ebiten.Termination
	}

	switch g.mode {
	case modeSetup:
		g.updateSetup()
		return nil
	case modeHelp:
		if len(inpututil.AppendJustPressedKeys(nil)) > 0 {
			g.mode = modeTerminal
		}
		_ = ebiten.AppendInputChars(nil)
		return nil
	}

	if alt && !ctrl {
		if g.hotkeys() {
			_ = ebiten.AppendInputChars(nil) // discard Option-key symbols on macOS
			return nil
		}
	}

	shift := ebiten.IsKeyPressed(ebiten.KeyShift)
	if g.updateMouse(ctrl, shift, meta) {
		_ = ebiten.AppendInputChars(nil)
		return nil
	}
	select {
	case b := <-g.pasteCh:
		g.send(b, true)
	default:
	}
	var out []byte

	for i, k := range fKeys {
		if !inpututil.IsKeyJustPressed(k) {
			continue
		}
		dk := dasher.KeyF1 + dasher.Key(i)
		if alt && !ctrl {
			// Option/Alt+F1-F3 give F13-F15; Option/Alt+F5-F8 give C1-C4,
			// for keyboards without those keys.
			switch {
			case i <= 2:
				dk = dasher.KeyF13 + dasher.Key(i)
			case i >= 4 && i <= 7:
				dk = dasher.KeyC1 + dasher.Key(i-4)
			default:
				continue
			}
		}
		out = append(out, dasher.Encode(dk, shift, ctrl)...)
	}

	special := []struct {
		k  ebiten.Key
		dk dasher.Key
	}{
		{ebiten.KeyArrowUp, dasher.KeyUp}, {ebiten.KeyArrowDown, dasher.KeyDown},
		{ebiten.KeyArrowLeft, dasher.KeyLeft}, {ebiten.KeyArrowRight, dasher.KeyRight},
		{ebiten.KeyHome, dasher.KeyHome}, {ebiten.KeyEnd, dasher.KeyEraseEOL},
		{ebiten.KeyPageDown, dasher.KeyErasePage},
		{ebiten.KeyEscape, dasher.KeyEsc}, {ebiten.KeyTab, dasher.KeyTab},
	}
	recallDir := 0
	for _, s := range special {
		if (s.k == ebiten.KeyArrowUp || s.k == ebiten.KeyArrowDown) && g.arrowsRecall() && !shift && !ctrl {
			// Line recall instead of D200 cursor up/down (Shift+arrow still
			// sends the D200 cursor code).
			if repeating(s.k) {
				recallDir = map[ebiten.Key]int{ebiten.KeyArrowUp: -1, ebiten.KeyArrowDown: 1}[s.k]
			}
			continue
		}
		if repeating(s.k) {
			out = append(out, dasher.Encode(s.dk, shift, ctrl)...)
		}
	}
	if repeating(ebiten.KeyEnter) || repeating(ebiten.KeyNumpadEnter) {
		if g.prof.EnterSends == "CR" {
			out = append(out, 015)
		} else {
			out = append(out, 012)
		}
	}

	// Control characters. AltGr (Ctrl+Alt) on European keyboards is text.
	if ctrl && !alt {
		for k := ebiten.KeyA; k <= ebiten.KeyZ; k++ {
			if repeating(k) {
				out = append(out, byte(k-ebiten.KeyA)+1)
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) {
			out = append(out, 033)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBackslash) {
			out = append(out, 034)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
			out = append(out, 035)
		}
	}

	chars := ebiten.AppendInputChars(nil)
	if !(ctrl != alt) && !meta { // neither modifier, or AltGr
		for _, r := range chars {
			if r >= 040 && r < 0177 {
				out = append(out, byte(r))
			}
		}
	}
	g.send(out, true)

	// Erase keys go last so they follow any characters typed this frame.
	if repeating(ebiten.KeyBackspace) {
		g.sendErase(g.backspaceCode())
	}
	if repeating(ebiten.KeyDelete) {
		g.sendErase([]byte{0177})
	}
	if recallDir != 0 {
		g.recallLine(recallDir)
	}
	return nil
}

// recallLines is how many typed lines the Up arrow can bring back.
const recallLines = 10

// arrowsRecall reports whether Up/Down recall typed lines (the default)
// rather than sending the D200 cursor codes.
func (g *Game) arrowsRecall() bool { return g.prof.Arrows != "cursor" }

// recallLine replaces the line being typed with an earlier one (dir -1,
// Up) or a later one (dir +1, Down), by sending erase keys and then the
// line's characters, so the host sees ordinary typing.
func (g *Game) recallLine(dir int) {
	if g.currentLine() == nil {
		g.notConnected()
		return
	}
	n, line, ok := g.recall.Step(dir)
	if !ok {
		return
	}
	for i := 0; i < n; i++ {
		g.sendErase(g.backspaceCode())
	}
	g.send(line, true)
}

// hotkeys handles Option/Alt local commands. It returns true if one fired.
func (g *Game) hotkeys() bool {
	jp := inpututil.IsKeyJustPressed
	switch {
	case jp(ebiten.KeyB):
		g.sendBreak()
	case jp(ebiten.KeyH):
		g.showHex = !g.showHex
		w, h := ebiten.WindowSize()
		if g.showHex {
			ebiten.SetWindowSize(w, h*(dasher.Rows+1+hexRows)/(dasher.Rows+1))
		} else {
			ebiten.SetWindowSize(w, h*(dasher.Rows+1)/(dasher.Rows+1+hexRows))
		}
	case jp(ebiten.KeyO):
		g.hexOctal = !g.hexOctal
	case jp(ebiten.KeyS):
		g.enterSetup()
	case jp(ebiten.KeyE):
		g.prof.LocalEcho = !g.prof.LocalEcho
		g.flash(fmt.Sprintf("Local echo %s", onOff(g.prof.LocalEcho)))
	case jp(ebiten.KeyC):
		g.term.Reset()
	case jp(ebiten.KeyR):
		g.connect()
	case jp(ebiten.KeyD):
		g.disconnect()
		g.flash("Disconnected")
	case jp(ebiten.KeyP):
		g.prof.Phosphor = next([]string{"green", "white", "amber"}, g.prof.Phosphor, 1)
	case jp(ebiten.KeyK), jp(ebiten.KeySlash):
		g.mode = modeHelp
	case jp(ebiten.KeyQ):
		g.quit = true
	default:
		return false
	}
	return true
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// ---------------------------------------------------------------------------
// Rendering

func (g *Game) Layout(w, h int) (int, int) {
	fw, fh := g.LayoutF(float64(w), float64(h))
	return int(fw), int(fh)
}

// LayoutF renders at the display's native pixel density so text is crisp
// on Retina and other high-DPI screens.
func (g *Game) LayoutF(w, h float64) (float64, float64) {
	s := ebiten.Monitor().DeviceScaleFactor()
	if s <= 0 {
		s = 1
	}
	g.scale = s
	return math.Ceil(w * s), math.Ceil(h * s)
}

func (g *Game) gridRows() int {
	n := dasher.Rows + 1
	if g.showHex && g.mode == modeTerminal {
		n += hexRows
	}
	return n
}

// fit chooses a font size so the grid fills the window.
func (g *Game) fit(w, h int) (ox, oy float64) {
	rows := float64(g.gridRows())
	size := math.Floor(math.Min(float64(w)/(dasher.Cols*0.6), float64(h)/(rows*1.25)))
	if size < 6 {
		size = 6
	}
	if size != g.faceSize || g.face == nil {
		g.face = &text.GoTextFace{Source: g.faceSrc, Size: size}
		g.faceSize = size
		g.cellW = text.Advance("M", g.face)
		m := g.face.Metrics()
		g.cellH = math.Ceil(size * 1.25)
		g.textTop = math.Round((g.cellH - (m.HAscent + m.HDescent)) / 2)
	}
	ox = math.Floor((float64(w) - g.cellW*dasher.Cols) / 2)
	oy = math.Floor((float64(h) - g.cellH*rows) / 2)
	return
}

var background = color.RGBA{0x0a, 0x0d, 0x0a, 0xff}

func (g *Game) phosphor(dim bool) color.RGBA {
	c := map[string]color.RGBA{
		"green": {0x4c, 0xff, 0x7a, 0xff},
		"white": {0xe8, 0xec, 0xe8, 0xff},
		"amber": {0xff, 0xb0, 0x30, 0xff},
	}[g.prof.Phosphor]
	if c.A == 0 {
		c = color.RGBA{0x4c, 0xff, 0x7a, 0xff}
	}
	if dim {
		f := func(v uint8) uint8 { return uint8(float64(v)*0.5 + 4) }
		c = color.RGBA{f(c.R), f(c.G), f(c.B), 0xff}
	}
	return c
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(background)
	b := screen.Bounds()
	ox, oy := g.fit(b.Dx(), b.Dy())

	switch g.mode {
	case modeSetup:
		g.drawPage(screen, ox, oy, g.setupPage())
	case modeHelp:
		g.drawPage(screen, ox, oy, helpPage())
	default:
		g.mouse.ox, g.mouse.oy = ox, oy
		g.drawTerminal(screen, ox, oy)
	}
	g.drawStatus(screen, ox, oy+g.cellH*dasher.Rows)
	if g.showHex && g.mode == modeTerminal {
		g.drawHex(screen, ox, oy+g.cellH*(dasher.Rows+1))
	}
	if pl := g.promptLines(); pl != nil {
		// Framed box over the middle of the screen.
		top := oy + g.cellH*float64((dasher.Rows-len(pl))/2)
		h := g.cellH * float64(len(pl))
		g.rect(screen, ox-4, top-4, g.cellW*dasher.Cols+8, h+8, g.phosphor(false))
		g.rect(screen, ox, top, g.cellW*dasher.Cols, h, background)
		g.drawPage(screen, ox, top, pl)
	}
}

func (g *Game) rect(dst *ebiten.Image, x, y, w, h float64, c color.Color) {
	vector.FillRect(dst, float32(x), float32(y), float32(w), float32(h), c, false)
}

func (g *Game) str(dst *ebiten.Image, x, y float64, s string, c color.Color) {
	if strings.TrimSpace(s) == "" {
		return
	}
	// text.Draw places y at the top of the line box; centre it in the cell.
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y+g.textTop)
	op.ColorScale.ScaleWithColor(c)
	text.Draw(dst, s, g.face, op)
}

func (g *Game) drawTerminal(screen *ebiten.Image, ox, oy float64) {
	g.term.Snapshot(&g.snap)
	s := &g.snap
	blinkOn := (g.ticks/30)%2 == 0
	cursorOn := (g.ticks/20)%2 == 0
	thick := math.Max(1, math.Round(g.cellH/16))

	g.mu.Lock()
	belling := time.Now().Before(g.bellUtil)
	g.mu.Unlock()
	if belling {
		g.rect(screen, ox-4, oy-4, g.cellW*dasher.Cols+8, g.cellH*dasher.Rows+8, g.phosphor(true))
		g.rect(screen, ox, oy, g.cellW*dasher.Cols, g.cellH*dasher.Rows, background)
	}

	for y := 0; y < dasher.Rows; y++ {
		py := oy + float64(y)*g.cellH
		// Backgrounds and underlines first.
		for x := 0; x < dasher.Cols; x++ {
			c := s.Cells[y][x]
			px := ox + float64(x)*g.cellW
			fg := g.phosphor(c.Attr&dasher.AttrDim != 0)
			rev := c.Attr&dasher.AttrReverse != 0
			if x == s.X && y == s.Y && cursorOn {
				rev = !rev
			}
			if g.selected(x, y) {
				rev = !rev
			}
			if rev {
				g.rect(screen, px, py, g.cellW+0.5, g.cellH, fg)
			}
			hidden := c.Attr&dasher.AttrBlink != 0 && s.BlinkEnabled && !blinkOn
			if c.Attr&dasher.AttrUnderline != 0 && !hidden {
				uc := color.Color(fg)
				if rev {
					uc = background
				}
				g.rect(screen, px, py+g.cellH-2*thick, g.cellW+0.5, thick, uc)
			}
		}
		// Then glyphs, in runs of the same colour.
		var run []byte
		var runColor color.RGBA
		runX := 0
		flush := func(end int) {
			if len(run) > 0 {
				g.str(screen, ox+float64(runX)*g.cellW, py, string(run), runColor)
			}
			run = run[:0]
			runX = end
		}
		for x := 0; x < dasher.Cols; x++ {
			c := s.Cells[y][x]
			ch := c.Ch
			if c.Attr&dasher.AttrBlink != 0 && s.BlinkEnabled && !blinkOn {
				ch = ' '
			}
			fg := g.phosphor(c.Attr&dasher.AttrDim != 0)
			rev := c.Attr&dasher.AttrReverse != 0
			if x == s.X && y == s.Y && cursorOn {
				rev = !rev
			}
			if g.selected(x, y) {
				rev = !rev
			}
			col := fg
			if rev {
				col = background
			}
			if len(run) == 0 {
				runX, runColor = x, col
			} else if col != runColor {
				flush(x)
				runColor = col
			}
			run = append(run, ch)
		}
		flush(dasher.Cols)
	}
}

func (g *Game) drawStatus(screen *ebiten.Image, ox, oy float64) {
	fg := g.phosphor(false)
	g.rect(screen, ox, oy, g.cellW*dasher.Cols, g.cellH, g.phosphor(true))

	g.mu.Lock()
	l, lerr, msg, busy := g.line, g.lineErr, "", g.connecting
	if time.Now().Before(g.statusUntil) {
		msg = g.statusMsg
	}
	g.mu.Unlock()

	var left string
	switch {
	case msg != "":
		left = msg
	case busy:
		left = "Connecting " + describeTarget(g.prof) + " ..."
	case l != nil:
		left = l.Describe()
		if g.prof.RxDelErase && (g.prof.Kind == "serial" || g.prof.Kind == "") {
			left += "  [LOOPBACK]"
		}
		if l.Held() {
			left += "  [HELD]"
		}
	case lerr != "":
		left = "Disconnected: " + lerr
	default:
		left = "Not connected"
	}
	right := altName + "+K keys  " + altName + "+S setup"
	if g.mode != modeTerminal {
		right = ""
	}
	width := dasher.Cols - len(right) - 2
	if len(left) > width {
		left = left[:width]
	}
	line := " " + left + strings.Repeat(" ", width-len(left)) + right + " "
	_ = fg
	g.str(screen, ox, oy, line, background)
}

func shortPort(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (g *Game) drawHex(screen *ebiten.Image, ox, oy float64) {
	fg := g.phosphor(false)
	dim := g.phosphor(true)
	base := "octal"
	if !g.hexOctal {
		base = "hex"
	}
	l := g.currentLine()
	var rx, tx int64
	if l != nil {
		rx, tx = l.Counts()
	}
	title := fmt.Sprintf(" Line monitor (%s, %s+O to switch)   RX %d  TX %d bytes", base, altName, rx, tx)
	g.str(screen, ox, oy, title, dim)

	g.mu.Lock()
	lines := g.trace
	if len(lines) > hexRows-1 {
		lines = lines[len(lines)-(hexRows-1):]
	}
	lines = append([]traceLine(nil), lines...)
	g.mu.Unlock()

	for i, t := range lines {
		var sb strings.Builder
		if t.data == nil {
			sb.WriteString(" TX <BREAK>")
		} else {
			if t.tx {
				sb.WriteString(" TX ")
			} else {
				sb.WriteString(" RX ")
			}
			for j := 0; j < bytesPerLine; j++ {
				if j < len(t.data) {
					if g.hexOctal {
						fmt.Fprintf(&sb, "%03o ", t.data[j])
					} else {
						fmt.Fprintf(&sb, " %02X ", t.data[j])
					}
				} else {
					sb.WriteString("    ")
				}
			}
			sb.WriteString(" ")
			for _, b := range t.data {
				fmt.Fprintf(&sb, "%-4s", dasher.Mnemonic(b))
			}
		}
		c := fg
		if t.tx {
			c = dim
		}
		g.str(screen, ox, oy+float64(i+1)*g.cellH, sb.String(), c)
	}
}

// pageLine is one line of a full-screen page (setup, help).
type pageLine struct {
	text     string
	selected bool
	dim      bool
}

func (g *Game) drawPage(screen *ebiten.Image, ox, oy float64, lines []pageLine) {
	fg := g.phosphor(false)
	for i, l := range lines {
		if i >= dasher.Rows {
			break
		}
		py := oy + float64(i)*g.cellH
		c := color.Color(fg)
		if l.dim {
			c = g.phosphor(true)
		}
		if l.selected {
			g.rect(screen, ox, py, g.cellW*dasher.Cols, g.cellH, fg)
			c = background
		}
		g.str(screen, ox, py, l.text, c)
	}
}

func helpPage() []pageLine {
	a := altName
	t := []string{
		"",
		"  DASHER D200 EMULATOR - KEYBOARD",
		"",
		"  D200 key            Press",
		"  F1 - F12            F1 - F12  (Shift / Ctrl / Ctrl+Shift as on a D200)",
		"  F13 - F15           F13 - F15, or " + a + "+F1 - F3",
		"  C1 - C4             " + a + "+F5 - F8   (Shift for shifted C keys)",
		"  NEW LINE (012)      Enter  (can be set to send CR in setup)",
		"  Cursor keys, HOME   Arrow keys, Home",
		"  Recall typed line   Up / Down (last 10 lines; Shift+Up/Down = cursor)",
		"  Web address         Click it to open it in your browser",
		"  Copy                Drag over text (copied when you let go), or " + copyKeys,
		"  Paste               Right-click, or " + pasteKeys,
		"  ERASE EOL / PAGE    End / Page Down",
		"  RUBOUT (177)        Backspace (Mac delete)",
		"",
		"  LOCAL COMMANDS",
		"  " + a + "+B  send BREAK (Eclipse virtual console)",
		"  " + a + "+S  setup        " + a + "+R  reconnect     " + a + "+D  disconnect",
		"  " + a + "+H  line monitor " + a + "+O  octal / hex   " + a + "+E  local echo",
		"  " + a + "+C  clear screen " + a + "+P  phosphor      " + a + "+Q  quit",
		"",
	}
	if runtime.GOOS == "darwin" {
		t = append(t, "  On a Mac, F-keys may need the fn key held, or turn on 'Use F1, F2,",
			"  etc. keys as standard function keys' in System Settings > Keyboard.")
	}
	t = append(t, "", "  Press any key to return.")
	out := make([]pageLine, len(t))
	for i, s := range t {
		out[i] = pageLine{text: s}
	}
	return out
}
