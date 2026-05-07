package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.olrik.dev/subspace/stats"
)

// Internal page hosts. Link pages are served under pages.subspace.pub
// (with p.subspace.pub as alias). Statistics stays on its own hostname.
const (
	PagesHost      = "pages.subspace.pub"
	PagesHostAlias = "p.subspace.pub"
	StatsHost      = "statistics.subspace.pub"
	StatsHostAlias = "stats.subspace.pub"
)

// IsInternalHost returns true if the hostname serves internal pages.
func IsInternalHost(hostname string) bool {
	switch hostname {
	case PagesHost, PagesHostAlias, StatsHost, StatsHostAlias:
		return true
	}
	return false
}

// StatusProvider returns upstream health and status as JSON-encodable data.
type StatusProvider func() any

// PageInfo describes a configured page with its parsed content.
type PageInfo struct {
	Name  string      // primary page name (path segment under pages.subspace.pub)
	Alias string      // optional alias page name
	Page  *PageConfig // parsed page data
}

// TagDef is a single tag definition exposed to the frontend so it can
// render colored pills for tag references on links and lists. Alias is
// the label displayed on the pill (defaults to Name when unset).
type TagDef struct {
	Name  string `json:"Name"`
	Alias string `json:"Alias,omitempty"`
	Color string `json:"Color"`
}

// SearchEngineDef is a single external search engine exposed to the
// frontend search palette. The dashboard uses Name (and optional Alias)
// as the inline keyword, and substitutes the user's query into URL by
// replacing the literal "{query}" placeholder. Fallback opts the
// engine into the no-match fallback list.
type SearchEngineDef struct {
	Name        string `json:"name"`
	Alias       string `json:"alias,omitempty"`
	URL         string `json:"url"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	Fallback    bool   `json:"fallback,omitempty"`
	URLEncode   string `json:"urlEncode,omitempty"`
}

// searchEnginesResponse is the JSON shape returned by /api/search-engines.
type searchEnginesResponse struct {
	Engines []SearchEngineDef `json:"engines"`
	Default string            `json:"default,omitempty"`
}

// linksResponse is the JSON shape returned by the /api/links endpoint.
// It embeds the page configuration and adds the tag color map so the
// frontend can render tag pills without a second round-trip.
type linksResponse struct {
	*PageConfig
	Tags map[string]TagDef `json:"Tags,omitempty"`
}

// searchLink is a single link returned by the /api/all-links endpoint,
// annotated with the page and section it belongs to.
type searchLink struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Icon        string   `json:"icon,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Page        string   `json:"page"`
	Section     string   `json:"section"`
}

// navEntry is a single menu item returned by the /api/nav endpoint.
type navEntry struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Active bool  `json:"active"`
	Icon  string `json:"icon,omitempty"`
	Name  string `json:"name,omitempty"`
	Alias string `json:"alias,omitempty"`
}

// Handler serves internal pages at pages.subspace.pub and stats.subspace.pub.
type Handler struct {
	mu       sync.RWMutex
	mux      *http.ServeMux
	pageList []PageInfo
	// pagesByName maps page name → index into pageList
	pagesByName map[string]int
	stats       *stats.Collector
	store       *stats.Store
	status      StatusProvider
	tags        map[string]TagDef
	// searchEngines maps engine name → definition, exposed to the
	// frontend search palette via /api/search-engines.
	// defaultEngine names the engine to use as the no-match fallback row.
	searchEngines map[string]SearchEngineDef
	defaultEngine string
	// faviconCache stores fetched favicons keyed by host so we don't
	// hammer the engine origin once per dashboard tab. Both successful
	// fetches and upstream failures are cached (with different TTLs)
	// so a missing favicon doesn't translate into a fetch on every
	// page load.
	faviconCache map[string]faviconEntry
	// faviconFetcher is overridable for tests; the default issues an
	// HTTP GET against the engine host.
	faviconFetcher func(ctx context.Context, scheme, host string) faviconEntry
	// configErrors is the list of non-fatal config problems surfaced
	// in the internal-pages banner. It's overwritten by
	// SetConfigErrors after each successful (re)load.
	configErrors []string
	// reloadError is set by SetReloadError when a config reload fails
	// to parse. It's prepended to configErrors in the API response so
	// the operator sees that the *current* config is stale relative
	// to what's on disk. Cleared by SetConfigErrors on a successful
	// reload.
	reloadError string
	// configVersion increments on every successful (re)load. The
	// frontend polls /api/config-errors, records this value on first
	// fetch, and triggers a hard reload when it changes — so an
	// operator editing the config doesn't need to remember to refresh
	// each open dashboard tab.
	configVersion uint64
	// blackholeRoutes lists the route patterns currently pointing at
	// the built-in blackhole pseudo-upstream (via or fallback). Driven
	// by SetBlackholeRoutes during config (re)load and surfaced via
	// /api/blackhole so the dashboard only renders the "Blocked Traffic"
	// card when the user actually has blackhole rules configured.
	blackholeRoutes []string
}

// New creates a pages handler. The collector is used for live stats
// and the store provides historical time-series data.
func New(pageList []PageInfo, collector *stats.Collector, store *stats.Store) *Handler {
	h := &Handler{
		stats:        collector,
		store:        store,
		faviconCache: make(map[string]faviconEntry),
	}
	h.faviconFetcher = h.realFetchFavicon
	h.buildMux(pageList)
	return h
}

// buildMux creates a new ServeMux with routes for all configured pages.
func (h *Handler) buildMux(pageList []PageInfo) {
	mux := http.NewServeMux()
	nameMap := make(map[string]int)

	frontendFS, _ := fs.Sub(frontend, "frontend")
	fileServer := http.FileServer(http.FS(frontendFS))

	// Register link page routes under pages.subspace.pub/{name}/...
	for i, lp := range pageList {
		names := []string{lp.Name}
		if lp.Alias != "" {
			names = append(names, lp.Alias)
		}
		for _, name := range names {
			nameMap[name] = i
			for _, host := range []string{PagesHost, PagesHostAlias} {
				prefix := "/" + name
				mux.HandleFunc(host+prefix+"/api/links", h.handleLinksAPI)
				mux.HandleFunc(host+prefix+"/api/all-links", h.handleAllLinksAPI)
				mux.HandleFunc(host+prefix+"/api/search-engines", h.handleSearchEnginesAPI)
				mux.HandleFunc(host+prefix+"/api/favicon", h.handleFaviconAPI)
				mux.HandleFunc(host+prefix+"/api/nav", h.handleNavAPI)
				mux.HandleFunc(host+prefix+"/api/config-errors", h.handleConfigErrorsAPI)
				mux.Handle(host+prefix+"/static/", http.StripPrefix(prefix+"/static/", fileServer))
				mux.HandleFunc(host+prefix+"/", h.handleDashboard)
			}
		}
	}

	// Root redirect: pages.subspace.pub/ → first configured page,
	// or to the statistics dashboard when no pages are defined (which
	// is otherwise a redirect loop, since the fallback would point back
	// at this same handler). HTTP because internal pages are HTTP-only.
	rootRedirect := "http://" + StatsHost + "/"
	if len(pageList) > 0 {
		rootRedirect = "/" + pageList[0].Name + "/"
	}
	for _, host := range []string{PagesHost, PagesHostAlias} {
		target := rootRedirect // capture for closure
		mux.HandleFunc(host+"/", func(w http.ResponseWriter, r *http.Request) {
			// Only redirect the exact root path; other paths are unknown pages
			if r.URL.Path != "/" {
				http.Redirect(w, r, "https://subspace.pub/guide/troubleshooting?host="+r.Host+r.URL.Path+"#page-not-defined", http.StatusFound)
				return
			}
			http.Redirect(w, r, target, http.StatusFound)
		})
	}

	// Statistics routes (host-based, unchanged)
	for _, host := range []string{StatsHost, StatsHostAlias} {
		mux.HandleFunc(host+"/api/timeseries", h.handleTimeseriesAPI)
		mux.HandleFunc(host+"/api/snapshot", h.handleSnapshotAPI)
		mux.HandleFunc(host+"/api/status", h.handleStatusAPI)
		mux.HandleFunc(host+"/api/top", h.handleTopAPI)
		mux.HandleFunc(host+"/api/all-links", h.handleAllLinksAPI)
		mux.HandleFunc(host+"/api/search-engines", h.handleSearchEnginesAPI)
		mux.HandleFunc(host+"/api/favicon", h.handleFaviconAPI)
		mux.HandleFunc(host+"/api/nav", h.handleNavAPI)
		mux.HandleFunc(host+"/api/config-errors", h.handleConfigErrorsAPI)
		mux.HandleFunc(host+"/api/blackhole", h.handleBlackholeAPI)
		mux.HandleFunc(host+"/api/blackhole/top", h.handleBlackholeTopAPI)
		mux.Handle(host+"/static/", http.StripPrefix("/static/", fileServer))
		mux.HandleFunc(host+"/", h.handleStatistics)
	}

	h.mu.Lock()
	h.mux = mux
	h.pageList = pageList
	h.pagesByName = nameMap
	h.mu.Unlock()
}

// SetStatusProvider sets the function used by the /api/status endpoint
// to return upstream health data.
func (h *Handler) SetStatusProvider(fn StatusProvider) {
	h.mu.Lock()
	h.status = fn
	h.mu.Unlock()
}

// SetTags installs the global tag color palette used to render pills
// in the page UI. The map is read by /api/links responses and by
// ValidateTagReferences.
func (h *Handler) SetTags(tags map[string]TagDef) {
	h.mu.Lock()
	h.tags = tags
	h.mu.Unlock()
}

// SetSearchEngines installs the configured external search engines
// surfaced by the dashboard search palette via /api/search-engines.
// The defaultName parameter is the engine the frontend renders as the
// no-match fallback row; pass "" to disable the fallback.
func (h *Handler) SetSearchEngines(engines map[string]SearchEngineDef, defaultName string) {
	h.mu.Lock()
	h.searchEngines = engines
	h.defaultEngine = defaultName
	h.mu.Unlock()
}

// SetConfigErrors replaces the list of non-fatal config errors that
// the internal-pages banner displays. Call after each successful
// (re)load with the freshly-collected errors. Passing nil or an
// empty slice clears the banner. A successful reload also clears
// any prior reload-failure notice and bumps the config version that
// /api/config-errors reports so open dashboards force-reload.
func (h *Handler) SetConfigErrors(errs []string) {
	h.mu.Lock()
	h.configErrors = append([]string(nil), errs...)
	h.reloadError = ""
	h.configVersion++
	h.mu.Unlock()
}

// SetReloadError records that the most recent config reload failed
// to parse, while the previous (good) config remains in effect. The
// message is prepended to the banner so the operator notices the
// drift; it's cleared on the next successful reload.
func (h *Handler) SetReloadError(msg string) {
	h.mu.Lock()
	h.reloadError = msg
	h.mu.Unlock()
}

// SetBlackholeRoutes records the patterns currently routed to the
// blackhole pseudo-upstream (either as `via=` or `fallback=`). The
// statistics dashboard shows a dedicated "Blocked Traffic" card only
// when this list is non-empty so users without any blackhole rules
// don't see an empty section. Call after each successful (re)load.
func (h *Handler) SetBlackholeRoutes(patterns []string) {
	h.mu.Lock()
	h.blackholeRoutes = append([]string(nil), patterns...)
	h.mu.Unlock()
}

// ValidateTagReferences walks every loaded page and returns one entry
// per link or list that references a tag not present in the configured
// palette. The frontend already falls back to a neutral color for
// unknown tags, so the page still renders — these messages are
// surfaced in the config error banner so the operator notices.
// Intended to be called after SetTags at startup and after each
// successful page reload.
func (h *Handler) ValidateTagReferences() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var errs []string
	for _, lp := range h.pageList {
		if lp.Page == nil {
			continue
		}
		for _, section := range lp.Page.Sections {
			for _, tag := range section.Tags {
				if _, ok := h.tags[tag]; !ok {
					errs = append(errs, fmt.Sprintf("page %q section %q references unknown tag %q", lp.Name, section.Name, tag))
				}
			}
			for _, link := range section.Links {
				for _, tag := range link.Tags {
					if _, ok := h.tags[tag]; !ok {
						errs = append(errs, fmt.Sprintf("page %q section %q link %q references unknown tag %q", lp.Name, section.Name, link.Name, tag))
					}
				}
			}
		}
	}
	return errs
}

// ReloadPages rebuilds the mux with a new set of link pages.
func (h *Handler) ReloadPages(pages []PageInfo) {
	h.buildMux(pages)
}

// IncludedFiles returns the absolute paths of every file pulled in by
// a `markdown include="..."` across all currently loaded pages. Used
// by the config watcher so editing an included markdown file triggers
// a page reload. Paths for missing includes are returned too — the
// file might appear later, and the watcher subscribes to its parent
// directory regardless.
func (h *Handler) IncludedFiles() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []string
	for _, lp := range h.pageList {
		if lp.Page == nil {
			continue
		}
		for _, item := range lp.Page.Items {
			if item.Markdown != nil && item.Markdown.IncludePath != "" {
				out = append(out, item.Markdown.IncludePath)
			}
		}
	}
	return out
}

// ServeHTTP handles an internal page request by writing the HTTP
// response directly to the connection.
func (h *Handler) ServeHTTP(conn net.Conn, req *http.Request) {
	h.mu.RLock()
	mux := h.mux
	h.mu.RUnlock()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Prevent browsers from caching internal page responses so they
	// don't replay stale redirects from the external fallback server.
	rec.Header().Set("Cache-Control", "no-store")

	resp := rec.Result()
	defer resp.Body.Close()
	resp.Write(conn)
}

// pageName extracts the page name from the request URL path.
// For a path like "/dev/api/links", it returns "dev".
func pageName(r *http.Request) string {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		return p[:i]
	}
	return p
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := frontend.ReadFile("frontend/index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (h *Handler) handleStatistics(w http.ResponseWriter, r *http.Request) {
	data, err := frontend.ReadFile("frontend/statistics.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleTopAPI returns Top-N entries for upstreams, domains, or routes
// over a window. Query parameters:
//   - kind:     "upstreams" (default), "domains", "routes"
//   - metric:   "bytes_total" (default), "success", "failures",
//               "bytes_in", "bytes_out"
//   - duration: window in seconds (default 86400 = 24h)
//   - n:        max entries (default 10, capped at 100)
func (h *Handler) handleTopAPI(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.Error(w, "statistics not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()

	kind := q.Get("kind")
	if kind == "" {
		kind = "upstreams"
	}

	metric := q.Get("metric")
	if metric == "" {
		metric = "bytes_total"
	}

	duration := 24 * time.Hour
	if d := q.Get("duration"); d != "" {
		if seconds, err := strconv.Atoi(d); err == nil && seconds > 0 {
			duration = time.Duration(seconds) * time.Second
		}
	}

	limit := 10
	if n := q.Get("n"); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100
	}

	to := time.Now()
	from := to.Add(-duration)

	var (
		top []stats.TopEntry
		err error
	)
	switch kind {
	case "upstreams":
		top, err = h.store.TopUpstreams(from, to, metric, limit)
	case "domains":
		top, err = h.store.TopDomains(from, to, metric, limit)
	case "routes":
		top, err = h.store.TopRoutes(from, to, metric, limit)
	default:
		http.Error(w, "unknown kind (want upstreams, domains, or routes)", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if top == nil {
		top = []stats.TopEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":   kind,
		"metric": metric,
		"window": int(duration.Seconds()),
		"limit":  limit,
		"top":    top,
	})
}

func (h *Handler) handleTimeseriesAPI(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.Error(w, "statistics not available", http.StatusServiceUnavailable)
		return
	}

	duration := time.Hour
	if d := r.URL.Query().Get("duration"); d != "" {
		if seconds, err := strconv.Atoi(d); err == nil && seconds > 0 {
			duration = time.Duration(seconds) * time.Second
		}
	}

	from := time.Now().Add(-duration)
	to := time.Now()

	series, err := h.store.Query(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(series)
}

func (h *Handler) handleStatusAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	statusFn := h.status
	h.mu.RUnlock()

	if statusFn == nil {
		http.Error(w, "status not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusFn())
}

func (h *Handler) handleSnapshotAPI(w http.ResponseWriter, r *http.Request) {
	if h.stats == nil {
		http.Error(w, "statistics not available", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.stats.Snapshot())
}

func (h *Handler) handleLinksAPI(w http.ResponseWriter, r *http.Request) {
	name := pageName(r)

	h.mu.RLock()
	idx, ok := h.pagesByName[name]
	var pageCfg *PageConfig
	if ok && idx < len(h.pageList) {
		pageCfg = h.pageList[idx].Page
	}
	tags := h.tags
	h.mu.RUnlock()

	if pageCfg == nil {
		pageCfg = &PageConfig{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(linksResponse{PageConfig: pageCfg, Tags: tags})
}

func (h *Handler) handleNavAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	pages := h.pageList
	h.mu.RUnlock()

	reqName := pageName(r)
	reqHost := r.Host
	var nav []navEntry

	for _, lp := range pages {
		label := lp.Name
		if lp.Page != nil && lp.Page.Title != "" {
			label = lp.Page.Title
		}
		active := reqName == lp.Name || reqName == lp.Alias
		nav = append(nav, navEntry{
			Label:  label,
			URL:    "http://" + PagesHost + "/" + lp.Name + "/",
			Active: active,
			Name:   lp.Name,
			Alias:  lp.Alias,
		})
	}

	// Statistics is always in the nav
	statsActive := reqHost == StatsHost || reqHost == StatsHostAlias
	nav = append(nav, navEntry{
		Label:  "Statistics",
		URL:    "http://" + StatsHost + "/",
		Active: statsActive,
		Icon:   "fa-chart-line",
		Name:   "stats",
		Alias:  "statistics",
	})

	// External links (icon-only)
	nav = append(nav,
		navEntry{Label: "Documentation", URL: "https://subspace.pub/", Icon: "fa-book"},
		navEntry{Label: "GitHub", URL: "https://github.com/davidolrik/subspace", Icon: "si-github"},
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nav)
}

// configErrorsResponse is the JSON shape returned by /api/config-errors.
type configErrorsResponse struct {
	Errors  []string `json:"errors"`
	Version uint64   `json:"version"`
}

func (h *Handler) handleConfigErrorsAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	combined := make([]string, 0, len(h.configErrors)+1)
	if h.reloadError != "" {
		combined = append(combined, h.reloadError)
	}
	combined = append(combined, h.configErrors...)
	version := h.configVersion
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configErrorsResponse{Errors: combined, Version: version})
}

// blackholeResponse describes the blackhole-route configuration plus
// the live drop counters so the dashboard can render its "Blocked
// Traffic" card from a single fetch.
type blackholeResponse struct {
	Active   bool                `json:"active"`
	Patterns []string            `json:"patterns,omitempty"`
	Stats    *stats.UpstreamStats `json:"stats,omitempty"`
}

func (h *Handler) handleBlackholeAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	patterns := append([]string(nil), h.blackholeRoutes...)
	collector := h.stats
	h.mu.RUnlock()

	resp := blackholeResponse{
		Active:   len(patterns) > 0,
		Patterns: patterns,
	}
	if collector != nil {
		snap := collector.Snapshot()
		if us, ok := snap.Upstreams["blackhole"]; ok {
			resp.Stats = &us
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleBlackholeTopAPI returns the top-N route patterns currently
// pointing at the blackhole pseudo-upstream, ranked by the requested
// metric over the requested window. Uses the same time-windowed
// differential as /api/top so the numbers agree with the regular
// "Top Routes" card; differs only in that the result set is filtered
// to blackhole patterns server-side, which keeps the answer correct
// even when blackhole patterns fall outside the global top-N.
//
// Query parameters mirror /api/top:
//   - metric:   "bytes_total" (default), "success", "failures",
//               "bytes_in", "bytes_out"
//   - duration: window in seconds (default 86400 = 24h)
//   - n:        max entries (default 10, capped at 100)
func (h *Handler) handleBlackholeTopAPI(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		http.Error(w, "statistics not available", http.StatusServiceUnavailable)
		return
	}

	h.mu.RLock()
	patterns := append([]string(nil), h.blackholeRoutes...)
	h.mu.RUnlock()

	q := r.URL.Query()

	metric := q.Get("metric")
	if metric == "" {
		metric = "bytes_total"
	}

	duration := 24 * time.Hour
	if d := q.Get("duration"); d != "" {
		if seconds, err := strconv.Atoi(d); err == nil && seconds > 0 {
			duration = time.Duration(seconds) * time.Second
		}
	}

	limit := 10
	if n := q.Get("n"); n != "" {
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100
	}

	to := time.Now()
	from := to.Add(-duration)

	top, err := h.store.TopRoutesIn(from, to, metric, limit, patterns)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if top == nil {
		top = []stats.TopEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"kind":   "blackhole",
		"metric": metric,
		"window": int(duration.Seconds()),
		"limit":  limit,
		"top":    top,
	})
}

func (h *Handler) handleSearchEnginesAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	engines := make([]SearchEngineDef, 0, len(h.searchEngines))
	for _, e := range h.searchEngines {
		engines = append(engines, e)
	}
	defaultEngine := h.defaultEngine
	h.mu.RUnlock()

	// Stable order so the frontend can render engines deterministically
	// in the no-match fallback list and so tests aren't flaky.
	sort.Slice(engines, func(i, j int) bool { return engines[i].Name < engines[j].Name })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(searchEnginesResponse{Engines: engines, Default: defaultEngine})
}

func (h *Handler) handleAllLinksAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	pages := h.pageList
	h.mu.RUnlock()

	var links []searchLink
	for _, lp := range pages {
		if lp.Page == nil {
			continue
		}
		pageLabel := lp.Name
		if lp.Page.Title != "" {
			pageLabel = lp.Page.Title
		}
		for _, section := range lp.Page.Sections {
			for _, link := range section.Links {
				links = append(links, searchLink{
					Name:        link.Name,
					URL:         link.URL,
					Icon:        link.Icon,
					Description: link.Description,
					Tags:        link.Tags,
					Page:        pageLabel,
					Section:     section.Name,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

// faviconEntry is one cached favicon. Empty Body + non-2xx Status
// indicates a negative cache record (engine had no /favicon.ico or the
// fetch otherwise failed) so the client gets a 404 without us hitting
// the engine again.
type faviconEntry struct {
	Body        []byte
	ContentType string
	Status      int
	FetchedAt   time.Time
}

const (
	faviconPositiveTTL = 24 * time.Hour
	faviconNegativeTTL = 1 * time.Hour
	faviconMaxBytes    = 256 * 1024 // arbitrary cap; favicons should be tiny
	faviconFetchTimeout = 5 * time.Second
)

// handleFaviconAPI proxies favicon requests for engine hosts. The
// response is cached in memory so subsequent dashboard tabs don't
// re-fetch from the engine origin. We also set a long Cache-Control
// header so the browser caches the bytes for the session even after
// the in-memory cache evicts them.
//
// The host parameter is validated against the configured engine list
// to keep the endpoint from being used as an arbitrary favicon proxy.
func (h *Handler) handleFaviconAPI(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	if host == "" {
		http.Error(w, "missing host parameter", http.StatusBadRequest)
		return
	}

	scheme, ok := h.engineSchemeForHost(host)
	if !ok {
		http.Error(w, "host is not a configured search engine", http.StatusForbidden)
		return
	}

	// Cache lookup: serve immediately if we have a fresh entry,
	// regardless of whether it was a positive or negative result.
	if entry, fresh := h.lookupFavicon(host); fresh {
		writeFaviconResponse(w, entry)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), faviconFetchTimeout)
	defer cancel()
	entry := h.faviconFetcher(ctx, scheme, host)
	entry.FetchedAt = time.Now()
	h.storeFavicon(host, entry)
	writeFaviconResponse(w, entry)
}

func (h *Handler) engineSchemeForHost(host string) (string, bool) {
	h.mu.RLock()
	engines := h.searchEngines
	h.mu.RUnlock()
	hostLower := strings.ToLower(host)
	for _, e := range engines {
		u, err := url.Parse(e.URL)
		if err != nil {
			continue
		}
		if strings.ToLower(u.Host) == hostLower {
			return u.Scheme, true
		}
	}
	return "", false
}

func (h *Handler) lookupFavicon(host string) (faviconEntry, bool) {
	h.mu.RLock()
	entry, ok := h.faviconCache[host]
	h.mu.RUnlock()
	if !ok {
		return faviconEntry{}, false
	}
	ttl := faviconPositiveTTL
	if entry.Status < 200 || entry.Status >= 300 {
		ttl = faviconNegativeTTL
	}
	if time.Since(entry.FetchedAt) > ttl {
		return faviconEntry{}, false
	}
	return entry, true
}

func (h *Handler) storeFavicon(host string, entry faviconEntry) {
	h.mu.Lock()
	if h.faviconCache == nil {
		h.faviconCache = map[string]faviconEntry{}
	}
	h.faviconCache[host] = entry
	h.mu.Unlock()
}

// realFetchFavicon performs the actual HTTP GET against the engine
// host's /favicon.ico. Errors and non-2xx responses are recorded as
// negative cache entries so we don't keep retrying.
func (h *Handler) realFetchFavicon(ctx context.Context, scheme, host string) faviconEntry {
	client := &http.Client{Timeout: faviconFetchTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+host+"/favicon.ico", nil)
	if err != nil {
		return faviconEntry{Status: http.StatusBadGateway}
	}
	req.Header.Set("User-Agent", "subspace-favicon-cache/1")
	resp, err := client.Do(req)
	if err != nil {
		return faviconEntry{Status: http.StatusBadGateway}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return faviconEntry{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes))
	if err != nil {
		return faviconEntry{Status: http.StatusBadGateway}
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/x-icon"
	}
	return faviconEntry{
		Body:        body,
		ContentType: ct,
		Status:      http.StatusOK,
	}
}

func writeFaviconResponse(w http.ResponseWriter, entry faviconEntry) {
	if entry.Status < 200 || entry.Status >= 300 || len(entry.Body) == 0 {
		// Cache the negative result in the browser too, but with the
		// same shorter TTL we use server-side so a freshly-published
		// favicon still gets picked up after an hour.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.Error(w, "favicon unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(entry.Body)
}
