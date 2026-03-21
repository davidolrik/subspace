package upstream

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

// HTTPConnectDialer connects to targets through an HTTP CONNECT proxy.
type HTTPConnectDialer struct {
	proxyAddr string
	username  string
	password  string
	dialer    net.Dialer
}

// NewHTTPConnectDialer creates a dialer that tunnels through an HTTP CONNECT proxy.
func NewHTTPConnectDialer(proxyAddr, username, password string) *HTTPConnectDialer {
	return &HTTPConnectDialer{
		proxyAddr: proxyAddr,
		username:  username,
		password:  password,
	}
}

func (d *HTTPConnectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.dialer.DialContext(ctx, "tcp", d.proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("connecting to proxy %s: %w", d.proxyAddr, err)
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: addr},
		Host:   addr,
		Header: make(http.Header),
	}

	if d.username != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(d.username + ":" + d.password))
		connectReq.Header.Set("Proxy-Authorization", "Basic "+creds)
	}

	if err := connectReq.Write(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("writing CONNECT request: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading CONNECT response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT returned %d", resp.StatusCode)
	}

	// After a successful CONNECT, the connection is now a raw tunnel.
	// Return a buffered conn in case the bufio.Reader consumed extra bytes.
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, reader: br}, nil
	}
	return conn, nil
}

// bufferedConn wraps a net.Conn with a bufio.Reader to replay buffered bytes.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// newBufioReader creates a bufio.Reader wrapping r.
func newBufioReader(r io.Reader) *bufio.Reader {
	return bufio.NewReader(r)
}
