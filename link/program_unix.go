//go:build !windows

package link

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// ProgramConn runs a local program (such as a Data General emulator) in a
// pseudo-terminal, so it behaves as if typed at a real terminal.
type ProgramConn struct {
	*stream
	cmd *exec.Cmd
}

// StartProgram runs command through the shell in an 80x24 pseudo-terminal.
// dir, if set, is the working directory.
func StartProgram(command, dir, termType string, h Handlers) (*ProgramConn, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("no program command entered")
	}
	shell, args := shellCommand(command)
	restoreJobSignals()
	cmd := exec.Command(shell, args...)
	cmd.Dir = expandHome(dir)
	cmd.Env = append(os.Environ(), "TERM="+termType, "COLUMNS=80", "LINES=24")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		return nil, fmt.Errorf("starting %q: %w", command, err)
	}
	p := &ProgramConn{cmd: cmd}
	p.stream = newStream(h, f, func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGHUP)
			go func() {
				time.Sleep(2 * time.Second)
				_ = cmd.Process.Kill()
			}()
		}
		return f.Close()
	}, "program: "+firstWord(command))
	go func() { _ = cmd.Wait() }()
	p.stream.start(&ptyReader{f})
	return p, nil
}

// Break isn't meaningful for a pseudo-terminal.
func (p *ProgramConn) Break(time.Duration) error {
	return errors.New("BREAK isn't available for a local program - use the emulator's own key (e.g. Ctrl+E in SimH)")
}

// ptyReader turns the EIO a pseudo-terminal returns when the program
// exits into a clear message.
type ptyReader struct{ f *os.File }

func (r *ptyReader) Read(b []byte) (int, error) {
	n, err := r.f.Read(b)
	if err != nil && (errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)) {
		err = errors.New("program exited")
	}
	return n, err
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

// restoreJobSignals makes sure Ctrl+C, Ctrl+Backslash and Ctrl+Z work in the program.
// If this app was started with those signals ignored (for example as a
// background job from a script), the program would inherit "ignore" and
// Ctrl+C would do nothing. Handling them here instead means the program
// starts with the normal default actions.
func restoreJobSignals() {
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP} {
		if signal.Ignored(sig) {
			signal.Notify(make(chan os.Signal, 1), sig)
		}
	}
}
