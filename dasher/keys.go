package dasher

// Key identifies a D200 keyboard key that sends a fixed code, independent of
// how the host computer's keyboard is laid out.
type Key int

const (
	KeyNone Key = iota
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyF13
	KeyF14
	KeyF15
	KeyC1
	KeyC2
	KeyC3
	KeyC4
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEraseEOL
	KeyErasePage
	KeyNewLine
	KeyCR
	KeyRubout
	KeyEsc
	KeyTab
)

// Encode returns the bytes a D200 sends for key with the given modifiers.
//
// Function keys follow the DG convention: F1–F14 send 036 followed by
// 0161–0176 ('q'–'~') and F15 sends 036 0160 ('p'). Shift subtracts 020,
// Ctrl subtracts 0100 and Ctrl+Shift subtracts 0120 from the second byte.
// C1–C4 send 036 0134–0137; with Shift, 036 0130–0133.
func Encode(k Key, shift, ctrl bool) []byte {
	switch {
	case k >= KeyF1 && k <= KeyF15:
		var c byte
		if k == KeyF15 {
			c = 0160
		} else {
			c = 0161 + byte(k-KeyF1)
		}
		switch {
		case ctrl && shift:
			c -= 0120
		case ctrl:
			c -= 0100
		case shift:
			c -= 020
		}
		return []byte{036, c}
	case k >= KeyC1 && k <= KeyC4:
		c := 0134 + byte(k-KeyC1)
		if shift {
			c -= 4
		}
		return []byte{036, c}
	}
	switch k {
	case KeyUp:
		return []byte{cCursorUp}
	case KeyDown:
		return []byte{cCursorDown}
	case KeyLeft:
		return []byte{cCursorLeft}
	case KeyRight:
		return []byte{cCursorRight}
	case KeyHome:
		return []byte{cHome}
	case KeyEraseEOL:
		return []byte{cEraseEOL}
	case KeyErasePage:
		return []byte{cErasePage}
	case KeyNewLine:
		return []byte{cNL}
	case KeyCR:
		return []byte{cCR}
	case KeyRubout:
		return []byte{cDEL}
	case KeyEsc:
		return []byte{033}
	case KeyTab:
		return []byte{cTab}
	}
	return nil
}

// Mnemonic returns a short name for a received or sent byte, used by the
// diagnostic panel. Control codes get their D200 meaning.
func Mnemonic(b byte) string {
	b &= 0177
	if m, ok := mnemonics[b]; ok {
		return m
	}
	if b >= 040 && b < 0177 {
		return string(rune(b))
	}
	return "·"
}

var mnemonics = map[byte]string{
	000: "NUL", 001: "PFM", 003: "BLE", 004: "BLD", 005: "RWA",
	006: "ACK", 007: "BEL", 010: "HOM", 011: "TAB", 012: "NL",
	013: "EOL", 014: "EPG", 015: "CR", 016: "BL+", 017: "BL-",
	020: "WWA", 021: "PSC", 022: "RL+", 023: "RL-", 024: "UL+",
	025: "UL-", 027: "UP", 030: "RT", 031: "LT", 032: "DN",
	033: "ESC", 034: "DM+", 035: "DM-", 036: "CMD", 037: "US",
	0177: "DEL",
}
