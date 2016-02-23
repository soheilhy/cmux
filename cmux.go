package cmux

import (
	"fmt"
	"io"
	"net"
	"sync"
)

// Matcher matches a connection based on its content.
type Matcher func(io.Reader) bool

// ErrorHandler handles an error and returns whether
// the mux should continue serving the listener.
type ErrorHandler func(error) bool

var _ net.Error = ErrNotMatched{}

// ErrNotMatched is returned whenever a connection is not matched by any of
// the matchers registered in the multiplexer.
type ErrNotMatched struct {
	c net.Conn
}

func (e ErrNotMatched) Error() string {
	return fmt.Sprintf("mux: connection %v not matched by an matcher",
		e.c.RemoteAddr())
}

// Temporary implements the net.Error interface.
func (e ErrNotMatched) Temporary() bool { return true }

// Timeout implements the net.Error interface.
func (e ErrNotMatched) Timeout() bool { return false }

type errListenerClosed string

func (e errListenerClosed) Error() string   { return string(e) }
func (e errListenerClosed) Temporary() bool { return false }
func (e errListenerClosed) Timeout() bool   { return false }

// ErrListenerClosed is returned from muxListener.Accept when the underlying
// listener is closed.
var ErrListenerClosed = errListenerClosed("mux: listener closed")

// New instantiates a new connection multiplexer.
func New(l net.Listener) CMux {
	return NewSize(l, 1024)
}

// NewSize instantiates a new connection multiplexer which buffers up to size
// connections in its child listeners.
func NewSize(l net.Listener, size int) CMux {
	return &cMux{
		root:   l,
		bufLen: size,
		errh:   func(_ error) bool { return true },
	}
}

// CMux is a multiplexer for network connections.
type CMux interface {
	// Match returns a net.Listener that sees (i.e., accepts) only
	// the connections matched by at least one of the matcher.
	//
	// The order used to call Match determines the priority of matchers.
	Match(...Matcher) net.Listener
	// Serve starts multiplexing the listener. Serve blocks and perhaps
	// should be invoked concurrently within a go routine.
	Serve() error
	// HandleError registers an error handler that handles listener errors.
	HandleError(ErrorHandler)
}

type matchersListener struct {
	ss []Matcher
	l  muxListener
}

type cMux struct {
	root   net.Listener
	bufLen int
	errh   ErrorHandler
	sls    []matchersListener
}

func (m *cMux) Match(matchers ...Matcher) net.Listener {
	ml := muxListener{
		Listener: m.root,
		connc:    make(chan net.Conn, m.bufLen),
	}
	m.sls = append(m.sls, matchersListener{ss: matchers, l: ml})
	return ml
}

func (m *cMux) Serve() error {
	var wg sync.WaitGroup

	defer func() {
		wg.Wait()

		for _, sl := range m.sls {
			close(sl.l.connc)
		}
	}()

	for {
		c, err := m.root.Accept()
		if err != nil {
			if !m.handleErr(err) {
				return err
			}
			continue
		}

		wg.Add(1)
		go m.serve(c, &wg)
	}
}

func (m *cMux) serve(c net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()

	muc := newMuxConn(c)
	for _, sl := range m.sls {
		for _, s := range sl.ss {
			matched := s(muc.sniffer())
			muc.reset()
			if matched {
				sl.l.connc <- muc
				return
			}
		}
	}

	_ = c.Close()
	err := ErrNotMatched{c: c}
	if !m.handleErr(err) {
		_ = m.root.Close()
	}
}

func (m *cMux) HandleError(h ErrorHandler) {
	m.errh = h
}

func (m *cMux) handleErr(err error) bool {
	if !m.errh(err) {
		return false
	}

	if ne, ok := err.(net.Error); ok {
		return ne.Temporary()
	}

	return false
}

type muxListener struct {
	net.Listener
	connc chan net.Conn
}

func (l muxListener) Accept() (net.Conn, error) {
	c, ok := <-l.connc
	if !ok {
		return nil, ErrListenerClosed
	}
	return c, nil
}

// MuxConn wraps a net.Conn and provides transparent sniffing of connection data.
type MuxConn struct {
	net.Conn
	buf buffer
}

func newMuxConn(c net.Conn) *MuxConn {
	return &MuxConn{
		Conn: c,
	}
}

// From the io.Reader documentation:
//
// When Read encounters an error or end-of-file condition after
// successfully reading n > 0 bytes, it returns the number of
// bytes read.  It may return the (non-nil) error from the same call
// or return the error (and n == 0) from a subsequent call.
// An instance of this general case is that a Reader returning
// a non-zero number of bytes at the end of the input stream may
// return either err == EOF or err == nil.  The next Read should
// return 0, EOF.
//
// This function implements the latter behaviour, returning the
// (non-nil) error from the same call.
func (m *MuxConn) Read(b []byte) (int, error) {
	n1, err := m.buf.Read(b)
	if n1 == len(b) || err != io.EOF {
		return n1, err
	}
	n2, err := m.Conn.Read(b[n1:])
	return n1 + n2, err
}

func (m *MuxConn) sniffer() io.Reader {
	return io.MultiReader(&m.buf, io.TeeReader(m.Conn, &m.buf))
}

func (m *MuxConn) reset() {
	m.buf.resetRead()
}
