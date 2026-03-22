package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
)

// SOCKS5 protocol constants
const (
	socks5Version       = 0x05
	socks5AuthNone      = 0x00
	socks5AuthNoAccept  = 0xFF
	socks5CmdConnect    = 0x01
	socks5AddrIPv4      = 0x01
	socks5AddrDomain    = 0x03
	socks5AddrIPv6      = 0x04
	socks5StatusOK      = 0x00
	socks5StatusFailure = 0x01
)

// handleSOCKS5 performs the SOCKS5 server-side handshake, extracts the
// target address, and relays traffic through the appropriate upstream.
func (s *Server) handleSOCKS5(conn *PeekConn) {
	// --- Auth negotiation ---
	// Client sends: version(1) + nmethods(1) + methods(nmethods)
	// We already peeked the version byte (0x05). Read the full greeting.
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		slog.Debug("SOCKS5 read greeting failed", "error", err)
		return
	}

	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		slog.Debug("SOCKS5 read methods failed", "error", err)
		return
	}

	// Accept "no authentication" only
	hasNoAuth := false
	for _, m := range methods {
		if m == socks5AuthNone {
			hasNoAuth = true
			break
		}
	}

	if !hasNoAuth {
		conn.Write([]byte{socks5Version, socks5AuthNoAccept})
		return
	}

	// Respond: version(1) + selected method(1)
	conn.Write([]byte{socks5Version, socks5AuthNone})

	// --- Connect request ---
	// Client sends: version(1) + cmd(1) + rsv(1) + addrtype(1) + addr(var) + port(2)
	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, reqHeader); err != nil {
		slog.Debug("SOCKS5 read request failed", "error", err)
		return
	}

	if reqHeader[1] != socks5CmdConnect {
		s.socks5Reply(conn, socks5StatusFailure, "0.0.0.0", 0)
		return
	}

	// Read target address
	hostname, err := s.socks5ReadAddr(conn, reqHeader[3])
	if err != nil {
		slog.Debug("SOCKS5 read address failed", "error", err)
		s.socks5Reply(conn, socks5StatusFailure, "0.0.0.0", 0)
		return
	}

	// Read target port
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		slog.Debug("SOCKS5 read port failed", "error", err)
		return
	}
	port := binary.BigEndian.Uint16(portBuf)
	targetAddr := net.JoinHostPort(hostname, strconv.Itoa(int(port)))

	// --- Route and dial ---
	route := s.dialerFor(hostname)

	slog.Debug("SOCKS5", "target", targetAddr, "via", route.upstream)

	upstream, err := route.dialer.DialContext(s.ctx, "tcp", targetAddr)
	if err != nil {
		if isDNSError(err) {
			slog.Error("DNS lookup failed", "host", hostname, "error", err)
			s.Stats.IncError("dns_failed")
		} else {
			slog.Error("SOCKS5 dial failed", "target", targetAddr, "via", route.upstream, "error", err)
			s.Stats.IncUpstream(route.upstream, false)
			s.Stats.IncError("dial_failed")
		}
		s.socks5Reply(conn, socks5StatusFailure, "0.0.0.0", 0)
		return
	}

	s.Stats.IncUpstream(route.upstream, true)

	// Send success reply
	s.socks5Reply(conn, socks5StatusOK, "0.0.0.0", 0)

	// Relay traffic
	rawConn, buffered := conn.Unwrap()
	result := Relay(rawConn, upstream, buffered)
	s.Stats.AddUpstreamBytes(route.upstream, result.BytesIn, result.BytesOut)
}

// socks5ReadAddr reads the target address based on the address type byte.
func (s *Server) socks5ReadAddr(r io.Reader, addrType byte) (string, error) {
	switch addrType {
	case socks5AddrIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil

	case socks5AddrDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return "", err
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(r, domain); err != nil {
			return "", err
		}
		return string(domain), nil

	case socks5AddrIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		return net.IP(addr).String(), nil

	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type: %d", addrType)
	}
}

// socks5Reply sends a SOCKS5 reply with the given status.
func (s *Server) socks5Reply(conn *PeekConn, status byte, bindAddr string, bindPort uint16) {
	ip := net.ParseIP(bindAddr).To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	reply := []byte{
		socks5Version,
		status,
		0x00, // reserved
		socks5AddrIPv4,
		ip[0], ip[1], ip[2], ip[3],
		byte(bindPort >> 8), byte(bindPort & 0xff),
	}
	conn.Write(reply)
}
