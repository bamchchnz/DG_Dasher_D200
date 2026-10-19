package dasher

import "strings"

// Pos is a screen position (column X, row Y).
type Pos struct{ X, Y int }

func (p Pos) index() int { return p.Y*Cols + p.X }

// Ordered returns a and b with the earlier screen position first.
func Ordered(a, b Pos) (Pos, Pos) {
	if b.index() < a.index() {
		return b, a
	}
	return a, b
}

// Selected reports whether (x, y) lies in the selection from a to b
// (inclusive, in reading order, like selecting text in a document).
func Selected(a, b Pos, x, y int) bool {
	a, b = Ordered(a, b)
	i := y*Cols + x
	return i >= a.index() && i <= b.index()
}

// SelectionText returns the text from a to b, with trailing spaces removed
// from each line and lines joined with "\n".
func (s *Snapshot) SelectionText(a, b Pos) string {
	a, b = Ordered(a, b)
	var lines []string
	for y := a.Y; y <= b.Y; y++ {
		x0, x1 := 0, Cols-1
		if y == a.Y {
			x0 = a.X
		}
		if y == b.Y {
			x1 = b.X
		}
		lines = append(lines, strings.TrimRight(s.Text(y)[x0:x1+1], " "))
	}
	return strings.Join(lines, "\n")
}

// URLAt returns the web address (http:// or https://) under (x, y), or "".
// An address that runs off the end of a line and continues on the next,
// as a long one does on an 80-column screen, is followed onto that line.
func (s *Snapshot) URLAt(x, y int) string {
	var b strings.Builder
	for row := 0; row < Rows; row++ {
		b.WriteString(s.Text(row))
	}
	screen := b.String()
	i := y*Cols + x
	if i < 0 || i >= len(screen) || screen[i] == ' ' {
		return ""
	}
	// The word under the pointer, bounded by spaces or the screen edges.
	start, end := i, i
	for start > 0 && isURLChar(screen[start-1]) {
		start--
	}
	for end < len(screen) && isURLChar(screen[end]) {
		end++
	}
	word := screen[start:end]
	at := i - start
	// Find the address in the word that the pointer is on.
	for _, scheme := range []string{"https://", "http://"} {
		for off := 0; ; {
			k := strings.Index(word[off:], scheme)
			if k < 0 {
				break
			}
			k += off
			u := strings.TrimRight(word[k:], ".,;:!?'\")]}>")
			if next := nextScheme(word[k+len(scheme):]); next >= 0 {
				u = word[k : k+len(scheme)+next]
			}
			if at >= k && at < k+len(u) && len(u) > len(scheme) {
				return u
			}
			off = k + len(scheme)
		}
	}
	return ""
}

func nextScheme(s string) int {
	n := -1
	for _, sc := range []string{"https://", "http://"} {
		if k := strings.Index(s, sc); k >= 0 && (n < 0 || k < n) {
			n = k
		}
	}
	return n
}

func isURLChar(c byte) bool {
	return c > ' ' && c < 0177 && c != '<' && c != '>' && c != '"' && c != '`'
}
