package main

import (
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/bamchchnz/dasher200/dasher"
	"github.com/bamchchnz/dasher200/link"
)

// promptState is a question shown over the screen (password, passphrase,
// new SSH host key) while a connection waits for the answer.
type promptState struct {
	question string
	echo     bool
	input    []rune
	answer   chan promptAnswer
}

type promptAnswer struct {
	text string
	err  error
}

// ask is called from a connection goroutine and blocks until the user
// answers in the window.
func (g *Game) ask(question string, echo bool) (string, error) {
	ch := make(chan promptAnswer, 1)
	g.mu.Lock()
	g.prompt = &promptState{question: question, echo: echo, answer: ch}
	g.mu.Unlock()
	a := <-ch
	return a.text, a.err
}

// updatePrompt handles keyboard input while a prompt is open. It returns
// true if a prompt is open (so normal input is skipped).
func (g *Game) updatePrompt() bool {
	g.mu.Lock()
	p := g.prompt
	g.mu.Unlock()
	if p == nil {
		return false
	}
	for _, r := range ebiten.AppendInputChars(nil) {
		if r >= 32 && r != 127 && len(p.input) < 200 {
			p.input = append(p.input, r)
		}
	}
	finish := func(a promptAnswer) {
		g.mu.Lock()
		g.prompt = nil
		g.mu.Unlock()
		p.answer <- a
	}
	switch {
	case repeating(ebiten.KeyBackspace) && len(p.input) > 0:
		p.input = p.input[:len(p.input)-1]
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter):
		finish(promptAnswer{text: string(p.input)})
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		finish(promptAnswer{err: link.ErrCancelled})
	}
	return true
}

// promptLines lays the prompt out as page lines for drawing.
func (g *Game) promptLines() []pageLine {
	g.mu.Lock()
	p := g.prompt
	g.mu.Unlock()
	if p == nil {
		return nil
	}
	out := []pageLine{{text: ""}}
	for _, l := range wrap(p.question, dasher.Cols-6) {
		out = append(out, pageLine{text: "  " + l})
	}
	shown := string(p.input)
	if !p.echo {
		shown = strings.Repeat("*", len(p.input))
	}
	if len(shown) > dasher.Cols-8 {
		shown = shown[len(shown)-(dasher.Cols-8):]
	}
	out = append(out, pageLine{text: "  > " + shown + "_"},
		pageLine{text: "  Enter to send, Esc to cancel", dim: true}, pageLine{text: ""})
	return out
}

// wrap splits text into lines of at most width characters.
func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		for len(w) > width { // very long word, e.g. a fingerprint
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			lines = append(lines, w[:width])
			w = w[width:]
		}
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) <= width:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// describeTarget is a short description of where a profile connects.
func describeTarget(p Profile) string {
	switch p.Kind {
	case "telnet":
		return "to " + p.Host
	case "ssh":
		return "to " + p.User + "@" + p.Host
	case "program":
		return "- starting program"
	}
	return "to " + shortPort(p.Port)
}
