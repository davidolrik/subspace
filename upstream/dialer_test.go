package upstream

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	socks5 "github.com/armon/go-socks5"
)

// startEchoServer starts a TCP server that echoes back what it receives.
// Returns the listener address.
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	return ln.Addr().String()
}

func TestDirectDialer(t *testing.T) {
	addr := startEchoServer(t)

	d := NewDirectDialer()
	conn, err := d.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("DialContext failed: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello subspace")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("ReadFull failed: %v", err)
	}

	if string(buf) != "hello subspace" {
		t.Errorf("direct: got %q, want %q", buf, msg)
	}
}

// startHTTPConnectProxy starts a mock HTTP CONNECT proxy server.
// If requireAuth is non-empty, it requires Proxy-Authorization with that value.
func startHTTPConnectProxy(t *testing.T, requireAuth string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleCONNECT(conn, requireAuth)
		}
	}()

	return ln.Addr().String()
}

func handleCONNECT(conn net.Conn, requireAuth string) {
	defer conn.Close()

	req, err := http.ReadRequest(newBufioReader(conn))
	if err != nil {
		return
	}

	if req.Method != http.MethodConnect {
		conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\n\r\n"))
		return
	}

	if requireAuth != "" {
		got := req.Header.Get("Proxy-Authorization")
		if got != requireAuth {
			conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
			return
		}
	}

	target, err := net.Dial("tcp", req.Host)
	if err != nil {
		conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer target.Close()

	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	done := make(chan struct{}, 2)
	go func() { io.Copy(target, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, target); done <- struct{}{} }()
	<-done
}

func TestHTTPConnectDialer(t *testing.T) {
	echoAddr := startEchoServer(t)
	proxyAddr := startHTTPConnectProxy(t, "")

	d := NewHTTPConnectDialer(proxyAddr, "", "")
	conn, err := d.DialContext(context.Background(), "tcp", echoAddr)
	if err != nil {
		t.Fatalf("DialContext failed: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello via http connect")
	conn.Write(msg)
	buf := make([]byte, len(msg))
	io.ReadFull(conn, buf)

	if string(buf) != "hello via http connect" {
		t.Errorf("http connect: got %q, want %q", buf, msg)
	}
}

func TestHTTPConnectDialerWithAuth(t *testing.T) {
	echoAddr := startEchoServer(t)

	expectedAuth := "Basic dXNlcjpwYXNz"
	proxyAddr := startHTTPConnectProxy(t, expectedAuth)

	// Without auth should fail
	dNoAuth := NewHTTPConnectDialer(proxyAddr, "", "")
	conn, err := dNoAuth.DialContext(context.Background(), "tcp", echoAddr)
	if err == nil {
		conn.Close()
		t.Fatal("expected error without auth, got nil")
	}

	// With auth should succeed
	dAuth := NewHTTPConnectDialer(proxyAddr, "user", "pass")
	conn, err = dAuth.DialContext(context.Background(), "tcp", echoAddr)
	if err != nil {
		t.Fatalf("DialContext with auth failed: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello with auth")
	conn.Write(msg)
	buf := make([]byte, len(msg))
	io.ReadFull(conn, buf)

	if string(buf) != "hello with auth" {
		t.Errorf("http connect auth: got %q, want %q", buf, msg)
	}
}

// startSOCKS5Server starts a SOCKS5 proxy server. Returns the listener address.
func startSOCKS5Server(t *testing.T) string {
	t.Helper()
	conf := &socks5.Config{}
	server, err := socks5.New(conf)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go server.Serve(ln)
	return ln.Addr().String()
}

func TestSOCKS5Dialer(t *testing.T) {
	echoAddr := startEchoServer(t)
	socksAddr := startSOCKS5Server(t)

	d, err := NewSOCKS5Dialer(socksAddr, "", "")
	if err != nil {
		t.Fatalf("NewSOCKS5Dialer failed: %v", err)
	}

	conn, err := d.DialContext(context.Background(), "tcp", echoAddr)
	if err != nil {
		t.Fatalf("DialContext failed: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello via socks5")
	conn.Write(msg)
	buf := make([]byte, len(msg))
	io.ReadFull(conn, buf)

	if string(buf) != "hello via socks5" {
		t.Errorf("socks5: got %q, want %q", buf, msg)
	}
}
