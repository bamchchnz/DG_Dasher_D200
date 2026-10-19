//go:build !darwin && !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// The clipboard on Linux is reached through a helper program: wl-copy /
// wl-paste on Wayland, xclip or xsel on X11.
var errNoClipboardTool = errors.New("no clipboard tool found - install wl-clipboard (Wayland) or xclip (X11)")

func clipTool(paste bool) (*exec.Cmd, error) {
	type tool struct{ name, copyArg, pasteArg string }
	var tools []tool
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		tools = append(tools, tool{"wl-copy", "", "--no-newline"})
	}
	tools = append(tools, tool{"xclip", "-selection clipboard", "-selection clipboard -o"},
		tool{"xsel", "--clipboard --input", "--clipboard --output"})
	for _, t := range tools {
		name := t.name
		if paste && name == "wl-copy" {
			name = "wl-paste"
		}
		if _, err := exec.LookPath(name); err != nil {
			continue
		}
		arg := t.copyArg
		if paste {
			arg = t.pasteArg
		}
		return exec.Command(name, strings.Fields(arg)...), nil
	}
	return nil, errNoClipboardTool
}

// copyText puts s on the clipboard.
func copyText(s string) error {
	cmd, err := clipTool(false)
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(s)
	// xclip and wl-copy stay running to serve the clipboard, so don't wait.
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// pasteText returns the clipboard's text.
func pasteText() (string, error) {
	cmd, err := clipTool(true)
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	return string(out), err
}

// openURL opens a web address in the default browser.
func openURL(u string) error {
	cmd := exec.Command("xdg-open", u)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

const copyKeys, pasteKeys = "Ctrl+Shift+C", "Ctrl+Shift+V"
