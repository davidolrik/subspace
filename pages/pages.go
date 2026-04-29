package pages

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
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
}

// New creates a pages handler. The collector is used for live stats
// and the store provides historical time-series data.
func New(pageList []PageInfo, collector *stats.Collector, store *stats.Store) *Handler {
	h := &Handler{
		stats: collector,
		store: store,
	}
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
				mux.HandleFunc(host+prefix+"/api/nav", h.handleNavAPI)
				mux.HandleFunc(host+prefix+"/api/config-errors", h.handleConfigErrorsAPI)
				mux.Handle(host+prefix+"/static/", http.StripPrefix(prefix+"/static/", fileServer))
				mux.HandleFunc(host+prefix+"/", h.handleDashboard)
			}
		}
	}

	// Root redirect: pages.subspace.pub/ → first configured page
	rootRedirect := "/" // fallback (won't match anything useful)
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
		mux.HandleFunc(host+"/api/all-links", h.handleAllLinksAPI)
		mux.HandleFunc(host+"/api/nav", h.handleNavAPI)
		mux.HandleFunc(host+"/api/config-errors", h.handleConfigErrorsAPI)
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

// SetConfigErrors replaces the list of non-fatal config errors that
// the internal-pages banner displays. Call after each successful
// (re)load with the freshly-collected errors. Passing nil or an
// empty slice clears the banner. A successful reload also clears
// any prior reload-failure notice.
func (h *Handler) SetConfigErrors(errs []string) {
	h.mu.Lock()
	h.configErrors = append([]string(nil), errs...)
	h.reloadError = ""
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
	Errors []string `json:"errors"`
}

func (h *Handler) handleConfigErrorsAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	combined := make([]string, 0, len(h.configErrors)+1)
	if h.reloadError != "" {
		combined = append(combined, h.reloadError)
	}
	combined = append(combined, h.configErrors...)
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configErrorsResponse{Errors: combined})
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
