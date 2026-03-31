package cmd

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/control"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/pages"
	"go.olrik.dev/subspace/proxy"
	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/route"
	"go.olrik.dev/subspace/upstream"
)

func newServeCommand(configFile *string) *cobra.Command {
	return &cobra.Command{
		Use:              "serve",
		Short:            "Start the proxy server",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.ParseFile(*configFile)
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

			defer closeDialers(dialers)
			pool := upstream.NewPool(upstream.PoolConfig{})
			srv := proxy.NewServer(ln, matcher, dialers, pool)

			// Start health monitor for upstream proxies
			monitor := upstream.NewMonitor(buildMonitorTargets(cfg), 10*time.Second, 3*time.Second)
			monitor.Start()
			defer monitor.Stop()
			srv.SetMonitor(monitor)

			// Open the statistics database
			statsDBPath := filepath.Join(filepath.Dir(*configFile), "stats.db")
			statsStore, err := stats.OpenStore(statsDBPath)
			if err != nil {
				return fmt.Errorf("opening stats database: %w", err)
			}
			defer statsStore.Close()

			// Start the periodic stats recorder
			recorder := stats.NewRecorder(srv.Stats, statsStore, stats.DefaultRecorderConfig())
			go recorder.Run()
			defer recorder.Stop()

			// Set up internal pages (link pages, statistics, error pages)
			pageInfos, err := loadPages(cfg)
			if err != nil {
				return err
			}
			pagesHandler := pages.New(pageInfos, srv.Stats, statsStore)
			srv.Pages = pagesHandler

			// Ensure the control socket directory exists
			if err := os.MkdirAll(filepath.Dir(cfg.ControlSocket), 0700); err != nil {
				return fmt.Errorf("creating control socket directory: %w", err)
			}

			// Start control socket (with access to proxy stats and upstream health)
			ctrlSrv, err := control.NewServer(cfg.ControlSocket, logBuf, srv.Stats, monitor, pool)
			if err != nil {
				return fmt.Errorf("control socket: %w", err)
			}
			defer ctrlSrv.Close()
			go ctrlSrv.Serve()

			// Give the statistics page access to upstream health data
			pagesHandler.SetStatusProvider(func() any { return ctrlSrv.Status() })

			slog.Info("subspace listening",
				"version", Version,
				"addr", cfg.Listen,
				"control", cfg.ControlSocket,
				"upstreams", len(cfg.Upstreams),
				"routes", len(cfg.Routes),
			)

			// Watch config files for changes (main config + included files)
			go watchConfig(cfg, srv, ctrlSrv, pagesHandler, monitor, dialers)

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
		rules[i] = route.Rule{Pattern: r.Pattern, Upstream: r.Via, Fallback: r.Fallback, File: r.File}
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
	case "wireguard":
		return upstream.NewWireGuardDialer(upstream.WireGuardConfig{
			PrivateKey: u.PrivateKey,
			PublicKey:  u.PublicKey,
			Endpoint:   u.Endpoint,
			Address:    u.Address,
			DNS:        u.DNS,
		})
	default:
		return nil, fmt.Errorf("unknown upstream type %q", u.Type)
	}
}

// closeDialers closes any dialers that implement io.Closer (e.g. WireGuard).
func closeDialers(dialers map[string]upstream.Dialer) {
	for _, d := range dialers {
		if c, ok := d.(interface{ Close() error }); ok {
			c.Close()
		}
	}
}

// buildMonitorTargets extracts upstream addresses from config for the health monitor.
// WireGuard upstreams are excluded because they use UDP and cannot be TCP health-checked.
func buildMonitorTargets(cfg *config.Config) map[string]upstream.MonitorTarget {
	targets := make(map[string]upstream.MonitorTarget, len(cfg.Upstreams))
	for name, u := range cfg.Upstreams {
		if u.Type == "wireguard" {
			continue
		}
		targets[name] = upstream.MonitorTarget{Type: u.Type, Address: u.Address}
	}
	return targets
}

// watchConfig watches the main config file and all included files for changes,
// reloading the proxy server's routing when any of them are modified.
func watchConfig(currentCfg *config.Config, srv *proxy.Server, ctrlSrv *control.Server, pagesHandler *pages.Handler, currentMonitor *upstream.Monitor, currentDialers map[string]upstream.Dialer) {
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

			// Ignore non-KDL files (e.g. stats.db, WAL/SHM files)
			if ext := filepath.Ext(eventAbs); ext != ".kdl" {
				continue
			}

			newCfg, newMonitor, newDialers := reloadConfig(currentCfg, srv, ctrlSrv, pagesHandler, currentMonitor, currentDialers)
			if newCfg == nil {
				continue
			}
			currentMonitor = newMonitor
			currentDialers = newDialers

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
func reloadConfig(currentCfg *config.Config, srv *proxy.Server, ctrlSrv *control.Server, pagesHandler *pages.Handler, currentMonitor *upstream.Monitor, currentDialers map[string]upstream.Dialer) (*config.Config, *upstream.Monitor, map[string]upstream.Dialer) {
	// The main config file is always the first in IncludedFiles
	mainFile := currentCfg.IncludedFiles[0]

	newCfg, err := config.ParseFile(mainFile)
	if err != nil {
		slog.Warn("config reload: invalid config, keeping current", "error", err)
		return nil, currentMonitor, currentDialers
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
		return nil, currentMonitor, currentDialers
	}

	// Replace the health monitor with one for the new upstream set
	currentMonitor.Stop()
	newMonitor := upstream.NewMonitor(buildMonitorTargets(newCfg), 10*time.Second, 3*time.Second)
	newMonitor.Start()

	srv.Reload(matcher, dialers)
	srv.SetMonitor(newMonitor)
	ctrlSrv.SetMonitor(newMonitor)

	// Close old dialers that hold resources (e.g. WireGuard tunnels)
	closeDialers(currentDialers)

	// Reload link pages
	if pagesHandler != nil {
		pageInfos, err := loadPages(newCfg)
		if err != nil {
			slog.Warn("config reload: failed to load link pages, keeping current", "error", err)
		} else {
			pagesHandler.ReloadPages(pageInfos)
		}
	}

	slog.Info("config reloaded",
		"upstreams", len(newCfg.Upstreams),
		"routes", len(newCfg.Routes),
	)

	return newCfg, newMonitor, dialers
}

// loadPages parses all configured link page files into PageInfo structs.
func loadPages(cfg *config.Config) ([]pages.PageInfo, error) {
	var infos []pages.PageInfo
	for _, pg := range cfg.Pages {
		pageCfg, err := pages.ParsePageFile(pg.File)
		if err != nil {
			return nil, fmt.Errorf("loading page %q: %w", pg.File, err)
		}
		infos = append(infos, pages.PageInfo{
			Name:  pg.Name,
			Alias: pg.Alias,
			Page:  pageCfg,
		})
	}
	return infos, nil
}
