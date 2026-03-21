package proxy

import (
	"log/slog"
	"net"
	"net/http"
)

// handleCONNECT handles an HTTP CONNECT request by establishing a tunnel
// to the target host through the appropriate upstream dialer.
func (s *Server) handleCONNECT(conn *PeekConn, req *http.Request) {
	targetAddr := req.Host

	// Ensure the target has a port
	_, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		targetAddr = targetAddr + ":443"
	}

	host, _, _ := net.SplitHostPort(targetAddr)
	route := s.dialerFor(host)

	slog.Info("CONNECT", "target", targetAddr, "via", route.upstream)

	upstream, err := route.dialer.DialContext(s.ctx, "tcp", targetAddr)
	if err != nil {
		slog.Error("CONNECT dial failed", "target", targetAddr, "via", route.upstream, "error", err)
		s.Stats.IncUpstream(route.upstream, false)
		s.Stats.IncError("dial_failed")
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		conn.Close()
		return
	}

	s.Stats.IncUpstream(route.upstream, true)
	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Unwrap to raw conn for zero-copy relay. After CONNECT + 200 response,
	// the bufio.Reader should have no buffered bytes.
	rawConn, buffered := conn.Unwrap()
	result := Relay(rawConn, upstream, buffered)
	s.Stats.AddUpstreamBytes(route.upstream, result.BytesIn, result.BytesOut)
}
