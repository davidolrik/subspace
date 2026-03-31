package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"go.olrik.dev/subspace/route"
	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/upstream"
)

// errUpstreamUnhealthy is returned by dialWithFallback when all upstreams
// (primary and fallback) are unreachable.
var errUpstreamUnhealthy = errors.New("upstream is not reachable")

// InternalPages serves requests for pages.subspace.pub and stats.subspace.pub.
type InternalPages interface {
	ServeHTTP(conn net.Conn, req *http.Request)
}

// Server is the main proxy server that accepts connections and dispatches
// them to the appropriate handler based on protocol detection.
type Server struct {
	listener    net.Listener
	mu          sync.RWMutex
	matcher     *route.Matcher
	dialers     map[string]upstream.Dialer
	direct      upstream.Dialer
	monitor     *upstream.Monitor
	ctx         context.Context
	cancel      context.CancelFunc
	listenPort  string
	Stats       *stats.Collector
	Pool        *upstream.Pool
	IdleTimeout time.Duration
	Pages       InternalPages
}

// NewServer creates a new proxy server. The pool parameter is optional —
// pass nil to disable upstream connection pooling.
func NewServer(listener net.Listener, matcher *route.Matcher, dialers map[string]upstream.Dialer, pool *upstream.Pool) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	_, port, _ := net.SplitHostPort(listener.Addr().String())

	return &Server{
		listener:    listener,
		matcher:     matcher,
		dialers:     dialers,
		direct:      upstream.NewDirectDialer(),
		ctx:         ctx,
		cancel:      cancel,
		listenPort:  port,
		Stats:       stats.New(),
		Pool:        pool,
		IdleTimeout: 60 * time.Second,
	}
}

// Serve accepts connections and handles them. Blocks until the listener is closed
// or the context is cancelled.
func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return nil
			default:
				return err
			}
		}
		go s.handleConn(conn)
	}
}

// Close shuts down the server.
func (s *Server) Close() error {
	s.cancel()
	if s.Pool != nil {
		s.Pool.Close()
	}
	return s.listener.Close()
}

func (s *Server) handleConn(conn net.Conn) {
	s.Stats.IncActive()
	defer s.Stats.DecActive()

	pc := NewPeekConn(conn)
	defer pc.Close()

	// Peek at the first byte to classify the connection
	first, err := pc.Peek(1)
	if err != nil {
		slog.Debug("peek failed", "error", err)
		s.Stats.IncError("peek_failed")
		return
	}

	if first[0] == 0x16 {
		s.Stats.IncProtocol("HTTPS")
		s.handleTLS(pc, s.listenPort)
		return
	}

	if first[0] == 0x05 {
		s.Stats.IncProtocol("SOCKS5")
		s.handleSOCKS5(pc)
		return
	}

	// HTTP keep-alive loop: read requests until the client signals close,
	// the connection is idle too long, or a handler takes over (CONNECT/WebSocket).
	for {
		pc.Conn.SetReadDeadline(time.Now().Add(s.IdleTimeout))

		req, err := http.ReadRequest(pc.reader)
		if err != nil {
			if !isTimeoutOrEOF(err) {
				slog.Error("HTTP request parse failed", "error", err)
				s.Stats.IncError("parse_failed")
			}
			return
		}

		pc.Conn.SetReadDeadline(time.Time{}) // clear deadline for handler

		if req.Method == http.MethodConnect {
			_, port, _ := net.SplitHostPort(req.Host)
			if port == "443" || port == "" {
				s.Stats.IncProtocol("HTTPS")
			} else {
				s.Stats.IncProtocol("CONNECT:" + port)
			}
			s.handleCONNECT(pc, req)
			return // CONNECT takes over the connection
		}

		if isWebSocketUpgrade(req) {
			s.Stats.IncProtocol("WebSocket")
		} else {
			s.Stats.IncProtocol("HTTP")
		}

		if !s.handleHTTP(pc, req) {
			return
		}
	}
}

// isTimeoutOrEOF returns true for errors that indicate a graceful
// connection close or idle timeout — not worth logging as an error.
func isTimeoutOrEOF(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// SetMonitor sets the health monitor used for proactive upstream health
// checking. May be called before or after Serve.
func (s *Server) SetMonitor(m *upstream.Monitor) {
	s.mu.Lock()
	s.monitor = m
	s.mu.Unlock()
}

// Reload atomically swaps the route matcher and upstream dialers.
// Drains the connection pool since upstream configs may have changed.
func (s *Server) Reload(matcher *route.Matcher, dialers map[string]upstream.Dialer) {
	s.mu.Lock()
	s.matcher = matcher
	s.dialers = dialers
	s.mu.Unlock()

	if s.Pool != nil {
		s.Pool.DrainAll()
	}
}

// resolvedRoute holds the result of routing a hostname: which upstream
// was selected and the dialer to use, plus an optional fallback.
type resolvedRoute struct {
	upstream         string
	dialer           upstream.Dialer
	fallbackUpstream string
	fallbackDialer   upstream.Dialer
}

// isDNSError returns true if the error is caused by a failed DNS lookup.
func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// routeFor returns the upstream dialer (and optional fallback) for the given
// hostname, or the direct dialer if no route matches.
func (s *Server) routeFor(hostname string) resolvedRoute {
	s.mu.RLock()
	matcher := s.matcher
	dialers := s.dialers
	s.mu.RUnlock()

	rule := matcher.Resolve(hostname)
	if rule == nil {
		return resolvedRoute{upstream: "direct", dialer: s.direct}
	}

	r := resolvedRoute{upstream: rule.Upstream, dialer: s.direct}
	if rule.Upstream != "direct" {
		if d, ok := dialers[rule.Upstream]; ok {
			r.dialer = d
		}
	}

	if rule.Fallback != "" {
		r.fallbackUpstream = rule.Fallback
		if rule.Fallback == "direct" {
			r.fallbackDialer = s.direct
		} else if d, ok := dialers[rule.Fallback]; ok {
			r.fallbackDialer = d
		}
	}

	return r
}

// dialWithFallback attempts to connect to the target using the primary
// upstream, falling back if unhealthy or on dial failure. Returns the
// connection, the upstream name that was used, and any error.
func (s *Server) dialWithFallback(route resolvedRoute, network, addr string) (net.Conn, string, error) {
	s.mu.RLock()
	monitor := s.monitor
	s.mu.RUnlock()

	primaryHealthy := monitor == nil || monitor.IsHealthy(route.upstream)

	if primaryHealthy {
		conn, err := route.dialer.DialContext(s.ctx, network, addr)
		if err == nil {
			return conn, route.upstream, nil
		}
		// Primary dial failed — try fallback if available
		if route.fallbackDialer != nil {
			fallbackHealthy := monitor == nil || monitor.IsHealthy(route.fallbackUpstream)
			if fallbackHealthy {
				conn, err := route.fallbackDialer.DialContext(s.ctx, network, addr)
				if err == nil {
					return conn, route.fallbackUpstream, nil
				}
				return nil, route.fallbackUpstream, err
			}
		}
		return nil, route.upstream, err
	}

	// Primary is unhealthy — try fallback
	if route.fallbackDialer != nil {
		fallbackHealthy := monitor == nil || monitor.IsHealthy(route.fallbackUpstream)
		if fallbackHealthy {
			conn, err := route.fallbackDialer.DialContext(s.ctx, network, addr)
			if err == nil {
				return conn, route.fallbackUpstream, nil
			}
			return nil, route.fallbackUpstream, err
		}
	}

	return nil, route.upstream, fmt.Errorf("%w: %s", errUpstreamUnhealthy, route.upstream)
}
