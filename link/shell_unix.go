//go:build !windows && !darwin

package link

import "os"

// shellCommand returns how to run a Local program command: through the
// user's shell, or /bin/sh.
func shellCommand(command string) (string, []string) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell, []string{"-c", command}
}
