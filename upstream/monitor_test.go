package upstream

import (
	"net"
	"sync"
	"testing"
	"time"
)

func startListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Accept connections in background so health checks can connect
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return ln
}

func TestMonitorHealthyUpstream(t *testing.T) {
	ln := startListener(t)
	defer ln.Close()

	m := NewMonitor(
		map[string]MonitorTarget{"up": {Type: "http", Address: ln.Addr().String()}},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	// Wait for first check cycle
	time.Sleep(100 * time.Millisecond)

	if !m.IsHealthy("up") {
		t.Error("expected upstream 'up' to be healthy")
	}
}

func TestMonitorUnhealthyUpstream(t *testing.T) {
	m := NewMonitor(
		map[string]MonitorTarget{"down": {Type: "http", Address: "127.0.0.1:1"}},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	// Wait for first check cycle
	time.Sleep(100 * time.Millisecond)

	if m.IsHealthy("down") {
		t.Error("expected upstream 'down' to be unhealthy")
	}
}

func TestMonitorTransitionToUnhealthy(t *testing.T) {
	ln := startListener(t)

	m := NewMonitor(
		map[string]MonitorTarget{"flaky": {Type: "http", Address: ln.Addr().String()}},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	time.Sleep(100 * time.Millisecond)
	if !m.IsHealthy("flaky") {
		t.Fatal("expected upstream to be healthy initially")
	}

	ln.Close()
	time.Sleep(150 * time.Millisecond)

	if m.IsHealthy("flaky") {
		t.Error("expected upstream to become unhealthy after listener closed")
	}
}

func TestMonitorTransitionToHealthy(t *testing.T) {
	// Pick an address but don't listen yet
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	m := NewMonitor(
		map[string]MonitorTarget{"recovering": {Type: "http", Address: addr}},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	time.Sleep(100 * time.Millisecond)
	if m.IsHealthy("recovering") {
		t.Fatal("expected upstream to be unhealthy initially")
	}

	// Start listening on the same address
	ln2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("could not re-bind to %s: %v", addr, err)
	}
	defer ln2.Close()
	go func() {
		for {
			conn, err := ln2.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	time.Sleep(150 * time.Millisecond)
	if !m.IsHealthy("recovering") {
		t.Error("expected upstream to become healthy after listener started")
	}
}

func TestMonitorStop(t *testing.T) {
	m := NewMonitor(
		map[string]MonitorTarget{"x": {Type: "http", Address: "127.0.0.1:1"}},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	m.Stop()
	// Should not panic or hang
}

func TestMonitorDirectAlwaysHealthy(t *testing.T) {
	m := NewMonitor(
		map[string]MonitorTarget{},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	if !m.IsHealthy("direct") {
		t.Error("expected 'direct' to always be healthy")
	}
}

func TestMonitorUnknownUpstreamUnhealthy(t *testing.T) {
	m := NewMonitor(
		map[string]MonitorTarget{},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	if m.IsHealthy("nonexistent") {
		t.Error("expected unknown upstream to be unhealthy")
	}
}

func TestMonitorConcurrentAccess(t *testing.T) {
	ln := startListener(t)
	defer ln.Close()

	m := NewMonitor(
		map[string]MonitorTarget{"concurrent": {Type: "http", Address: ln.Addr().String()}},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.IsHealthy("concurrent")
		}()
	}
	wg.Wait()
}

func TestMonitorStatus(t *testing.T) {
	ln := startListener(t)
	defer ln.Close()

	m := NewMonitor(
		map[string]MonitorTarget{
			"up":   {Type: "http", Address: ln.Addr().String()},
			"down": {Type: "socks5", Address: "127.0.0.1:1"},
		},
		50*time.Millisecond,
		50*time.Millisecond,
	)
	m.Start()
	defer m.Stop()

	time.Sleep(100 * time.Millisecond)

	status := m.Status()
	if len(status) != 2 {
		t.Fatalf("got %d entries, want 2", len(status))
	}

	up, ok := status["up"]
	if !ok {
		t.Fatal("missing 'up' in status")
	}
	if !up.Healthy {
		t.Error("expected 'up' to be healthy")
	}
	if up.Latency <= 0 {
		t.Error("expected positive latency for 'up'")
	}

	down, ok := status["down"]
	if !ok {
		t.Fatal("missing 'down' in status")
	}
	if down.Healthy {
		t.Error("expected 'down' to be unhealthy")
	}
}
