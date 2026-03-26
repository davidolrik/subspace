package pages

import (
	"encoding/json"
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

// searchLink is a single link returned by the /api/all-links endpoint,
// annotated with the page and section it belongs to.
type searchLink struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	Page        string `json:"page"`
	Section     string `json:"section"`
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
	h.mu.RUnlock()

	if pageCfg == nil {
		pageCfg = &PageConfig{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pageCfg)
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
					Page:        pageLabel,
					Section:     section.Name,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}
