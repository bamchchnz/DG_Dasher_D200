//go:build windows

package main

import (
	"os/exec"
	"strings"
	"syscall"
)

// hidden runs a helper program without flashing a console window.
func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

// copyText puts s on the clipboard (with Windows line endings).
func copyText(s string) error {
	cmd := hidden(exec.Command("clip.exe"))
	cmd.Stdin = strings.NewReader(strings.ReplaceAll(s, "\n", "\r\n"))
	return cmd.Run()
}

// pasteText returns the clipboard's text.
func pasteText() (string, error) {
	out, err := hidden(exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-Command", "Get-Clipboard -Raw")).Output()
	return strings.TrimSuffix(string(out), "\r\n"), err
}

// openURL opens a web address in the default browser.
func openURL(u string) error {
	return hidden(exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", u)).Run()
}

const copyKeys, pasteKeys = "Ctrl+Shift+C", "Ctrl+Shift+V"
