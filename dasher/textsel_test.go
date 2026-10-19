package dasher

import (
	"strings"
	"testing"
)

func TestURLAtFindsAddress(t *testing.T) {
	term := New()
	term.Feed([]byte("To authenticate, visit:\012\012\011https://login.tailscale.com/a/1a2b3c4d5e6f\012\012"))
	s := snap(term)
	want := "https://login.tailscale.com/a/1a2b3c4d5e6f"
	for _, x := range []int{8, 20, 49} {
		if got := s.URLAt(x, 2); got != want {
			t.Fatalf("URLAt(%d,2) = %q", x, got)
		}
	}
	if got := s.URLAt(3, 0); got != "" {
		t.Fatalf("plain text gave %q", got)
	}
}

func TestURLAtFollowsWrappedAddress(t *testing.T) {
	term := New()
	u := "https://login.tailscale.com/a/" + strings.Repeat("x", 70) // wraps past column 80
	term.Feed([]byte("Go to " + u + " now)."))
	s := snap(term)
	if got := s.URLAt(10, 0); got != u {
		t.Fatalf("first line: %q", got)
	}
	if got := s.URLAt(5, 1); got != u {
		t.Fatalf("continuation line: %q", got)
	}
}

func TestURLAtTrimsPunctuation(t *testing.T) {
	term := New()
	term.Feed([]byte("(see http://example.com/x)."))
	if got := snap(term).URLAt(8, 0); got != "http://example.com/x" {
		t.Fatalf("got %q", got)
	}
}

func TestSelectionText(t *testing.T) {
	term := New()
	term.Feed([]byte("HELLO WORLD\012SECOND LINE\012THIRD"))
	s := snap(term)
	got := s.SelectionText(Pos{6, 0}, Pos{5, 1})
	if got != "WORLD\nSECOND" {
		t.Fatalf("got %q", got)
	}
	// Reversed drag gives the same text.
	if s.SelectionText(Pos{5, 1}, Pos{6, 0}) != got {
		t.Fatal("reversed selection differs")
	}
	if !Selected(Pos{6, 0}, Pos{5, 1}, 70, 0) || Selected(Pos{6, 0}, Pos{5, 1}, 6, 1) {
		t.Fatal("Selected wrong")
	}
}
