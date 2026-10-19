package link

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthFailedMentionsSecurityKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if msg := authFailed("secondary", "bam").Error(); strings.Contains(msg, "security key") ||
		!strings.Contains(msg, "Local program") {
		t.Fatalf("without sk key: %s", msg)
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pub := "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29t yk1\n"
	if err := os.WriteFile(filepath.Join(dir, "yk1.pub"), []byte(pub), 0o600); err != nil {
		t.Fatal(err)
	}
	msg := authFailed("secondary", "bam").Error()
	if !strings.Contains(msg, "security key") || !strings.Contains(msg, "ssh bam@secondary") {
		t.Fatalf("with sk key: %s", msg)
	}
}

func TestKnownKeyAlgorithms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	line := "secondary ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKlQzk1ZMN5v34jQODnBrEVchqFmmn16rTBxeK0MZnQY\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := knownKeyAlgorithms("secondary:22"); len(got) != 1 || got[0] != "ssh-ed25519" {
		t.Fatalf("known host: %v", got)
	}
	if got := knownKeyAlgorithms("other:22"); got != nil {
		t.Fatalf("unknown host: %v", got)
	}
}
