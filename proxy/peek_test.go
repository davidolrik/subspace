package proxy

import (
	"io"
	"net"
	"testing"
)

func TestPeekConnUnwrapReturnBufferedBytes(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		server.Write([]byte("hello world"))
		server.Close()
	}()

	pc := NewPeekConn(client)

	// Peek triggers a read-ahead into the buffer
	peeked, err := pc.Peek(5)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if string(peeked) != "hello" {
		t.Fatalf("Peek = %q, want %q", peeked, "hello")
	}

	// Unwrap returns everything the bufio.Reader buffered
	rawConn, buffered := pc.Unwrap()

	// Combine buffered + remaining from raw conn
	rest, _ := io.ReadAll(rawConn)
	full := string(buffered) + string(rest)
	if full != "hello world" {
		t.Errorf("full data = %q, want %q", full, "hello world")
	}

	if len(buffered) == 0 {
		t.Error("expected non-empty buffered bytes")
	}
}

func TestPeekConnUnwrapAfterPartialRead(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		server.Write([]byte("ABCDEF"))
		server.Close()
	}()

	pc := NewPeekConn(client)

	// Peek triggers read-ahead
	pc.Peek(4)

	// Read 2 bytes — consumes A, B from buffer
	buf := make([]byte, 2)
	n, err := pc.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "AB" {
		t.Fatalf("Read = %q, want %q", buf[:n], "AB")
	}

	// Unwrap returns remaining buffered bytes
	rawConn, buffered := pc.Unwrap()

	rest, _ := io.ReadAll(rawConn)
	remaining := string(buffered) + string(rest)
	if remaining != "CDEF" {
		t.Errorf("remaining = %q, want %q", remaining, "CDEF")
	}

	if len(buffered) == 0 {
		t.Error("expected non-empty buffered bytes after partial read")
	}
}

func TestPeekConnUnwrapNothingBuffered(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	pc := NewPeekConn(client)

	// Don't peek, just unwrap
	rawConn, buffered := pc.Unwrap()
	if len(buffered) != 0 {
		t.Errorf("buffered = %q, want empty", buffered)
	}

	go func() {
		server.Write([]byte("data"))
		server.Close()
	}()

	data, _ := io.ReadAll(rawConn)
	if string(data) != "data" {
		t.Errorf("got %q, want %q", data, "data")
	}
}

func TestRelayWithResidualBytes(t *testing.T) {
	// Use TCP so CloseWrite works and io.Copy terminates properly
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// "client" side
	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// "client" side of the proxy (accepted conn)
	clientProxy, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}

	// "upstream" pair
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	upstreamConn, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	upstreamServer, err := ln2.Accept()
	if err != nil {
		t.Fatal(err)
	}

	residual := []byte("residual-")

	// Client sends data then closes write
	go func() {
		clientConn.Write([]byte("data"))
		clientConn.(*net.TCPConn).CloseWrite()
	}()

	// Upstream server reads everything, then replies
	received := make(chan string, 1)
	go func() {
		all, _ := io.ReadAll(upstreamServer)
		received <- string(all)
		upstreamServer.Write([]byte("reply"))
		upstreamServer.(*net.TCPConn).CloseWrite()
	}()

	result := Relay(clientProxy, upstreamConn, residual)

	got := <-received
	if got != "residual-data" {
		t.Errorf("upstream received %q, want %q", got, "residual-data")
	}

	if result.BytesOut != int64(len("residual-data")) {
		t.Errorf("BytesOut = %d, want %d", result.BytesOut, len("residual-data"))
	}

	// Read the reply from the client side
	reply, _ := io.ReadAll(clientConn)
	if string(reply) != "reply" {
		t.Errorf("client received %q, want %q", reply, "reply")
	}

	if result.BytesIn != int64(len("reply")) {
		t.Errorf("BytesIn = %d, want %d", result.BytesIn, len("reply"))
	}
}

func TestRelayWithNilResidual(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	clientProxy, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	upstreamConn, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	upstreamServer, err := ln2.Accept()
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		clientConn.Write([]byte("hello"))
		clientConn.(*net.TCPConn).CloseWrite()
	}()

	received := make(chan string, 1)
	go func() {
		all, _ := io.ReadAll(upstreamServer)
		received <- string(all)
		upstreamServer.Write([]byte("world"))
		upstreamServer.(*net.TCPConn).CloseWrite()
	}()

	result := Relay(clientProxy, upstreamConn, nil)

	got := <-received
	if got != "hello" {
		t.Errorf("upstream received %q, want %q", got, "hello")
	}
	if result.BytesOut != 5 {
		t.Errorf("BytesOut = %d, want 5", result.BytesOut)
	}
	if result.BytesIn != 5 {
		t.Errorf("BytesIn = %d, want 5", result.BytesIn)
	}
}
