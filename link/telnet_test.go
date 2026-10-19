package link

import (
	"bytes"
	"testing"
)

func newTestTelnet(w *bytes.Buffer) *TelnetConn {
	t := &TelnetConn{termType: "D200", sentWill: map[byte]bool{}, sentDo: map[byte]bool{}}
	t.stream = newStream(Handlers{}, w, nil, "test")
	return t
}

func TestTelnetEscapesIAC(t *testing.T) {
	var w bytes.Buffer
	tc := newTestTelnet(&w)
	got := tc.filterOut([]byte{1, 0xFF, 2, '\r'})
	want := []byte{1, 0xFF, 0xFF, 2, '\r', 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("filterOut=%v want %v", got, want)
	}
}

func TestTelnetStripsCommandsAndAnswers(t *testing.T) {
	var w bytes.Buffer
	tc := newTestTelnet(&w)
	in := []byte{'A', tIAC, tWILL, optEcho, 'B', tIAC, tIAC, tIAC, tSB, optTType, 1, tIAC, tSE, 'C', '\r', 0, 'D'}
	got := tc.filterIn(in)
	if !bytes.Equal(got, []byte{'A', 'B', 0xFF, 'C', '\r', 'D'}) {
		t.Fatalf("filterIn=%v", got)
	}
	replies := w.Bytes()
	if !bytes.Contains(replies, []byte{tIAC, tDO, optEcho}) {
		t.Fatalf("no DO ECHO reply: %v", replies)
	}
	if !bytes.Contains(replies, append([]byte{tIAC, tSB, optTType, 0}, []byte("D200")...)) {
		t.Fatalf("no terminal type reply: %v", replies)
	}
	// A repeated WILL ECHO must not be answered again (no negotiation loop).
	w.Reset()
	tc.filterIn([]byte{tIAC, tWILL, optEcho})
	if w.Len() != 0 {
		t.Fatalf("answered a repeated WILL: %v", w.Bytes())
	}
}
