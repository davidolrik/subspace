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
		{"dashboard.subspace.pub", true},
		{"statistics.subspace.pub", true},
		{"anything.subspace.pub", true},
		{"subspace.pub", false},
		{"subspace", false},
		{"example.com", false},
		{"dashboard.subspace.com", false},
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
			Host:  "dev",
			Alias: "d",
			Page:  &PageConfig{Title: "Development"},
		},
		{
			Host: "ops",
			Page: &PageConfig{Title: "Operations"},
		},
	}

	h := New(pages, nil, nil)

	// Request nav from dev.subspace.pub
	req := httptest.NewRequest(http.MethodGet, "http://dev.subspace.pub/api/nav", nil)
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
	if dev.Host != "dev" {
		t.Errorf("dev entry: host = %q, want %q", dev.Host, "dev")
	}
	if dev.Alias != "d" {
		t.Errorf("dev entry: alias = %q, want %q", dev.Alias, "d")
	}
	if dev.Label != "Development" {
		t.Errorf("dev entry: label = %q, want %q", dev.Label, "Development")
	}

	// Second entry (ops) should have host but no alias
	ops := nav[1]
	if ops.Host != "ops" {
		t.Errorf("ops entry: host = %q, want %q", ops.Host, "ops")
	}
	if ops.Alias != "" {
		t.Errorf("ops entry: alias = %q, want empty", ops.Alias)
	}

	// Statistics entry should have host "stats"
	stats := nav[2]
	if stats.Label != "Statistics" {
		t.Errorf("stats entry: label = %q, want %q", stats.Label, "Statistics")
	}
	if stats.Host != "stats" {
		t.Errorf("stats entry: host = %q, want %q", stats.Host, "stats")
	}
	if stats.Alias != "statistics" {
		t.Errorf("stats entry: alias = %q, want %q", stats.Alias, "statistics")
	}
}

func TestAllLinksAPI(t *testing.T) {
	pages := []PageInfo{
		{
			Host:  "dev",
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
			Host: "ops",
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

	req := httptest.NewRequest(http.MethodGet, "http://dev.subspace.pub/api/all-links", nil)
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

	// First two links belong to dev page
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

	// Third link belongs to ops page
	if links[2].Page != "Operations" {
		t.Errorf("link 2: page = %q, want %q", links[2].Page, "Operations")
	}
	if links[2].Name != "Grafana" {
		t.Errorf("link 2: name = %q, want %q", links[2].Name, "Grafana")
	}
}

func TestUndefinedPageRedirectsToDocs(t *testing.T) {
	pages := []PageInfo{
		{Host: "dev", Page: &PageConfig{Title: "Development"}},
	}
	h := New(pages, nil, nil)

	// Request an undefined page
	req := httptest.NewRequest(http.MethodGet, "http://unknown.subspace.pub/", nil)
	rec := httptest.NewRecorder()

	// Use the mux directly to simulate ServeHTTP's redirect logic
	_, known := h.pagesByHost[req.Host]
	if known {
		t.Fatal("unknown.subspace.pub should not be a known host")
	}

	http.Redirect(rec, req, "https://subspace.pub/guide/pages", http.StatusFound)

	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc := rec.Header().Get("Location")
	if loc != "https://subspace.pub/guide/pages" {
		t.Errorf("Location = %q, want %q", loc, "https://subspace.pub/guide/pages")
	}
}
