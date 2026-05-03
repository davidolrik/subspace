package pages

import (
	"testing"
)

func TestParsePage(t *testing.T) {
	input := []byte(`
list "Development" {
	link "GitHub" url="https://github.com/org/repo"
	link "CI/CD" url="https://ci.example.com"
}

list "Infrastructure" {
	link "Grafana" url="https://grafana.example.com"
	link "Prometheus" url="https://prom.example.com"
}
`)

	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	if len(cfg.Sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(cfg.Sections))
	}

	dev := cfg.Sections[0]
	if dev.Name != "Development" {
		t.Errorf("sections[0].Name = %q, want %q", dev.Name, "Development")
	}
	if len(dev.Links) != 2 {
		t.Fatalf("sections[0] has %d links, want 2", len(dev.Links))
	}
	if dev.Links[0].Name != "GitHub" || dev.Links[0].URL != "https://github.com/org/repo" {
		t.Errorf("sections[0].Links[0] = %+v", dev.Links[0])
	}
	if dev.Links[1].Name != "CI/CD" || dev.Links[1].URL != "https://ci.example.com" {
		t.Errorf("sections[0].Links[1] = %+v", dev.Links[1])
	}

	infra := cfg.Sections[1]
	if infra.Name != "Infrastructure" {
		t.Errorf("sections[1].Name = %q, want %q", infra.Name, "Infrastructure")
	}
	if len(infra.Links) != 2 {
		t.Fatalf("sections[1] has %d links, want 2", len(infra.Links))
	}
}

func TestParsePageListSubtitle(t *testing.T) {
	input := []byte(`
list "Repos" {
	title "GitHub"
	link "subspace" url="https://github.com/davidolrik/subspace"
	link "kdl-go"   url="https://github.com/sblinch/kdl-go"
	title "GitLab"
	link "internal" url="https://gitlab.example.com/team/internal"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	if len(cfg.Sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(cfg.Sections))
	}
	s := cfg.Sections[0]

	// Items preserve KDL order with subtitles interleaved.
	if len(s.Items) != 5 {
		t.Fatalf("got %d items, want 5: %+v", len(s.Items), s.Items)
	}
	wantKinds := []string{"subtitle", "link", "link", "subtitle", "link"}
	wantNames := []string{"GitHub", "subspace", "kdl-go", "GitLab", "internal"}
	for i, item := range s.Items {
		if item.Kind != wantKinds[i] {
			t.Errorf("Items[%d].Kind = %q, want %q", i, item.Kind, wantKinds[i])
		}
		if item.Name != wantNames[i] {
			t.Errorf("Items[%d].Name = %q, want %q", i, item.Name, wantNames[i])
		}
	}

	// Links is the flat link-only view. Subtitles are excluded.
	if len(s.Links) != 3 {
		t.Fatalf("got %d links, want 3 (subtitles excluded): %+v", len(s.Links), s.Links)
	}
	if s.Links[0].URL != "https://github.com/davidolrik/subspace" {
		t.Errorf("Links[0].URL = %q", s.Links[0].URL)
	}
}

func TestParsePageListSubtitleRequiresName(t *testing.T) {
	cfg, errs := ParsePage([]byte(`
list "Repos" {
	title
	link "x" url="https://x.example"
}
`))
	if len(errs) == 0 {
		t.Fatal("expected error for title with no argument")
	}
	// The bare title is skipped, but the rest of the list still parses.
	if cfg == nil || len(cfg.Sections) != 1 {
		t.Fatalf("expected partial section to remain, got cfg=%+v", cfg)
	}
	if len(cfg.Sections[0].Links) != 1 || cfg.Sections[0].Links[0].Name != "x" {
		t.Fatalf("link after bad title should still parse, got %+v", cfg.Sections[0].Links)
	}
}

func TestParsePageTitleAndFooter(t *testing.T) {
	input := []byte(`
title "My Links"
footer "Acme Corp — Internal Use Only"

list "Dev" {
	link "GitHub" url="https://github.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if cfg.Title != "My Links" {
		t.Errorf("Title = %q, want %q", cfg.Title, "My Links")
	}
	if cfg.Footer != "Acme Corp — Internal Use Only" {
		t.Errorf("Footer = %q, want %q", cfg.Footer, "Acme Corp — Internal Use Only")
	}
}

func TestParsePageTitleDefault(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if cfg.Title != "" {
		t.Errorf("Title = %q, want empty (default applied by frontend)", cfg.Title)
	}
	if cfg.Footer != "" {
		t.Errorf("Footer = %q, want empty", cfg.Footer)
	}
}

func TestParsePageEmpty(t *testing.T) {
	cfg, errs := ParsePage([]byte(""))
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if len(cfg.Sections) != 0 {
		t.Errorf("got %d sections, want 0", len(cfg.Sections))
	}
}

func TestParsePageLinksSectionMissingName(t *testing.T) {
	input := []byte(`
links {
	link "GitHub" url="https://github.com"
}
`)
	_, errs := ParsePage(input)
	if len(errs) == 0 {
		t.Fatal("expected error for list section without name")
	}
}

func TestParsePageLinkMissingName(t *testing.T) {
	input := []byte(`
list "Dev" {
	link url="https://github.com"
}
`)
	_, errs := ParsePage(input)
	if len(errs) == 0 {
		t.Fatal("expected error for link without name")
	}
}

func TestParsePageLinkMissingURL(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub"
}
`)
	_, errs := ParsePage(input)
	if len(errs) == 0 {
		t.Fatal("expected error for link without url")
	}
}

func TestParsePageIconAndDescription(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com" icon="si-github" description="Source code repository"
	link "Docs" url="https://docs.example.com" icon="fa-book"
	link "API" url="https://api.example.com" description="REST API"
	link "Plain" url="https://plain.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	links := cfg.Sections[0].Links

	if links[0].Icon != "si-github" {
		t.Errorf("links[0].Icon = %q, want %q", links[0].Icon, "si-github")
	}
	if links[0].Description != "Source code repository" {
		t.Errorf("links[0].Description = %q, want %q", links[0].Description, "Source code repository")
	}

	if links[1].Icon != "fa-book" {
		t.Errorf("links[1].Icon = %q, want %q", links[1].Icon, "fa-book")
	}
	if links[1].Description != "" {
		t.Errorf("links[1].Description = %q, want empty", links[1].Description)
	}

	if links[2].Icon != "" {
		t.Errorf("links[2].Icon = %q, want empty", links[2].Icon)
	}
	if links[2].Description != "REST API" {
		t.Errorf("links[2].Description = %q, want %q", links[2].Description, "REST API")
	}

	if links[3].Icon != "" || links[3].Description != "" {
		t.Errorf("links[3] should have no icon or description: %+v", links[3])
	}
}

func TestParsePageListColor(t *testing.T) {
	input := []byte(`
list "Dev" color="#ff6b6b" {
	link "GitHub" url="https://github.com"
}

list "Ops" {
	link "Grafana" url="https://grafana.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	if cfg.Sections[0].Color != "#ff6b6b" {
		t.Errorf("sections[0].Color = %q, want %q", cfg.Sections[0].Color, "#ff6b6b")
	}
	if cfg.Sections[1].Color != "" {
		t.Errorf("sections[1].Color = %q, want empty", cfg.Sections[1].Color)
	}
}

func TestParsePageListIcon(t *testing.T) {
	input := []byte(`
list "Dev" icon="fa-code" {
	link "GitHub" url="https://github.com"
}

list "Ops" icon="si-grafana" color="#00ff88" {
	link "Grafana" url="https://grafana.example.com"
}

list "Docs" {
	link "Wiki" url="https://wiki.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	if cfg.Sections[0].Icon != "fa-code" {
		t.Errorf("sections[0].Icon = %q, want %q", cfg.Sections[0].Icon, "fa-code")
	}
	if cfg.Sections[1].Icon != "si-grafana" {
		t.Errorf("sections[1].Icon = %q, want %q", cfg.Sections[1].Icon, "si-grafana")
	}
	if cfg.Sections[1].Color != "#00ff88" {
		t.Errorf("sections[1].Color = %q, want %q", cfg.Sections[1].Color, "#00ff88")
	}
	if cfg.Sections[2].Icon != "" {
		t.Errorf("sections[2].Icon = %q, want empty", cfg.Sections[2].Icon)
	}
}

func TestParsePageLinkTags(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com" tags="prod external"
	link "Wiki" url="https://wiki.example.com" tags="internal"
	link "Plain" url="https://plain.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	links := cfg.Sections[0].Links

	if got, want := links[0].Tags, []string{"external", "prod"}; !equalStringSlices(got, want) {
		t.Errorf("links[0].Tags = %v, want %v", got, want)
	}
	if got, want := links[1].Tags, []string{"internal"}; !equalStringSlices(got, want) {
		t.Errorf("links[1].Tags = %v, want %v", got, want)
	}
	if links[2].Tags != nil {
		t.Errorf("links[2].Tags = %v, want nil", links[2].Tags)
	}
}

func TestParsePageListTags(t *testing.T) {
	input := []byte(`
list "Dev" tags="internal wip" {
	link "GitHub" url="https://github.com"
}

list "Ops" {
	link "Grafana" url="https://grafana.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}

	if got, want := cfg.Sections[0].Tags, []string{"internal", "wip"}; !equalStringSlices(got, want) {
		t.Errorf("sections[0].Tags = %v, want %v", got, want)
	}
	if cfg.Sections[1].Tags != nil {
		t.Errorf("sections[1].Tags = %v, want nil", cfg.Sections[1].Tags)
	}
}

func TestParsePageTagsExtraWhitespace(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com" tags="  prod    external  "
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if got, want := cfg.Sections[0].Links[0].Tags, []string{"external", "prod"}; !equalStringSlices(got, want) {
		t.Errorf("Tags = %v, want %v", got, want)
	}
}

func TestParsePageTagsSorted(t *testing.T) {
	input := []byte(`
list "Dev" tags="zebra alpha middle" {
	link "X" url="https://x.example.com" tags="charlie alpha bravo"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if got, want := cfg.Sections[0].Tags, []string{"alpha", "middle", "zebra"}; !equalStringSlices(got, want) {
		t.Errorf("list Tags = %v, want sorted %v", got, want)
	}
	if got, want := cfg.Sections[0].Links[0].Tags, []string{"alpha", "bravo", "charlie"}; !equalStringSlices(got, want) {
		t.Errorf("link Tags = %v, want sorted %v", got, want)
	}
}

func TestParsePageTagsEmpty(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com" tags=""
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if cfg.Sections[0].Links[0].Tags != nil {
		t.Errorf("Tags should be nil for empty tags property, got %v", cfg.Sections[0].Links[0].Tags)
	}
}

func equalStringSlices(a, b []string) bool {
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

// A page whose KDL is syntactically broken should still produce a
// (non-nil) PageConfig so the page stays registered. The dashboard
// then renders empty content with the error in the config-error
// banner — better than redirecting the user to the troubleshooting
// docs.
func TestParsePageKDLSyntaxErrorKeepsEmptyPage(t *testing.T) {
	cfg, errs := ParsePage([]byte(`list "HQ" icon=fa-home color="#0f0" {`))
	if cfg == nil {
		t.Fatal("expected non-nil PageConfig even when KDL parsing fails")
	}
	if len(errs) == 0 {
		t.Fatal("expected the KDL syntax error to be reported")
	}
	if len(cfg.Items) != 0 || len(cfg.Sections) != 0 {
		t.Errorf("expected empty content, got Items=%v Sections=%v", cfg.Items, cfg.Sections)
	}
}
func TestParsePageUnknownTopLevel(t *testing.T) {
	input := []byte(`
something "foo"
`)
	_, errs := ParsePage(input)
	if len(errs) == 0 {
		t.Fatal("expected error for unknown top-level node")
	}
}

func TestParsePageUnknownChild(t *testing.T) {
	input := []byte(`
list "Dev" {
	widget "foo" url="https://example.com"
}
`)
	_, errs := ParsePage(input)
	if len(errs) == 0 {
		t.Fatal("expected error for unknown child node in list section")
	}
}

// Lenient parse: an unknown child node in a list (e.g. a `title` written
// against an older subspace version that didn't yet support it) must not
// drop the whole page. The error is reported, but the surrounding links
// still parse so the dashboard renders something useful instead of
// redirecting the user to the troubleshooting docs.
func TestParsePageLenientUnknownChildKeepsValidLinks(t *testing.T) {
	input := []byte(`
title "My Links"

list "Repos" {
	title "GitHub"
	link "subspace" url="https://github.com/davidolrik/subspace"
	widget "broken"
	link "kdl-go" url="https://github.com/sblinch/kdl-go"
}

list "Ops" {
	link "Grafana" url="https://grafana.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if cfg == nil {
		t.Fatal("expected partial PageConfig even with non-fatal errors")
	}
	if len(errs) == 0 {
		t.Fatal("expected the unknown-child error to be reported")
	}
	if cfg.Title != "My Links" {
		t.Errorf("Title = %q, want %q", cfg.Title, "My Links")
	}
	if len(cfg.Sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(cfg.Sections))
	}
	repos := cfg.Sections[0]
	if len(repos.Links) != 2 {
		t.Errorf("Repos.Links = %d, want 2 (widget skipped)", len(repos.Links))
	}
	if repos.Links[0].Name != "subspace" || repos.Links[1].Name != "kdl-go" {
		t.Errorf("Repos.Links names = %q,%q", repos.Links[0].Name, repos.Links[1].Name)
	}
	if cfg.Sections[1].Name != "Ops" {
		t.Errorf("second section should be Ops, got %q", cfg.Sections[1].Name)
	}
}

// Lenient parse: a top-level unknown node skips itself but the rest of
// the document (other lists, footer, title) still parses.
func TestParsePageLenientUnknownTopLevelKeepsLists(t *testing.T) {
	input := []byte(`
title "Stuff"
mystery "value"

list "Dev" {
	link "GitHub" url="https://github.com"
}
`)
	cfg, errs := ParsePage(input)
	if cfg == nil {
		t.Fatal("expected partial PageConfig even with non-fatal errors")
	}
	if len(errs) == 0 {
		t.Fatal("expected the unknown-top-level error to be reported")
	}
	if cfg.Title != "Stuff" {
		t.Errorf("Title = %q, want %q", cfg.Title, "Stuff")
	}
	if len(cfg.Sections) != 1 || cfg.Sections[0].Name != "Dev" {
		t.Fatalf("expected Dev section to survive, got %+v", cfg.Sections)
	}
}
