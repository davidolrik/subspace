package proxy

import (
	"errors"
	"log/slog"
	"net"
	"net/http"

	"go.olrik.dev/subspace/pages"
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

	// TLS connections to pages.subspace.pub / stats.subspace.pub pass
	// through to the external redirect server for HTTPS → HTTP redirection.

	route := s.routeFor(host)

	slog.Debug("CONNECT", "target", targetAddr, "via", route.upstream)

	upstreamConn, usedUpstream, err := s.dialWithFallback(route, "tcp", targetAddr)
	if err != nil {
		if isDNSError(err) {
			slog.Error("DNS lookup failed", "host", host, "error", err)
			s.Stats.IncError("dns_failed")
			conn.Write(pages.ErrorPage(502, "Host Not Found", host))
		} else if errors.Is(err, errUpstreamUnhealthy) {
			slog.Error("upstream unavailable", "host", host, "via", usedUpstream)
			s.Stats.IncError("dial_failed")
			conn.Write(pages.ErrorPage(502, "Upstream Unavailable", "Upstream '"+usedUpstream+"' is not reachable"))
		} else {
			slog.Error("CONNECT dial failed", "target", targetAddr, "via", usedUpstream, "error", err)
			s.Stats.IncUpstream(usedUpstream, false)
			s.Stats.IncError("dial_failed")
			conn.Write(pages.ErrorPage(502, "Dial Failed", err.Error()))
		}
		conn.Close()
		return
	}

	s.Stats.IncUpstream(usedUpstream, true)
	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Unwrap to raw conn for zero-copy relay. After CONNECT + 200 response,
	// the bufio.Reader should have no buffered bytes.
	rawConn, buffered := conn.Unwrap()
	result := Relay(rawConn, upstreamConn, buffered)
	s.Stats.AddUpstreamBytes(usedUpstream, result.BytesIn, result.BytesOut)
}
