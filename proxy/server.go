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

	"go.olrik.dev/subspace/pages"
	"go.olrik.dev/subspace/route"
	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/upstream"
)

// errUpstreamUnhealthy is returned by dialWithFallback when all upstreams
// (primary and fallback) are unreachable.
var errUpstreamUnhealthy = errors.New("upstream is not reachable")

// errBlackhole is returned by dialWithFallback when the resolved
// upstream (primary or fallback) is the built-in blackhole, signalling
// that the per-protocol handler should refuse the connection rather
// than dial. The handler chooses the wire format (HTTP 451, SOCKS5
// 0x02, raw close).
var errBlackhole = errors.New("route is blackholed")

// blackholeUpstream is the reserved name of the built-in pseudo-upstream
// that drops traffic. Mirrors "direct" — no upstream block is required
// and no dialer is registered for it.
const blackholeUpstream = "blackhole"

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
	// The protocol counter is incremented at most once per TCP connection so
	// HTTP keep-alive doesn't inflate counts relative to HTTPS/CONNECT, which
	// only get one classification event each.
	counted := false
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
			if !counted {
				_, port, _ := net.SplitHostPort(req.Host)
				if port == "443" || port == "" {
					s.Stats.IncProtocol("HTTPS")
				} else {
					s.Stats.IncProtocol("CONNECT:" + port)
				}
			}
			s.handleCONNECT(pc, req)
			return // CONNECT takes over the connection
		}

		// Requests to internal hosts (the daemon's own dashboard / pages) are
		// self-traffic, not external traffic the proxy is forwarding — exclude
		// them from the protocol breakdown so dashboard polling doesn't drown
		// out real client activity in the chart.
		if !counted && !isInternalHTTPRequest(s.Pages, req) {
			if isWebSocketUpgrade(req) {
				s.Stats.IncProtocol("WebSocket")
			} else {
				s.Stats.IncProtocol("HTTP")
			}
			counted = true
		}

		if !s.handleHTTP(pc, req) {
			return
		}
	}
}

// isInternalHTTPRequest reports whether req targets the daemon's own
// internal pages (dashboard, /api/*). These are served by the daemon
// itself and should be excluded from external-traffic statistics.
// Returns false when no internal-pages handler is wired up, since in
// that case the host would be forwarded to an upstream like any other.
func isInternalHTTPRequest(p InternalPages, req *http.Request) bool {
	if p == nil {
		return false
	}
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return pages.IsInternalHost(host)
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
// was selected and the dialer to use, plus an optional fallback. The
// pattern is the matched rule's pattern, or "direct" when nothing
// matched — used to attribute traffic to a route in the stats reports.
type resolvedRoute struct {
	pattern          string
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

// recordBlackhole is the bookkeeping side of a blackhole drop. bytesIn
// is the request bytes the proxy received from the client before the
// drop; bytesOut is the bytes of the synthetic refusal sent back (or 0
// for protocols like SOCKS5 and TLS pass-through that have no in-band
// response). The per-protocol handlers wrap this with their own wire
// format.
func (s *Server) recordBlackhole(host, pattern string, bytesIn, bytesOut int64) {
	s.Stats.IncUpstream(blackholeUpstream, true)
	s.Stats.IncDomain(host, true)
	s.Stats.IncRoute(pattern, true)
	s.Stats.AddUpstreamBytes(blackholeUpstream, bytesIn, bytesOut)
	s.Stats.AddDomainBytes(host, bytesIn, bytesOut)
	s.Stats.AddRouteBytes(pattern, bytesIn, bytesOut)
}

// routeFor returns the upstream dialer (and optional fallback) for the given
// hostname, or the direct dialer if no route matches. The blackhole
// pseudo-upstream is recorded as-is and has no dialer; dialWithFallback
// recognises the name and short-circuits with errBlackhole.
func (s *Server) routeFor(hostname string) resolvedRoute {
	s.mu.RLock()
	matcher := s.matcher
	dialers := s.dialers
	s.mu.RUnlock()

	rule := matcher.Resolve(hostname)
	if rule == nil {
		return resolvedRoute{pattern: "direct", upstream: "direct", dialer: s.direct}
	}

	r := resolvedRoute{pattern: rule.Pattern, upstream: rule.Upstream, dialer: s.direct}
	switch rule.Upstream {
	case "direct":
		// dialer already set to s.direct above.
	case blackholeUpstream:
		// No dialer — dialWithFallback short-circuits on the name.
		r.dialer = nil
	default:
		if d, ok := dialers[rule.Upstream]; ok {
			r.dialer = d
		}
	}

	if rule.Fallback != "" {
		r.fallbackUpstream = rule.Fallback
		switch rule.Fallback {
		case "direct":
			r.fallbackDialer = s.direct
		case blackholeUpstream:
			r.fallbackDialer = nil
		default:
			if d, ok := dialers[rule.Fallback]; ok {
				r.fallbackDialer = d
			}
		}
	}

	return r
}

// dialWithFallback attempts to connect to the target using the primary
// upstream, falling back if unhealthy or on dial failure. Returns the
// connection, the upstream name that was used, and any error. When the
// resolved upstream is the blackhole pseudo-upstream the function
// returns errBlackhole without dialing — the per-protocol handler then
// emits a refusal in the appropriate wire format.
func (s *Server) dialWithFallback(route resolvedRoute, network, addr string) (net.Conn, string, error) {
	// Primary blackhole short-circuits without consulting the monitor.
	if route.upstream == blackholeUpstream {
		return nil, blackholeUpstream, errBlackhole
	}

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
		if route.fallbackUpstream == blackholeUpstream {
			return nil, blackholeUpstream, errBlackhole
		}
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
	if route.fallbackUpstream == blackholeUpstream {
		return nil, blackholeUpstream, errBlackhole
	}
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
