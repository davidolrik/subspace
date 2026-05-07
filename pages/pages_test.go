package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.olrik.dev/subspace/stats"
)

func TestIsInternalHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"pages.subspace.pub", true},
		{"p.subspace.pub", true},
		{"stats.subspace.pub", true},
		{"statistics.subspace.pub", true},
		{"subspace.pub", false},
		{"dashboard.subspace.pub", false},
		{"example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsInternalHost(tt.host)
		if got != tt.want {
			t.Errorf("IsInternalHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestNavAPIIncludesHostAndAlias(t *testing.T) {
	pages := []PageInfo{
		{
			Name:  "dev",
			Alias: "d",
			Page:  &PageConfig{Title: "Development"},
		},
		{
			Name: "ops",
			Page: &PageConfig{Title: "Operations"},
		},
	}

	h := New(pages, nil, nil)

	// Request nav from pages.subspace.pub/dev/
	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/nav", nil)
	rec := httptest.NewRecorder()
	h.handleNavAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var nav []navEntry
	if err := json.NewDecoder(rec.Body).Decode(&nav); err != nil {
		t.Fatalf("decoding nav: %v", err)
	}

	// Should have: dev, ops, statistics, docs, github = 5 entries
	if len(nav) < 3 {
		t.Fatalf("expected at least 3 nav entries, got %d", len(nav))
	}

	// First entry (dev) should have host and alias
	dev := nav[0]
	if dev.Name != "dev" {
		t.Errorf("dev entry: name = %q, want %q", dev.Name, "dev")
	}
	if dev.Alias != "d" {
		t.Errorf("dev entry: alias = %q, want %q", dev.Alias, "d")
	}
	if dev.Label != "Development" {
		t.Errorf("dev entry: label = %q, want %q", dev.Label, "Development")
	}
	if !dev.Active {
		t.Error("dev entry: should be active for request to /dev/")
	}

	// Second entry (ops) should have host but no alias
	ops := nav[1]
	if ops.Name != "ops" {
		t.Errorf("ops entry: name = %q, want %q", ops.Name, "ops")
	}
	if ops.Alias != "" {
		t.Errorf("ops entry: alias = %q, want empty", ops.Alias)
	}

	// Statistics entry should have host "stats"
	stats := nav[2]
	if stats.Label != "Statistics" {
		t.Errorf("stats entry: label = %q, want %q", stats.Label, "Statistics")
	}
	if stats.Name != "stats" {
		t.Errorf("stats entry: name = %q, want %q", stats.Name, "stats")
	}
	if stats.Alias != "statistics" {
		t.Errorf("stats entry: alias = %q, want %q", stats.Alias, "statistics")
	}
}

func TestAllLinksAPI(t *testing.T) {
	pages := []PageInfo{
		{
			Name:  "dev",
			Alias: "d",
			Page: &PageConfig{
				Title: "Development",
				Sections: []ListSection{
					{
						Name: "Repos",
						Links: []Link{
							{Name: "GitHub", URL: "https://github.com/org", Icon: "si-github"},
							{Name: "CI", URL: "https://ci.example.com", Description: "Build pipelines"},
						},
					},
				},
			},
		},
		{
			Name: "ops",
			Page: &PageConfig{
				Title: "Operations",
				Sections: []ListSection{
					{
						Name: "Monitoring",
						Links: []Link{
							{Name: "Grafana", URL: "https://grafana.example.com", Icon: "fa-chart-line"},
						},
					},
				},
			},
		},
	}

	h := New(pages, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/all-links", nil)
	rec := httptest.NewRecorder()
	h.handleAllLinksAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var links []searchLink
	if err := json.NewDecoder(rec.Body).Decode(&links); err != nil {
		t.Fatalf("decoding all-links: %v", err)
	}

	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}

	if links[0].Page != "Development" {
		t.Errorf("link 0: page = %q, want %q", links[0].Page, "Development")
	}
	if links[0].Name != "GitHub" {
		t.Errorf("link 0: name = %q, want %q", links[0].Name, "GitHub")
	}
	if links[0].Section != "Repos" {
		t.Errorf("link 0: section = %q, want %q", links[0].Section, "Repos")
	}

	if links[1].Description != "Build pipelines" {
		t.Errorf("link 1: description = %q, want %q", links[1].Description, "Build pipelines")
	}

	if links[2].Page != "Operations" {
		t.Errorf("link 2: page = %q, want %q", links[2].Page, "Operations")
	}
	if links[2].Name != "Grafana" {
		t.Errorf("link 2: name = %q, want %q", links[2].Name, "Grafana")
	}
}

func TestUndefinedPageRedirectsToDocs(t *testing.T) {
	pages := []PageInfo{
		{Name: "dev", Page: &PageConfig{Title: "Development"}},
	}
	h := New(pages, nil, nil)

	// Request an undefined page path
	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/unknown/", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}
	if !contains(loc, "troubleshooting") || !contains(loc, "page-not-defined") {
		t.Errorf("Location = %q, want troubleshooting redirect", loc)
	}
}

func TestRootRedirectsToFirstPage(t *testing.T) {
	pages := []PageInfo{
		{Name: "dev", Page: &PageConfig{Title: "Development"}},
		{Name: "ops", Page: &PageConfig{Title: "Operations"}},
	}
	h := New(pages, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	if loc != "/dev/" {
		t.Errorf("Location = %q, want %q", loc, "/dev/")
	}
}

func TestValidateTagReferencesOK(t *testing.T) {
	pageInfos := []PageInfo{
		{
			Name: "dev",
			Page: &PageConfig{
				Sections: []ListSection{
					{
						Name: "Repos",
						Tags: []string{"prod"},
						Links: []Link{
							{Name: "GitHub", URL: "https://github.com", Tags: []string{"prod", "internal"}},
						},
					},
				},
			},
		},
	}
	h := New(pageInfos, nil, nil)
	h.SetTags(map[string]TagDef{
		"prod":     {Name: "prod", Color: "#00ff88"},
		"internal": {Name: "internal", Color: "#ff6b6b"},
	})

	if errs := h.ValidateTagReferences(); len(errs) != 0 {
		t.Fatalf("ValidateTagReferences() = %v, want nil", errs)
	}
}

func TestValidateTagReferencesUnknownLink(t *testing.T) {
	pageInfos := []PageInfo{
		{
			Name: "dev",
			Page: &PageConfig{
				Sections: []ListSection{
					{
						Name: "Repos",
						Links: []Link{
							{Name: "GitHub", URL: "https://github.com", Tags: []string{"ghost"}},
						},
					},
				},
			},
		},
	}
	h := New(pageInfos, nil, nil)
	h.SetTags(map[string]TagDef{"prod": {Name: "prod", Color: "#00ff88"}})

	errs := h.ValidateTagReferences()
	if len(errs) == 0 {
		t.Fatal("expected an error for unknown tag reference")
	}
	for _, want := range []string{"ghost", "GitHub", "Repos", "dev"} {
		if !anyContains(errs, want) {
			t.Errorf("errors %v should mention %q", errs, want)
		}
	}
}

func TestValidateTagReferencesUnknownList(t *testing.T) {
	pageInfos := []PageInfo{
		{
			Name: "dev",
			Page: &PageConfig{
				Sections: []ListSection{
					{
						Name: "Repos",
						Tags: []string{"phantom"},
						Links: []Link{
							{Name: "GitHub", URL: "https://github.com"},
						},
					},
				},
			},
		},
	}
	h := New(pageInfos, nil, nil)
	h.SetTags(map[string]TagDef{})

	errs := h.ValidateTagReferences()
	if len(errs) == 0 {
		t.Fatal("expected an error for unknown tag reference on list")
	}
	for _, want := range []string{"phantom", "Repos", "dev"} {
		if !anyContains(errs, want) {
			t.Errorf("errors %v should mention %q", errs, want)
		}
	}
}

func TestValidateTagReferencesCollectsAll(t *testing.T) {
	pageInfos := []PageInfo{
		{
			Name: "dev",
			Page: &PageConfig{
				Sections: []ListSection{
					{
						Name: "Repos",
						Tags: []string{"phantom"},
						Links: []Link{
							{Name: "GitHub", URL: "https://github.com", Tags: []string{"ghost"}},
						},
					},
				},
			},
		},
	}
	h := New(pageInfos, nil, nil)
	h.SetTags(map[string]TagDef{})

	errs := h.ValidateTagReferences()
	if len(errs) != 2 {
		t.Fatalf("expected 2 collected errors (list + link), got %d: %v", len(errs), errs)
	}
}

func anyContains(errs []string, sub string) bool {
	for _, e := range errs {
		if contains(e, sub) {
			return true
		}
	}
	return false
}

func TestConfigErrorsAPI(t *testing.T) {
	h := New(nil, nil, nil)
	h.SetConfigErrors([]string{"route X bad", "tag Y missing"})

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/config-errors", nil)
	rec := httptest.NewRecorder()
	h.handleConfigErrorsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Errors) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(resp.Errors), resp.Errors)
	}
}

func TestBlackholeAPIInactiveByDefault(t *testing.T) {
	h := New(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://stats.subspace.pub/api/blackhole", nil)
	rec := httptest.NewRecorder()
	h.handleBlackholeAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp blackholeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Active {
		t.Error("Active = true, want false when no patterns are registered")
	}
	if len(resp.Patterns) != 0 {
		t.Errorf("Patterns = %v, want empty", resp.Patterns)
	}
}

func TestBlackholeAPIActiveAfterSet(t *testing.T) {
	collector := stats.New()
	collector.IncUpstream("blackhole", true)
	collector.IncUpstream("blackhole", true)
	collector.AddUpstreamBytes("blackhole", 1024, 256)

	h := New(nil, collector, nil)
	h.SetBlackholeRoutes([]string{".ads.example.com", "*.telemetry.com"})

	req := httptest.NewRequest(http.MethodGet, "http://stats.subspace.pub/api/blackhole", nil)
	rec := httptest.NewRecorder()
	h.handleBlackholeAPI(rec, req)

	var resp blackholeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Active {
		t.Error("Active = false, want true after SetBlackholeRoutes with patterns")
	}
	if len(resp.Patterns) != 2 {
		t.Fatalf("Patterns = %v, want 2 entries", resp.Patterns)
	}
	if resp.Stats == nil {
		t.Fatal("Stats = nil, want populated from collector")
	}
	if resp.Stats.Success != 2 {
		t.Errorf("Stats.Success = %d, want 2", resp.Stats.Success)
	}
	if resp.Stats.BytesIn != 1024 {
		t.Errorf("Stats.BytesIn = %d, want 1024", resp.Stats.BytesIn)
	}
}

func TestConfigErrorsAPIPrependsReloadFailure(t *testing.T) {
	h := New(nil, nil, nil)
	h.SetConfigErrors([]string{"existing problem"})
	h.SetReloadError("config reload failed (using previous config): boom")

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/config-errors", nil)
	rec := httptest.NewRecorder()
	h.handleConfigErrorsAPI(rec, req)

	var resp struct {
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Errors) != 2 {
		t.Fatalf("got %d errors, want 2: %v", len(resp.Errors), resp.Errors)
	}
	if !contains(resp.Errors[0], "reload failed") {
		t.Errorf("first error should be the reload failure, got: %v", resp.Errors)
	}
}

func TestConfigErrorsAPIClearsReloadFailureOnSuccess(t *testing.T) {
	h := New(nil, nil, nil)
	h.SetConfigErrors([]string{"existing problem"})
	h.SetReloadError("reload failed: boom")
	// A successful reload supplies a fresh error list (possibly empty).
	h.SetConfigErrors(nil)

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/config-errors", nil)
	rec := httptest.NewRecorder()
	h.handleConfigErrorsAPI(rec, req)

	var resp struct {
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected zero errors after clearing, got %v", resp.Errors)
	}
}

func TestConfigErrorsAPIVersionIncrements(t *testing.T) {
	h := New(nil, nil, nil)

	fetch := func() uint64 {
		req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/config-errors", nil)
		rec := httptest.NewRecorder()
		h.handleConfigErrorsAPI(rec, req)
		var resp struct {
			Version uint64 `json:"version"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Version
	}

	if v := fetch(); v != 0 {
		t.Errorf("initial version = %d, want 0", v)
	}

	h.SetConfigErrors(nil)
	v1 := fetch()
	if v1 != 1 {
		t.Errorf("after first SetConfigErrors version = %d, want 1", v1)
	}

	h.SetConfigErrors([]string{"some problem"})
	v2 := fetch()
	if v2 != 2 {
		t.Errorf("after second SetConfigErrors version = %d, want 2", v2)
	}
}

func TestHandleLinksAPIIncludesTags(t *testing.T) {
	pageInfos := []PageInfo{
		{
			Name: "dev",
			Page: &PageConfig{
				Title: "Dev",
				Sections: []ListSection{
					{
						Name: "Repos",
						Links: []Link{
							{Name: "GitHub", URL: "https://github.com", Tags: []string{"prod"}},
						},
					},
				},
			},
		},
	}
	h := New(pageInfos, nil, nil)
	h.SetTags(map[string]TagDef{
		"prod": {Name: "prod", Color: "#00ff88"},
	})

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/links", nil)
	rec := httptest.NewRecorder()
	h.handleLinksAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Title    string
		Sections []ListSection
		Tags     map[string]TagDef
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Title != "Dev" {
		t.Errorf("Title = %q, want %q", resp.Title, "Dev")
	}
	if len(resp.Sections) != 1 || resp.Sections[0].Links[0].Name != "GitHub" {
		t.Errorf("unexpected sections: %+v", resp.Sections)
	}
	prod, ok := resp.Tags["prod"]
	if !ok {
		t.Fatal("response.Tags missing 'prod'")
	}
	if prod.Color != "#00ff88" {
		t.Errorf("prod.Color = %q, want %q", prod.Color, "#00ff88")
	}
}

func TestHandleSearchEnginesAPI(t *testing.T) {
	pages := []PageInfo{
		{Name: "dev", Page: &PageConfig{Title: "Development"}},
	}
	h := New(pages, nil, nil)

	h.SetSearchEngines(map[string]SearchEngineDef{
		"google":   {Name: "google", Alias: "g", URL: "https://www.google.com/search?q={query}", Icon: "si-google"},
		"metacpan": {Name: "metacpan", Alias: "cpan", URL: "https://metacpan.org/search?q={query}"},
	}, "google")

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/search-engines", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp searchEnginesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Default != "google" {
		t.Errorf("default = %q, want %q", resp.Default, "google")
	}
	if len(resp.Engines) != 2 {
		t.Fatalf("got %d engines, want 2", len(resp.Engines))
	}

	byName := map[string]SearchEngineDef{}
	for _, e := range resp.Engines {
		byName[e.Name] = e
	}
	g, ok := byName["google"]
	if !ok {
		t.Fatal("missing google in response")
	}
	if g.Alias != "g" {
		t.Errorf("google.Alias = %q, want %q", g.Alias, "g")
	}
	if g.URL != "https://www.google.com/search?q={query}" {
		t.Errorf("google.URL = %q, want templated URL", g.URL)
	}
	if g.Icon != "si-google" {
		t.Errorf("google.Icon = %q, want %q", g.Icon, "si-google")
	}
}

func TestSearchEnginesAPIHotReload(t *testing.T) {
	pages := []PageInfo{
		{Name: "dev", Page: &PageConfig{Title: "Development"}},
	}
	h := New(pages, nil, nil)

	h.SetSearchEngines(map[string]SearchEngineDef{
		"google": {Name: "google", URL: "https://www.google.com/search?q={query}"},
	}, "google")

	h.SetSearchEngines(map[string]SearchEngineDef{
		"ddg": {Name: "ddg", URL: "https://duckduckgo.com/?q={query}"},
	}, "ddg")

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/search-engines", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	var resp searchEnginesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Default != "ddg" {
		t.Errorf("default = %q, want %q", resp.Default, "ddg")
	}
	if len(resp.Engines) != 1 || resp.Engines[0].Name != "ddg" {
		t.Errorf("engines = %+v, want only ddg", resp.Engines)
	}
}

func TestSearchEnginesAPIEmpty(t *testing.T) {
	pages := []PageInfo{
		{Name: "dev", Page: &PageConfig{Title: "Development"}},
	}
	h := New(pages, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "http://pages.subspace.pub/dev/api/search-engines", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp searchEnginesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Default != "" {
		t.Errorf("default = %q, want empty", resp.Default)
	}
	if len(resp.Engines) != 0 {
		t.Errorf("engines = %+v, want empty", resp.Engines)
	}
}

func TestFaviconAPICachesOneFetchPerHost(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			atomic.AddInt32(&hits, 1)
			w.Header().Set("Content-Type", "image/x-icon")
			w.Write([]byte("FAKEICONBYTES"))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)
	h.SetSearchEngines(map[string]SearchEngineDef{
		"test": {Name: "test", URL: upstream.URL + "/search?q={query}"},
	}, "")

	get := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet,
			"http://pages.subspace.pub/dev/api/favicon?host="+host, nil)
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		return rec
	}

	// First call: cache miss, upstream fetched once.
	rec1 := get(upURL.Host)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call: status = %d, want 200 (body %q)", rec1.Code, rec1.Body.String())
	}
	if got := rec1.Body.String(); got != "FAKEICONBYTES" {
		t.Errorf("first call: body = %q, want passthrough of upstream bytes", got)
	}
	if got := rec1.Header().Get("Content-Type"); got != "image/x-icon" {
		t.Errorf("first call: Content-Type = %q, want image/x-icon", got)
	}
	if cc := rec1.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("first call: Cache-Control = %q, want max-age directive", cc)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Fatalf("upstream hits after 1 call = %d, want 1", h)
	}

	// Second call: cache hit, no upstream fetch.
	rec2 := get(upURL.Host)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second call: status = %d, want 200", rec2.Code)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("upstream hits after 2 calls = %d, want 1 (cached)", h)
	}
}

func TestFaviconAPIRejectsUnknownHost(t *testing.T) {
	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)
	h.SetSearchEngines(map[string]SearchEngineDef{
		"google": {Name: "google", URL: "https://www.google.com/search?q={query}"},
	}, "")

	req := httptest.NewRequest(http.MethodGet,
		"http://pages.subspace.pub/dev/api/favicon?host=evil.example.com", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("unknown host: status = %d, want 403 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestFaviconAPIRejectsMissingHost(t *testing.T) {
	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)
	h.SetSearchEngines(map[string]SearchEngineDef{
		"google": {Name: "google", URL: "https://www.google.com/search?q={query}"},
	}, "")

	req := httptest.NewRequest(http.MethodGet,
		"http://pages.subspace.pub/dev/api/favicon", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing host: status = %d, want 400", rec.Code)
	}
}

func TestFaviconAPICachesUpstreamFailure(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)
	h.SetSearchEngines(map[string]SearchEngineDef{
		"test": {Name: "test", URL: upstream.URL + "/search?q={query}"},
	}, "")

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet,
			"http://pages.subspace.pub/dev/api/favicon?host="+upURL.Host, nil)
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(); rec.Code != http.StatusNotFound {
		t.Fatalf("first call: status = %d, want 404", rec.Code)
	}
	if rec := get(); rec.Code != http.StatusNotFound {
		t.Fatalf("second call: status = %d, want 404 (cached negative)", rec.Code)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("upstream hits = %d, want 1 (negative cache prevents re-fetch)", h)
	}
}

func TestHandleTopAPI(t *testing.T) {
	store, err := stats.OpenStore(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now()
	for i, snap := range []stats.Snapshot{
		{
			Domains: map[string]stats.UpstreamStats{"github.com": {BytesIn: 0}},
			Routes:  map[string]stats.UpstreamStats{"*.corp": {BytesIn: 0}},
		},
		{
			Domains: map[string]stats.UpstreamStats{"github.com": {BytesIn: 1500}},
			Routes:  map[string]stats.UpstreamStats{"*.corp": {BytesIn: 5000}},
		},
	} {
		ts := now.Add(time.Duration(i-2) * time.Minute)
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}

	h := New([]PageInfo{{Name: "stats"}}, nil, store)

	cases := []struct {
		kind     string
		expect   string
		minValue int64
	}{
		{"domains", "github.com", 1500},
		{"routes", "*.corp", 5000},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet,
			"http://stats.subspace.pub/api/top?kind="+c.kind+"&metric=bytes_in&duration=3600&n=5", nil)
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("kind=%s: status = %d (body %s)", c.kind, rec.Code, rec.Body.String())
		}
		var resp struct {
			Kind   string           `json:"kind"`
			Metric string           `json:"metric"`
			Top    []stats.TopEntry `json:"top"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("kind=%s decode: %v", c.kind, err)
		}
		if resp.Kind != c.kind {
			t.Errorf("kind=%s: response.kind = %q", c.kind, resp.Kind)
		}
		if len(resp.Top) == 0 || resp.Top[0].Name != c.expect {
			t.Errorf("kind=%s: top = %+v, want first entry %q", c.kind, resp.Top, c.expect)
		}
		if resp.Top[0].Value < c.minValue {
			t.Errorf("kind=%s: top value = %d, want >= %d", c.kind, resp.Top[0].Value, c.minValue)
		}
	}
}

func TestHandleTopAPIRejectsUnknownKind(t *testing.T) {
	store, _ := stats.OpenStore(filepath.Join(t.TempDir(), "stats.db"))
	defer store.Close()
	h := New([]PageInfo{{Name: "stats"}}, nil, store)
	req := httptest.NewRequest(http.MethodGet,
		"http://stats.subspace.pub/api/top?kind=widgets", nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown kind: status = %d, want 400", rec.Code)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
