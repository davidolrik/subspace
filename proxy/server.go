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

// ListenerConfig binds a net.Listener to per-listener settings. The
// Private flag makes every connection accepted on that listener
// "private" — domain-identifying stats writes are suppressed while
// protocol, upstream and byte rollups still record. Label is a
// cosmetic name used in logs to disambiguate listeners.
type ListenerConfig struct {
	Net     net.Listener
	Private bool
	Label   string
}

// Server is the main proxy server that accepts connections and dispatches
// them to the appropriate handler based on protocol detection. Multiple
// listeners are supported; each may have its own private/label settings.
type Server struct {
	listeners   []boundListener
	mu          sync.RWMutex
	matcher     *route.Matcher
	dialers     map[string]upstream.Dialer
	direct      upstream.Dialer
	monitor     *upstream.Monitor
	ctx         context.Context
	cancel      context.CancelFunc
	Stats       *stats.Collector
	Pool        *upstream.Pool
	IdleTimeout time.Duration
	Pages       InternalPages
}

// boundListener pairs a ListenerConfig with the pre-extracted port so
// handleTLS can build the upstream target address without re-parsing
// Addr() on the hot path.
type boundListener struct {
	cfg  ListenerConfig
	port string
}

// NewServer creates a new proxy server. The pool parameter is optional —
// pass nil to disable upstream connection pooling. At least one
// listener is required; passing zero is a programming error.
func NewServer(listeners []ListenerConfig, matcher *route.Matcher, dialers map[string]upstream.Dialer, pool *upstream.Pool) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	bound := make([]boundListener, len(listeners))
	for i, l := range listeners {
		_, port, _ := net.SplitHostPort(l.Net.Addr().String())
		bound[i] = boundListener{cfg: l, port: port}
	}

	return &Server{
		listeners:   bound,
		matcher:     matcher,
		dialers:     dialers,
		direct:      upstream.NewDirectDialer(),
		ctx:         ctx,
		cancel:      cancel,
		Stats:       stats.New(),
		Pool:        pool,
		IdleTimeout: 60 * time.Second,
	}
}

// Serve accepts connections on every configured listener and handles
// them. Blocks until all listeners are closed or the context is
// cancelled. Returns the first non-shutdown Accept error encountered,
// or nil when Close was called.
func (s *Server) Serve() error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(s.listeners))

	for _, l := range s.listeners {
		wg.Add(1)
		go func(l boundListener) {
			defer wg.Done()
			for {
				conn, err := l.cfg.Net.Accept()
				if err != nil {
					select {
					case <-s.ctx.Done():
						return
					default:
						errCh <- err
						return
					}
				}
				go s.handleConn(conn, l)
			}
		}(l)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// Close shuts down the server.
func (s *Server) Close() error {
	s.cancel()
	if s.Pool != nil {
		s.Pool.Close()
	}
	var firstErr error
	for _, l := range s.listeners {
		if err := l.cfg.Net.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Server) handleConn(conn net.Conn, l boundListener) {
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
		s.handleTLS(pc, l)
		return
	}

	if first[0] == 0x05 {
		s.Stats.IncProtocol("SOCKS5")
		s.handleSOCKS5(pc, l)
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
			s.handleCONNECT(pc, req, l)
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

		if !s.handleHTTP(pc, req, l) {
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
//
// Private is the effective privacy flag for the connection: true when
// either the listener that accepted the connection is marked private,
// or the matched rule has private=true. The stats helpers consult this
// field to decide whether to skip per-domain and per-route writes.
type resolvedRoute struct {
	pattern          string
	upstream         string
	dialer           upstream.Dialer
	fallbackUpstream string
	fallbackDialer   upstream.Dialer
	private          bool
}

// isDNSError returns true if the error is caused by a failed DNS lookup.
func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// recordSuccess records a successful connection. When private is true,
// the per-domain and per-route counters are skipped — the upstream
// rollup still records so totals reconcile with reality.
func (s *Server) recordSuccess(host, pattern, usedUpstream string, private bool) {
	s.Stats.IncUpstream(usedUpstream, true)
	if private {
		return
	}
	s.Stats.IncDomain(host, true)
	s.Stats.IncRoute(pattern, true)
}

// recordFailure mirrors recordSuccess on the failure side.
func (s *Server) recordFailure(host, pattern, usedUpstream string, private bool) {
	s.Stats.IncUpstream(usedUpstream, false)
	if private {
		return
	}
	s.Stats.IncDomain(host, false)
	s.Stats.IncRoute(pattern, false)
}

// recordBytes adds transferred bytes to the upstream rollup, plus the
// per-domain and per-route totals when the connection is not private.
func (s *Server) recordBytes(host, pattern, usedUpstream string, bytesIn, bytesOut int64, private bool) {
	s.Stats.AddUpstreamBytes(usedUpstream, bytesIn, bytesOut)
	if private {
		return
	}
	s.Stats.AddDomainBytes(host, bytesIn, bytesOut)
	s.Stats.AddRouteBytes(pattern, bytesIn, bytesOut)
}

// recordBlackhole is the bookkeeping side of a blackhole drop. bytesIn
// is the request bytes the proxy received from the client before the
// drop; bytesOut is the bytes of the synthetic refusal sent back (or 0
// for protocols like SOCKS5 and TLS pass-through that have no in-band
// response). The per-protocol handlers wrap this with their own wire
// format. Private connections skip the domain and route attributions,
// keeping only the blackhole upstream rollup.
func (s *Server) recordBlackhole(host, pattern string, bytesIn, bytesOut int64, private bool) {
	s.Stats.IncUpstream(blackholeUpstream, true)
	s.Stats.AddUpstreamBytes(blackholeUpstream, bytesIn, bytesOut)
	if private {
		return
	}
	s.Stats.IncDomain(host, true)
	s.Stats.IncRoute(pattern, true)
	s.Stats.AddDomainBytes(host, bytesIn, bytesOut)
	s.Stats.AddRouteBytes(pattern, bytesIn, bytesOut)
}

// routeFor returns the upstream dialer (and optional fallback) for the given
// hostname, or the direct dialer if no route matches. The blackhole
// pseudo-upstream is recorded as-is and has no dialer; dialWithFallback
// recognises the name and short-circuits with errBlackhole.
//
// The listenerPrivate flag is OR-folded into the resolved route's
// private bit so handlers downstream only need to consult one value.
func (s *Server) routeFor(hostname string, listenerPrivate bool) resolvedRoute {
	s.mu.RLock()
	matcher := s.matcher
	dialers := s.dialers
	s.mu.RUnlock()

	rule := matcher.Resolve(hostname)
	if rule == nil {
		return resolvedRoute{pattern: "direct", upstream: "direct", dialer: s.direct, private: listenerPrivate}
	}

	r := resolvedRoute{
		pattern:  rule.Pattern,
		upstream: rule.Upstream,
		dialer:   s.direct,
		private:  listenerPrivate || rule.Private,
	}
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
