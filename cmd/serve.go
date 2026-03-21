package cmd

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/control"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/proxy"
	"go.olrik.dev/subspace/route"
	"go.olrik.dev/subspace/upstream"
)

var serveCmd = &cobra.Command{
	Use:              "serve",
	Short:            "Start the proxy server",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ParseFile(configFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Set up log buffer and handler so all slog output is captured
		logBuf := control.NewLogBuffer(1000)
		logHandler := control.NewLogHandler(logBuf, nil)
		// Tee to stderr (colored handler) and the buffer
		stderrHandler := style.NewLogHandler(os.Stderr, nil)
		slog.SetDefault(slog.New(newTeeHandler(stderrHandler, logHandler)))

		// Build initial routing state
		matcher, dialers, err := buildRouting(cfg)
		if err != nil {
			return err
		}

		// Start proxy listener
		ln, err := net.Listen("tcp", cfg.Listen)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
		}

		pool := upstream.NewPool(upstream.PoolConfig{})
		srv := proxy.NewServer(ln, matcher, dialers, pool)

		// Ensure the control socket directory exists
		if err := os.MkdirAll(filepath.Dir(cfg.ControlSocket), 0700); err != nil {
			return fmt.Errorf("creating control socket directory: %w", err)
		}

		// Start control socket (with access to proxy stats and upstream health)
		upstreamInfo := buildUpstreamInfo(cfg)
		ctrlSrv, err := control.NewServer(cfg.ControlSocket, logBuf, srv.Stats, upstreamInfo, pool)
		if err != nil {
			return fmt.Errorf("control socket: %w", err)
		}
		defer ctrlSrv.Close()
		go ctrlSrv.Serve()

		slog.Info("subspace listening",
			"version", Version,
			"addr", cfg.Listen,
			"control", cfg.ControlSocket,
			"upstreams", len(cfg.Upstreams),
			"routes", len(cfg.Routes),
		)

		// Watch config files for changes (main config + included files)
		go watchConfig(cfg, srv, ctrlSrv)

		// Graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			slog.Info("shutting down")
			srv.Close()
		}()

		return srv.Serve()
	},
}

// buildRouting creates a route matcher and dialer map from the config.
func buildRouting(cfg *config.Config) (*route.Matcher, map[string]upstream.Dialer, error) {
	dialers := make(map[string]upstream.Dialer)
	for name, u := range cfg.Upstreams {
		d, err := buildDialer(u)
		if err != nil {
			return nil, nil, fmt.Errorf("upstream %q: %w", name, err)
		}
		dialers[name] = d
	}

	rules := make([]route.Rule, len(cfg.Routes))
	for i, r := range cfg.Routes {
		rules[i] = route.Rule{Pattern: r.Pattern, Upstream: r.Via}
	}
	matcher := route.NewMatcher(rules)

	return matcher, dialers, nil
}

func buildDialer(u config.Upstream) (upstream.Dialer, error) {
	switch u.Type {
	case "http":
		return upstream.NewHTTPConnectDialer(u.Address, u.Username, u.Password), nil
	case "socks5":
		return upstream.NewSOCKS5Dialer(u.Address, u.Username, u.Password)
	default:
		return nil, fmt.Errorf("unknown upstream type %q", u.Type)
	}
}

// buildUpstreamInfo extracts upstream metadata from config for the status endpoint.
func buildUpstreamInfo(cfg *config.Config) map[string]control.UpstreamInfo {
	info := make(map[string]control.UpstreamInfo, len(cfg.Upstreams))
	for name, u := range cfg.Upstreams {
		info[name] = control.UpstreamInfo{Type: u.Type, Address: u.Address}
	}
	return info
}

// watchConfig watches the main config file and all included files for changes,
// reloading the proxy server's routing when any of them are modified.
func watchConfig(currentCfg *config.Config, srv *proxy.Server, ctrlSrv *control.Server) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("config watcher setup failed", "error", err)
		return
	}
	defer watcher.Close()

	// Build the set of watched files and their directories
	watchedFiles := make(map[string]bool)
	watchedDirs := make(map[string]bool)
	for _, f := range currentCfg.IncludedFiles {
		watchedFiles[f] = true
		dir := filepath.Dir(f)
		if !watchedDirs[dir] {
			if err := watcher.Add(dir); err != nil {
				slog.Error("config watcher add failed", "path", dir, "error", err)
				return
			}
			watchedDirs[dir] = true
		}
	}

	slog.Info("watching config for changes", "files", len(watchedFiles))

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			// React to changes in known files, or new files in watched
			// directories (they may match an existing glob include).
			eventAbs, _ := filepath.Abs(event.Name)
			eventDir := filepath.Dir(eventAbs)
			if !watchedFiles[eventAbs] && !watchedDirs[eventDir] {
				continue
			}

			newCfg := reloadConfig(currentCfg, srv, ctrlSrv)
			if newCfg == nil {
				continue
			}

			// Update watched file set — includes may have changed
			newFiles := make(map[string]bool)
			newDirs := make(map[string]bool)
			for _, f := range newCfg.IncludedFiles {
				newFiles[f] = true
				newDirs[filepath.Dir(f)] = true
			}

			// Watch new directories
			for dir := range newDirs {
				if !watchedDirs[dir] {
					if err := watcher.Add(dir); err != nil {
						slog.Error("config watcher add failed", "path", dir, "error", err)
					}
				}
			}
			// Remove old directories
			for dir := range watchedDirs {
				if !newDirs[dir] {
					watcher.Remove(dir)
				}
			}

			watchedFiles = newFiles
			watchedDirs = newDirs
			currentCfg = newCfg

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "error", err)
		}
	}
}

// reloadConfig re-parses the config from the main file (which resolves
// includes), validates it, and applies it. Returns the new config on
// success, or nil if the reload failed.
func reloadConfig(currentCfg *config.Config, srv *proxy.Server, ctrlSrv *control.Server) *config.Config {
	// The main config file is always the first in IncludedFiles
	mainFile := currentCfg.IncludedFiles[0]

	newCfg, err := config.ParseFile(mainFile)
	if err != nil {
		slog.Warn("config reload: invalid config, keeping current", "error", err)
		return nil
	}

	// Warn about settings that require a restart
	if newCfg.Listen != currentCfg.Listen {
		slog.Warn("config reload: listen address changed, requires restart to take effect",
			"current", currentCfg.Listen, "new", newCfg.Listen)
	}
	if newCfg.ControlSocket != currentCfg.ControlSocket {
		slog.Warn("config reload: control_socket changed, requires restart to take effect",
			"current", currentCfg.ControlSocket, "new", newCfg.ControlSocket)
	}

	matcher, dialers, err := buildRouting(newCfg)
	if err != nil {
		slog.Warn("config reload: failed to build routing, keeping current", "error", err)
		return nil
	}

	srv.Reload(matcher, dialers)
	ctrlSrv.SetUpstreams(buildUpstreamInfo(newCfg))

	slog.Info("config reloaded",
		"upstreams", len(newCfg.Upstreams),
		"routes", len(newCfg.Routes),
	)

	return newCfg
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
