package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/upstream"
)

func testEntry(level slog.Level, msg string) LogEntry {
	return LogEntry{Level: level, Time: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), Message: msg}
}

func TestControlServerStreamLogs(t *testing.T) {
	buf := NewLogBuffer(100)
	buf.Append(testEntry(slog.LevelInfo, "old line 1"))
	buf.Append(testEntry(slog.LevelInfo, "old line 2"))
	buf.Append(testEntry(slog.LevelInfo, "old line 3"))

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET /logs?n=2 HTTP/1.1\r\nHost: localhost\r\n\r\n")

	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}

	lines := readChunkedLines(t, scanner, 2)
	if len(lines) != 2 {
		t.Fatalf("got %d buffered lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "old line 2") || !strings.Contains(lines[1], "old line 3") {
		t.Errorf("buffered lines = %v", lines)
	}

	// Brief pause to ensure the server's subscription is active
	time.Sleep(100 * time.Millisecond)
	buf.Append(testEntry(slog.LevelInfo, "live line"))
	liveLines := readChunkedLines(t, scanner, 1)
	if len(liveLines) != 1 {
		t.Fatalf("got %d live lines, want 1", len(liveLines))
	}
	if !strings.Contains(liveLines[0], "live line") {
		t.Errorf("live line = %q, want it to contain %q", liveLines[0], "live line")
	}
}

func TestControlServerDefaultLines(t *testing.T) {
	buf := NewLogBuffer(100)
	for i := 0; i < 20; i++ {
		buf.Append(testEntry(slog.LevelInfo, fmt.Sprintf("line %d", i)))
	}

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, "GET /logs HTTP/1.1\r\nHost: localhost\r\n\r\n")

	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}

	lines := readChunkedLines(t, scanner, 10)
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10", len(lines))
	}
	if !strings.Contains(lines[0], "line 10") || !strings.Contains(lines[9], "line 19") {
		t.Errorf("lines[0]=%q lines[9]=%q", lines[0], lines[9])
	}
}

func TestControlServerLevelFilter(t *testing.T) {
	buf := NewLogBuffer(100)
	buf.Append(testEntry(slog.LevelDebug, "debug msg"))
	buf.Append(testEntry(slog.LevelInfo, "info msg"))
	buf.Append(testEntry(slog.LevelWarn, "warn msg"))
	buf.Append(testEntry(slog.LevelError, "error msg"))

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Request only errors
	fmt.Fprintf(conn, "GET /logs?n=10&level=error HTTP/1.1\r\nHost: localhost\r\n\r\n")

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}

	lines := readChunkedLines(t, scanner, 1)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "error msg") {
		t.Errorf("line = %q, want it to contain %q", lines[0], "error msg")
	}
}

func TestControlServerColorParam(t *testing.T) {
	buf := NewLogBuffer(100)
	buf.Append(testEntry(slog.LevelInfo, "colored test"))

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)

	// Without color
	resp, err := client.Get("http://subspace/logs?n=1&follow=false")
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Scan()
	plainLine := scanner.Text()
	resp.Body.Close()

	if strings.Contains(plainLine, "\033[") {
		t.Errorf("plain mode should not contain ANSI escapes: %q", plainLine)
	}
	if !strings.Contains(plainLine, "colored test") {
		t.Errorf("expected message in output: %q", plainLine)
	}

	// With color
	resp, err = client.Get("http://subspace/logs?n=1&follow=false&color=true")
	if err != nil {
		t.Fatal(err)
	}
	scanner = bufio.NewScanner(resp.Body)
	scanner.Scan()
	colorLine := scanner.Text()
	resp.Body.Close()

	if !strings.Contains(colorLine, "\033[") {
		t.Errorf("color mode should contain ANSI escapes: %q", colorLine)
	}
	if !strings.Contains(colorLine, "colored test") {
		t.Errorf("expected message in output: %q", colorLine)
	}
}

func TestControlServerCloseStopsServeAndConns(t *testing.T) {
	// Close is synchronous and force-closes active connections: after it
	// returns, Serve has returned (no accept loop left) and an in-flight
	// streaming handler's connection has been closed rather than left
	// dangling — which closing only the listener wouldn't achieve.
	buf := NewLogBuffer(16)
	buf.Append(testEntry(slog.LevelInfo, "a line"))

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve() }()

	// Open a streaming log connection so there's an active handler the
	// accept-loop shutdown alone wouldn't reach.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /logs?n=1 HTTP/1.1\r\nHost: localhost\r\n\r\n")

	// Read past the response headers so the handler is actively streaming.
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// Close blocked until Serve returned, so it must already be done.
	select {
	case <-served:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Close()")
	}

	// The active connection must have been closed by Close(): a read
	// should reach EOF rather than block until the deadline.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 256)
	for {
		if _, rerr := conn.Read(b); rerr != nil {
			if ne, ok := rerr.(net.Error); ok && ne.Timeout() {
				t.Error("active connection still open after Close (read timed out)")
			}
			break
		}
	}

	// The socket file is removed.
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("socket file not removed after Close: stat err=%v", err)
	}
}

func TestControlServerCloseIdempotent(t *testing.T) {
	// Close must be safe to call more than once and return consistently.
	buf := NewLogBuffer(16)
	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	go srv.Serve()
	time.Sleep(15 * time.Millisecond)

	if err := srv.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := srv.Close(); err != nil { // must not panic; same result
		t.Errorf("second Close: %v", err)
	}
}

func tempSocket(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("/tmp", "subspace-test-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	os.Remove(path)
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func readChunkedLines(t *testing.T, scanner *bufio.Scanner, n int) []string {
	t.Helper()
	var lines []string
	deadline := time.After(3 * time.Second)

	for len(lines) < n {
		select {
		case <-deadline:
			t.Fatalf("timed out after reading %d/%d lines", len(lines), n)
			return lines
		default:
		}

		if !scanner.Scan() {
			t.Fatalf("scanner ended after %d/%d lines: %v", len(lines), n, scanner.Err())
			return lines
		}
		line := scanner.Text()
		if isChunkSize(line) || line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func isChunkSize(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func TestControlServerNotFound(t *testing.T) {
	buf := NewLogBuffer(100)

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, buf, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := http.Client{
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}

	resp, err := client.Get("http://localhost/notfound")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// --- status endpoint tests ---

func startTestMonitor(t *testing.T, targets map[string]upstream.MonitorTarget) *upstream.Monitor {
	t.Helper()
	m := upstream.NewMonitor(targets, 50*time.Millisecond, 50*time.Millisecond)
	m.Start()
	t.Cleanup(m.Stop)
	// Wait for first check cycle
	time.Sleep(100 * time.Millisecond)
	return m
}

func TestStatusEndpointHealthy(t *testing.T) {
	// Start a TCP listener to act as a healthy upstream
	healthyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer healthyLn.Close()
	go func() {
		for {
			c, err := healthyLn.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	collector := stats.New()
	monitor := startTestMonitor(t, map[string]upstream.MonitorTarget{
		"test-proxy": {Type: "http", Address: healthyLn.Addr().String()},
	})

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), collector, nil, monitor, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Get("http://subspace/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	us, ok := status.Upstreams["test-proxy"]
	if !ok {
		t.Fatal("missing upstream test-proxy in response")
	}
	if !us.Healthy {
		t.Error("expected test-proxy to be healthy")
	}
	if us.Type != "http" {
		t.Errorf("type = %q, want %q", us.Type, "http")
	}
}

func TestStatusEndpointUnhealthy(t *testing.T) {
	collector := stats.New()
	monitor := startTestMonitor(t, map[string]upstream.MonitorTarget{
		"dead-proxy": {Type: "socks5", Address: "127.0.0.1:1"},
	})

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), collector, nil, monitor, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Get("http://subspace/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	us, ok := status.Upstreams["dead-proxy"]
	if !ok {
		t.Fatal("missing upstream dead-proxy in response")
	}
	if us.Healthy {
		t.Error("expected dead-proxy to be unhealthy")
	}
}

func TestStatusEndpointBlackhole(t *testing.T) {
	// blackhole is a built-in pseudo-upstream with no monitor target;
	// it should appear in /status alongside "direct" with type
	// "blackhole", healthy=true, and any collected stats attached.
	collector := stats.New()
	collector.IncUpstream("blackhole", true)
	collector.IncUpstream("blackhole", true)
	collector.AddUpstreamBytes("blackhole", 4321, 256)

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), collector, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Get("http://subspace/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	us, ok := status.Upstreams["blackhole"]
	if !ok {
		t.Fatal("missing upstream blackhole in /status response")
	}
	if us.Type != "blackhole" {
		t.Errorf("type = %q, want %q", us.Type, "blackhole")
	}
	if !us.Healthy {
		t.Error("blackhole should always be reported healthy (it never dials)")
	}
	if us.Stats == nil {
		t.Fatal("expected stats for blackhole upstream")
	}
	if us.Stats.Success != 2 {
		t.Errorf("blackhole stats success = %d, want 2", us.Stats.Success)
	}
	if us.Stats.BytesIn != 4321 {
		t.Errorf("blackhole stats bytes_in = %d, want 4321", us.Stats.BytesIn)
	}
}

func TestStatusEndpointJSON(t *testing.T) {
	collector := stats.New()
	collector.IncProtocol("HTTP")
	collector.IncUpstream("my-upstream", true)

	monitor := startTestMonitor(t, map[string]upstream.MonitorTarget{
		"my-upstream": {Type: "http", Address: "127.0.0.1:1"},
	})

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), collector, nil, monitor, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Get("http://subspace/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if status.Connections.Total != 1 {
		t.Errorf("connections.total = %d, want 1", status.Connections.Total)
	}

	us := status.Upstreams["my-upstream"]
	if us.Stats == nil {
		t.Fatal("expected stats for my-upstream")
	}
	if us.Stats.Success != 1 {
		t.Errorf("upstream stats success = %d, want 1", us.Stats.Success)
	}
}

func TestControlServerStatsPurge(t *testing.T) {
	dbPath := tempSocket(t) + ".db" // tempSocket gives us a unique path
	os.Remove(dbPath)
	store, err := stats.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(); os.Remove(dbPath) })

	// Record a single snapshot that names two domains.
	err = store.Record(time.Now(), stats.Snapshot{
		Domains: map[string]stats.UpstreamStats{
			"private.example.com": {Success: 1},
			"other.example.com":   {Success: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	collector := stats.New()
	collector.IncDomain("private.example.com", true)
	collector.IncDomain("other.example.com", true)

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), collector, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Post("http://subspace/stats/purge?domain=private.example.com", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got PurgeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Domain != "private.example.com" {
		t.Errorf("domain = %q, want %q", got.Domain, "private.example.com")
	}
	if got.Removed != 1 {
		t.Errorf("removed = %d, want 1", got.Removed)
	}

	// The in-memory counter should be gone too.
	snap := collector.Snapshot()
	if _, ok := snap.Domains["private.example.com"]; ok {
		t.Error("private.example.com still in collector snapshot after purge")
	}
	if _, ok := snap.Domains["other.example.com"]; !ok {
		t.Error("other.example.com should still be present in collector snapshot")
	}
}

func TestControlServerStatsPurgeRequiresDomain(t *testing.T) {
	dbPath := tempSocket(t) + ".db"
	os.Remove(dbPath)
	store, err := stats.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(); os.Remove(dbPath) })

	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), nil, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Post("http://subspace/stats/purge", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing domain", resp.StatusCode)
	}
}

func TestControlServerStatsPurgeRejectsGet(t *testing.T) {
	sockPath := tempSocket(t)
	srv, err := NewServer(sockPath, NewLogBuffer(10), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	client := unixClient(sockPath)
	resp, err := client.Get("http://subspace/stats/purge?domain=x")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for GET", resp.StatusCode)
	}
}

func unixClient(sockPath string) http.Client {
	return http.Client{
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
	}
}
