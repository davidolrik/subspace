package upstream

import (
	"context"
	"net"

	"golang.org/x/net/proxy"
)

// SOCKS5Dialer connects to targets through a SOCKS5 proxy.
type SOCKS5Dialer struct {
	dialer proxy.Dialer
}

// NewSOCKS5Dialer creates a dialer that tunnels through a SOCKS5 proxy.
// If username is empty, no authentication is used.
func NewSOCKS5Dialer(proxyAddr, username, password string) (*SOCKS5Dialer, error) {
	var auth *proxy.Auth
	if username != "" {
		auth = &proxy.Auth{
			User:     username,
			Password: password,
		}
	}

	d, err := proxy.SOCKS5("tcp", proxyAddr, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}

	return &SOCKS5Dialer{dialer: d}, nil
}

func (d *SOCKS5Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// proxy.SOCKS5 returns a proxy.Dialer which may also implement
	// proxy.ContextDialer for context support.
	if cd, ok := d.dialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, network, addr)
	}
	return d.dialer.Dial(network, addr)
}
