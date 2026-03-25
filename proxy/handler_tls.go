package proxy

import (
	"fmt"
	"log/slog"
)

// extractSNI parses the TLS ClientHello from the peeked bytes and returns
// the SNI server name. Returns an error if the ClientHello is malformed
// or doesn't contain an SNI extension.
func extractSNI(data []byte) (string, error) {
	// TLS record header: type(1) + version(2) + length(2)
	if len(data) < 5 {
		return "", fmt.Errorf("too short for TLS record header")
	}
	if data[0] != 0x16 {
		return "", fmt.Errorf("not a TLS handshake record")
	}
	recordLen := int(data[3])<<8 | int(data[4])
	data = data[5:]
	if len(data) < recordLen {
		// We may not have the full record, but try to parse what we have
	}

	// Handshake header: type(1) + length(3)
	if len(data) < 4 {
		return "", fmt.Errorf("too short for handshake header")
	}
	if data[0] != 0x01 {
		return "", fmt.Errorf("not a ClientHello (type=%d)", data[0])
	}
	data = data[4:]

	// ClientHello: version(2) + random(32)
	if len(data) < 34 {
		return "", fmt.Errorf("too short for ClientHello")
	}
	data = data[34:]

	// Session ID: length(1) + data
	if len(data) < 1 {
		return "", fmt.Errorf("too short for session ID length")
	}
	sessLen := int(data[0])
	data = data[1:]
	if len(data) < sessLen {
		return "", fmt.Errorf("too short for session ID")
	}
	data = data[sessLen:]

	// Cipher suites: length(2) + data
	if len(data) < 2 {
		return "", fmt.Errorf("too short for cipher suites length")
	}
	csLen := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if len(data) < csLen {
		return "", fmt.Errorf("too short for cipher suites")
	}
	data = data[csLen:]

	// Compression methods: length(1) + data
	if len(data) < 1 {
		return "", fmt.Errorf("too short for compression methods length")
	}
	compLen := int(data[0])
	data = data[1:]
	if len(data) < compLen {
		return "", fmt.Errorf("too short for compression methods")
	}
	data = data[compLen:]

	// Extensions: length(2) + data
	if len(data) < 2 {
		return "", fmt.Errorf("no extensions present")
	}
	extLen := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if len(data) < extLen {
		extLen = len(data)
	}
	data = data[:extLen]

	// Iterate extensions looking for SNI (type 0x0000)
	for len(data) >= 4 {
		extType := int(data[0])<<8 | int(data[1])
		extDataLen := int(data[2])<<8 | int(data[3])
		data = data[4:]
		if len(data) < extDataLen {
			break
		}

		if extType == 0x0000 {
			return parseSNIExtension(data[:extDataLen])
		}
		data = data[extDataLen:]
	}

	return "", fmt.Errorf("no SNI extension found")
}

// parseSNIExtension parses the SNI extension data and returns the host name.
func parseSNIExtension(data []byte) (string, error) {
	// SNI extension: list_length(2) + entries
	if len(data) < 2 {
		return "", fmt.Errorf("SNI extension too short")
	}
	data = data[2:] // skip list length

	// Each entry: type(1) + length(2) + name
	for len(data) >= 3 {
		nameType := data[0]
		nameLen := int(data[1])<<8 | int(data[2])
		data = data[3:]
		if len(data) < nameLen {
			break
		}
		if nameType == 0x00 { // host_name
			return string(data[:nameLen]), nil
		}
		data = data[nameLen:]
	}

	return "", fmt.Errorf("no host_name in SNI extension")
}

// handleTLS handles a TLS connection by extracting the SNI from the ClientHello,
// looking up the appropriate dialer, connecting to the target, and relaying traffic.
func (s *Server) handleTLS(conn *PeekConn, listenPort string) {
	// Peek the TLS record header (5 bytes) to determine the full record length,
	// then peek the complete ClientHello to reliably extract SNI.
	header, err := conn.Peek(5)
	if err != nil || len(header) < 5 {
		slog.Error("TLS peek failed", "error", err)
		conn.Close()
		return
	}

	recordLen := int(header[3])<<8 | int(header[4])
	peekLen := 5 + recordLen
	// Cap at the bufio.Reader buffer size to avoid errors
	const maxPeek = 4096
	if peekLen > maxPeek {
		peekLen = maxPeek
	}

	buf, err := conn.Peek(peekLen)
	if err != nil && len(buf) < 5 {
		slog.Error("TLS peek failed", "error", err)
		conn.Close()
		return
	}

	sni, err := extractSNI(buf)
	if err != nil {
		slog.Error("SNI extraction failed", "error", err)
		s.Stats.IncError("sni_failed")
		conn.Close()
		return
	}

	// Internal pages are HTTP-only, but TLS connections to *.subspace.pub
	// are passed through to the external redirect server which redirects
	// HTTPS → HTTP so the daemon can intercept the plain HTTP request.

	targetAddr := sni + ":" + listenPort
	route := s.dialerFor(sni)

	slog.Debug("TLS", "sni", sni, "target", targetAddr, "via", route.upstream)

	upstream, err := route.dialer.DialContext(s.ctx, "tcp", targetAddr)
	if err != nil {
		if isDNSError(err) {
			slog.Error("DNS lookup failed", "sni", sni, "error", err)
			s.Stats.IncError("dns_failed")
		} else {
			slog.Error("TLS dial failed", "sni", sni, "target", targetAddr, "via", route.upstream, "error", err)
			s.Stats.IncUpstream(route.upstream, false)
			s.Stats.IncError("dial_failed")
		}
		conn.Close()
		return
	}

	s.Stats.IncUpstream(route.upstream, true)
	rawConn, buffered := conn.Unwrap()
	result := Relay(rawConn, upstream, buffered)
	s.Stats.AddUpstreamBytes(route.upstream, result.BytesIn, result.BytesOut)
}
