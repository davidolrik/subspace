package proxy

import (
	"io"
	"net"
	"sync"
)

// closeWriter is an interface for connections that support half-close.
type closeWriter interface {
	CloseWrite() error
}

// RelayResult holds the byte counts from a bidirectional relay.
type RelayResult struct {
	BytesIn  int64 // bytes from b to a (client ← upstream)
	BytesOut int64 // bytes from a to b (client → upstream)
}

// Relay copies data bidirectionally between a (client) and b (upstream)
// until one side closes or encounters an error. Both connections are
// closed when done. Returns byte counts.
//
// If residual is non-empty, those bytes are written to b before
// starting the bidirectional copy. This supports unwrapping a PeekConn:
// any bytes buffered by the peek reader are forwarded first, then both
// directions use raw net.Conn for kernel-level zero-copy (splice/sendfile).
func Relay(a, b net.Conn, residual []byte) RelayResult {
	var wg sync.WaitGroup
	var bytesOut, bytesIn int64
	wg.Add(2)

	// client → upstream
	go func() {
		defer wg.Done()
		var n int64
		if len(residual) > 0 {
			written, err := b.Write(residual)
			n += int64(written)
			if err != nil {
				if cw, ok := b.(closeWriter); ok {
					cw.CloseWrite()
				}
				return
			}
		}
		copied, _ := io.Copy(b, a)
		n += copied
		bytesOut = n
		if cw, ok := b.(closeWriter); ok {
			cw.CloseWrite()
		}
	}()

	// upstream → client
	go func() {
		defer wg.Done()
		n, _ := io.Copy(a, b)
		bytesIn = n
		if cw, ok := a.(closeWriter); ok {
			cw.CloseWrite()
		}
	}()

	wg.Wait()
	a.Close()
	b.Close()

	return RelayResult{BytesIn: bytesIn, BytesOut: bytesOut}
}
