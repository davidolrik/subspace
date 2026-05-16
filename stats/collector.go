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
	// Domains tracks per-destination-host activity. Keys are the
	// hostname extracted from the request (Host header / SNI / SOCKS5
	// destination). Empty hostnames are not recorded.
	Domains map[string]UpstreamStats `json:"domains,omitempty"`
	// Routes tracks per-route-pattern activity. Keys are the matched
	// route pattern from the config (e.g. "*.corp.example") or the
	// literal "direct" when no rule matched. Empty patterns are not
	// recorded.
	Routes map[string]UpstreamStats `json:"routes,omitempty"`
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
	domains   map[string]*upstreamCounters
	routes    map[string]*upstreamCounters
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
		domains:   make(map[string]*upstreamCounters),
		routes:    make(map[string]*upstreamCounters),
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

// IncDomain records a connection attempt to the given destination
// host. Empty hostnames are ignored so handler paths that reach the
// instrumentation site without a usable hostname (peek failures, SNI
// missing, etc.) don't pollute the report with anonymous entries.
func (c *Collector) IncDomain(host string, success bool) {
	if host == "" {
		return
	}
	d := c.getOrCreateNamed(&c.domains, host)
	if success {
		d.success.Add(1)
	} else {
		d.failures.Add(1)
	}
}

// AddDomainBytes adds byte transfer counts for a destination host.
func (c *Collector) AddDomainBytes(host string, bytesIn, bytesOut int64) {
	if host == "" {
		return
	}
	d := c.getOrCreateNamed(&c.domains, host)
	d.bytesIn.Add(bytesIn)
	d.bytesOut.Add(bytesOut)
}

// IncRoute records a connection routed by the given pattern. Empty
// patterns are ignored. Use the literal "direct" for traffic that did
// not match any rule.
func (c *Collector) IncRoute(pattern string, success bool) {
	if pattern == "" {
		return
	}
	r := c.getOrCreateNamed(&c.routes, pattern)
	if success {
		r.success.Add(1)
	} else {
		r.failures.Add(1)
	}
}

// AddRouteBytes adds byte transfer counts for a route pattern.
func (c *Collector) AddRouteBytes(pattern string, bytesIn, bytesOut int64) {
	if pattern == "" {
		return
	}
	r := c.getOrCreateNamed(&c.routes, pattern)
	r.bytesIn.Add(bytesIn)
	r.bytesOut.Add(bytesOut)
}

// ForgetDomain removes the live in-memory counter for the named host.
// Used by the stats purge command so the dashboard's running totals
// stop including a domain that's just been wiped from the historical
// store. Returns true if a counter was actually removed.
func (c *Collector) ForgetDomain(host string) bool {
	if host == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.domains[host]; !ok {
		return false
	}
	delete(c.domains, host)
	return true
}

func (c *Collector) getOrCreateNamed(m *map[string]*upstreamCounters, name string) *upstreamCounters {
	c.mu.RLock()
	if u, ok := (*m)[name]; ok {
		c.mu.RUnlock()
		return u
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if u, ok := (*m)[name]; ok {
		return u
	}
	u := &upstreamCounters{}
	(*m)[name] = u
	return u
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
		Domains:     make(map[string]UpstreamStats, len(c.domains)),
		Routes:      make(map[string]UpstreamStats, len(c.routes)),
	}

	for k, v := range c.protocols {
		snap.Protocols[k] = v.Load()
	}
	for k, v := range c.errors {
		snap.Errors[k] = v.Load()
	}
	copyUpstreamCounters(snap.Upstreams, c.upstreams)
	copyUpstreamCounters(snap.Domains, c.domains)
	copyUpstreamCounters(snap.Routes, c.routes)

	return snap
}

func copyUpstreamCounters(dst map[string]UpstreamStats, src map[string]*upstreamCounters) {
	for k, v := range src {
		dst[k] = UpstreamStats{
			Success:  v.success.Load(),
			Failures: v.failures.Load(),
			BytesIn:  v.bytesIn.Load(),
			BytesOut: v.bytesOut.Load(),
		}
	}
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
