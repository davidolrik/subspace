package proxy

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"go.olrik.dev/subspace/pages"
)

// handleHTTP handles a plaintext HTTP request, either forwarding it to the
// target or upgrading to a WebSocket tunnel. Returns true if the connection
// should be kept alive for another request.
func (s *Server) handleHTTP(conn *PeekConn, req *http.Request, l boundListener) bool {
	host := req.Host
	if host == "" {
		slog.Error("HTTP request missing Host header")
		s.Stats.IncError("bad_request")
		conn.Write(pages.ErrorPage(400, "Bad Request", "Missing Host header"))
		return false
	}

	// Determine target address
	targetAddr := host
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		targetAddr = targetAddr + ":80"
	}

	hostname, _, _ := net.SplitHostPort(targetAddr)

	// Serve internal pages for pages.subspace.pub and stats.subspace.pub
	if s.Pages != nil && pages.IsInternalHost(hostname) {
		s.Pages.ServeHTTP(conn, req)
		return false
	}

	route := s.routeFor(hostname, l.cfg.Private)

	// WebSocket upgrade takes over the connection
	if isWebSocketUpgrade(req) {
		s.handleWebSocket(conn, req, targetAddr, route)
		return false
	}

	keepAlive := !req.Close

	slog.Debug("HTTP", "host", host, "method", req.Method, "path", req.URL.Path, "via", route.upstream)

	// Try the connection pool first, fall back to dialing fresh
	var upstreamConn net.Conn
	usedUpstream := route.upstream
	if s.Pool != nil {
		upstreamConn = s.Pool.Get(route.upstream, targetAddr)
	}
	if upstreamConn == nil {
		var err error
		upstreamConn, usedUpstream, err = s.dialWithFallback(route, "tcp", targetAddr)
		if err != nil {
			if errors.Is(err, errBlackhole) {
				s.blackholeHTTP(conn, req, hostname, route.pattern, route.private)
				return false
			}
			if errors.Is(err, errIgnored) {
				slog.Debug("ignore dropped", "protocol", "HTTP", "host", hostname, "pattern", route.pattern)
				s.recordIgnore()
				return false
			}
			if isDNSError(err) {
				slog.Error("DNS lookup failed", "host", hostname, "error", err)
				s.Stats.IncError("dns_failed")
				s.recordHostFailure(hostname, route.pattern)
				conn.Write(pages.ErrorPage(502, "Host Not Found", hostname))
			} else if errors.Is(err, errUpstreamUnhealthy) {
				slog.Error("upstream unavailable", "host", hostname, "via", usedUpstream)
				s.Stats.IncError("dial_failed")
				s.recordFailure(hostname, route.pattern, usedUpstream)
				conn.Write(pages.ErrorPage(502, "Upstream Unavailable", "Upstream '"+usedUpstream+"' is not reachable"))
			} else {
				slog.Error("HTTP dial failed", "target", targetAddr, "via", usedUpstream, "error", err)
				s.recordFailure(hostname, route.pattern, usedUpstream)
				s.Stats.IncError("dial_failed")
				conn.Write(pages.ErrorPage(502, "Dial Failed", err.Error()))
			}
			return false
		}
	}

	s.recordSuccess(hostname, route.pattern, usedUpstream, route.private)

	// Forward the request to the upstream
	cw := &countingWriter{w: upstreamConn}
	if err := req.Write(cw); err != nil {
		slog.Error("HTTP request write failed", "error", err)
		upstreamConn.Close()
		conn.Write(pages.ErrorPage(502, "Request Failed", err.Error()))
		return false
	}

	// Parse the response so we know where it ends (required for keep-alive)
	br := bufio.NewReader(upstreamConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		slog.Error("HTTP response read failed", "error", err)
		upstreamConn.Close()
		conn.Write(pages.ErrorPage(502, "Response Failed", err.Error()))
		return false
	}
	defer resp.Body.Close()

	// Signal to the client whether we'll keep the connection open
	if !keepAlive {
		resp.Header.Set("Connection", "close")
	} else {
		resp.Header.Del("Connection")
	}

	// Write the response back to the client
	crw := &countingWriter{w: conn}
	if err := resp.Write(crw); err != nil {
		slog.Error("HTTP response write failed", "error", err)
		upstreamConn.Close()
		return false
	}

	s.recordBytes(hostname, route.pattern, usedUpstream, crw.n, cw.n, route.private)

	// Return connection to pool if reusable, otherwise close
	if s.Pool != nil && !resp.Close && br.Buffered() == 0 {
		s.Pool.Put(usedUpstream, targetAddr, upstreamConn)
	} else {
		upstreamConn.Close()
	}

	return keepAlive
}

// handleWebSocket handles a WebSocket upgrade by forwarding the upgrade request
// to the target and then relaying traffic bidirectionally.
func (s *Server) handleWebSocket(conn *PeekConn, req *http.Request, targetAddr string, route resolvedRoute) {
	slog.Debug("WebSocket", "host", req.Host, "target", targetAddr, "via", route.upstream)

	hostname, _, _ := net.SplitHostPort(targetAddr)

	upstreamConn, usedUpstream, err := s.dialWithFallback(route, "tcp", targetAddr)
	if err != nil {
		if errors.Is(err, errBlackhole) {
			s.blackholeHTTP(conn, req, hostname, route.pattern, route.private)
			return
		}
		if errors.Is(err, errIgnored) {
			slog.Debug("ignore dropped", "protocol", "WebSocket", "host", hostname, "pattern", route.pattern)
			s.recordIgnore()
			return
		}
		if isDNSError(err) {
			slog.Error("DNS lookup failed", "host", req.Host, "error", err)
			s.Stats.IncError("dns_failed")
			s.recordHostFailure(hostname, route.pattern)
			conn.Write(pages.ErrorPage(502, "Host Not Found", req.Host))
		} else if errors.Is(err, errUpstreamUnhealthy) {
			slog.Error("upstream unavailable", "host", req.Host, "via", usedUpstream)
			s.Stats.IncError("dial_failed")
			s.recordFailure(hostname, route.pattern, usedUpstream)
			conn.Write(pages.ErrorPage(502, "Upstream Unavailable", "Upstream '"+usedUpstream+"' is not reachable"))
		} else {
			slog.Error("WebSocket dial failed", "target", targetAddr, "via", usedUpstream, "error", err)
			s.recordFailure(hostname, route.pattern, usedUpstream)
			s.Stats.IncError("dial_failed")
			conn.Write(pages.ErrorPage(502, "Dial Failed", err.Error()))
		}
		return
	}

	s.recordSuccess(hostname, route.pattern, usedUpstream, route.private)

	// Forward the original upgrade request to the upstream
	if err := req.Write(upstreamConn); err != nil {
		slog.Error("WebSocket request write failed", "error", err)
		upstreamConn.Close()
		conn.Write(pages.ErrorPage(502, "Request Failed", err.Error()))
		return
	}

	rawConn, buffered := conn.Unwrap()
	result := Relay(rawConn, upstreamConn, buffered)
	s.recordBytes(hostname, route.pattern, usedUpstream, result.BytesIn, result.BytesOut, route.private)
}

// countingWriter wraps an io.Writer and counts the bytes written.
type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

func isWebSocketUpgrade(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Connection"), "upgrade") &&
		strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

// blackholeHTTP refuses an HTTP (or WebSocket-upgrade) request whose
// route resolves to the blackhole pseudo-upstream. The request size is
// estimated by serialising it to a counting discard so the dashboard
// can show how much traffic was prevented from leaving the machine.
// Private connections skip the per-domain/per-route attributions.
func (s *Server) blackholeHTTP(conn io.Writer, req *http.Request, hostname, pattern string, private bool) {
	cw := &countingWriter{w: io.Discard}
	_ = req.Write(cw)
	body := pages.ErrorPage(451, "Unavailable For Legal Reasons", hostname)
	n, _ := conn.Write(body)
	slog.Debug("blackhole refused", "protocol", "HTTP", "host", hostname, "pattern", pattern, "req_bytes", cw.n, "resp_bytes", n)
	s.recordBlackhole(hostname, pattern, cw.n, int64(n), private)
}
