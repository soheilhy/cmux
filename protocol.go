package cmux

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	defaultBufSize = 1024
)

var (
	// prefix is the string we look for at the start of a connection
	// to check if this connection is using the proxy protocol
	prefix    = []byte("PROXY ")
	prefixLen = len(prefix)
)

func (m *MuxConn) checkPrefix() error {
	buf := make([]byte, defaultBufSize)
	n, err := m.Read(buf)

	reader := bufio.NewReader(bytes.NewReader(buf[:n]))

	// Incrementally check each byte of the prefix
	for i := 1; i <= prefixLen; i++ {
		inp, err := reader.Peek(i)
		if err != nil {
			return err
		}

		// Check for a prefix mismatch, quit early
		if !bytes.Equal(inp, prefix[:i]) {
			m.buf.buffer.Write(buf[:n])
			m.doneSniffing()
			return nil
		}
	}

	// Read the header line
	headerLine, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	// Strip the carriage return and new line
	header := headerLine[:len(headerLine)-2]

	// Split on spaces, should be (PROXY <type> <src addr> <dst addr> <src port> <dst port>)
	parts := strings.Split(header, " ")
	if len(parts) < 2 {
		return fmt.Errorf("invalid header line: %s", header)
	}

	// Verify the type is known
	switch parts[1] {
	case "UNKNOWN":
		return nil
	case "TCP4":
	case "TCP6":
	default:
		return fmt.Errorf("unhandled address type: %s", parts[1])
	}

	if len(parts) != 6 {
		return fmt.Errorf("invalid header line: %s", header)
	}

	// Parse out the source address
	ip := net.ParseIP(parts[2])
	if ip == nil {
		return fmt.Errorf("invalid source ip: %s", parts[2])
	}
	port, err := strconv.Atoi(parts[4])
	if err != nil {
		return fmt.Errorf("invalid source port: %s", parts[4])
	}
	m.srcAddr = &net.TCPAddr{IP: ip, Port: port}

	// Parse out the destination address
	ip = net.ParseIP(parts[3])
	if ip == nil {
		return fmt.Errorf("invalid destination ip: %s", parts[3])
	}
	port, err = strconv.Atoi(parts[5])
	if err != nil {
		return fmt.Errorf("invalid destination port: %s", parts[5])
	}
	m.dstAddr = &net.TCPAddr{IP: ip, Port: port}

	if n != len(headerLine) {
		m.buf.buffer.Write(buf[len(headerLine):n])
		m.doneSniffing()
	}

	return nil
}

func (m *MuxConn) RemoteAddr() net.Addr {
	if m.srcAddr != nil {
		return m.srcAddr
	}
	return m.Conn.RemoteAddr()
}

func (m *MuxConn) LocalAddr() net.Addr {
	if m.dstAddr != nil {
		return m.dstAddr
	}
	return m.Conn.LocalAddr()
}
