package cmux

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/rpc"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

const (
	testHTTP1Resp = "http1"
	rpcVal        = 1234
)

func safeServe(errCh chan<- error, muxl CMux) {
	if err := muxl.Serve(); !strings.Contains(err.Error(), "use of closed network connection") {
		errCh <- err
	}
}

func safeDial(t *testing.T, addr net.Addr) (*rpc.Client, func()) {
	c, err := rpc.Dial(addr.Network(), addr.String())
	if err != nil {
		t.Fatal(err)
	}
	return c, func() {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

type chanListener struct {
	net.Listener
	connCh chan net.Conn
}

func newChanListener() *chanListener {
	return &chanListener{connCh: make(chan net.Conn, 1)}
}

func (l *chanListener) Accept() (net.Conn, error) {
	if c, ok := <-l.connCh; ok {
		return c, nil
	}
	return nil, errors.New("use of closed network connection")
}

func testListener(t *testing.T) (net.Listener, func()) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	return l, func() {
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

type testHTTP1Handler struct{}

func (h *testHTTP1Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, testHTTP1Resp)
}

func runTestHTTPServer(errCh chan<- error, l net.Listener) {
	var mu sync.Mutex
	conns := make(map[net.Conn]struct{})

	defer func() {
		mu.Lock()
		for c := range conns {
			if err := c.Close(); err != nil {
				errCh <- err
			}
		}
		mu.Unlock()
	}()

	s := &http.Server{
		Handler: &testHTTP1Handler{},
		ConnState: func(c net.Conn, state http.ConnState) {
			mu.Lock()
			switch state {
			case http.StateNew:
				conns[c] = struct{}{}
			case http.StateClosed:
				delete(conns, c)
			}
			mu.Unlock()
		},
	}
	if err := s.Serve(l); err != ErrListenerClosed {
		errCh <- err
	}
}

func runTestHTTP1Client(t *testing.T, addr net.Addr) {
	r, err := http.Get("http://" + addr.String())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := r.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}

	if string(b) != testHTTP1Resp {
		t.Errorf("invalid response: want=%s got=%s", testHTTP1Resp, b)
	}
}

type TestRPCRcvr struct{}

func (r TestRPCRcvr) Test(i int, j *int) error {
	*j = i
	return nil
}

func runTestRPCServer(errCh chan<- error, l net.Listener) {
	s := rpc.NewServer()
	if err := s.Register(TestRPCRcvr{}); err != nil {
		errCh <- err
	}
	for {
		c, err := l.Accept()
		if err != nil {
			if err != ErrListenerClosed {
				errCh <- err
			}
			return
		}
		go s.ServeConn(c)
	}
}

func runTestRPCClient(t *testing.T, addr net.Addr) {
	c, cleanup := safeDial(t, addr)
	defer cleanup()

	var num int
	if err := c.Call("TestRPCRcvr.Test", rpcVal, &num); err != nil {
		t.Fatal(err)
	}

	if num != rpcVal {
		t.Errorf("wrong rpc response: want=%d got=%v", rpcVal, num)
	}
}

func TestRead(t *testing.T) {
	defer leakCheck(t)()
	errCh := make(chan error)
	defer func() {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}()
	const payload = "hello world\r\n"
	const mult = 2

	writer, reader := net.Pipe()
	go func() {
		if _, err := io.WriteString(writer, strings.Repeat(payload, mult)); err != nil {
			t.Fatal(err)
		}
	}()

	l := newChanListener()
	defer close(l.connCh)
	l.connCh <- reader
	muxl := New(l)
	// Register a bogus matcher to force reading from the conn.
	muxl.Match(HTTP2())
	anyl := muxl.Match(Any())
	go safeServe(errCh, muxl)
	muxedConn, err := anyl.Accept()
	if err != nil {
		t.Fatal(err)
	}
	var b [mult * len(payload)]byte
	n, err := muxedConn.Read(b[:])
	if err != nil {
		t.Fatal(err)
	}
	if e := len(b); n != e {
		t.Errorf("expected to read %d bytes, but read %d bytes", e, n)
	}
}

func TestAny(t *testing.T) {
	defer leakCheck(t)()
	errCh := make(chan error)
	defer func() {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}()
	l, cleanup := testListener(t)
	defer cleanup()

	muxl := New(l)
	httpl := muxl.Match(Any())

	go runTestHTTPServer(errCh, httpl)
	go safeServe(errCh, muxl)

	runTestHTTP1Client(t, l.Addr())
}

func TestHTTP2(t *testing.T) {
	defer leakCheck(t)()
	errCh := make(chan error)
	defer func() {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}()
	writer, reader := net.Pipe()
	go func() {
		if _, err := io.WriteString(writer, http2.ClientPreface); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	l := newChanListener()
	l.connCh <- reader
	muxl := New(l)
	// Register a bogus matcher that only reads one byte.
	muxl.Match(func(r io.Reader) bool {
		var b [1]byte
		_, _ = r.Read(b[:])
		return false
	})
	h2l := muxl.Match(HTTP2())
	go safeServe(errCh, muxl)
	close(l.connCh)
	if muxedConn, err := h2l.Accept(); err != nil {
		t.Fatal(err)
	} else {
		var b [len(http2.ClientPreface)]byte
		if _, err := muxedConn.Read(b[:]); err != io.EOF {
			t.Fatal(err)
		}
		if string(b[:]) != http2.ClientPreface {
			t.Errorf("got unexpected read %s, expected %s", b, http2.ClientPreface)
		}
	}
}

func TestHTTPGoRPC(t *testing.T) {
	defer leakCheck(t)()
	errCh := make(chan error)
	defer func() {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}()
	l, cleanup := testListener(t)
	defer cleanup()

	muxl := New(l)
	httpl := muxl.Match(HTTP2(), HTTP1Fast())
	rpcl := muxl.Match(Any())

	go runTestHTTPServer(errCh, httpl)
	go runTestRPCServer(errCh, rpcl)
	go safeServe(errCh, muxl)

	runTestHTTP1Client(t, l.Addr())
	runTestRPCClient(t, l.Addr())
}

func TestErrorHandler(t *testing.T) {
	defer leakCheck(t)()
	errCh := make(chan error)
	defer func() {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}()
	l, cleanup := testListener(t)
	defer cleanup()

	muxl := New(l)
	httpl := muxl.Match(HTTP2(), HTTP1Fast())

	go runTestHTTPServer(errCh, httpl)
	go safeServe(errCh, muxl)

	var errCount uint32
	muxl.HandleError(func(err error) bool {
		if atomic.AddUint32(&errCount, 1) == 1 {
			if _, ok := err.(ErrNotMatched); !ok {
				t.Errorf("unexpected error: %v", err)
			}
		}
		return true
	})

	c, cleanup := safeDial(t, l.Addr())
	defer cleanup()

	var num int
	for atomic.LoadUint32(&errCount) == 0 {
		if err := c.Call("TestRPCRcvr.Test", rpcVal, &num); err == nil {
			// The connection is simply closed.
			t.Errorf("unexpected rpc success after %d errors", atomic.LoadUint32(&errCount))
		}
	}
}

func TestClose(t *testing.T) {
	defer leakCheck(t)()
	errCh := make(chan error)
	defer func() {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}()
	l := newChanListener()

	muxl := NewSize(l, 0)
	anyl := muxl.Match(Any())

	defer func() {
		// Listener is closed.
		close(l.connCh)

		// Root listener is closed now.
		if _, err := anyl.Accept(); err != ErrListenerClosed {
			t.Fatal(err)
		}
	}()

	go safeServe(errCh, muxl)

	c1, c2 := net.Pipe()

	l.connCh <- c1

	// Simulate the child listener being temporarily blocked. The connection
	// multiplexer must be unbuffered (size=0). Before the fix, this would cause
	// connections to be lost.
	time.Sleep(50 * time.Millisecond)

	go func() {
		// Simulate the child listener being temporarily blocked. The connection
		// multiplexer must be unbuffered (size=0). Before the fix, this would cause
		// connections to be lost.
		time.Sleep(50 * time.Millisecond)

		l.connCh <- c2
	}()

	// First connection goes through. Before the fix, c1 would be lost while we
	// were temporarily blocked, so this consumed c2.
	if _, err := anyl.Accept(); err != nil {
		t.Fatal(err)
	}

	// Second connection goes through. Before the fix, c2 would be consumed above, so
	// this would block indefinitely.
	if _, err := anyl.Accept(); err != nil {
		t.Fatal(err)
	}
}

// Cribbed from google.golang.org/grpc/test/end2end_test.go.

// interestingGoroutines returns all goroutines we care about for the purpose
// of leak checking. It excludes testing or runtime ones.
func interestingGoroutines() (gs []string) {
	buf := make([]byte, 2<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for _, g := range strings.Split(string(buf), "\n\n") {
		sl := strings.SplitN(g, "\n", 2)
		if len(sl) != 2 {
			continue
		}
		stack := strings.TrimSpace(sl[1])
		if strings.HasPrefix(stack, "testing.RunTests") {
			continue
		}

		if stack == "" ||
			strings.Contains(stack, "testing.Main(") ||
			strings.Contains(stack, "runtime.goexit") ||
			strings.Contains(stack, "created by runtime.gc") ||
			strings.Contains(stack, "interestingGoroutines") ||
			strings.Contains(stack, "runtime.MHeap_Scavenger") {
			continue
		}
		gs = append(gs, g)
	}
	sort.Strings(gs)
	return
}

// leakCheck snapshots the currently-running goroutines and returns a
// function to be run at the end of tests to see whether any
// goroutines leaked.
func leakCheck(t testing.TB) func() {
	orig := map[string]bool{}
	for _, g := range interestingGoroutines() {
		orig[g] = true
	}
	return func() {
		// Loop, waiting for goroutines to shut down.
		// Wait up to 5 seconds, but finish as quickly as possible.
		deadline := time.Now().Add(5 * time.Second)
		for {
			var leaked []string
			for _, g := range interestingGoroutines() {
				if !orig[g] {
					leaked = append(leaked, g)
				}
			}
			if len(leaked) == 0 {
				return
			}
			if time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			for _, g := range leaked {
				t.Errorf("Leaked goroutine: %v", g)
			}
			return
		}
	}
}
