//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// copyText puts s on the clipboard.
func copyText(s string) error {
	cmd := exec.Command("/usr/bin/pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

// pasteText returns the clipboard's text.
func pasteText() (string, error) {
	out, err := exec.Command("/usr/bin/pbpaste").Output()
	return string(out), err
}

// openURL opens a web address in the default browser.
func openURL(u string) error {
	return exec.Command("/usr/bin/open", u).Run()
}

const copyKeys, pasteKeys = "Cmd+C", "Cmd+V"
