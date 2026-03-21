package stats

import (
	"sync"
	"sync/atomic"
)

// UpstreamStats holds per-upstream counters.
type UpstreamStats struct {
	Success  int64 `json:"success"`
	Failures int64 `json:"failures"`
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`
}

// Snapshot is a point-in-time copy of all statistics.
type Snapshot struct {
	Connections int64                    `json:"connections"`
	Active      int64                    `json:"active"`
	Protocols   map[string]int64         `json:"protocols"`
	Errors      map[string]int64         `json:"errors"`
	Upstreams   map[string]UpstreamStats `json:"upstreams"`
}

// upstreamCounters holds the atomic counters for one upstream.
type upstreamCounters struct {
	success  atomic.Int64
	failures atomic.Int64
	bytesIn  atomic.Int64
	bytesOut atomic.Int64
}

// Collector accumulates proxy statistics using atomic operations.
type Collector struct {
	connections atomic.Int64
	active      atomic.Int64

	mu        sync.RWMutex
	protocols map[string]*atomic.Int64
	errors    map[string]*atomic.Int64
	upstreams map[string]*upstreamCounters
}

// New creates a Collector with pre-populated counters for known protocol
// and error types, avoiding mutex acquisition on the hot path.
func New() *Collector {
	protocols := map[string]*atomic.Int64{
		"TLS":       {},
		"HTTP":      {},
		"CONNECT":   {},
		"WebSocket": {},
	}
	errors := map[string]*atomic.Int64{
		"peek_failed":  {},
		"parse_failed": {},
		"sni_failed":   {},
		"dial_failed":  {},
		"bad_request":  {},
	}
	return &Collector{
		protocols: protocols,
		errors:    errors,
		upstreams: make(map[string]*upstreamCounters),
	}
}

// IncProtocol increments the connection count and the per-protocol counter.
func (c *Collector) IncProtocol(protocol string) {
	c.connections.Add(1)
	c.getOrCreateCounter(c.protocols, &c.mu, protocol).Add(1)
}

// IncError increments an error counter by name.
func (c *Collector) IncError(name string) {
	c.getOrCreateCounter(c.errors, &c.mu, name).Add(1)
}

// IncActive increments the active connection gauge.
func (c *Collector) IncActive() {
	c.active.Add(1)
}

// DecActive decrements the active connection gauge.
func (c *Collector) DecActive() {
	c.active.Add(-1)
}

// IncUpstream increments the success or failure counter for an upstream.
func (c *Collector) IncUpstream(name string, success bool) {
	u := c.getOrCreateUpstream(name)
	if success {
		u.success.Add(1)
	} else {
		u.failures.Add(1)
	}
}

// AddUpstreamBytes adds byte transfer counts for an upstream.
func (c *Collector) AddUpstreamBytes(name string, bytesIn, bytesOut int64) {
	u := c.getOrCreateUpstream(name)
	u.bytesIn.Add(bytesIn)
	u.bytesOut.Add(bytesOut)
}

// Snapshot returns a point-in-time copy of all statistics.
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snap := Snapshot{
		Connections: c.connections.Load(),
		Active:      c.active.Load(),
		Protocols:   make(map[string]int64, len(c.protocols)),
		Errors:      make(map[string]int64, len(c.errors)),
		Upstreams:   make(map[string]UpstreamStats, len(c.upstreams)),
	}

	for k, v := range c.protocols {
		snap.Protocols[k] = v.Load()
	}
	for k, v := range c.errors {
		snap.Errors[k] = v.Load()
	}
	for k, v := range c.upstreams {
		snap.Upstreams[k] = UpstreamStats{
			Success:  v.success.Load(),
			Failures: v.failures.Load(),
			BytesIn:  v.bytesIn.Load(),
			BytesOut: v.bytesOut.Load(),
		}
	}

	return snap
}

func (c *Collector) getOrCreateCounter(m map[string]*atomic.Int64, mu *sync.RWMutex, key string) *atomic.Int64 {
	mu.RLock()
	if v, ok := m[key]; ok {
		mu.RUnlock()
		return v
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if v, ok := m[key]; ok {
		return v
	}
	v := &atomic.Int64{}
	m[key] = v
	return v
}

func (c *Collector) getOrCreateUpstream(name string) *upstreamCounters {
	c.mu.RLock()
	if u, ok := c.upstreams[name]; ok {
		c.mu.RUnlock()
		return u
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if u, ok := c.upstreams[name]; ok {
		return u
	}
	u := &upstreamCounters{}
	c.upstreams[name] = u
	return u
}
