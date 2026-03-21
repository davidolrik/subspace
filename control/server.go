package control

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/upstream"
)

// PoolStatsSource provides pool metrics for the status endpoint.
type PoolStatsSource interface {
	Stats() upstream.PoolStats
}

// Server provides a control socket for streaming logs and other management tasks.
type Server struct {
	listener  net.Listener
	buf       *LogBuffer
	collector *stats.Collector
	pool      PoolStatsSource
	mux       *http.ServeMux
	sockPath  string

	mu        sync.RWMutex
	upstreams map[string]UpstreamInfo
}

// NewServer creates a control server listening on the given Unix socket path.
// The upstreams and pool parameters are optional (pass nil to omit).
func NewServer(sockPath string, buf *LogBuffer, collector *stats.Collector, upstreams map[string]UpstreamInfo, pool PoolStatsSource) (*Server, error) {
	// Remove stale socket file
	os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", sockPath, err)
	}

	s := &Server{
		listener:  ln,
		buf:       buf,
		collector: collector,
		pool:      pool,
		mux:       http.NewServeMux(),
		sockPath:  sockPath,
		upstreams: upstreams,
	}

	s.mux.HandleFunc("/logs", s.handleLogs)
	s.mux.HandleFunc("/stats", s.handleStats)
	s.mux.HandleFunc("/status", s.handleStatus)

	return s, nil
}

// SetUpstreams updates the upstream info used by the /status endpoint.
func (s *Server) SetUpstreams(upstreams map[string]UpstreamInfo) {
	s.mu.Lock()
	s.upstreams = upstreams
	s.mu.Unlock()
}

// Serve starts accepting connections. Blocks until the listener is closed.
func (s *Server) Serve() error {
	srv := &http.Server{Handler: s.mux}
	return srv.Serve(s.listener)
}

// Close shuts down the control server and removes the socket file.
func (s *Server) Close() error {
	err := s.listener.Close()
	os.Remove(s.sockPath)
	return err
}

// SocketPath returns the path to the Unix socket.
func (s *Server) SocketPath() string {
	return s.sockPath
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Parse the number of historical lines to return
	n := 10
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		parsed, err := strconv.Atoi(nStr)
		if err == nil && parsed >= 0 {
			n = parsed
		}
	}

	// Parse minimum log level
	minLevel := slog.LevelInfo
	if lvl := r.URL.Query().Get("level"); lvl != "" {
		minLevel = parseLevel(lvl)
	}

	// Set headers for streaming (Go's net/http handles chunked encoding automatically)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Parse follow mode (default: true)
	follow := r.URL.Query().Get("follow") != "false"

	// Parse color mode (default: false)
	color := r.URL.Query().Get("color") == "true"

	// Send buffered entries filtered by level
	entries := s.buf.Last(n, minLevel)
	for _, e := range entries {
		fmt.Fprintln(w, FormatEntry(e, color))
	}
	flusher.Flush()

	if !follow {
		return
	}

	// Subscribe for live streaming
	ch := s.buf.Subscribe()
	defer s.buf.Unsubscribe(ch)

	// Stream live entries filtered by level
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-ch:
			if entry.Level >= minLevel {
				fmt.Fprintln(w, FormatEntry(entry, color))
				flusher.Flush()
			}
		}
	}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug", "dbg":
		return slog.LevelDebug
	case "info", "inf":
		return slog.LevelInfo
	case "warn", "wrn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		http.Error(w, "stats not available", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	snap := s.collector.Snapshot()
	json.NewEncoder(w).Encode(snap)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	upstreams := s.upstreams
	s.mu.RUnlock()

	// Run health checks concurrently
	type healthResult struct {
		name    string
		healthy bool
		latency time.Duration
	}

	results := make(chan healthResult, len(upstreams))
	for name, info := range upstreams {
		go func(name string, info UpstreamInfo) {
			start := time.Now()
			conn, err := net.DialTimeout("tcp", info.Address, 3*time.Second)
			latency := time.Since(start)
			if err != nil {
				results <- healthResult{name: name, healthy: false, latency: latency}
				return
			}
			conn.Close()
			results <- healthResult{name: name, healthy: true, latency: latency}
		}(name, info)
	}

	healthMap := make(map[string]healthResult, len(upstreams))
	for range upstreams {
		hr := <-results
		healthMap[hr.name] = hr
	}

	// Build response
	var snap stats.Snapshot
	if s.collector != nil {
		snap = s.collector.Snapshot()
	}

	resp := StatusResponse{
		Upstreams: make(map[string]UpstreamStatus, len(upstreams)),
		Connections: ConnectionStatus{
			Total:  snap.Connections,
			Active: snap.Active,
		},
	}

	for name, info := range upstreams {
		hr := healthMap[name]
		us := UpstreamStatus{
			Type:    info.Type,
			Address: info.Address,
			Healthy: hr.healthy,
			Latency: hr.latency.Round(time.Millisecond).String(),
		}
		if ustats, ok := snap.Upstreams[name]; ok {
			us.Stats = &ustats
		}
		resp.Upstreams[name] = us
	}

	// Include the built-in "direct" upstream with its stats (no health check)
	directStatus := UpstreamStatus{
		Type:    "direct",
		Healthy: true,
	}
	if ustats, ok := snap.Upstreams["direct"]; ok {
		directStatus.Stats = &ustats
	}
	resp.Upstreams["direct"] = directStatus

	if s.pool != nil {
		ps := s.pool.Stats()
		resp.Pool = &ps
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
