package dasher

import (
	"reflect"
	"testing"
)

// typeLine types s and Enter, with the host echoing.
func typeLine(r *Recall, s string) {
	r.Typed([]byte(s), false)
	r.HostOutput()
	r.Typed([]byte{012}, false)
}

// apply performs a Step like the UI does: erase, then type the line.
func apply(r *Recall, dir int) (int, string, bool) {
	n, line, ok := r.Step(dir)
	if ok {
		for i := 0; i < n; i++ {
			r.Erased()
		}
		r.Typed(line, false)
	}
	return n, string(line), ok
}

func TestRecallCyclesThroughTenLines(t *testing.T) {
	r := NewRecall(10)
	for i := 0; i < 12; i++ {
		typeLine(r, string(rune('a'+i)))
	}
	if got := r.Lines(); len(got) != 10 || got[0] != "c" || got[9] != "l" {
		t.Fatalf("kept %v", got)
	}
	var seen []string
	for i := 0; i < 11; i++ {
		_, l, _ := apply(r, -1)
		seen = append(seen, l)
	}
	want := []string{"l", "k", "j", "i", "h", "g", "f", "e", "d", "c", "l"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("Up sequence %v", seen)
	}
}

func TestRecallErasesCurrentLineAndRestoresDraft(t *testing.T) {
	r := NewRecall(10)
	typeLine(r, "DIR")
	r.Typed([]byte("TY"), false)
	n, l, ok := apply(r, -1)
	if !ok || n != 2 || l != "DIR" {
		t.Fatalf("up: erase %d line %q", n, l)
	}
	n, l, ok = apply(r, +1)
	if !ok || n != 3 || l != "TY" {
		t.Fatalf("down back to draft: erase %d line %q", n, l)
	}
	if _, _, ok := apply(r, +1); ok {
		t.Fatal("down on the new line should do nothing")
	}
}

func TestRecallSkipsUnechoedAndUntrackedLines(t *testing.T) {
	r := NewRecall(10)
	r.Typed([]byte("secret\012"), false) // password: host didn't echo
	r.Typed([]byte("ab\011c"), false)    // Tab completion: can't follow
	r.HostOutput()
	r.Typed([]byte{015}, false)
	r.Typed([]byte("x"), false)
	r.Erased()
	r.Typed([]byte("ok"), false)
	r.HostOutput()
	r.Typed([]byte{015}, false)
	typeLine(r, "ok") // a repeat is kept once
	if got := r.Lines(); !reflect.DeepEqual(got, []string{"ok"}) {
		t.Fatalf("kept %v", got)
	}
	r.Typed([]byte("pw\012"), true) // local echo on: always kept
	if got := r.Lines(); len(got) != 2 || got[1] != "pw" {
		t.Fatalf("local echo line not kept: %v", got)
	}
}

func TestRecallEmpty(t *testing.T) {
	r := NewRecall(10)
	if _, _, ok := r.Step(-1); ok {
		t.Fatal("empty history should do nothing")
	}
}
