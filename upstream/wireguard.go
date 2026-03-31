package upstream

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// WireGuardConfig holds the configuration for a userspace WireGuard tunnel.
type WireGuardConfig struct {
	PrivateKey string // base64-encoded private key
	PublicKey  string // base64-encoded peer public key
	Endpoint   string // peer endpoint (host:port)
	Address    string // local tunnel address with CIDR (e.g. "10.0.0.2/32")
	DNS        string // optional DNS server address
	ListenPort int    // optional local listen port (0 = auto)
}

// WireGuardDialer connects to targets through a userspace WireGuard tunnel.
type WireGuardDialer struct {
	tnet *netstack.Net
	dev  *device.Device
	tdev tun.Device
}

// NewWireGuardDialer creates a dialer that routes traffic through a userspace
// WireGuard tunnel. The tunnel runs entirely in-process using wireguard-go
// and gVisor's netstack — no root privileges or kernel module required.
func NewWireGuardDialer(cfg WireGuardConfig) (*WireGuardDialer, error) {
	// Parse and validate the private key
	privKeyBytes, err := base64.StdEncoding.DecodeString(cfg.PrivateKey)
	if err != nil || len(privKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid private key: must be 32 bytes base64-encoded")
	}

	// Parse and validate the public key
	pubKeyBytes, err := base64.StdEncoding.DecodeString(cfg.PublicKey)
	if err != nil || len(pubKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid public key: must be 32 bytes base64-encoded")
	}

	// Parse the tunnel address
	prefix, err := netip.ParsePrefix(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid tunnel address %q: %w", cfg.Address, err)
	}

	// Parse optional DNS server
	var dnsAddrs []netip.Addr
	if cfg.DNS != "" {
		dnsAddr, err := netip.ParseAddr(cfg.DNS)
		if err != nil {
			return nil, fmt.Errorf("invalid DNS address %q: %w", cfg.DNS, err)
		}
		dnsAddrs = append(dnsAddrs, dnsAddr)
	}

	// Create the userspace TUN device with netstack
	tdev, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{prefix.Addr()},
		dnsAddrs,
		1420, // standard WireGuard MTU
	)
	if err != nil {
		return nil, fmt.Errorf("creating netstack TUN: %w", err)
	}

	// Create the WireGuard device
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))

	// Configure the device using the UAPI format
	uapi := fmt.Sprintf("private_key=%s\n", hex.EncodeToString(privKeyBytes))
	if cfg.ListenPort > 0 {
		uapi += fmt.Sprintf("listen_port=%d\n", cfg.ListenPort)
	}
	uapi += fmt.Sprintf(
		"public_key=%s\nendpoint=%s\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n",
		hex.EncodeToString(pubKeyBytes),
		cfg.Endpoint,
	)

	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		tdev.Close()
		return nil, fmt.Errorf("configuring WireGuard device: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		tdev.Close()
		return nil, fmt.Errorf("bringing up WireGuard device: %w", err)
	}

	return &WireGuardDialer{
		tnet: tnet,
		dev:  dev,
		tdev: tdev,
	}, nil
}

// DialContext establishes a connection through the WireGuard tunnel.
func (d *WireGuardDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.tnet.DialContext(ctx, network, addr)
}

// Listen creates a TCP listener on the WireGuard tunnel's network stack.
// This is primarily useful for testing.
func (d *WireGuardDialer) Listen(network, addr string) (net.Listener, error) {
	tcpAddr, err := net.ResolveTCPAddr(network, addr)
	if err != nil {
		return nil, err
	}
	return d.tnet.ListenTCP(tcpAddr)
}

// Close shuts down the WireGuard tunnel and releases all resources.
func (d *WireGuardDialer) Close() error {
	// device.Close() also closes the tun device, so we only call dev.Close().
	d.dev.Close()
	return nil
}
