package dasher

import "sync"

// Recall keeps the last few lines typed at the keyboard so that the Up and
// Down arrow keys can bring them back, like command history in a shell. It
// works with any host that edits lines with RUBOUT (AOS/VS CLI, RDOS, Unix
// shells): recalling a line erases what is on the current line with the
// erase key, then types the old line, ready to be edited or sent with Enter.
//
// It follows what is typed since the last Enter. If a key it can't follow is
// pressed (Tab, Esc, cursor keys, function keys) the line on the host may no
// longer match, so the line is not saved.
//
// A line is only saved if the host echoed something while it was typed (or
// local echo is on), so passwords typed at a prompt that doesn't echo them
// are not kept. History is kept in memory only, never written to disk.
type Recall struct {
	mu    sync.Mutex
	max   int
	hist  [][]byte // oldest first
	cur   []byte   // the line typed since the last Enter
	valid bool     // cur is known to match the host's line
	echo  bool     // the host sent something while cur was being typed
	pos   int      // -1 = editing a new line; otherwise 0 = newest entry
	draft []byte   // the new line, saved while browsing history
}

// NewRecall keeps up to max lines.
func NewRecall(max int) *Recall {
	return &Recall{max: max, valid: true, pos: -1}
}

// Typed records keyboard bytes sent to the host.
func (r *Recall) Typed(b []byte, localEcho bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range b {
		switch {
		case c >= 040 && c < 0177:
			r.cur = append(r.cur, c)
		case c == 012 || c == 015: // Enter
			r.commit(localEcho)
		case c == 025: // Ctrl+U: erase line on AOS/VS and Unix
			r.cur, r.valid = r.cur[:0], true
		case c == 003: // Ctrl+C: interrupt, a fresh line follows
			r.cur, r.valid, r.echo, r.pos = r.cur[:0], true, false, -1
		default:
			r.valid = false
		}
	}
}

// Erased records one erase (RUBOUT) key.
func (r *Recall) Erased() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cur) > 0 {
		r.cur = r.cur[:len(r.cur)-1]
	}
}

// HostOutput records that the host sent something.
func (r *Recall) HostOutput() {
	r.mu.Lock()
	if len(r.cur) > 0 {
		r.echo = true
	}
	r.mu.Unlock()
}

// Reset forgets the current line (for example on a new connection). The
// history itself is kept.
func (r *Recall) Reset() {
	r.mu.Lock()
	r.cur, r.valid, r.echo, r.pos, r.draft = nil, true, false, -1, nil
	r.mu.Unlock()
}

func (r *Recall) commit(localEcho bool) {
	if len(r.cur) > 0 && r.valid && (r.echo || localEcho) {
		line := append([]byte(nil), r.cur...)
		// Keep each line once, moving a repeat to the newest place.
		for i, h := range r.hist {
			if string(h) == string(line) {
				r.hist = append(r.hist[:i], r.hist[i+1:]...)
				break
			}
		}
		r.hist = append(r.hist, line)
		if len(r.hist) > r.max {
			r.hist = r.hist[len(r.hist)-r.max:]
		}
	}
	r.cur, r.valid, r.echo, r.pos, r.draft = r.cur[:0], true, false, -1, nil
}

// Step moves through the history: dir -1 for older (Up), +1 for newer
// (Down). Up from the oldest line goes round to the newest again. It
// returns how many characters to erase and the line to type in their place;
// ok is false if there is nothing to do. The caller sends the erases and the
// line as ordinary keys, which updates the current line through Erased and
// Typed.
func (r *Recall) Step(dir int) (erase int, line []byte, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.hist)
	if n == 0 {
		return 0, nil, false
	}
	switch {
	case dir < 0:
		if r.pos < 0 {
			r.draft = append([]byte(nil), r.cur...)
		}
		r.pos = (r.pos + 1) % n
		line = r.hist[n-1-r.pos]
	case r.pos < 0:
		return 0, nil, false // already on the new line
	case r.pos == 0:
		r.pos = -1
		line = r.draft
	default:
		r.pos--
		line = r.hist[n-1-r.pos]
	}
	return len(r.cur), append([]byte(nil), line...), true
}

// Lines returns the saved lines, oldest first (for tests and display).
func (r *Recall) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.hist))
	for i, h := range r.hist {
		out[i] = string(h)
	}
	return out
}
