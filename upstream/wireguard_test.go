package upstream

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestWireGuardDialerPeerToPeer(t *testing.T) {
	// Create two WireGuard endpoints that can talk to each other in-process.
	// Endpoint A acts as a "server" with an HTTP listener on its tunnel address.
	// Endpoint B dials through its tunnel to reach A's HTTP server.

	cfgA, cfgB := generateWireGuardPair(t)

	dialerA, err := NewWireGuardDialer(cfgA)
	if err != nil {
		t.Fatalf("NewWireGuardDialer(A): %v", err)
	}
	defer dialerA.Close()

	dialerB, err := NewWireGuardDialer(cfgB)
	if err != nil {
		t.Fatalf("NewWireGuardDialer(B): %v", err)
	}
	defer dialerB.Close()

	// Start an HTTP server on A's tunnel stack
	ln, err := dialerA.Listen("tcp", "10.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen on A: %v", err)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from wireguard")
	})}
	go srv.Serve(ln)
	defer srv.Close()

	// Dial from B through the tunnel to A's listener
	addr := ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialerB.DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("DialContext(B→A): %v", err)
	}
	defer conn.Close()

	// Make an HTTP request over the tunnel connection
	fmt.Fprintf(conn, "GET / HTTP/1.0\r\nHost: test\r\n\r\n")

	body, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if got := string(body); !contains(got, "hello from wireguard") {
		t.Errorf("response = %q, want it to contain %q", got, "hello from wireguard")
	}
}

func TestWireGuardDialerInvalidKey(t *testing.T) {
	_, err := NewWireGuardDialer(WireGuardConfig{
		PrivateKey: "not-valid-base64-key",
		PublicKey:  "also-not-valid",
		Endpoint:   "127.0.0.1:51820",
		Address:    "10.0.0.1/32",
	})
	if err == nil {
		t.Fatal("expected error for invalid keys")
	}
}

func TestWireGuardDialerInvalidAddress(t *testing.T) {
	_, err := NewWireGuardDialer(WireGuardConfig{
		PrivateKey: "EKL0Q3zEMrKNR3tYWuJLLgqK7kvzdPCo+aN9xTJQ42o=",
		PublicKey:  "bq1Ob1Z9UhxNUbxYbt0Vd/RyYY6mPvwBM4d+1TsihyY=",
		Endpoint:   "127.0.0.1:51820",
		Address:    "not-an-address",
	})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestWireGuardDialerClose(t *testing.T) {
	cfg, _ := generateWireGuardPair(t)

	d, err := NewWireGuardDialer(cfg)
	if err != nil {
		t.Fatalf("NewWireGuardDialer: %v", err)
	}

	// Close should not panic and should be idempotent
	d.Close()
	d.Close()
}

// generateWireGuardPair creates two WireGuard configs that can communicate.
// A = 10.0.0.1, B = 10.0.0.2. Both use localhost UDP ports.
func generateWireGuardPair(t *testing.T) (WireGuardConfig, WireGuardConfig) {
	t.Helper()

	// Allocate two random UDP ports for the endpoints
	portA := allocUDPPort(t)
	portB := allocUDPPort(t)

	// Valid WireGuard key pairs (private key clamped, public key derived via curve25519)
	privA := "EKL0Q3zEMrKNR3tYWuJLLgqK7kvzdPCo+aN9xTJQ42o="
	pubA := "gUHMjOGfDj/2+OZr/5IV6sI/yWz/SL55OF3ZuNxIjFg="

	privB := "sOI9BDR0EsyHGfckF9Oqk9oQKlcFtH/OfiPPUrIauEk="
	pubB := "bq1Ob1Z9UhxNUbxYbt0Vd/RyYY6mPvwBM4d+1TsihyY="

	cfgA := WireGuardConfig{
		PrivateKey: privA,
		PublicKey:  pubB, // A's peer is B
		Endpoint:   fmt.Sprintf("127.0.0.1:%d", portB),
		Address:    "10.0.0.1/24",
		ListenPort: portA,
	}

	cfgB := WireGuardConfig{
		PrivateKey: privB,
		PublicKey:  pubA, // B's peer is A
		Endpoint:   fmt.Sprintf("127.0.0.1:%d", portA),
		Address:    "10.0.0.2/24",
		ListenPort: portB,
	}

	return cfgA, cfgB
}

func allocUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	conn.Close()
	return port
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
