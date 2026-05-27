package cmd

import (
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
)

// defaultPprofListen is the bind address used when pprof is enabled
// without an explicit listen address. Loopback-only by default: pprof
// exposes runtime internals and lets callers trigger expensive CPU/heap
// profiles, so it must not be reachable off-host unless the operator
// deliberately changes it.
const defaultPprofListen = "127.0.0.1:6060"

// pprofServer exposes Go's net/http/pprof endpoints on a dedicated
// listener. It mirrors control.Server's lifecycle: Close closes the
// listener and active connections, blocks until Serve has returned,
// and is idempotent.
type pprofServer struct {
	listener  net.Listener
	srv       *http.Server
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// newPprofServer binds addr and prepares the pprof handlers. The
// listener is opened eagerly so a bad address fails fast at startup.
func newPprofServer(addr string) (*pprofServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	// Register the pprof handlers on a dedicated mux rather than relying
	// on the net/http DefaultServeMux that importing net/http/pprof
	// populates — the rest of the daemon never serves that mux.
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return &pprofServer{
		listener: ln,
		srv:      &http.Server{Handler: mux},
		done:     make(chan struct{}),
	}, nil
}

// Addr reports the actual bound address, which is useful when the
// configured port was 0 (auto-assign).
func (p *pprofServer) Addr() string { return p.listener.Addr().String() }

// Serve accepts connections until Close is called.
func (p *pprofServer) Serve() error {
	defer close(p.done)
	return p.srv.Serve(p.listener)
}

// Close stops the server, closing the listener and any active
// connections, and blocks until Serve has returned. Idempotent: safe
// to call multiple times; repeated calls return the same error.
func (p *pprofServer) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.srv.Close()
		<-p.done
	})
	return p.closeErr
}

// isLoopbackListen reports whether addr binds only the loopback
// interface. A missing host (e.g. ":6060") or a wildcard address binds
// every interface and is treated as non-loopback so the operator gets
// warned before exposing pprof off-host.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
