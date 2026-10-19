package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/bamchchnz/dasher200/dasher"
	"github.com/bamchchnz/dasher200/link"
)

const noProfile = "(unsaved)"

type setupState struct {
	sel      int
	editing  bool // typing into a text field
	advanced bool // show the advanced (normally hidden) settings
	ports    []string
	profile  string
	naming   bool
	name     []rune
}

var (
	baudRates = []string{"110", "150", "300", "600", "1200", "2400", "4800", "9600", "19200", "38400"}
	dataBits  = []string{"7", "8"}
	parities  = []string{"none", "even", "odd", "mark", "space"}
	stopBits  = []string{"1", "2"}
	flows     = []string{"none", "xonxoff", "rtscts"}
	enterOpts = []string{"NL (012: return + line feed)", "CR (015: return only)"}
	bsOpts    = []string{"DEL (177, DG rubout)", "BS (010, homes a D200!)", "LEFT (031, cursor left)"}
	rxDelOpts = []string{"ignore (as a real D200)", "erase previous char (loopback)"}
	onOffOpts = []string{"off", "on"}
	modeOpts  = []string{"Connected to computer", "Loopback test"}
	kindOpts  = []string{"Serial", "Telnet", "SSH", "Local program"}
	kindKeys  = []string{"serial", "telnet", "ssh", "program"}
	phosphors = []string{"green", "white", "amber"}
	arrowOpts = []string{"recall last 10 typed lines", "D200 cursor up/down"}
)

type field struct {
	label    string
	values   func(g *Game) []string
	get      func(g *Game) string
	set      func(g *Game, v string)
	advanced bool               // hidden unless advanced settings are shown
	show     func(g *Game) bool // nil = always shown
	text     bool               // free-text field, edited by typing
	digits   bool               // text field accepts digits only
}

func isKind(kinds ...string) func(g *Game) bool {
	return func(g *Game) bool {
		k := g.prof.Kind
		if k == "" {
			k = "serial"
		}
		for _, x := range kinds {
			if x == k {
				return true
			}
		}
		return false
	}
}

func kindLabel(k string) string {
	for i, x := range kindKeys {
		if x == k {
			return kindOpts[i]
		}
	}
	return kindOpts[0]
}

func kindKey(label string) string {
	for i, x := range kindOpts {
		if x == label {
			return kindKeys[i]
		}
	}
	return "serial"
}

func portText(g *Game) string {
	if g.prof.TCPPort == 0 {
		return ""
	}
	return strconv.Itoa(g.prof.TCPPort)
}

const modeCustom = "Custom - press A to see advanced settings"

// applyMode sets the keyboard/echo settings for one of the two modes.
// Both send NL for Enter and DEL (rubout) for Backspace, which is what DG
// systems expect; they differ in how a received DEL is shown.
func (g *Game) applyMode(m string) {
	g.prof.EnterSends = "NL"
	g.prof.Backspace = "DEL"
	g.prof.LocalEcho = false
	g.prof.RxDelErase = m == modeOpts[1]
}

// currentMode reports which mode the settings match, or Custom.
func (g *Game) currentMode() string {
	p := g.prof
	if p.EnterSends == "NL" && p.Backspace == "DEL" && !p.LocalEcho {
		if p.RxDelErase {
			return modeOpts[1]
		}
		return modeOpts[0]
	}
	return modeCustom
}

// visibleFields returns the setup lines currently shown.
func (g *Game) visibleFields() []field {
	var out []field
	for _, f := range fields {
		if (!f.advanced || g.setup.advanced) && (f.show == nil || f.show(g)) {
			out = append(out, f)
		}
	}
	return out
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

// pick returns the option whose first word is key, or the first option.
func pick(opts []string, key string) string {
	for _, o := range opts {
		if firstWord(o) == key {
			return o
		}
	}
	return opts[0]
}

var serialOnly = isKind("serial")

var fields = []field{
	{label: "Profile", values: func(g *Game) []string { return append([]string{noProfile}, g.cfg.profileNames()...) },
		get: func(g *Game) string { return g.setup.profile },
		set: func(g *Game, v string) {
			g.setup.profile = v
			if p, ok := g.cfg.Profiles[v]; ok {
				g.prof = p
			}
		}},
	{label: "Connection", values: func(*Game) []string { return kindOpts },
		get: func(g *Game) string { return kindLabel(g.prof.Kind) },
		set: func(g *Game, v string) { g.prof.Kind = kindKey(v) }},
	{label: "Mode", values: func(*Game) []string { return modeOpts },
		get: func(g *Game) string { return g.currentMode() },
		set: func(g *Game, v string) { g.applyMode(v) }, show: serialOnly},
	{label: "Serial port", values: func(g *Game) []string { return g.portChoices() },
		get: func(g *Game) string { return g.prof.Port }, set: func(g *Game, v string) { g.prof.Port = v }, show: serialOnly},
	{label: "Baud rate", values: func(*Game) []string { return baudRates },
		get: func(g *Game) string { return strconv.Itoa(g.prof.Baud) }, set: func(g *Game, v string) { g.prof.Baud = atoi(v) }, show: serialOnly},
	{label: "Data bits", values: func(*Game) []string { return dataBits },
		get: func(g *Game) string { return strconv.Itoa(g.prof.DataBits) }, set: func(g *Game, v string) { g.prof.DataBits = atoi(v) }, show: serialOnly},
	{label: "Parity", values: func(*Game) []string { return parities },
		get: func(g *Game) string { return g.prof.Parity }, set: func(g *Game, v string) { g.prof.Parity = v }, show: serialOnly},
	{label: "Stop bits", values: func(*Game) []string { return stopBits },
		get: func(g *Game) string { return strconv.Itoa(g.prof.StopBits) }, set: func(g *Game, v string) { g.prof.StopBits = atoi(v) }, show: serialOnly},
	{label: "Flow control", values: func(*Game) []string { return flows },
		get: func(g *Game) string { return g.prof.Flow }, set: func(g *Game, v string) { g.prof.Flow = v }, show: serialOnly},
	{label: "Host", get: func(g *Game) string { return g.prof.Host },
		set: func(g *Game, v string) { g.prof.Host = strings.TrimSpace(v) }, show: isKind("telnet", "ssh"), text: true},
	{label: "Port", get: portText,
		set: func(g *Game, v string) { g.prof.TCPPort = atoi(v) }, show: isKind("telnet", "ssh"), text: true, digits: true},
	{label: "User", get: func(g *Game) string { return g.prof.User },
		set: func(g *Game, v string) { g.prof.User = strings.TrimSpace(v) }, show: isKind("ssh"), text: true},
	{label: "Command", get: func(g *Game) string { return g.prof.Command },
		set: func(g *Game, v string) { g.prof.Command = v }, show: isKind("program"), text: true},
	{label: "Start in folder", get: func(g *Game) string { return g.prof.WorkDir },
		set: func(g *Game, v string) { g.prof.WorkDir = v }, show: isKind("program"), text: true},
	{label: "Phosphor", values: func(*Game) []string { return phosphors },
		get: func(g *Game) string { return g.prof.Phosphor }, set: func(g *Game, v string) { g.prof.Phosphor = v }},
	{label: "Up/Down arrows", values: func(*Game) []string { return arrowOpts },
		get: func(g *Game) string {
			if g.prof.Arrows == "cursor" {
				return arrowOpts[1]
			}
			return arrowOpts[0]
		},
		set: func(g *Game, v string) {
			g.prof.Arrows = "recall"
			if v == arrowOpts[1] {
				g.prof.Arrows = "cursor"
			}
		}},
	{label: "Terminal type", get: func(g *Game) string { return g.prof.TermType },
		set: func(g *Game, v string) { g.prof.TermType = strings.TrimSpace(v) }, advanced: true,
		show: isKind("telnet", "ssh", "program"), text: true},
	{label: "Strip 8th bit", values: func(*Game) []string { return onOffOpts },
		get: func(g *Game) string { return onOff(g.prof.StripHighBit) }, set: func(g *Game, v string) { g.prof.StripHighBit = v == "on" }, advanced: true},
	{label: "Enter key sends", values: func(*Game) []string { return enterOpts },
		get: func(g *Game) string { return pick(enterOpts, g.prof.EnterSends) },
		set: func(g *Game, v string) { g.prof.EnterSends = firstWord(v) }, advanced: true},
	{label: "Backspace sends", values: func(*Game) []string { return bsOpts },
		get: func(g *Game) string { return pick(bsOpts, g.prof.Backspace) },
		set: func(g *Game, v string) { g.prof.Backspace = firstWord(v) }, advanced: true},
	{label: "Received DEL", values: func(*Game) []string { return rxDelOpts },
		get: func(g *Game) string {
			if g.prof.RxDelErase {
				return rxDelOpts[1]
			}
			return rxDelOpts[0]
		},
		set: func(g *Game, v string) { g.prof.RxDelErase = v == rxDelOpts[1] }, advanced: true, show: serialOnly},
	{label: "Local echo", values: func(*Game) []string { return onOffOpts },
		get: func(g *Game) string { return onOff(g.prof.LocalEcho) }, set: func(g *Game, v string) { g.prof.LocalEcho = v == "on" }, advanced: true},
}

func (g *Game) portChoices() []string {
	ports := g.setup.ports
	if g.prof.Port != "" {
		found := false
		for _, p := range ports {
			if p == g.prof.Port {
				found = true
			}
		}
		if !found {
			ports = append([]string{g.prof.Port}, ports...)
		}
	}
	if len(ports) == 0 {
		return []string{""}
	}
	return ports
}

func (g *Game) enterSetup() {
	g.mode = modeSetup
	g.setup.ports = link.Ports()
	g.setup.naming = false
	if g.setup.profile == "" {
		g.setup.profile = noProfile
	}
	if g.prof.Port == "" && len(g.setup.ports) > 0 {
		g.prof.Port = g.setup.ports[0]
	}
	g.setup.sel = 1 // Connection
	g.setup.editing = false
}

// next steps through values cyclically, starting from cur.
func next(values []string, cur string, dir int) string {
	if len(values) == 0 {
		return cur
	}
	for i, v := range values {
		if v == cur {
			return values[(i+dir+len(values))%len(values)]
		}
	}
	return values[0]
}

func (g *Game) updateSetup() {
	st := &g.setup
	jp := inpututil.IsKeyJustPressed
	chars := ebiten.AppendInputChars(nil)

	if st.naming {
		for _, r := range chars {
			if r >= 040 && r < 0177 && len(st.name) < 40 {
				st.name = append(st.name, r)
			}
		}
		switch {
		case repeating(ebiten.KeyBackspace) && len(st.name) > 0:
			st.name = st.name[:len(st.name)-1]
		case jp(ebiten.KeyEnter) || jp(ebiten.KeyNumpadEnter):
			if n := string(st.name); n != "" && n != noProfile {
				g.cfg.Profiles[n] = g.prof
				st.profile = n
				if err := g.cfg.save(); err != nil {
					g.flash("Could not save: " + err.Error())
				} else {
					g.flash("Saved profile " + n)
				}
			}
			st.naming = false
		case jp(ebiten.KeyEscape):
			st.naming = false
		}
		return
	}

	vis := g.visibleFields()
	if st.sel >= len(vis) {
		st.sel = len(vis) - 1
	}
	f := vis[st.sel]

	// Typing into a text field.
	if st.editing {
		v := f.get(g)
		for _, r := range chars {
			if r < 32 || r == 127 || len(v) >= 200 || (f.digits && (r < '0' || r > '9')) {
				continue
			}
			v += string(r)
		}
		if repeating(ebiten.KeyBackspace) && len(v) > 0 {
			v = v[:len(v)-1]
		}
		if v != f.get(g) {
			f.set(g, v)
			g.markEdited(f.label)
		}
		if jp(ebiten.KeyEnter) || jp(ebiten.KeyNumpadEnter) || jp(ebiten.KeyEscape) || jp(ebiten.KeyTab) ||
			jp(ebiten.KeyArrowUp) || jp(ebiten.KeyArrowDown) {
			st.editing = false
		}
		return
	}

	switch {
	case repeating(ebiten.KeyArrowUp):
		st.sel = (st.sel + len(vis) - 1) % len(vis)
	case repeating(ebiten.KeyArrowDown), jp(ebiten.KeyTab):
		st.sel = (st.sel + 1) % len(vis)
	case f.text && (jp(ebiten.KeyArrowRight) || jp(ebiten.KeySpace) || jp(ebiten.KeyArrowLeft)):
		st.editing = true
	case !f.text && repeating(ebiten.KeyArrowLeft):
		f.set(g, next(f.values(g), f.get(g), -1))
		g.markEdited(f.label)
	case !f.text && (repeating(ebiten.KeyArrowRight) || jp(ebiten.KeySpace)):
		f.set(g, next(f.values(g), f.get(g), 1))
		g.markEdited(f.label)
	case jp(ebiten.KeyA):
		cur := f.label
		st.advanced = !st.advanced
		st.sel = 0
		for i, vf := range g.visibleFields() {
			if vf.label == cur {
				st.sel = i
			}
		}
	case jp(ebiten.KeyEnter), jp(ebiten.KeyNumpadEnter):
		// Connect in the background; if it fails we come back here and
		// the error is shown below the settings.
		g.cfg.Last = g.prof
		_ = g.cfg.save()
		g.mode = modeTerminal
		g.connect()
	case jp(ebiten.KeyEscape):
		g.mode = modeTerminal
		g.cfg.Last = g.prof
		_ = g.cfg.save()
	case jp(ebiten.KeyR):
		st.ports = link.Ports()
		g.flash(fmt.Sprintf("Found %d serial port(s)", len(st.ports)))
	case jp(ebiten.KeyN):
		st.naming = true
		st.name = nil
		if st.profile != noProfile {
			st.name = []rune(st.profile)
		}
	case jp(ebiten.KeyDelete) || (jp(ebiten.KeyBackspace) && st.sel == 0):
		if _, ok := g.cfg.Profiles[st.profile]; ok {
			delete(g.cfg.Profiles, st.profile)
			_ = g.cfg.save()
			g.flash("Deleted profile " + st.profile)
			st.profile = noProfile
		}
	}
}

// markEdited detaches from the named profile once a setting is changed, so
// the change is not mistaken for the saved profile.
func (g *Game) markEdited(label string) {
	if label == "Profile" {
		return
	}
	if p, ok := g.cfg.Profiles[g.setup.profile]; ok && p != g.prof {
		g.setup.profile = noProfile
	}
}

func (g *Game) setupPage() []pageLine {
	st := &g.setup
	out := []pageLine{
		{},
		{text: "  DASHER D200 EMULATOR - SETUP" + strings.Repeat(" ", 34) + "v" + version},
		{},
	}
	for i, f := range g.visibleFields() {
		v := f.get(g)
		if f.label == "Serial port" && v == "" {
			v = "(no serial ports found - plug in adapter, press R)"
		}
		if f.text {
			if i == st.sel && st.editing {
				v += "_"
			} else if v == "" {
				v = textPlaceholder(f.label, g)
			}
		}
		if len(v) > 54 {
			v = "..." + v[len(v)-51:]
		}
		line := fmt.Sprintf("  %-16s < %s >", f.label, v)
		if f.text {
			line = fmt.Sprintf("  %-16s [ %s ]", f.label, v)
		}
		out = append(out, pageLine{text: line, selected: i == st.sel})
	}
	out = append(out, pageLine{})
	if st.naming {
		out = append(out, pageLine{text: "  Save profile as: " + string(st.name) + "_"},
			pageLine{text: "  Type a name and press Enter to save (an existing name is updated)", dim: true},
			pageLine{text: "  Esc cancels without saving", dim: true})
	} else {
		out = append(out,
			pageLine{text: setupHint(g), dim: true},
			pageLine{text: "  N save as profile   Del delete profile   R rescan ports", dim: true},
			pageLine{text: advancedHint(st.advanced), dim: true})
	}
	out = append(out, pageLine{})
	for _, n := range g.setupNotes() {
		out = append(out, pageLine{text: "  " + n})
	}
	g.mu.Lock()
	if g.lineErr != "" && g.line == nil {
		for i, l := range wrap("Last error: "+g.lineErr, dasher.Cols-4) {
			if i < 6 {
				out = append(out, pageLine{text: "  " + l})
			}
		}
	}
	g.mu.Unlock()
	return out
}

func (g *Game) setupNotes() []string {
	var n []string
	if g.prof.Flow == "xonxoff" {
		n = append(n, "Note: with XON/XOFF, host codes 021/023 are taken as flow control,",
			"so the Dasher print-screen and roll-disable commands are lost.")
	}
	if (g.prof.Parity == "mark" || g.prof.Parity == "space") && g.prof.DataBits == 8 && runtime.GOOS == "darwin" {
		n = append(n, "Note: macOS cannot do mark/space parity with 8 data bits; use 7.")
	}
	return n
}

func setupHint(g *Game) string {
	vis := g.visibleFields()
	if g.setup.sel < len(vis) && vis[g.setup.sel].label == "Profile" {
		if len(g.cfg.Profiles) == 0 {
			return "  No saved profiles yet: set up a connection, then press N to save it"
		}
		return "  Left/Right choose a saved profile   Enter connect   Esc back"
	}
	if g.setup.sel < len(vis) && vis[g.setup.sel].text {
		if g.setup.editing {
			return "  Type the text, Backspace to delete, Enter when done"
		}
		return "  Up/Down select   Right edit text   Enter connect   Esc back"
	}
	return "  Up/Down select   Left/Right change   Enter connect   Esc back"
}

// textPlaceholder is shown in an empty text field.
func textPlaceholder(label string, g *Game) string {
	switch label {
	case "Port":
		if g.prof.Kind == "ssh" {
			return "(22)"
		}
		return "(23)"
	case "Host":
		return "(name or address)"
	case "User":
		return "(login name)"
	case "Command":
		return "(program to run, e.g. ./mvemu)"
	case "Start in folder":
		return "(optional)"
	case "Terminal type":
		return "(d200)"
	}
	return ""
}

func advancedHint(shown bool) string {
	if shown {
		return "  A hide advanced settings"
	}
	return "  A show advanced settings (Enter/Backspace codes, echo, terminal type)"
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
