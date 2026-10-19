//go:build darwin

package link

import "os"

// shellCommand returns how to run a Local program command on macOS.
//
// An app opened from Finder gets a bare environment: no SHELL and a PATH of
// just /usr/bin:/bin:/usr/sbin:/sbin. So "ssh" would find Apple's own ssh,
// not Homebrew's, and Apple's ssh can't use FIDO security keys such as a
// YubiKey, failing with "Permission denied (publickey)" without asking for a
// touch. Running the command through a login shell (-l) reads ~/.zprofile
// etc. exactly as Terminal does, so the same programs are found.
func shellCommand(command string) (string, []string) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh" // the macOS default
	}
	return shell, []string{"-l", "-c", command}
}
