package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/bamchchnz/dasher200/link"
)

// Profile is a saved connection plus terminal preferences.
type Profile struct {
	link.Settings
	EnterSends string `json:"enterSends"` // "NL" (012) or "CR" (015)
	Backspace  string `json:"backspace"`  // "DEL" (177), "BS" (010) or "LEFT" (031)
	RxDelErase bool   `json:"rxDelErase"` // received 177 erases (loopback testing)
	LocalEcho  bool   `json:"localEcho"`
	Phosphor   string `json:"phosphor"` // green, white, amber
	Arrows     string `json:"arrows"`   // Up/Down: "" or "recall" = line recall, "cursor" = D200 cursor codes

	// Connection type and network/program settings.
	Kind     string `json:"kind"` // serial, telnet, ssh, program
	Host     string `json:"host"`
	TCPPort  int    `json:"tcpPort"` // 0 = default (23 telnet, 22 SSH)
	User     string `json:"user"`
	Command  string `json:"command"`  // local program command line
	WorkDir  string `json:"workDir"`  // local program working directory
	TermType string `json:"termType"` // reported to SSH/telnet/program, default d200
}

func defaultProfile() Profile {
	return Profile{Settings: link.Defaults(), EnterSends: "NL", Backspace: "DEL", Phosphor: "green", Kind: "serial", TermType: "d200"}
}

// Config is persisted between runs.
type Config struct {
	Last     Profile            `json:"last"`
	Profiles map[string]Profile `json:"profiles"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "Dasher200", "config.json")
}

func loadConfig() *Config {
	c := &Config{Last: defaultProfile(), Profiles: map[string]Profile{}}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, c)
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return c
}

func (c *Config) save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (c *Config) profileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
