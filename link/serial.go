// Package link handles the serial connection to the DG host: line settings,
// flow control, BREAK, and a record of traffic for the diagnostic panel.
package link

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
)

// Settings describes one serial line configuration.
type Settings struct {
	Port         string `json:"port"`
	Baud         int    `json:"baud"`
	DataBits     int    `json:"dataBits"` // 7 or 8
	Parity       string `json:"parity"`   // none, even, odd, mark, space
	StopBits     int    `json:"stopBits"` // 1 or 2
	Flow         string `json:"flow"`     // none, xonxoff, rtscts
	StripHighBit bool   `json:"stripHighBit"`
}

// Defaults are a common starting point for a DG console line.
func Defaults() Settings {
	return Settings{Baud: 9600, DataBits: 8, Parity: "none", StopBits: 1, Flow: "none", StripHighBit: true}
}

func (s Settings) String() string {
	p := "N"
	if len(s.Parity) > 0 {
		p = strings.ToUpper(s.Parity[:1])
	}
	flow := map[string]string{"none": "no flow ctl", "xonxoff": "XON/XOFF", "rtscts": "RTS/CTS"}[s.Flow]
	return fmt.Sprintf("%d %d%s%d %s", s.Baud, s.DataBits, p, s.StopBits, flow)
}

// Ports lists serial ports, with macOS call-out (cu.*) devices first since
// tty.* devices block on open until carrier detect is asserted.
func Ports() []string {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil
	}
	sort.SliceStable(ports, func(i, j int) bool {
		ci := strings.Contains(ports[i], "/cu.")
		cj := strings.Contains(ports[j], "/cu.")
		if ci != cj {
			return ci
		}
		return ports[i] < ports[j]
	})
	return ports
}

// Traffic is one chunk of bytes for the diagnostic panel.
type Traffic struct {
	TX   bool
	Data []byte
}

// Line is an open serial connection.
type Line struct {
	port     serial.Port
	settings Settings

	// forceBit7 emulates mark (set) or space (clear) parity on transmit when
	// the line is 7 data bits: the port runs 8N and bit 7 carries the parity.
	emulate   bool
	forceBit7 byte

	out    chan []byte
	done   chan struct{}
	closed sync.Once

	xoff    atomic.Bool // host has sent XOFF
	ctsLow  atomic.Bool // RTS/CTS: host is holding CTS low
	RxCount atomic.Int64
	TxCount atomic.Int64

	onData  func([]byte)
	onTrace func(Traffic)
	onError func(error)
}

// Open opens the port. onData receives host bytes for the terminal; onTrace
// receives raw traffic in both directions; onError is called once if the
// line fails (for example, the USB adapter is unplugged).
func Open(s Settings, onData func([]byte), onTrace func(Traffic), onError func(error)) (*Line, error) {
	if s.Port == "" {
		return nil, errors.New("no serial port selected")
	}
	mode := &serial.Mode{BaudRate: s.Baud, DataBits: s.DataBits, StopBits: serial.OneStopBit}
	if s.StopBits == 2 {
		mode.StopBits = serial.TwoStopBits
	}
	l := &Line{settings: s, out: make(chan []byte, 256), done: make(chan struct{}),
		onData: onData, onTrace: onTrace, onError: onError}

	switch s.Parity {
	case "even":
		mode.Parity = serial.EvenParity
	case "odd":
		mode.Parity = serial.OddParity
	case "mark", "space":
		if s.DataBits == 7 {
			// Portable emulation: macOS has no native mark/space parity.
			mode.DataBits = 8
			mode.Parity = serial.NoParity
			l.emulate = true
			if s.Parity == "mark" {
				l.forceBit7 = 0200
			}
		} else if s.Parity == "mark" {
			mode.Parity = serial.MarkParity
		} else {
			mode.Parity = serial.SpaceParity
		}
	default:
		mode.Parity = serial.NoParity
	}

	p, err := serial.Open(s.Port, mode)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", s.Port, err)
	}
	l.port = p
	_ = p.SetReadTimeout(50 * time.Millisecond)
	_ = p.SetDTR(true)
	_ = p.SetRTS(true)

	go l.readLoop()
	go l.writeLoop()
	return l, nil
}

// Settings returns the settings the line was opened with.
func (l *Line) Settings() Settings { return l.settings }

// XOFF reports whether transmission is currently held by the host.
func (l *Line) XOFF() bool { return l.xoff.Load() || l.ctsLow.Load() }

// Send queues bytes for transmission to the host.
func (l *Line) Send(b []byte) {
	if len(b) == 0 {
		return
	}
	c := append([]byte(nil), b...)
	select {
	case l.out <- c:
	case <-l.done:
	}
}

// Break holds the line in the spacing (BREAK) state for d. On an Eclipse
// with the virtual console, a BREAK on the console line halts the CPU and
// enters the virtual console.
func (l *Line) Break(d time.Duration) error {
	return l.port.Break(d)
}

// Close shuts the line down.
func (l *Line) Close() {
	l.closed.Do(func() {
		close(l.done)
		_ = l.port.Close()
	})
}

func (l *Line) fail(err error) {
	select {
	case <-l.done:
		return // closed deliberately; not an error
	default:
	}
	l.Close()
	if l.onError != nil {
		l.onError(err)
	}
}

func (l *Line) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := l.port.Read(buf)
		if n > 0 {
			raw := buf[:n]
			l.RxCount.Add(int64(n))
			if l.onTrace != nil {
				l.onTrace(Traffic{Data: append([]byte(nil), raw...)})
			}
			data := make([]byte, 0, n)
			for _, b := range raw {
				c := b
				if l.settings.StripHighBit || l.settings.DataBits == 7 || l.emulate {
					c &= 0177
				}
				if l.settings.Flow == "xonxoff" {
					// Note: on a Dasher, 021 and 023 are also "print screen"
					// and "roll disable"; with XON/XOFF selected they are
					// consumed as flow control.
					if c == 023 {
						l.xoff.Store(true)
						continue
					}
					if c == 021 {
						l.xoff.Store(false)
						continue
					}
				}
				data = append(data, c)
			}
			if len(data) > 0 && l.onData != nil {
				l.onData(data)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("serial port closed")
			}
			l.fail(err)
			return
		}
		select {
		case <-l.done:
			return
		default:
		}
	}
}

func (l *Line) writeLoop() {
	for {
		select {
		case <-l.done:
			return
		case b := <-l.out:
			for i := range b {
				if l.emulate {
					b[i] = b[i]&0177 | l.forceBit7
				}
			}
			for len(b) > 0 {
				if !l.waitClear() {
					return
				}
				// Send one byte at a time while flow control is active so a
				// mid-burst XOFF or CTS drop is honoured promptly.
				chunk := b
				if l.settings.Flow != "none" {
					chunk = b[:1]
				}
				n, err := l.port.Write(chunk)
				if err != nil {
					l.fail(err)
					return
				}
				if n > 0 {
					l.TxCount.Add(int64(n))
					if l.onTrace != nil {
						l.onTrace(Traffic{TX: true, Data: append([]byte(nil), chunk[:n]...)})
					}
				}
				b = b[n:]
			}
		}
	}
}

// waitClear blocks while the host is holding off transmission. It returns
// false if the line is closed while waiting.
func (l *Line) waitClear() bool {
	for {
		held := l.xoff.Load()
		if l.settings.Flow == "rtscts" {
			bits, err := l.port.GetModemStatusBits()
			cts := err != nil || bits.CTS // if the status can't be read, don't block forever
			l.ctsLow.Store(!cts)
			held = held || !cts
		}
		if !held {
			return true
		}
		select {
		case <-l.done:
			return false
		case <-time.After(5 * time.Millisecond):
		}
	}
}
