package cmux

import (
	"net"
	"testing"
)

func TestMuxConn_CheckPrefix(t *testing.T) {
	// Create a listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		// Accept a connection from the listener
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("failed to accept connection: %v", err)
			return
		}

		// Write a PROXY header to the connection
		_, err = conn.Write([]byte("PROXY TCP4 192.168.1.1 192.168.1.2 1234 5678\r\n"))
		if err != nil {
			t.Errorf("failed to write PROXY header: %v", err)
			return
		}

		// Close the connection
		conn.Close()
	}()

	// Dial the listener with a MuxConn
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial listener: %v", err)
	}

	muxConn := newMuxConn(conn)

	// Call checkPrefix to parse the PROXY header
	err = muxConn.checkPrefix()
	if err != nil {
		t.Errorf("checkPrefix returned error: %v", err)
	}

	// Verify the source and destination addresses were parsed correctly
	expectedSrc := "192.168.1.1:1234"
	expectedDst := "192.168.1.2:5678"
	if muxConn.RemoteAddr().String() != expectedSrc {
		t.Errorf("RemoteAddr() returned %s, expected %s", muxConn.RemoteAddr().String(), expectedSrc)
	}
	if muxConn.LocalAddr().String() != expectedDst {
		t.Errorf("LocalAddr() returned %s, expected %s", muxConn.LocalAddr().String(), expectedDst)
	}
}
