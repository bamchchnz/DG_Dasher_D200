//go:build windows

package link

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ProgramConn runs a local program with its input and output connected to
// the terminal. On Windows this uses plain pipes, not a pseudo-terminal, so
// programs that insist on a real console may not work; for those, run the
// program separately and connect with telnet instead.
type ProgramConn struct {
	*stream
	cmd *exec.Cmd
}

// StartProgram runs command through cmd.exe.
func StartProgram(command, dir, termType string, h Handlers) (*ProgramConn, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("no program command entered")
	}
	command = forceSSHTTY(command)
	cmd := exec.Command("cmd.exe", "/C", command)
	cmd.Dir = expandHome(dir)
	cmd.Env = append(os.Environ(), "TERM="+termType)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", command, err)
	}
	p := &ProgramConn{cmd: cmd}
	p.stream = newStream(h, stdin, func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return stdin.Close()
	}, "program: "+firstWord(command))
	go func() {
		_ = cmd.Wait()
		pw.CloseWithError(errors.New("program exited"))
	}()
	p.stream.start(pr)
	return p, nil
}

// Break isn't meaningful for a local program.
func (p *ProgramConn) Break(time.Duration) error {
	return errors.New("BREAK isn't available for a local program - use the emulator's own key")
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i > 0 {
		s = s[:i]
	}
	return shortName(s)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
