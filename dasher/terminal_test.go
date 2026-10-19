package dasher

import (
	"bytes"
	"strings"
	"testing"
)

func snap(t *Terminal) *Snapshot {
	var s Snapshot
	t.Snapshot(&s)
	return &s
}

func row(t *Terminal, y int) string { return strings.TrimRight(snap(t).Text(y), " ") }

func TestPrintAndNewLine(t *testing.T) {
	term := New()
	term.Feed([]byte("HELLO\012WORLD"))
	if got := row(term, 0); got != "HELLO" {
		t.Fatalf("row0=%q", got)
	}
	if got := row(term, 1); got != "WORLD" {
		t.Fatalf("row1=%q", got)
	}
	s := snap(term)
	if s.X != 5 || s.Y != 1 {
		t.Fatalf("cursor=%d,%d", s.X, s.Y)
	}
}

func TestHomeIsNotBackspace(t *testing.T) {
	term := New()
	term.Feed([]byte("ABC\012DEF\010X"))
	if got := row(term, 0); got != "XBC" {
		t.Fatalf("010 should home the cursor; row0=%q", got)
	}
}

func TestWriteWindowAddress(t *testing.T) {
	term := New()
	term.Feed([]byte{020, 10, 5, 'Z'})
	s := snap(term)
	if s.Cells[5][10].Ch != 'Z' || s.X != 11 || s.Y != 5 {
		t.Fatalf("WWA failed: cell=%q cursor=%d,%d", s.Cells[5][10].Ch, s.X, s.Y)
	}
	// 0177 leaves the coordinate unchanged.
	term.Feed([]byte{020, 0177, 9, 'Q'})
	s = snap(term)
	if s.Cells[9][11].Ch != 'Q' {
		t.Fatalf("0177 column should keep column 11")
	}
}

func TestEraseEOLAndPage(t *testing.T) {
	term := New()
	term.Feed([]byte("ABCDEF\015\030\030\013"))
	if got := row(term, 0); got != "AB" {
		t.Fatalf("erase EOL: %q", got)
	}
	term.Feed([]byte{016, 'x', 014})
	s := snap(term)
	if row(term, 0) != "" || s.X != 0 || s.Y != 0 {
		t.Fatalf("erase page did not clear/home")
	}
	term.Feed([]byte("y"))
	if snap(term).Cells[0][0].Attr != 0 {
		t.Fatalf("erase page should reset attributes")
	}
}

func TestRollEnabledScrolls(t *testing.T) {
	term := New()
	for i := 0; i < Rows; i++ {
		term.Feed([]byte{byte('A' + i), 012})
	}
	// 24 lines each followed by NL: the first line has scrolled off.
	if got := row(term, 0); got != "B" {
		t.Fatalf("top row after scroll = %q", got)
	}
	if got := row(term, Rows-2); got != "X" {
		t.Fatalf("row 22 = %q", got)
	}
}

func TestRollDisabledWraps(t *testing.T) {
	term := New()
	term.Feed([]byte{023})
	for i := 0; i < Rows; i++ {
		term.Feed([]byte{byte('A' + i), 012})
	}
	term.Feed([]byte("Z"))
	if got := row(term, 0); got != "Z" {
		t.Fatalf("roll disabled should wrap to top; row0=%q", got)
	}
}

func TestLineWrapAtColumn80(t *testing.T) {
	term := New()
	term.Feed(bytes.Repeat([]byte("x"), Cols))
	s := snap(term)
	if s.X != 0 || s.Y != 1 {
		t.Fatalf("after 80 chars cursor=%d,%d", s.X, s.Y)
	}
}

func TestCursorMotionWraps(t *testing.T) {
	term := New()
	term.Feed([]byte{027}) // up from top row
	if s := snap(term); s.Y != Rows-1 {
		t.Fatalf("up from row 0 -> %d", s.Y)
	}
	term.Feed([]byte{010, 031}) // home, left
	if s := snap(term); s.X != Cols-1 || s.Y != Rows-1 {
		t.Fatalf("left from 0,0 -> %d,%d", s.X, s.Y)
	}
}

func TestAttributes(t *testing.T) {
	term := New()
	term.Feed([]byte{016, 'a', 017, 034, 'b', 035, 024, 'c', 025, 036, 'D', 'd', 036, 'E', 'e'})
	s := snap(term)
	want := []Attr{AttrBlink, AttrDim, AttrUnderline, AttrReverse, 0}
	for i, w := range want {
		if s.Cells[0][i].Attr != w {
			t.Fatalf("cell %d attr=%b want %b", i, s.Cells[0][i].Attr, w)
		}
	}
}

func TestReplies(t *testing.T) {
	term := New()
	var got [][]byte
	term.Reply = func(b []byte) { got = append(got, b) }
	term.Feed([]byte{020, 7, 3, 005, 036, 'C'})
	if len(got) != 2 || !bytes.Equal(got[0], []byte{037, 7, 3}) || got[1][0] != 036 {
		t.Fatalf("replies=%v", got)
	}
}

func TestExtendedCommandIsSwallowed(t *testing.T) {
	term := New()
	term.Feed([]byte{036, 'F', '@', 'O', 'K'})
	if got := row(term, 0); got != "OK" {
		t.Fatalf("036 F sequence leaked onto screen: %q", got)
	}
}

func TestDELIgnoredByDefaultAndOptionallyErases(t *testing.T) {
	term := New()
	term.Feed([]byte("ABC\177"))
	if got := row(term, 0); got != "ABC" {
		t.Fatalf("DEL should be ignored by default: %q", got)
	}
	term.SetEraseOnDEL(true)
	term.Feed([]byte("\177\177X"))
	if got := row(term, 0); got != "AX" {
		t.Fatalf("DEL erase: %q", got)
	}
}

func TestANSIEscapesSwallowed(t *testing.T) {
	term := New()
	term.Feed([]byte("\033[?2004hA\033[0mB\033=C"))
	if got := row(term, 0); got != "ABC" {
		t.Fatalf("ANSI sequences leaked: %q", got)
	}
}

func TestOSCTitleIsNotPrinted(t *testing.T) {
	term := New()
	var title string
	bells := 0
	term.Title = func(s string) { title = s }
	term.Bell = func() { bells++ }
	term.Feed([]byte("A\033]2;MV/8000 [00.02.08]    MIPS: 40.203\007B\033]0;x\033\\C\033(BD"))
	if got := row(term, 0); got != "ABCD" {
		t.Fatalf("escape strings leaked onto screen: %q", got)
	}
	if title != "x" {
		t.Fatalf("title=%q", title)
	}
	if bells != 0 {
		t.Fatalf("OSC terminator rang the bell")
	}
	term.Feed([]byte("\033]2;MV/8000 MIPS: 40.2\007"))
	if title != "MV/8000 MIPS: 40.2" {
		t.Fatalf("title=%q", title)
	}
}

func TestBell(t *testing.T) {
	term := New()
	n := 0
	term.Bell = func() { n++ }
	term.Feed([]byte{007})
	if n != 1 {
		t.Fatal("bell not rung")
	}
}

func TestFunctionKeys(t *testing.T) {
	cases := []struct {
		k           Key
		shift, ctrl bool
		want        []byte
	}{
		{KeyF1, false, false, []byte{036, 'q'}},
		{KeyF1, true, false, []byte{036, 'a'}},
		{KeyF1, false, true, []byte{036, '1'}},
		{KeyF1, true, true, []byte{036, '!'}},
		{KeyF14, false, false, []byte{036, '~'}},
		{KeyF15, false, false, []byte{036, 'p'}},
		{KeyF15, true, false, []byte{036, '`'}},
		{KeyC1, false, false, []byte{036, '\\'}},
		{KeyC4, true, false, []byte{036, '['}},
		{KeyUp, false, false, []byte{027}},
		{KeyRubout, false, false, []byte{0177}},
	}
	for _, c := range cases {
		if got := Encode(c.k, c.shift, c.ctrl); !bytes.Equal(got, c.want) {
			t.Errorf("Encode(%d,%v,%v)=%o want %o", c.k, c.shift, c.ctrl, got, c.want)
		}
	}
}
