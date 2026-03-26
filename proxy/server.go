package proxy

import (
	"context"
	"errors"
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
// was selected and the dialer to use.
type resolvedRoute struct {
	upstream string
	dialer   upstream.Dialer
}

// isDNSError returns true if the error is caused by a failed DNS lookup.
func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// dialerFor returns the upstream dialer and name for the given hostname,
// or the direct dialer if no route matches.
func (s *Server) dialerFor(hostname string) resolvedRoute {
	s.mu.RLock()
	matcher := s.matcher
	dialers := s.dialers
	s.mu.RUnlock()

	name := matcher.Match(hostname)
	if name != "" {
		if d, ok := dialers[name]; ok {
			return resolvedRoute{upstream: name, dialer: d}
		}
	}
	return resolvedRoute{upstream: "direct", dialer: s.direct}
}
