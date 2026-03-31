package upstream

import (
	"net"
	"sync"
	"time"
)

// MonitorTarget describes an upstream to health-check.
type MonitorTarget struct {
	Type    string
	Address string
}

// MonitorStatus holds the cached health state and target info of an upstream.
type MonitorStatus struct {
	Type    string
	Address string
	Healthy bool
	Latency time.Duration
}

type monitorEntry struct {
	healthy bool
	latency time.Duration
}

// Monitor runs periodic TCP health checks against upstream proxies and
// caches the results. It is safe for concurrent use.
type Monitor struct {
	targets  map[string]MonitorTarget
	interval time.Duration
	timeout  time.Duration

	mu      sync.RWMutex
	entries map[string]monitorEntry

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// NewMonitor creates a health monitor for the given upstream targets.
// Call Start to begin background checks.
func NewMonitor(targets map[string]MonitorTarget, interval, timeout time.Duration) *Monitor {
	// Initialize all entries as healthy (optimistic)
	entries := make(map[string]monitorEntry, len(targets))
	for name := range targets {
		entries[name] = monitorEntry{healthy: true}
	}

	return &Monitor{
		targets:  targets,
		interval: interval,
		timeout:  timeout,
		entries:  entries,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start begins the background health check loop. The first check runs
// immediately.
func (m *Monitor) Start() {
	go m.loop()
}

// Stop signals the background loop to exit and waits for it to finish.
// It is safe to call Stop multiple times.
func (m *Monitor) Stop() {
	m.stopOnce.Do(func() { close(m.stop) })
	<-m.done
}

// IsHealthy returns whether the named upstream is reachable. "direct" is
// always considered healthy. Unknown upstreams are considered unhealthy.
func (m *Monitor) IsHealthy(name string) bool {
	if name == "direct" {
		return true
	}
	m.mu.RLock()
	e, ok := m.entries[name]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	return e.healthy
}

// Status returns the cached health state of all monitored upstreams,
// including their target type and address.
func (m *Monitor) Status() map[string]MonitorStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]MonitorStatus, len(m.entries))
	for name, e := range m.entries {
		target := m.targets[name]
		result[name] = MonitorStatus{
			Type:    target.Type,
			Address: target.Address,
			Healthy: e.healthy,
			Latency: e.latency,
		}
	}
	return result
}

func (m *Monitor) loop() {
	defer close(m.done)

	// Run the first check immediately
	m.checkAll()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.checkAll()
		}
	}
}

func (m *Monitor) checkAll() {
	type result struct {
		name    string
		healthy bool
		latency time.Duration
	}

	results := make(chan result, len(m.targets))
	for name, target := range m.targets {
		go func(name string, target MonitorTarget) {
			start := time.Now()
			conn, err := net.DialTimeout("tcp", target.Address, m.timeout)
			latency := time.Since(start)
			if err != nil {
				results <- result{name: name, healthy: false, latency: latency}
				return
			}
			conn.Close()
			results <- result{name: name, healthy: true, latency: latency}
		}(name, target)
	}

	newEntries := make(map[string]monitorEntry, len(m.targets))
	for range m.targets {
		r := <-results
		newEntries[r.name] = monitorEntry{healthy: r.healthy, latency: r.latency}
	}

	m.mu.Lock()
	m.entries = newEntries
	m.mu.Unlock()
}
