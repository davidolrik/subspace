package cmd

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPprofServerServesAndCloses(t *testing.T) {
	// Bind an ephemeral loopback port so the test never collides with a
	// real pprof server or needs a fixed port.
	srv, err := newPprofServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("newPprofServer: %v", err)
	}
	go srv.Serve()
	defer srv.Close()

	// The pprof index should respond. Retry briefly while Serve spins up.
	url := "http://" + srv.Addr() + "/debug/pprof/"
	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pprof index never responded: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("pprof index status = %d, want 200", resp.StatusCode)
	}
	// The index lists the registered profiles, e.g. "goroutine".
	if !strings.Contains(string(body), "goroutine") {
		t.Errorf("pprof index body missing profile list:\n%s", body)
	}

	// Close is synchronous: after it returns the server is fully down.
	if err := srv.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := http.Get(url); err == nil {
		t.Error("server still accepting connections after Close")
	}

	// Close is idempotent.
	if err := srv.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestIsLoopbackListen(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:6060": true,
		"localhost:6060": true,
		"[::1]:6060":     true,
		"0.0.0.0:6060":   false,
		":6060":          false,
		"192.168.1.5:6060": false,
	}
	for addr, want := range cases {
		if got := isLoopbackListen(addr); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", addr, got, want)
		}
	}
}
