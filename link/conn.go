package link

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Conn is any connection the terminal can talk through: a serial line,
// telnet, SSH, or a local program running in a pseudo-terminal.
type Conn interface {
	Send(b []byte)
	Break(d time.Duration) error
	Close()
	Describe() string // short description for the status bar
	Held() bool       // transmission held by flow control
	Counts() (rx, tx int64)
}

// Handlers are the callbacks every connection uses.
type Handlers struct {
	OnData  func([]byte)  // bytes for the terminal
	OnTrace func(Traffic) // raw traffic for the line monitor
	OnError func(error)   // called once if the connection drops
	// Prompt asks the user a question (password, host key) and blocks
	// until answered. echo=false means the answer should be hidden.
	Prompt    func(question string, echo bool) (string, error)
	StripHigh bool // clear bit 7 on received bytes
}

// ErrCancelled is returned by Prompt when the user cancels.
var ErrCancelled = errors.New("cancelled")

// Describe, Held and Counts make the serial Line a Conn.

func (l *Line) Describe() string { return shortName(l.settings.Port) + "  " + l.settings.String() }
func (l *Line) Held() bool       { return l.XOFF() }
func (l *Line) Counts() (int64, int64) {
	return l.RxCount.Load(), l.TxCount.Load()
}

func shortName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

// stream is shared plumbing for byte-stream connections (telnet, SSH,
// program): a reader goroutine, a writer goroutine, counters and a
// single error/close path.
type stream struct {
	h      Handlers
	w      io.Writer
	closer func() error
	desc   string

	out    chan []byte
	done   chan struct{}
	closed sync.Once
	failed atomic.Bool

	rx, tx atomic.Int64

	// filterIn may rewrite received bytes (telnet strips IAC sequences).
	filterIn func([]byte) []byte
	// filterOut may rewrite bytes before sending (telnet escapes IAC).
	filterOut func([]byte) []byte
}

func newStream(h Handlers, w io.Writer, closer func() error, desc string) *stream {
	return &stream{h: h, w: w, closer: closer, desc: desc,
		out: make(chan []byte, 256), done: make(chan struct{})}
}

func (s *stream) start(r io.Reader) {
	go s.readLoop(r)
	go s.writeLoop()
}

func (s *stream) Describe() string { return s.desc }
func (s *stream) Held() bool       { return false }
func (s *stream) Counts() (int64, int64) {
	return s.rx.Load(), s.tx.Load()
}

func (s *stream) Send(b []byte) {
	if len(b) == 0 {
		return
	}
	c := append([]byte(nil), b...)
	select {
	case s.out <- c:
	case <-s.done:
	}
}

func (s *stream) Close() {
	s.closed.Do(func() {
		close(s.done)
		if s.closer != nil {
			_ = s.closer()
		}
	})
}

func (s *stream) fail(err error) {
	select {
	case <-s.done:
		return // closed deliberately
	default:
	}
	s.Close()
	if s.failed.CompareAndSwap(false, true) && s.h.OnError != nil {
		s.h.OnError(err)
	}
}

// writeRaw sends bytes directly (used for protocol replies).
func (s *stream) writeRaw(b []byte) error {
	_, err := s.w.Write(b)
	return err
}

func (s *stream) readLoop(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			raw := append([]byte(nil), buf[:n]...)
			s.rx.Add(int64(n))
			if s.h.OnTrace != nil {
				s.h.OnTrace(Traffic{Data: raw})
			}
			data := raw
			if s.filterIn != nil {
				data = s.filterIn(raw)
			}
			if s.h.StripHigh {
				for i := range data {
					data[i] &= 0177
				}
			}
			if len(data) > 0 && s.h.OnData != nil {
				s.h.OnData(data)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("connection closed by the other end")
			}
			s.fail(err)
			return
		}
	}
}

func (s *stream) writeLoop() {
	for {
		select {
		case <-s.done:
			return
		case b := <-s.out:
			wire := b
			if s.filterOut != nil {
				wire = s.filterOut(b)
			}
			if _, err := s.w.Write(wire); err != nil {
				s.fail(err)
				return
			}
			s.tx.Add(int64(len(wire)))
			if s.h.OnTrace != nil {
				s.h.OnTrace(Traffic{TX: true, Data: wire})
			}
		}
	}
}

// waitTimeout is the connect timeout for network connections.
const waitTimeout = 15 * time.Second
