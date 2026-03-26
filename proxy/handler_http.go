package proxy

import (
	"bufio"
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
func (s *Server) handleHTTP(conn *PeekConn, req *http.Request) bool {
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

	route := s.dialerFor(hostname)

	// WebSocket upgrade takes over the connection
	if isWebSocketUpgrade(req) {
		s.handleWebSocket(conn, req, targetAddr, route)
		return false
	}

	keepAlive := !req.Close

	slog.Debug("HTTP", "host", host, "method", req.Method, "path", req.URL.Path, "via", route.upstream)

	// Try the connection pool first, fall back to dialing fresh
	var upstreamConn net.Conn
	if s.Pool != nil {
		upstreamConn = s.Pool.Get(route.upstream, targetAddr)
	}
	if upstreamConn == nil {
		var err error
		upstreamConn, err = route.dialer.DialContext(s.ctx, "tcp", targetAddr)
		if err != nil {
			if isDNSError(err) {
				slog.Error("DNS lookup failed", "host", hostname, "error", err)
				s.Stats.IncError("dns_failed")
				conn.Write(pages.ErrorPage(502, "Host Not Found", hostname))
			} else {
				slog.Error("HTTP dial failed", "target", targetAddr, "via", route.upstream, "error", err)
				s.Stats.IncUpstream(route.upstream, false)
				s.Stats.IncError("dial_failed")
				conn.Write(pages.ErrorPage(502, "Dial Failed", err.Error()))
			}
			return false
		}
	}

	s.Stats.IncUpstream(route.upstream, true)

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

	s.Stats.AddUpstreamBytes(route.upstream, crw.n, cw.n)

	// Return connection to pool if reusable, otherwise close
	if s.Pool != nil && !resp.Close && br.Buffered() == 0 {
		s.Pool.Put(route.upstream, targetAddr, upstreamConn)
	} else {
		upstreamConn.Close()
	}

	return keepAlive
}

// handleWebSocket handles a WebSocket upgrade by forwarding the upgrade request
// to the target and then relaying traffic bidirectionally.
func (s *Server) handleWebSocket(conn *PeekConn, req *http.Request, targetAddr string, route resolvedRoute) {
	slog.Debug("WebSocket", "host", req.Host, "target", targetAddr, "via", route.upstream)

	upstreamConn, err := route.dialer.DialContext(s.ctx, "tcp", targetAddr)
	if err != nil {
		if isDNSError(err) {
			slog.Error("DNS lookup failed", "host", req.Host, "error", err)
			s.Stats.IncError("dns_failed")
			conn.Write(pages.ErrorPage(502, "Host Not Found", req.Host))
		} else {
			slog.Error("WebSocket dial failed", "target", targetAddr, "via", route.upstream, "error", err)
			s.Stats.IncUpstream(route.upstream, false)
			s.Stats.IncError("dial_failed")
			conn.Write(pages.ErrorPage(502, "Dial Failed", err.Error()))
		}
		return
	}

	s.Stats.IncUpstream(route.upstream, true)

	// Forward the original upgrade request to the upstream
	if err := req.Write(upstreamConn); err != nil {
		slog.Error("WebSocket request write failed", "error", err)
		upstreamConn.Close()
		conn.Write(pages.ErrorPage(502, "Request Failed", err.Error()))
		return
	}

	rawConn, buffered := conn.Unwrap()
	result := Relay(rawConn, upstreamConn, buffered)
	s.Stats.AddUpstreamBytes(route.upstream, result.BytesIn, result.BytesOut)
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
