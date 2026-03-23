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

// IsInternalHost returns true if the hostname is an internal *.subspace page.
func IsInternalHost(hostname string) bool {
	return strings.HasSuffix(hostname, ".subspace")
}

// IsRootHost returns true if the hostname is the subspace.dk entry point.
// This is only intercepted for plain HTTP, not HTTPS/CONNECT.
func IsRootHost(hostname string) bool {
	return hostname == "subspace.dk"
}

// StatusProvider returns upstream health and status as JSON-encodable data.
type StatusProvider func() any

// PageInfo describes a configured page with its parsed content.
type PageInfo struct {
	Host  string      // primary hostname (without .subspace)
	Alias string      // optional alias (without .subspace)
	Page  *PageConfig // parsed page data
}

// navEntry is a single menu item returned by the /api/nav endpoint.
type navEntry struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
	Icon   string `json:"icon,omitempty"`
}

// Handler serves internal pages for *.subspace hostnames.
type Handler struct {
	mu       sync.RWMutex
	mux      *http.ServeMux
	pageList []PageInfo
	// pagesByHost maps "host.subspace" → index into pageList
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
		hosts := []string{lp.Host + ".subspace"}
		if lp.Alias != "" {
			hosts = append(hosts, lp.Alias+".subspace")
		}
		for _, host := range hosts {
			hostMap[host] = idx
			mux.HandleFunc(host+"/api/links", h.handleLinksAPI)
			mux.HandleFunc(host+"/api/nav", h.handleNavAPI)
			mux.Handle(host+"/static/", http.StripPrefix("/static/", fileServer))
			mux.HandleFunc(host+"/", h.handleDashboard)
		}
	}

	// Root redirect: http://subspace/ → first page or stats
	redirectTarget := "http://stats.subspace/"
	if len(pageList) > 0 {
		redirectTarget = "http://" + pageList[0].Host + ".subspace/"
	}
	mux.HandleFunc("subspace.dk/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		http.Redirect(w, r, redirectTarget, http.StatusFound)
	})

	// Statistics routes
	for _, host := range []string{"statistics.subspace", "stats.subspace"} {
		mux.HandleFunc(host+"/api/timeseries", h.handleTimeseriesAPI)
		mux.HandleFunc(host+"/api/snapshot", h.handleSnapshotAPI)
		mux.HandleFunc(host+"/api/status", h.handleStatusAPI)
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
// response directly to the connection.
func (h *Handler) ServeHTTP(conn net.Conn, req *http.Request) {
	h.mu.RLock()
	mux := h.mux
	h.mu.RUnlock()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	resp.Write(conn)
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
		host := lp.Host + ".subspace"
		active := reqHost == host || reqHost == lp.Alias+".subspace"
		nav = append(nav, navEntry{
			Label:  label,
			URL:    "http://" + host + "/",
			Active: active,
		})
	}

	// Statistics is always in the nav
	statsActive := reqHost == "statistics.subspace" || reqHost == "stats.subspace"
	nav = append(nav, navEntry{
		Label:  "Statistics",
		URL:    "http://stats.subspace/",
		Active: statsActive,
		Icon:   "fa-chart-line",
	})

	// External links (icon-only)
	nav = append(nav,
		navEntry{Label: "Documentation", URL: "https://subspace.olrik.dev/", Icon: "fa-book"},
		navEntry{Label: "GitHub", URL: "https://github.com/davidolrik/subspace", Icon: "si-github"},
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nav)
}
