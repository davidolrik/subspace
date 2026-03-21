package upstream

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolGetEmpty(t *testing.T) {
	p := NewPool(PoolConfig{})
	defer p.Close()

	if conn := p.Get("upstream", "host:80"); conn != nil {
		t.Error("expected nil from empty pool")
	}
}

func TestPoolPutAndGet(t *testing.T) {
	p := NewPool(PoolConfig{})
	defer p.Close()

	ln, conn := tcpPair(t)
	defer ln.Close()

	p.Put("upstream", "host:80", conn)

	got := p.Get("upstream", "host:80")
	if got == nil {
		t.Fatal("expected connection from pool")
	}
	got.Close()

	// Second Get returns nil (connection was consumed)
	if c := p.Get("upstream", "host:80"); c != nil {
		t.Error("expected nil after connection was consumed")
		c.Close()
	}
}

func TestPoolKeyIsolation(t *testing.T) {
	p := NewPool(PoolConfig{})
	defer p.Close()

	ln1, conn1 := tcpPair(t)
	defer ln1.Close()
	ln2, conn2 := tcpPair(t)
	defer ln2.Close()

	p.Put("upstream-a", "host:80", conn1)
	p.Put("upstream-b", "host:80", conn2)

	// Get from upstream-a should not return upstream-b's connection
	got := p.Get("upstream-a", "host:80")
	if got == nil {
		t.Fatal("expected connection for upstream-a")
	}
	got.Close()

	if c := p.Get("upstream-a", "host:80"); c != nil {
		t.Error("expected nil for upstream-a after drain")
		c.Close()
	}

	// upstream-b should still have its connection
	got = p.Get("upstream-b", "host:80")
	if got == nil {
		t.Fatal("expected connection for upstream-b")
	}
	got.Close()
}

func TestPoolMaxIdlePerHost(t *testing.T) {
	p := NewPool(PoolConfig{MaxIdlePerHost: 2})
	defer p.Close()

	var lns []net.Listener
	var conns []net.Conn
	for i := 0; i < 3; i++ {
		ln, conn := tcpPair(t)
		lns = append(lns, ln)
		conns = append(conns, conn)
	}
	defer func() {
		for _, ln := range lns {
			ln.Close()
		}
	}()

	// Put 3 connections with max 2
	p.Put("up", "host:80", conns[0])
	p.Put("up", "host:80", conns[1])
	p.Put("up", "host:80", conns[2]) // should close the oldest

	// Should only get 2 back
	count := 0
	for {
		c := p.Get("up", "host:80")
		if c == nil {
			break
		}
		c.Close()
		count++
	}
	if count != 2 {
		t.Errorf("got %d connections, want 2", count)
	}
}

func TestPoolIdleEviction(t *testing.T) {
	p := NewPool(PoolConfig{IdleTimeout: 50 * time.Millisecond, EvictInterval: 25 * time.Millisecond})
	defer p.Close()

	ln, conn := tcpPair(t)
	defer ln.Close()

	p.Put("up", "host:80", conn)

	// Wait for eviction (idle timeout + evict interval + margin)
	time.Sleep(150 * time.Millisecond)

	if c := p.Get("up", "host:80"); c != nil {
		t.Error("expected nil after idle eviction")
		c.Close()
	}
}

func TestPoolDrainAll(t *testing.T) {
	p := NewPool(PoolConfig{})
	defer p.Close()

	ln1, conn1 := tcpPair(t)
	defer ln1.Close()
	ln2, conn2 := tcpPair(t)
	defer ln2.Close()

	p.Put("a", "host:80", conn1)
	p.Put("b", "host:80", conn2)

	p.DrainAll()

	if c := p.Get("a", "host:80"); c != nil {
		t.Error("expected nil after DrainAll")
		c.Close()
	}
	if c := p.Get("b", "host:80"); c != nil {
		t.Error("expected nil after DrainAll")
		c.Close()
	}

	// Pool should still be usable after DrainAll
	ln3, conn3 := tcpPair(t)
	defer ln3.Close()

	p.Put("a", "host:80", conn3)
	if c := p.Get("a", "host:80"); c == nil {
		t.Error("expected pool to still work after DrainAll")
	} else {
		c.Close()
	}
}

func TestPoolClose(t *testing.T) {
	p := NewPool(PoolConfig{})

	ln, conn := tcpPair(t)
	defer ln.Close()

	p.Put("up", "host:80", conn)
	p.Close()

	// Put after Close should immediately close the connection
	ln2, conn2 := tcpPair(t)
	defer ln2.Close()

	p.Put("up", "host:80", conn2)

	// Verify conn2 was closed by trying to write to it
	_, err := conn2.Write([]byte("test"))
	if err == nil {
		t.Error("expected write to fail on connection Put after Close")
	}
}

func TestPoolStats(t *testing.T) {
	p := NewPool(PoolConfig{})
	defer p.Close()

	ln1, conn1 := tcpPair(t)
	defer ln1.Close()
	ln2, conn2 := tcpPair(t)
	defer ln2.Close()

	// Miss
	p.Get("up", "host:80")

	// Put then Hit
	p.Put("up", "host:80", conn1)
	c := p.Get("up", "host:80")
	if c != nil {
		c.Close()
	}

	// Another miss
	p.Get("up", "host:80")

	// Put for idle count
	p.Put("up", "host:80", conn2)

	s := p.Stats()
	if s.Hits != 1 {
		t.Errorf("Hits = %d, want 1", s.Hits)
	}
	if s.Misses != 2 {
		t.Errorf("Misses = %d, want 2", s.Misses)
	}
	if s.IdleConns["up"] != 1 {
		t.Errorf("IdleConns[up] = %d, want 1", s.IdleConns["up"])
	}
}

func TestPoolStaleConnection(t *testing.T) {
	p := NewPool(PoolConfig{})
	defer p.Close()

	// Create a TCP pair manually so we can control the server side
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}

	serverConn, err := ln.Accept()
	if err != nil {
		conn.Close()
		ln.Close()
		t.Fatal(err)
	}
	ln.Close()

	// Close the server side to make the client connection stale
	serverConn.Close()

	// Give the OS a moment to propagate the close
	time.Sleep(10 * time.Millisecond)

	p.Put("up", "host:80", conn)

	// Get should detect the stale connection and return nil
	if c := p.Get("up", "host:80"); c != nil {
		t.Error("expected nil for stale connection")
		c.Close()
	}
}

// tcpPair creates a TCP listener and dials a connection to it.
// The caller should close the listener and the returned connection.
// The server-side accepted connection is accepted and kept alive
// by the listener (closing the listener closes it).
func tcpPair(t *testing.T) (net.Listener, net.Conn) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// Accept in background to keep the connection alive
	var accepted atomic.Value
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted.Store(c)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}

	return ln, conn
}
