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

// IsInternalHost returns true if the hostname is an internal *.subspace.pub page.
func IsInternalHost(hostname string) bool {
	return strings.HasSuffix(hostname, ".subspace.pub") && hostname != "subspace.pub"
}

// StatusProvider returns upstream health and status as JSON-encodable data.
type StatusProvider func() any

// PageInfo describes a configured page with its parsed content.
type PageInfo struct {
	Host  string      // primary hostname (without .subspace.pub)
	Alias string      // optional alias (without .subspace.pub)
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
	Label  string `json:"label"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
	Icon   string `json:"icon,omitempty"`
	Host   string `json:"host,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// Handler serves internal pages for *.subspace.pub hostnames.
type Handler struct {
	mu       sync.RWMutex
	mux      *http.ServeMux
	pageList []PageInfo
	// pagesByHost maps "host.subspace.pub" → index into pageList
	pagesByHost map[string]int
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
	hostMap := make(map[string]int)

	frontendFS, _ := fs.Sub(frontend, "frontend")
	fileServer := http.FileServer(http.FS(frontendFS))

	// Register link page routes
	for i, lp := range pageList {
		idx := i // capture for closure
		hosts := []string{lp.Host + ".subspace.pub"}
		if lp.Alias != "" {
			hosts = append(hosts, lp.Alias+".subspace.pub")
		}
		for _, host := range hosts {
			hostMap[host] = idx
			mux.HandleFunc(host+"/api/links", h.handleLinksAPI)
			mux.HandleFunc(host+"/api/all-links", h.handleAllLinksAPI)
			mux.HandleFunc(host+"/api/nav", h.handleNavAPI)
			mux.Handle(host+"/static/", http.StripPrefix("/static/", fileServer))
			mux.HandleFunc(host+"/", h.handleDashboard)
		}
	}

	// Statistics routes
	for _, host := range []string{"statistics.subspace.pub", "stats.subspace.pub"} {
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
	h.pagesByHost = hostMap
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
// response directly to the connection. If the host has no configured
// page, it redirects to the documentation.
func (h *Handler) ServeHTTP(conn net.Conn, req *http.Request) {
	h.mu.RLock()
	mux := h.mux
	_, known := h.pagesByHost[req.Host]
	h.mu.RUnlock()

	// Redirect unknown *.subspace.pub hosts to the troubleshooting page
	if !known && !isStatsHost(req.Host) {
		rec := httptest.NewRecorder()
		http.Redirect(rec, req, "https://subspace.pub/guide/troubleshooting?host="+req.Host+"#page-not-defined", http.StatusFound)
		resp := rec.Result()
		defer resp.Body.Close()
		resp.Write(conn)
		return
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	resp.Write(conn)
}

func isStatsHost(host string) bool {
	return host == "stats.subspace.pub" || host == "statistics.subspace.pub"
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
	h.mu.RLock()
	idx, ok := h.pagesByHost[r.Host]
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

	reqHost := r.Host
	var nav []navEntry

	for _, lp := range pages {
		label := lp.Host
		if lp.Page != nil && lp.Page.Title != "" {
			label = lp.Page.Title
		}
		host := lp.Host + ".subspace.pub"
		active := reqHost == host || reqHost == lp.Alias+".subspace"
		nav = append(nav, navEntry{
			Label:  label,
			URL:    "http://" + host + "/",
			Active: active,
			Host:   lp.Host,
			Alias:  lp.Alias,
		})
	}

	// Statistics is always in the nav
	statsActive := reqHost == "statistics.subspace.pub" || reqHost == "stats.subspace.pub"
	nav = append(nav, navEntry{
		Label:  "Statistics",
		URL:    "http://stats.subspace.pub/",
		Active: statsActive,
		Icon:   "fa-chart-line",
		Host:   "stats",
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
		pageLabel := lp.Host
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
