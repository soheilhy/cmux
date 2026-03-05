package h2

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"time"

	"golang.org/x/net/http2"
)

// validNextProto reports whether the proto is a valid ALPN protocol name.
// Everything is valid except the empty string and built-in protocol types,
// so that those can't be overridden with alternate implementations.
//
// grabbed from net/http.
func validNextProto(proto string) bool {
	switch proto {
	case "", "http/1.1", "http/1.0":
		return false
	}
	return true
}

// tlsRecordHeaderLooksLikeHTTP reports whether a TLS record header
// looks like it might've been a misdirected plaintext HTTP request.
//
// grabbed from net/http.
func tlsRecordHeaderLooksLikeHTTP(hdr [5]byte) bool {
	switch string(hdr[:]) {
	case "GET /", "HEAD ", "POST ", "PUT /", "OPTIO":
		return true
	}
	return false
}

type Server struct {
	hs       *http.Server
	srv      *http2.Server
	doneChan chan struct{}
}

type tlsConn interface {
	net.Conn
	ConnectionState() tls.ConnectionState
	HandshakeContext(ctx context.Context) error
}

// globalOptionsHandler responds to "OPTIONS *" requests.
//
// grabbed from net/http.
type globalOptionsHandler struct{}

func (globalOptionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Length", "0")
	if r.ContentLength != 0 {
		// Read up to 4KB of OPTIONS body (as mentioned in the
		// spec as being reserved for future use), but anything
		// over that is considered a waste of server resources
		// (or an attack) and we abort and close the connection,
		// courtesy of MaxBytesReader's EOF behavior.
		mb := http.MaxBytesReader(w, r.Body, 4<<10)
		io.Copy(io.Discard, mb)
	}
}

// initALPNRequest is an HTTP handler that initializes certain
// uninitialized fields in its *Request. Such partially-initialized
// Requests come from ALPN protocol handlers.
//
// grabbed from net/http.
type initALPNRequest struct {
	ctx context.Context
	c   tlsConn
	h   serverHandler
}

// BaseContext is an exported but unadvertised http.Handler method
// recognized by x/net/http2 to pass down a context; the TLSNextProto
// API predates context support so we shoehorn through the only
// interface we have available.
//
// grabbed from net/http.
func (h initALPNRequest) BaseContext() context.Context { return h.ctx }

func (h initALPNRequest) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.TLS == nil {
		req.TLS = &tls.ConnectionState{}
		*req.TLS = h.c.ConnectionState()
	}
	if req.Body == nil {
		req.Body = http.NoBody
	}
	if req.RemoteAddr == "" {
		req.RemoteAddr = h.c.RemoteAddr().String()
	}
	h.h.ServeHTTP(rw, req)
}

// serverHandler delegates to either the server's Handler or
// DefaultServeMux and also handles "OPTIONS *" requests.
//
// grabbed from net/http.
type serverHandler struct {
	srv *http.Server
}

func (sh serverHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	handler := sh.srv.Handler
	if handler == nil {
		handler = http.DefaultServeMux
	}
	if req.RequestURI == "*" && req.Method == "OPTIONS" {
		handler = globalOptionsHandler{}
	}

	handler.ServeHTTP(rw, req)
}

type conn struct {
	rwc net.Conn
	Server
}

func (c conn) tlsHandshakeTimeout() time.Duration {
	srv := c.hs
	var ret time.Duration
	for _, v := range [...]time.Duration{
		srv.ReadHeaderTimeout,
		srv.ReadTimeout,
		srv.WriteTimeout,
	} {
		if v <= 0 {
			continue
		}
		if ret == 0 || v < ret {
			ret = v
		}
	}
	return ret
}

func (c conn) serve(ctx context.Context) {
	remoteAddr := c.rwc.RemoteAddr().String()
	ctx = context.WithValue(ctx, http.LocalAddrContextKey, c.rwc.LocalAddr())

	defer func() {
		if err := recover(); err != nil && err != http.ErrAbortHandler {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			c.logf("http: panic serving %v: %v\n%s", remoteAddr, err, buf)
		}
	}()

	if tlsConn, ok := c.rwc.(tlsConn); ok {
		tlsTO := c.tlsHandshakeTimeout()
		if tlsTO > 0 {
			dl := time.Now().Add(tlsTO)
			c.rwc.SetReadDeadline(dl)
			c.rwc.SetWriteDeadline(dl)
		}
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			// If the handshake failed due to the client not speaking
			// TLS, assume they're speaking plaintext HTTP and write a
			// 400 response on the TLS conn's underlying net.Conn.
			if re, ok := err.(tls.RecordHeaderError); ok && re.Conn != nil && tlsRecordHeaderLooksLikeHTTP(re.RecordHeader) {
				io.WriteString(re.Conn, "HTTP/1.0 400 Bad Request\r\n\r\nClient sent an HTTP request to an HTTPS server.\n")
				re.Conn.Close()
				return
			}
			c.logf("http: TLS handshake error from %s: %v", c.rwc.RemoteAddr(), err)
			return
		}
		// Restore Conn-level deadlines.
		if tlsTO > 0 {
			c.rwc.SetReadDeadline(time.Time{})
			c.rwc.SetWriteDeadline(time.Time{})
		}
		tlsState := tlsConn.ConnectionState()
		if proto := tlsState.NegotiatedProtocol; validNextProto(proto) {
			h := (http.Handler)(initALPNRequest{ctx, tlsConn, serverHandler{c.hs}})

			type baseContexter interface {
				BaseContext() context.Context
			}

			if bc, ok := h.(baseContexter); ok {
				ctx = bc.BaseContext()
			}

			c.srv.ServeConn(c.rwc, &http2.ServeConnOpts{
				Context:    ctx,
				BaseConfig: c.hs,
				Handler:    h,
			})
			return
		}
	}

	c.rwc.Close()
}

func (h2 Server) logf(format string, args ...interface{}) {
	if h2.hs.ErrorLog != nil {
		h2.hs.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func (h2 Server) newConn(rwc net.Conn) conn {
	return conn{
		rwc:    rwc,
		Server: h2,
	}
}

func (h2 Server) Serve(l net.Listener) (err error) {
	srv := h2.hs

	baseCtx := context.Background()
	if srv.BaseContext != nil {
		baseCtx = srv.BaseContext(l)
		if baseCtx == nil {
			panic("BaseContext returned a nil context")
		}
	}

	var tempDelay time.Duration // how long to sleep on accept failure

	ctx := context.WithValue(baseCtx, http.ServerContextKey, srv)

	for {
		rw, err := l.Accept()
		if err != nil {
			select {
			case <-h2.doneChan:
				return http.ErrServerClosed
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				h2.logf("http: Accept error: %v; retrying in %v", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return err
		}

		connCtx := ctx
		if cc := srv.ConnContext; cc != nil {
			connCtx = cc(connCtx, rw)
			if connCtx == nil {
				panic("ConnContext returned nil")
			}
		}
		tempDelay = 0
		conn := h2.newConn(rw)
		go conn.serve(ctx)
	}
}

func NewServer(srv *http2.Server) *Server {
	return &Server{
		srv: srv,
	}
}

func (h2 *Server) ConfigureServer(hs *http.Server) {
	if h2.doneChan == nil {
		h2.doneChan = make(chan struct{})
	}
	if h2.srv == nil {
		h2.srv = &http2.Server{}
	}
	h2.hs = hs
	http2.ConfigureServer(hs, h2.srv)
	hs.RegisterOnShutdown(func() { close(h2.doneChan) })
}
