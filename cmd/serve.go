package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/control"
	"go.olrik.dev/subspace/env"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/pages"
	"go.olrik.dev/subspace/proxy"
	"go.olrik.dev/subspace/route"
	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/upstream"
)

// defaultEnvRefreshInterval is the cadence used when no explicit
// `env { refresh "..." }` is configured. Picked to match the
// operator-facing reference: noticeable enough that ${PUBLIC_IP}
// converges on a real network change within a minute, low enough
// that one shell spawn per minute is invisible on the CPU graph.
const defaultEnvRefreshInterval = 60 * time.Second

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

			// Build initial routing state. Per-upstream/dialer
			// failures are appended to cfg.Errors and the offending
			// item is dropped; only catastrophic problems return an
			// error here.
			matcher, dialers := buildRouting(cfg)

			// Start proxy listeners — at least one is required.
			if len(cfg.Listeners) == 0 {
				return fmt.Errorf("no listeners configured (need at least one `listen` directive)")
			}
			listeners, err := openListeners(cfg.Listeners)
			if err != nil {
				return err
			}

			defer closeDialers(dialers)
			pool := upstream.NewPool(upstream.PoolConfig{})
			srv := proxy.NewServer(listeners, matcher, dialers, pool)

			// Start health monitor for upstream proxies
			monitor := upstream.NewMonitor(buildMonitorTargets(cfg, dialers), 10*time.Second, 3*time.Second)
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

			// Start the periodic stats recorder. The default
			// (365d, from DefaultRecorderConfig) is used when no
			// `stats { retention "..." }` block is present. The
			// parser uses RetentionForever (-1) as an explicit
			// "keep everything" signal — translate that to the
			// recorder's own zero ("disabled") here.
			recorderCfg := stats.DefaultRecorderConfig()
			switch {
			case cfg.StatsRetention > 0:
				recorderCfg.Retention = cfg.StatsRetention
			case cfg.StatsRetention == config.RetentionForever:
				recorderCfg.Retention = 0
			}
			recorder := stats.NewRecorder(srv.Stats, statsStore, recorderCfg)
			// Run one-time database maintenance (legacy compaction +
			// VACUUM) and then start the recorder, both in the
			// background. The maintenance can take tens of seconds on a
			// large legacy database; keeping it off this path means it
			// never delays the proxy accept loop (srv.Serve, below) — the
			// proxy is decoupled from the stats store. The recorder starts
			// only after maintenance so the VACUUM has exclusive DB access
			// and doesn't fight the 5s snapshot writes.
			go func() {
				if err := statsStore.Maintain(); err != nil {
					slog.Error("stats database maintenance failed", "error", err)
				}
				recorder.Run()
			}()
			defer recorder.Stop()

			// Bootstrap the environment snapshot before parsing pages
			// so the first render of every markdown card already has
			// `${...}` tokens resolved. Capture is best-effort: any
			// shell error falls back to os.Environ() so the dashboard
			// still gets a usable environment. References aren't
			// registered yet, so this initial Replace can never report
			// a change — that's deliberate; we don't want the env tick
			// to mistake "snapshot was empty" for "operator just
			// changed PUBLIC_IP".
			envShell := env.ResolveShell(cfg.EnvShell)
			envSnap := env.NewSnapshot()
			initialEnv, _ := env.Capture(context.Background(), envShell)
			envSnap.Replace(initialEnv)
			pages.EnvLookup = envSnap.Lookup

			// Set up internal pages (link pages, statistics, error pages).
			// Pages that fail to load are skipped and their errors
			// joined into cfg.Errors.
			pageInfos := loadPages(cfg)
			envSnap.SetReferences(unionEnvRefs(pageInfos))
			pagesHandler := pages.New(pageInfos, srv.Stats, statsStore)
			pagesHandler.SetTags(tagDefs(cfg))
			pagesHandler.SetSearchEngines(engineDefs(cfg), cfg.DefaultSearchEngine)
			cfg.Errors = append(cfg.Errors, pagesHandler.ValidateTagReferences()...)
			srv.Pages = pagesHandler

			// Start the env refresher. The onChange callback re-
			// renders pages with the new values; it's gated by the
			// snapshot's referenced-vars-only change detection so
			// $RANDOM/PID churn from each shell spawn never triggers
			// a re-render.
			envInterval := cfg.EnvRefreshInterval
			if envInterval == 0 {
				envInterval = defaultEnvRefreshInterval
			}
			refresher := env.NewRefresher(envSnap, envInterval, env.CaptureWith(envShell), func() {
				reloadPagesForEnv(cfg, pagesHandler, envSnap)
			})
			go refresher.Run()
			defer refresher.Stop()

			// Ensure the control socket directory exists
			if err := os.MkdirAll(filepath.Dir(cfg.ControlSocket), 0700); err != nil {
				return fmt.Errorf("creating control socket directory: %w", err)
			}

			// Start control socket (with access to proxy stats and upstream health)
			ctrlSrv, err := control.NewServer(cfg.ControlSocket, logBuf, srv.Stats, statsStore, monitor, pool)
			if err != nil {
				return fmt.Errorf("control socket: %w", err)
			}
			defer ctrlSrv.Close()
			go ctrlSrv.Serve()

			// Give the statistics page access to upstream health data
			pagesHandler.SetStatusProvider(func() any { return ctrlSrv.Status() })

			// Start the pprof debug server if the operator enabled it.
			// Listen-address changes need a restart (see reloadConfig).
			if cfg.PprofEnabled {
				pprofAddr := cfg.PprofListen
				if pprofAddr == "" {
					pprofAddr = defaultPprofListen
				}
				if !isLoopbackListen(pprofAddr) {
					slog.Warn("pprof bound to a non-loopback address — this exposes runtime internals and lets anyone trigger expensive CPU/heap profiles; bind to localhost unless remote access is intended", "addr", pprofAddr)
				}
				pprofSrv, err := newPprofServer(pprofAddr)
				if err != nil {
					return fmt.Errorf("pprof listener: %w", err)
				}
				defer pprofSrv.Close()
				go pprofSrv.Serve()
				slog.Info("pprof profiler enabled", "addr", pprofSrv.Addr())
			}

			// Surface collected non-fatal config errors: log them so
			// the operator sees them on the terminal, and hand them
			// to the pages handler so the internal-pages banner
			// displays them.
			for _, msg := range cfg.Errors {
				slog.Warn("config error", "error", msg)
			}
			pagesHandler.SetConfigErrors(cfg.Errors)
			pagesHandler.SetBlackholeRoutes(blackholePatterns(cfg))

			slog.Info("subspace listening",
				"version", Version,
				"addrs", listenSummary(cfg.Listeners),
				"control", cfg.ControlSocket,
				"upstreams", len(cfg.Upstreams),
				"routes", len(cfg.Routes),
				"config_errors", len(cfg.Errors),
			)

			// Watch config files for changes (main config + included files)
			go watchConfig(cfg, srv, ctrlSrv, pagesHandler, monitor, dialers, envSnap)

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

// openListeners binds each configured Listener to a TCP socket and
// pairs it with the parsed private/label settings ready to hand to
// proxy.NewServer. If any bind fails, listeners that were already
// opened are closed before the error is returned so the operator
// doesn't end up with half a daemon running on a partial set of ports.
func openListeners(cfgs []config.Listener) ([]proxy.ListenerConfig, error) {
	out := make([]proxy.ListenerConfig, 0, len(cfgs))
	for _, lc := range cfgs {
		ln, err := net.Listen("tcp", lc.Address)
		if err != nil {
			for _, opened := range out {
				opened.Net.Close()
			}
			return nil, fmt.Errorf("listen on %s: %w", lc.Address, err)
		}
		out = append(out, proxy.ListenerConfig{
			Net:     ln,
			Private: lc.Private,
			Label:   lc.Label,
		})
	}
	return out, nil
}

// listenSummary returns a compact "addr[private:label] ..." string for
// log lines, joining multiple listeners with a space.
func listenSummary(ls []config.Listener) string {
	parts := make([]string, 0, len(ls))
	for _, l := range ls {
		s := l.Address
		var tags []string
		if l.Private {
			tags = append(tags, "private")
		}
		if l.Label != "" {
			tags = append(tags, l.Label)
		}
		if len(tags) > 0 {
			s += "[" + strings.Join(tags, ":") + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// sameListeners reports whether two Listener slices describe the same
// set in the same order. Used by the config reloader to decide whether
// to warn that listener changes need a restart.
func sameListeners(a, b []config.Listener) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// buildRouting creates a route matcher and dialer map from the
// config. Dialer construction failures (e.g. an invalid WireGuard
// key) are non-fatal: the offending upstream is dropped, the error is
// appended to cfg.Errors, and any route or fallback that pointed at
// the now-missing dialer is dropped or cleared so the rest of the
// proxy still works.
func buildRouting(cfg *config.Config) (*route.Matcher, map[string]upstream.Dialer) {
	dialers := make(map[string]upstream.Dialer)
	for name, u := range cfg.Upstreams {
		d, err := buildDialer(u)
		if err != nil {
			cfg.Errors = append(cfg.Errors, fmt.Sprintf("upstream %q: %v (dropped)", name, err))
			continue
		}
		dialers[name] = d
	}

	// Drop routes whose via lost its dialer at construction time, and
	// clear fallbacks that lost theirs. The built-in pseudo-upstreams
	// ("direct", "blackhole") are always available — direct connects
	// without a proxy, blackhole short-circuits inside the proxy
	// dispatcher — so neither needs a dialer in the map.
	kept := cfg.Routes[:0]
	for _, r := range cfg.Routes {
		if !isPseudoUpstream(r.Via) {
			if _, ok := dialers[r.Via]; !ok {
				cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q: upstream %q is unavailable (route dropped)", r.Pattern, r.Via))
				continue
			}
		}
		if r.Fallback != "" && !isPseudoUpstream(r.Fallback) {
			if _, ok := dialers[r.Fallback]; !ok {
				cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q: fallback upstream %q is unavailable (fallback cleared)", r.Pattern, r.Fallback))
				r.Fallback = ""
			}
		}
		kept = append(kept, r)
	}
	cfg.Routes = kept

	rules := make([]route.Rule, len(cfg.Routes))
	for i, r := range cfg.Routes {
		rules[i] = route.Rule{
			Pattern:  r.Pattern,
			Upstream: r.Via,
			Fallback: r.Fallback,
			Private:  r.Private,
			File:     r.File,
		}
	}
	matcher := route.NewMatcher(rules)

	return matcher, dialers
}

// blackholePatterns returns the patterns of every route currently
// pointing at the built-in blackhole pseudo-upstream, either as the
// primary `via=` target or as a `fallback=`. Patterns appear at most
// once, in route-declaration order so the dashboard reads top-to-bottom.
func blackholePatterns(cfg *config.Config) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, r := range cfg.Routes {
		if r.Via == "blackhole" || r.Fallback == "blackhole" {
			if _, ok := seen[r.Pattern]; ok {
				continue
			}
			seen[r.Pattern] = struct{}{}
			out = append(out, r.Pattern)
		}
	}
	return out
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

// buildMonitorTargets extracts upstream addresses from config for the
// health monitor. WireGuard upstreams are excluded because they use
// UDP and cannot be TCP health-checked. Upstreams whose dialer failed
// to build are also excluded so we don't flap a target that can never
// dial.
func buildMonitorTargets(cfg *config.Config, dialers map[string]upstream.Dialer) map[string]upstream.MonitorTarget {
	targets := make(map[string]upstream.MonitorTarget, len(cfg.Upstreams))
	for name, u := range cfg.Upstreams {
		if u.Type == "wireguard" {
			continue
		}
		if _, ok := dialers[name]; !ok {
			continue
		}
		targets[name] = upstream.MonitorTarget{Type: u.Type, Address: u.Address}
	}
	return targets
}

// watchConfig watches the main config file and all included files for changes,
// reloading the proxy server's routing when any of them are modified.
func watchConfig(currentCfg *config.Config, srv *proxy.Server, ctrlSrv *control.Server, pagesHandler *pages.Handler, currentMonitor *upstream.Monitor, currentDialers map[string]upstream.Dialer, envSnap *env.Snapshot) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("config watcher setup failed", "error", err)
		return
	}
	defer watcher.Close()

	// Build the set of watched files and their directories. The set
	// is the union of:
	//   - main config + transitively included KDL files
	//   - any markdown `include="..."` files referenced by pages.
	watchedFiles := make(map[string]bool)
	watchedDirs := make(map[string]bool)
	addWatch := func(f string) {
		watchedFiles[f] = true
		dir := filepath.Dir(f)
		if !watchedDirs[dir] {
			if err := watcher.Add(dir); err != nil {
				slog.Error("config watcher add failed", "path", dir, "error", err)
			}
			watchedDirs[dir] = true
		}
	}
	for _, f := range currentCfg.IncludedFiles {
		addWatch(f)
	}
	if pagesHandler != nil {
		for _, f := range pagesHandler.IncludedFiles() {
			addWatch(f)
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

			// Ignore files we don't actually care about — stats.db,
			// WAL/SHM files, the operator's editor swap files, etc.
			// Markdown includes are watched explicitly: only trigger
			// a reload for KDL config or for files in the watched
			// includes set.
			if !watchedFiles[eventAbs] {
				if ext := filepath.Ext(eventAbs); ext != ".kdl" {
					continue
				}
			}

			newCfg, newMonitor, newDialers := reloadConfig(currentCfg, srv, ctrlSrv, pagesHandler, currentMonitor, currentDialers, envSnap)
			if newCfg == nil {
				continue
			}
			currentMonitor = newMonitor
			currentDialers = newDialers

			// Update watched file set — KDL includes and markdown
			// include= files may have changed.
			newFiles := make(map[string]bool)
			newDirs := make(map[string]bool)
			for _, f := range newCfg.IncludedFiles {
				newFiles[f] = true
				newDirs[filepath.Dir(f)] = true
			}
			if pagesHandler != nil {
				for _, f := range pagesHandler.IncludedFiles() {
					newFiles[f] = true
					newDirs[filepath.Dir(f)] = true
				}
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
// success, or nil if the reload failed. Per-item validation errors
// during reload follow the same model as startup: collected, logged,
// and surfaced via the internal-pages banner.
func reloadConfig(currentCfg *config.Config, srv *proxy.Server, ctrlSrv *control.Server, pagesHandler *pages.Handler, currentMonitor *upstream.Monitor, currentDialers map[string]upstream.Dialer, envSnap *env.Snapshot) (*config.Config, *upstream.Monitor, map[string]upstream.Dialer) {
	// The main config file is always the first in IncludedFiles
	mainFile := currentCfg.IncludedFiles[0]

	newCfg, err := config.ParseFile(mainFile)
	if err != nil {
		slog.Warn("config reload: invalid config, keeping current", "error", err)
		if pagesHandler != nil {
			pagesHandler.SetReloadError(fmt.Sprintf("config reload failed (using previous config): %v", err))
		}
		return nil, currentMonitor, currentDialers
	}

	// Warn about settings that require a restart
	if !sameListeners(currentCfg.Listeners, newCfg.Listeners) {
		slog.Warn("config reload: listener set changed, requires restart to take effect",
			"current", listenSummary(currentCfg.Listeners), "new", listenSummary(newCfg.Listeners))
	}
	if newCfg.ControlSocket != currentCfg.ControlSocket {
		slog.Warn("config reload: control_socket changed, requires restart to take effect",
			"current", currentCfg.ControlSocket, "new", newCfg.ControlSocket)
	}
	if newCfg.PprofEnabled != currentCfg.PprofEnabled || newCfg.PprofListen != currentCfg.PprofListen {
		slog.Warn("config reload: pprof settings changed, requires restart to take effect",
			"current_enabled", currentCfg.PprofEnabled, "new_enabled", newCfg.PprofEnabled,
			"current_listen", currentCfg.PprofListen, "new_listen", newCfg.PprofListen)
	}

	matcher, dialers := buildRouting(newCfg)

	// Replace the health monitor with one for the new upstream set
	currentMonitor.Stop()
	newMonitor := upstream.NewMonitor(buildMonitorTargets(newCfg, dialers), 10*time.Second, 3*time.Second)
	newMonitor.Start()

	srv.Reload(matcher, dialers)
	srv.SetMonitor(newMonitor)
	ctrlSrv.SetMonitor(newMonitor)

	// Close old dialers that hold resources (e.g. WireGuard tunnels)
	closeDialers(currentDialers)

	// Reload link pages (skipping any that fail to parse).
	if pagesHandler != nil {
		pageInfos := loadPages(newCfg)
		pagesHandler.ReloadPages(pageInfos)
		pagesHandler.SetTags(tagDefs(newCfg))
		pagesHandler.SetSearchEngines(engineDefs(newCfg), newCfg.DefaultSearchEngine)
		newCfg.Errors = append(newCfg.Errors, pagesHandler.ValidateTagReferences()...)
		if envSnap != nil {
			// Page set may have new (or fewer) `${NAME}` references;
			// keep the env refresher's change-detection accurate.
			envSnap.SetReferences(unionEnvRefs(pageInfos))
		}
	}

	for _, msg := range newCfg.Errors {
		slog.Warn("config error", "error", msg)
	}
	if pagesHandler != nil {
		// Replaces both the previous error list and any prior
		// reload-failure notice (handled inside SetConfigErrors).
		pagesHandler.SetConfigErrors(newCfg.Errors)
		pagesHandler.SetBlackholeRoutes(blackholePatterns(newCfg))
	}

	slog.Info("config reloaded",
		"upstreams", len(newCfg.Upstreams),
		"routes", len(newCfg.Routes),
		"config_errors", len(newCfg.Errors),
	)

	return newCfg, newMonitor, dialers
}

// unionEnvRefs returns the union of `${NAME}` references across
// every loaded page. Used to tell the env snapshot which variables
// matter, so the refresher's change-detection skips $RANDOM and
// other per-spawn churn.
func unionEnvRefs(infos []pages.PageInfo) map[string]struct{} {
	out := make(map[string]struct{})
	for _, info := range infos {
		if info.Page == nil {
			continue
		}
		for k := range info.Page.EnvRefs {
			out[k] = struct{}{}
		}
	}
	return out
}

// reloadPagesForEnv is the env-refresher's onChange callback. It re-
// parses page KDL files (so `${NAME}` tokens pick up fresh values
// from the new snapshot) and rebuilds the pages mux without
// disturbing upstream routing or the dialer set — both heavy and
// unrelated to env churn. Page-parse errors discovered here are
// dropped on the floor: anything systemic (a missing include, bad
// KDL) was already surfaced by the most recent file-watcher reload
// or startup, and rendering keeps working via the include-failed
// placeholder cards.
func reloadPagesForEnv(cfg *config.Config, pagesHandler *pages.Handler, envSnap *env.Snapshot) {
	infos := make([]pages.PageInfo, 0, len(cfg.Pages))
	for _, pg := range cfg.Pages {
		pageCfg, _ := pages.ParsePageFile(pg.File)
		infos = append(infos, pages.PageInfo{
			Name:  pg.Name,
			Alias: pg.Alias,
			Page:  pageCfg,
		})
	}
	pagesHandler.ReloadPages(infos)
	envSnap.SetReferences(unionEnvRefs(infos))
}

// loadPages parses all configured link page files into PageInfo
// structs. Every configured page is registered, even when its KDL
// file is unreadable, syntactically broken, or has malformed nodes —
// keeping the page in the routing table means the operator lands on
// the dashboard with an error banner instead of getting redirected
// to the troubleshooting docs. All collected errors are appended to
// cfg.Errors so they surface in the banner and `subspace validate`
// exits non-zero.
func loadPages(cfg *config.Config) []pages.PageInfo {
	var infos []pages.PageInfo
	for _, pg := range cfg.Pages {
		pageCfg, errs := pages.ParsePageFile(pg.File)
		for _, e := range errs {
			cfg.Errors = append(cfg.Errors, fmt.Sprintf("page %q: %v", pg.File, e))
		}
		infos = append(infos, pages.PageInfo{
			Name:  pg.Name,
			Alias: pg.Alias,
			Page:  pageCfg,
		})
	}
	return infos
}

// tagDefs converts the parsed global tag palette into the form the
// pages handler exposes to the frontend.
func tagDefs(cfg *config.Config) map[string]pages.TagDef {
	out := make(map[string]pages.TagDef, len(cfg.Tags))
	for name, t := range cfg.Tags {
		out[name] = pages.TagDef{Name: t.Name, Alias: t.Alias, Color: t.Color}
	}
	return out
}

// engineDefs converts the parsed external search engines into the form
// the pages handler exposes to the frontend search palette.
func engineDefs(cfg *config.Config) map[string]pages.SearchEngineDef {
	out := make(map[string]pages.SearchEngineDef, len(cfg.SearchEngines))
	for name, e := range cfg.SearchEngines {
		out[name] = pages.SearchEngineDef{
			Name:        e.Name,
			Alias:       e.Alias,
			URL:         e.URL,
			Icon:        e.Icon,
			Description: e.Description,
			Fallback:    e.Fallback,
			URLEncode:   e.URLEncode,
		}
	}
	return out
}
