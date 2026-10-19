package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/bamchchnz/dasher200/dasher"
)

// Mouse and clipboard on the terminal screen:
//
//   - click a web address (http:// or https://) to open it in the browser
//   - drag to select text; it is copied to the clipboard when you let go
//   - right-click, or Cmd+V (Mac) / Ctrl+Shift+V, pastes the clipboard to
//     the host as if typed
//   - Cmd+C (Mac) / Ctrl+Shift+C copies the selection again

type mouseState struct {
	down     bool       // left button held
	dragged  bool       // moved to another cell while held
	from, to dasher.Pos // selection, while hasSel
	hasSel   bool
	shape    ebiten.CursorShapeType
	ox, oy   float64 // where the screen grid was last drawn
}

// cellAt converts a pointer position to a screen cell.
func (g *Game) cellAt(px, py int) (dasher.Pos, bool) {
	if g.cellW == 0 || g.cellH == 0 {
		return dasher.Pos{}, false
	}
	x := int((float64(px) - g.mouse.ox) / g.cellW)
	y := int((float64(py) - g.mouse.oy) / g.cellH)
	in := float64(px) >= g.mouse.ox && float64(py) >= g.mouse.oy && x < dasher.Cols && y < dasher.Rows
	return dasher.Pos{X: clamp(x, 0, dasher.Cols-1), Y: clamp(y, 0, dasher.Rows-1)}, in
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// updateMouse handles the mouse on the terminal screen. It returns true if
// it used the keyboard (a copy or paste shortcut).
func (g *Game) updateMouse(ctrl, shift, meta bool) bool {
	m := &g.mouse
	px, py := ebiten.CursorPosition()
	cell, in := g.cellAt(px, py)

	// Pointing-hand cursor over a web address.
	shape := ebiten.CursorShapeDefault
	if in && !m.down && g.snap.URLAt(cell.X, cell.Y) != "" {
		shape = ebiten.CursorShapePointer
	}
	if shape != m.shape {
		ebiten.SetCursorShape(shape)
		m.shape = shape
	}

	switch {
	case inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) && in:
		m.down, m.dragged, m.hasSel = true, false, false
		m.from, m.to = cell, cell
	case m.down && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft):
		if cell != m.from || m.dragged {
			m.dragged, m.hasSel = true, true
			m.to = cell
		}
	case m.down: // released
		m.down = false
		if m.dragged {
			g.copySelection()
		} else if u := g.snap.URLAt(m.from.X, m.from.Y); u != "" {
			g.flash("Opening " + u)
			go func() {
				if err := openURL(u); err != nil {
					g.flash("Couldn't open the browser: " + err.Error())
				}
			}()
		}
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) && in {
		g.paste()
	}

	// Keyboard shortcuts: Cmd+C / Cmd+V on a Mac, Ctrl+Shift+C / V elsewhere
	// (plain Ctrl+C must still reach the host).
	mod := ctrl && shift
	if runtime.GOOS == "darwin" {
		mod = meta
	}
	if mod && inpututil.IsKeyJustPressed(ebiten.KeyC) {
		g.copySelection()
		return true
	}
	if mod && inpututil.IsKeyJustPressed(ebiten.KeyV) {
		g.paste()
		return true
	}
	return false
}

// selected reports whether a cell is in the current selection.
func (g *Game) selected(x, y int) bool {
	return g.mouse.hasSel && dasher.Selected(g.mouse.from, g.mouse.to, x, y)
}

func (g *Game) copySelection() {
	if !g.mouse.hasSel {
		g.flash("Drag over some text first to select it")
		return
	}
	t := g.snap.SelectionText(g.mouse.from, g.mouse.to)
	go func() {
		if err := copyText(t); err != nil {
			g.flash("Copy failed: " + err.Error())
			return
		}
		g.flash(fmt.Sprintf("Copied %d characters", len(t)))
	}()
}

// paste sends the clipboard text to the host as if typed. Line breaks are
// sent as the Enter key, and only printable ASCII and Tab are passed on.
func (g *Game) paste() {
	if g.currentLine() == nil {
		g.notConnected()
		return
	}
	enter := byte(012)
	if g.prof.EnterSends == "CR" {
		enter = 015
	}
	go func() {
		s, err := pasteText()
		if err != nil {
			g.flash("Paste failed: " + err.Error())
			return
		}
		s = strings.ReplaceAll(s, "\r\n", "\n")
		out := make([]byte, 0, len(s))
		for i := 0; i < len(s) && len(out) < 20000; i++ {
			switch c := s[i]; {
			case c == '\n' || c == '\r':
				out = append(out, enter)
			case c == '\t' || (c >= 040 && c < 0177):
				out = append(out, c)
			}
		}
		if len(out) == 0 {
			g.flash("Nothing to paste")
			return
		}
		g.pasteCh <- out
	}()
}
