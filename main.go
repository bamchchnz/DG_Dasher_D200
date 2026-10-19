// Command dasher200 is a Data General Dasher D200 terminal emulator for
// connecting to real Nova and Eclipse hardware over a serial line.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/bamchchnz/dasher200/dasher"
	"github.com/bamchchnz/dasher200/link"
)

var version = "0.2.8"

func main() {
	var (
		port     = flag.String("port", "", "serial port to connect to at start-up")
		profile  = flag.String("profile", "", "saved profile to connect with at start-up")
		baud     = flag.Int("baud", 0, "baud rate (overrides the profile)")
		bits     = flag.Int("databits", 0, "data bits: 7 or 8")
		parity   = flag.String("parity", "", "parity: none, even, odd, mark, space")
		stop     = flag.Int("stopbits", 0, "stop bits: 1 or 2")
		flow     = flag.String("flow", "", "flow control: none, xonxoff, rtscts")
		list     = flag.Bool("list", false, "list serial ports and exit")
		showVer  = flag.Bool("version", false, "print the version and exit")
		hexPanel = flag.Bool("monitor", false, "start with the line monitor open")
		telnetTo = flag.String("telnet", "", "connect by telnet: host or host:port")
		sshTo    = flag.String("ssh", "", "connect by SSH: user@host or user@host:port")
		run      = flag.String("run", "", "run a local program (e.g. a DG emulator) in the terminal")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("dasher200", version)
		return
	}
	if *list {
		for _, p := range link.Ports() {
			fmt.Println(p)
		}
		return
	}

	useSoftwareGLIfNeeded()

	cfg := loadConfig()
	prof := cfg.Last
	if *profile != "" {
		p, ok := cfg.Profiles[*profile]
		if !ok {
			fmt.Fprintf(os.Stderr, "no saved profile named %q\n", *profile)
			os.Exit(1)
		}
		prof = p
	}
	if *port != "" {
		prof.Port = *port
		prof.Kind = "serial"
	}
	if *telnetTo != "" {
		prof.Kind = "telnet"
		prof.Host, prof.TCPPort = splitHostPort(*telnetTo)
	}
	if *sshTo != "" {
		prof.Kind = "ssh"
		hp := *sshTo
		if i := strings.LastIndex(hp, "@"); i >= 0 {
			prof.User, hp = hp[:i], hp[i+1:]
		}
		prof.Host, prof.TCPPort = splitHostPort(hp)
	}
	if *run != "" {
		prof.Kind = "program"
		prof.Command = *run
	}
	if *baud != 0 {
		prof.Baud = *baud
	}
	if *bits != 0 {
		prof.DataBits = *bits
	}
	if *parity != "" {
		prof.Parity = *parity
	}
	if *stop != 0 {
		prof.StopBits = *stop
	}
	if *flow != "" {
		prof.Flow = *flow
	}

	g, err := newGame(cfg, prof)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	g.showHex = *hexPanel
	if *port != "" || *profile != "" || *telnetTo != "" || *sshTo != "" || *run != "" {
		g.connect()
	} else {
		g.enterSetup()
	}

	rows := dasher.Rows + 1
	if g.showHex {
		rows += hexRows
	}
	ebiten.SetWindowTitle("Dasher D200")
	ebiten.SetWindowSize(960, 24*rows)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(60)
	ebiten.SetScreenClearedEveryFrame(true)

	err = ebiten.RunGame(g)
	g.disconnect()
	retryWithSoftwareGL(err) // returns only if no restart was needed
	g.cfg.Last = g.prof
	_ = g.cfg.save()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// splitHostPort splits "host" or "host:port"; port 0 means the default.
func splitHostPort(s string) (string, int) {
	if i := strings.LastIndex(s, ":"); i > 0 && !strings.Contains(s[i+1:], "]") {
		if n, err := strconv.Atoi(s[i+1:]); err == nil {
			return strings.Trim(s[:i], "[]"), n
		}
	}
	return strings.Trim(s, "[]"), 0
}
