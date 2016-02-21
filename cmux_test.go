package cmux

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/rpc"
	"testing"
)

const (
	testHTTP1Resp = "http1"
	rpcVal        = 1234
)

var testPort = 5125

func testAddr() string {
	testPort++
	return fmt.Sprintf("127.0.0.1:%d", testPort)
}

func testListener(t *testing.T) (net.Listener, string) {
	addr := testAddr()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return l, addr
}

type testHTTP1Handler struct{}

func (h *testHTTP1Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, testHTTP1Resp)
}

func runTestHTTPServer(t *testing.T, l net.Listener) {
	s := &http.Server{
		Handler: &testHTTP1Handler{},
	}
	if err := s.Serve(l); err != nil {
		t.Log(err)
	}
}

func runTestHTTP1Client(t *testing.T, addr string) {
	r, err := http.Get("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := r.Body.Close(); err != nil {
			t.Log(err)
		}
	}()
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
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

func runTestRPCServer(t *testing.T, l net.Listener) {
	s := rpc.NewServer()
	if err := s.Register(TestRPCRcvr{}); err != nil {
		t.Fatal(err)
	}
	for {
		c, err := l.Accept()
		if err != nil {
			t.Log(err)
			return
		}
		go s.ServeConn(c)
	}
}

func runTestRPCClient(t *testing.T, addr string) {
	c, err := rpc.Dial("tcp", addr)
	if err != nil {
		t.Error(err)
		return
	}

	var num int
	if err := c.Call("TestRPCRcvr.Test", rpcVal, &num); err != nil {
		t.Error(err)
		return
	}

	if num != rpcVal {
		t.Errorf("wrong rpc response: want=%d got=%v", rpcVal, num)
	}
}

func TestAny(t *testing.T) {
	l, addr := testListener(t)
	defer func() {
		if err := l.Close(); err != nil {
			t.Log(err)
		}
	}()

	muxl := New(l)
	httpl := muxl.Match(Any())

	go runTestHTTPServer(t, httpl)
	go func() {
		if err := muxl.Serve(); err != nil {
			t.Log(err)
		}
	}()

	r, err := http.Get("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := r.Body.Close(); err != nil {
			t.Log(err)
		}
	}()
	b, err := ioutil.ReadAll(r.Body)
	if string(b) != testHTTP1Resp {
		t.Errorf("invalid response: want=%s got=%s", testHTTP1Resp, b)
	}
}

func TestHTTPGoRPC(t *testing.T) {
	l, addr := testListener(t)
	defer func() {
		if err := l.Close(); err != nil {
			t.Log(err)
		}
	}()

	muxl := New(l)
	httpl := muxl.Match(HTTP2(), HTTP1Fast())
	rpcl := muxl.Match(Any())

	go runTestHTTPServer(t, httpl)
	go runTestRPCServer(t, rpcl)
	go func() {
		if err := muxl.Serve(); err != nil {
			t.Log(err)
		}
	}()

	runTestHTTP1Client(t, addr)
	runTestRPCClient(t, addr)
}

func TestErrorHandler(t *testing.T) {
	l, addr := testListener(t)
	defer func() {
		if err := l.Close(); err != nil {
			t.Log(err)
		}
	}()

	muxl := New(l)
	httpl := muxl.Match(HTTP2(), HTTP1Fast())

	go runTestHTTPServer(t, httpl)
	go func() {
		if err := muxl.Serve(); err != nil {
			t.Log(err)
		}
	}()

	firstErr := true
	muxl.HandleError(func(err error) bool {
		if !firstErr {
			return true
		}
		if _, ok := err.(ErrNotMatched); !ok {
			t.Errorf("unexpected error: %v", err)
		}
		firstErr = false
		return true
	})

	c, err := rpc.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	var num int
	if err := c.Call("TestRPCRcvr.Test", rpcVal, &num); err == nil {
		t.Error("rpc got a response")
	}
}

type closerConn struct {
	net.Conn
}

func (c closerConn) Close() error { return nil }

func TestClosed(t *testing.T) {
	mux := &cMux{}
	lis := mux.Match(Any()).(muxListener)
	close(lis.donec)
	mux.serve(closerConn{})
	_, err := lis.Accept()
	if _, ok := err.(errListenerClosed); !ok {
		t.Errorf("expected errListenerClosed got %v", err)
	}
}
