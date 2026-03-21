package upstream

import (
	"context"
	"net"
)

// Dialer establishes connections to target addresses, either directly or through a proxy.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// DirectDialer connects directly to the target without any proxy.
type DirectDialer struct {
	dialer net.Dialer
}

// NewDirectDialer creates a DirectDialer.
func NewDirectDialer() *DirectDialer {
	return &DirectDialer{}
}

func (d *DirectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, addr)
}
