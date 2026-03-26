package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
