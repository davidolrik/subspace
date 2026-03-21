package upstream

import (
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// PoolConfig holds tuning parameters for the connection pool.
type PoolConfig struct {
	MaxIdlePerHost int
	IdleTimeout    time.Duration
	EvictInterval  time.Duration // how often to sweep for stale entries
}

func (c PoolConfig) withDefaults() PoolConfig {
	if c.MaxIdlePerHost <= 0 {
		c.MaxIdlePerHost = 4
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 90 * time.Second
	}
	if c.EvictInterval <= 0 {
		c.EvictInterval = 10 * time.Second
	}
	return c
}

// PoolStats is a point-in-time snapshot of pool metrics.
type PoolStats struct {
	Hits      int64          `json:"hits"`
	Misses    int64          `json:"misses"`
	IdleConns map[string]int `json:"idle_conns"`
}

type poolKey struct {
	upstream string
	addr     string
}

type poolEntry struct {
	conn      net.Conn
	idleSince time.Time
}

// Pool manages idle upstream connections keyed by (upstream-name, target-address).
type Pool struct {
	mu     sync.Mutex
	conns  map[poolKey][]poolEntry
	cfg    PoolConfig
	closed bool
	done   chan struct{}

	hits   atomic.Int64
	misses atomic.Int64
}

// NewPool creates a connection pool with the given configuration.
// A background goroutine evicts idle connections.
func NewPool(cfg PoolConfig) *Pool {
	cfg = cfg.withDefaults()
	p := &Pool{
		conns: make(map[poolKey][]poolEntry),
		cfg:   cfg,
		done:  make(chan struct{}),
	}
	go p.evictLoop()
	return p
}

// Get returns an idle connection for the given upstream and address,
// or nil if none is available. Stale connections are discarded.
func (p *Pool) Get(upstream, addr string) net.Conn {
	key := poolKey{upstream, addr}

	p.mu.Lock()
	entries := p.conns[key]

	for len(entries) > 0 {
		// Pop from the end (most recently added = warmest)
		e := entries[len(entries)-1]
		entries = entries[:len(entries)-1]
		p.conns[key] = entries

		p.mu.Unlock()

		// Liveness check: read with a very short deadline.
		// A closed connection returns EOF; a live one returns a timeout.
		e.conn.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
		var buf [1]byte
		_, err := e.conn.Read(buf[:])
		e.conn.SetReadDeadline(time.Time{})

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout means the connection is still alive — no data available
				p.hits.Add(1)
				return e.conn
			}
			// Connection is dead, discard and try next
			e.conn.Close()
			p.mu.Lock()
			entries = p.conns[key]
			continue
		}

		// Got actual data on a supposedly idle connection — unexpected.
		// Discard it.
		e.conn.Close()
		p.mu.Lock()
		entries = p.conns[key]
	}

	p.mu.Unlock()
	p.misses.Add(1)
	return nil
}

// Put returns a connection to the pool. If the pool is full or closed,
// the connection is closed instead.
func (p *Pool) Put(upstream, addr string, conn net.Conn) {
	key := poolKey{upstream, addr}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		conn.Close()
		return
	}

	entries := p.conns[key]
	if len(entries) >= p.cfg.MaxIdlePerHost {
		// Pool full — close the oldest connection
		oldest := entries[0]
		entries = entries[1:]
		oldest.conn.Close()
	}

	p.conns[key] = append(entries, poolEntry{
		conn:      conn,
		idleSince: time.Now(),
	})
	p.mu.Unlock()
}

// DrainAll closes all idle connections without shutting down the pool.
// The pool remains usable for new connections after draining.
func (p *Pool) DrainAll() {
	p.mu.Lock()
	conns := p.conns
	p.conns = make(map[poolKey][]poolEntry)
	p.mu.Unlock()

	for _, entries := range conns {
		for _, e := range entries {
			e.conn.Close()
		}
	}
}

// Close shuts down the pool, closes all idle connections, and stops
// the eviction goroutine. Put calls after Close will immediately close
// the connection.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	conns := p.conns
	p.conns = make(map[poolKey][]poolEntry)
	p.mu.Unlock()

	close(p.done)

	for _, entries := range conns {
		for _, e := range entries {
			e.conn.Close()
		}
	}
}

// Stats returns a point-in-time snapshot of pool metrics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	idle := make(map[string]int)
	for key, entries := range p.conns {
		idle[key.upstream] += len(entries)
	}
	p.mu.Unlock()

	return PoolStats{
		Hits:      p.hits.Load(),
		Misses:    p.misses.Load(),
		IdleConns: idle,
	}
}

func (p *Pool) evictLoop() {
	ticker := time.NewTicker(p.cfg.EvictInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.evictStale()
		}
	}
}

func (p *Pool) evictStale() {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	for key, entries := range p.conns {
		live := entries[:0]
		for _, e := range entries {
			if now.Sub(e.idleSince) > p.cfg.IdleTimeout {
				e.conn.Close()
			} else {
				live = append(live, e)
			}
		}
		if len(live) == 0 {
			delete(p.conns, key)
		} else {
			p.conns[key] = live
		}
	}
}
