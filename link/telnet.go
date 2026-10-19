package link

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// Telnet protocol bytes (RFC 854 and friends).
const (
	tIAC  = 255
	tDONT = 254
	tDO   = 253
	tWONT = 252
	tWILL = 251
	tSB   = 250
	tBRK  = 243
	tSE   = 240

	optBinary = 0
	optEcho   = 1
	optSGA    = 3
	optTType  = 24
	optNAWS   = 31
)

// TelnetConn is a telnet client connection.
type TelnetConn struct {
	*stream
	conn     net.Conn
	termType string

	mu       sync.Mutex
	state    int // parser state
	cmd      byte
	sb       []byte
	binOut   bool // we agreed to send binary
	lastCR   bool
	sentWill map[byte]bool
	sentDo   map[byte]bool
}

// DialTelnet connects to host:port. termType is reported if the host asks
// for the terminal type (e.g. "D200").
func DialTelnet(host string, port int, termType string, h Handlers) (*TelnetConn, error) {
	if host == "" {
		return nil, fmt.Errorf("no host name entered")
	}
	if port == 0 {
		port = 23
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	c, err := net.DialTimeout("tcp", addr, waitTimeout)
	if err != nil {
		return nil, fmt.Errorf("can't reach %s: %w", addr, netReason(err))
	}
	t := &TelnetConn{conn: c, termType: termType,
		sentWill: map[byte]bool{}, sentDo: map[byte]bool{}}
	desc := "telnet " + host
	if port != 23 {
		desc += ":" + strconv.Itoa(port)
	}
	t.stream = newStream(h, c, c.Close, desc)
	t.stream.filterIn = t.filterIn
	t.stream.filterOut = t.filterOut
	t.stream.start(c)
	return t, nil
}

// Break sends the telnet BREAK command.
func (t *TelnetConn) Break(time.Duration) error {
	return t.writeRaw([]byte{tIAC, tBRK})
}

// filterOut doubles IAC bytes and, outside binary mode, follows a bare CR
// with NUL as the telnet spec requires.
func (t *TelnetConn) filterOut(b []byte) []byte {
	t.mu.Lock()
	bin := t.binOut
	t.mu.Unlock()
	out := make([]byte, 0, len(b)+4)
	for _, c := range b {
		out = append(out, c)
		if c == tIAC {
			out = append(out, tIAC)
		}
		if c == '\r' && !bin {
			out = append(out, 0)
		}
	}
	return out
}

const (
	stData = iota
	stIAC
	stCmd
	stSB
	stSBIAC
)

// filterIn removes telnet commands from received data and answers option
// negotiation.
func (t *TelnetConn) filterIn(b []byte) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := b[:0:0]
	for _, c := range b {
		switch t.state {
		case stData:
			if c == tIAC {
				t.state = stIAC
				continue
			}
			// CR NUL and CR LF from the host: drop the NUL after CR.
			if t.lastCR && c == 0 {
				t.lastCR = false
				continue
			}
			t.lastCR = c == '\r'
			out = append(out, c)
		case stIAC:
			switch c {
			case tIAC:
				out = append(out, tIAC)
				t.state = stData
			case tDO, tDONT, tWILL, tWONT:
				t.cmd = c
				t.state = stCmd
			case tSB:
				t.sb = t.sb[:0]
				t.state = stSB
			default:
				t.state = stData // NOP, GA, etc.
			}
		case stCmd:
			t.negotiate(t.cmd, c)
			t.state = stData
		case stSB:
			if c == tIAC {
				t.state = stSBIAC
			} else {
				t.sb = append(t.sb, c)
			}
		case stSBIAC:
			if c == tSE {
				t.subnegotiation(t.sb)
				t.state = stData
			} else {
				t.sb = append(t.sb, c)
				t.state = stSB
			}
		}
	}
	return out
}

func (t *TelnetConn) reply(cmd, opt byte) {
	_ = t.writeRaw([]byte{tIAC, cmd, opt})
}

// negotiate answers DO/DONT/WILL/WONT, replying only when our state
// changes so negotiations can't loop.
func (t *TelnetConn) negotiate(cmd, opt byte) {
	switch cmd {
	case tDO:
		switch opt {
		case optBinary, optSGA, optTType, optNAWS:
			if !t.sentWill[opt] {
				t.sentWill[opt] = true
				t.reply(tWILL, opt)
			}
			if opt == optBinary {
				t.binOut = true
			}
			if opt == optNAWS {
				// Window size: 80 x 24.
				_ = t.writeRaw([]byte{tIAC, tSB, optNAWS, 0, 80, 0, 24, tIAC, tSE})
			}
		default:
			t.reply(tWONT, opt)
		}
	case tDONT:
		if t.sentWill[opt] {
			t.sentWill[opt] = false
			t.reply(tWONT, opt)
		}
		if opt == optBinary {
			t.binOut = false
		}
	case tWILL:
		switch opt {
		case optEcho, optSGA, optBinary:
			if !t.sentDo[opt] {
				t.sentDo[opt] = true
				t.reply(tDO, opt)
			}
		default:
			t.reply(tDONT, opt)
		}
	case tWONT:
		if t.sentDo[opt] {
			t.sentDo[opt] = false
			t.reply(tDONT, opt)
		}
	}
}

func (t *TelnetConn) subnegotiation(sb []byte) {
	// TERMINAL-TYPE SEND -> IS <type>
	if len(sb) >= 2 && sb[0] == optTType && sb[1] == 1 {
		msg := []byte{tIAC, tSB, optTType, 0}
		msg = append(msg, []byte(t.termType)...)
		msg = append(msg, tIAC, tSE)
		_ = t.writeRaw(msg)
	}
}

// netReason strips Go's "dial tcp host:port:" prefix from network errors.
func netReason(err error) error {
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err
	}
	return err
}
