package proxy

import (
	"bufio"
	"net"
)

// PeekConn wraps a net.Conn with a bufio.Reader so that peeked bytes
// are replayed on subsequent reads.
type PeekConn struct {
	net.Conn
	reader *bufio.Reader
}

// NewPeekConn wraps conn with buffered reading.
func NewPeekConn(conn net.Conn) *PeekConn {
	return &PeekConn{
		Conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

// Peek returns the next n bytes without advancing the reader.
func (c *PeekConn) Peek(n int) ([]byte, error) {
	return c.reader.Peek(n)
}

// Read reads from the buffered reader, replaying any peeked bytes first.
func (c *PeekConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// Unwrap returns the underlying net.Conn and any bytes buffered by the
// reader that have not yet been consumed. After calling Unwrap, the
// PeekConn should not be used for reading — only for Write/Close via
// the embedded net.Conn.
//
// This allows the relay to use the raw conn directly, enabling kernel-
// level zero-copy optimizations (splice/sendfile) that are impossible
// when reads go through a bufio.Reader wrapper.
func (c *PeekConn) Unwrap() (conn net.Conn, buffered []byte) {
	n := c.reader.Buffered()
	if n > 0 {
		// Peek + Read to drain buffered bytes without risk of short read
		buf, _ := c.reader.Peek(n)
		buffered = make([]byte, n)
		copy(buffered, buf)
		c.reader.Discard(n)
	}
	return c.Conn, buffered
}
