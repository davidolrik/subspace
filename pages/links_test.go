package pages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestParsePageMarkdownTopLevelDefaultsToBand(t *testing.T) {
	input := []byte(`
markdown "Hello **world**"
list "Dev" {
	link "GitHub" url="https://github.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if len(cfg.Items) != 2 {
		t.Fatalf("Items length = %d, want 2", len(cfg.Items))
	}
	if cfg.Items[0].Kind != "markdown" || cfg.Items[0].Markdown == nil {
		t.Fatalf("Items[0] should be a markdown, got %+v", cfg.Items[0])
	}
	if cfg.Items[0].Markdown.Columns != 0 || cfg.Items[0].Markdown.Rows != 0 {
		t.Errorf("default markdown should have Columns=0 Rows=0 (band), got %+v", cfg.Items[0].Markdown)
	}
	if !strings.Contains(cfg.Items[0].Markdown.HTML, "<strong>world</strong>") {
		t.Errorf("Items[0].Markdown.HTML missing <strong>: %q", cfg.Items[0].Markdown.HTML)
	}
}

func TestParsePageMarkdownTopLevelColumnsValid(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 10} {
		input := []byte(fmt.Sprintf(`markdown columns=%d "x"`, n))
		cfg, errs := ParsePage(input)
		if len(errs) > 0 {
			t.Fatalf("columns=%d: errors: %v", n, errs)
		}
		md := cfg.Items[0].Markdown
		if md == nil || md.Columns != n {
			t.Errorf("columns=%d: Columns = %v, want %d", n, md, n)
		}
		// Setting columns alone defaults rows to "auto" so the
		// markdown card sizes itself to its neighbours' heights at
		// render time. The card stays a grid cell (not a band).
		if md != nil && (md.Rows != 0 || !md.RowsAuto) {
			t.Errorf("columns=%d: expected Rows=0 RowsAuto=true (auto default), got Rows=%d RowsAuto=%v", n, md.Rows, md.RowsAuto)
		}
	}
}

func TestParsePageMarkdownTopLevelRowsValid(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 5} {
		input := []byte(fmt.Sprintf(`markdown rows=%d "x"`, n))
		cfg, errs := ParsePage(input)
		if len(errs) > 0 {
			t.Fatalf("rows=%d: errors: %v", n, errs)
		}
		md := cfg.Items[0].Markdown
		if md == nil || md.Rows != n {
			t.Errorf("rows=%d: Rows = %v, want %d", n, md, n)
		}
		// rows-only forces Columns=1 so the markdown stays in the
		// surrounding grid as a 1-wide × N-tall card.
		if md != nil && md.Columns != 1 {
			t.Errorf("rows=%d: Columns = %d, want 1 (default when rows is set)", n, md.Columns)
		}
	}
}

func TestParsePageMarkdownTopLevelColumnsAndRows(t *testing.T) {
	cfg, errs := ParsePage([]byte(`markdown columns=2 rows=3 "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Columns != 2 || md.Rows != 3 {
		t.Errorf("expected Columns=2 Rows=3, got %+v", md)
	}
}

func TestParsePageMarkdownTopLevelColumnsInvalid(t *testing.T) {
	for _, src := range []string{
		`markdown columns="huge" "x"`,
		`markdown columns=0 "x"`,
		`markdown columns=-1 "x"`,
	} {
		cfg, errs := ParsePage([]byte(src))
		if len(errs) == 0 {
			t.Errorf("%q: expected an error", src)
		}
		md := cfg.Items[0].Markdown
		if md == nil {
			t.Fatalf("%q: markdown should still render", src)
		}
		// Invalid columns is treated as if absent — the markdown
		// remains a band (Columns == 0) since rows is also unset.
		if md.Columns != 0 || md.Rows != 0 {
			t.Errorf("%q: invalid columns should be treated as absent, got %+v", src, md)
		}
	}
}

func TestParsePageMarkdownTopLevelRowsInvalid(t *testing.T) {
	for _, src := range []string{
		`markdown rows="huge" "x"`,
		`markdown rows=0 "x"`,
		`markdown rows=-1 "x"`,
	} {
		cfg, errs := ParsePage([]byte(src))
		if len(errs) == 0 {
			t.Errorf("%q: expected an error", src)
		}
		md := cfg.Items[0].Markdown
		if md == nil {
			t.Fatalf("%q: markdown should still render", src)
		}
		if md.Columns != 0 || md.Rows != 0 {
			t.Errorf("%q: invalid rows should be treated as absent, got %+v", src, md)
		}
	}
}

func TestParsePageMarkdownTopLevelFloatRight(t *testing.T) {
	cfg, errs := ParsePage([]byte(`markdown float="right" columns=2 "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Float != "right" {
		t.Errorf("expected Float=\"right\", got %+v", md)
	}
	if md.Columns != 2 || md.Rows != 0 || !md.RowsAuto {
		t.Errorf("expected Columns=2 with auto rows, got %+v", md)
	}
}

func TestParsePageMarkdownTopLevelFloatLeftIsDefault(t *testing.T) {
	// Explicit float="left" round-trips to empty (the implicit default)
	// so the frontend doesn't need to special-case both spellings.
	cfg, errs := ParsePage([]byte(`markdown float="left" columns=2 "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Float != "" {
		t.Errorf("expected Float=\"\" (default-left), got %+v", md)
	}
}

func TestParsePageMarkdownTopLevelFloatAlonePromotesToCard(t *testing.T) {
	// float= without columns/rows still produces a grid card (not a
	// band) — otherwise "float right" on a band would be meaningless.
	// Rows defaults to "auto" so the card sizes itself.
	cfg, errs := ParsePage([]byte(`markdown float="right" "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Columns != 1 || md.Rows != 0 || !md.RowsAuto || md.Float != "right" {
		t.Errorf("expected 1-col auto-row grid card floated right, got %+v", md)
	}
}

func TestParsePageMarkdownTopLevelFloatInvalid(t *testing.T) {
	cfg, errs := ParsePage([]byte(`markdown float="middle" columns=2 "x"`))
	if len(errs) == 0 {
		t.Fatal("expected error for invalid float value")
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Float != "" {
		t.Errorf("invalid float should be ignored (default-left), got %+v", md)
	}
}

func TestParsePageTopLevelUnknownPropertyErrors(t *testing.T) {
	// Unknown properties on a top-level node are typos worth flagging.
	cases := []struct {
		name string
		src  string
		key  string
	}{
		{"markdown", `markdown foo="bar" "x"`, "foo"},
		{"list", `list "X" foo="bar" {
	link "y" url="https://example.com"
}`, "foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errs := ParsePage([]byte(c.src))
			if len(errs) == 0 {
				t.Fatalf("expected an error for unknown property %q", c.key)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Error(), c.key) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected error mentioning %q, got %v", c.key, errs)
			}
		})
	}
}

func TestParsePageInsideListUnknownPropertyIgnored(t *testing.T) {
	// Inside a list, unknown properties on links/markdown stay
	// lenient — operators frequently sketch with comments like
	// `note="..."` and we shouldn't fail their config for it.
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com" foo="bar"
	markdown unknown="x" "_inline_"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Errorf("expected no errors for unknown props inside a list, got %v", errs)
	}
	if len(cfg.Sections) != 1 || len(cfg.Sections[0].Links) != 1 {
		t.Fatalf("page should still render, got %+v", cfg)
	}
}

func TestParsePageMarkdownInclude(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(mdPath, []byte("## From file\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := []byte(`markdown include="./notes.md"`)
	cfg, errs := ParsePageWithBase(src, dir)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil {
		t.Fatal("expected a markdown item")
	}
	if !strings.Contains(md.HTML, "From file</h2>") {
		t.Errorf("expected included content rendered, got: %q", md.HTML)
	}
	abs, _ := filepath.Abs(mdPath)
	if md.IncludePath != abs {
		t.Errorf("IncludePath = %q, want %q", md.IncludePath, abs)
	}
}

func TestParsePageMarkdownIncludeMissingFallsBackToInline(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`markdown include="./gone.md" "Hello **fallback**"`)
	cfg, errs := ParsePageWithBase(src, dir)
	if len(errs) == 0 {
		t.Fatal("expected an error for missing include")
	}
	md := cfg.Items[0].Markdown
	if md == nil || !strings.Contains(md.HTML, "<strong>fallback</strong>") {
		t.Errorf("expected fallback rendered, got %+v", md)
	}
}

func TestParsePageMarkdownIncludeMissingNoFallbackPlaceholder(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`markdown include="./gone.md"`)
	cfg, errs := ParsePageWithBase(src, dir)
	if len(errs) == 0 {
		t.Fatal("expected an error for missing include")
	}
	md := cfg.Items[0].Markdown
	if md == nil {
		t.Fatal("expected a placeholder markdown item")
	}
	if !strings.Contains(md.HTML, "md-alert") {
		t.Errorf("placeholder should render as an alert, got: %q", md.HTML)
	}
	if !strings.Contains(md.HTML, "gone.md") {
		t.Errorf("placeholder should mention the failed path, got: %q", md.HTML)
	}
}

func TestParsePageMarkdownIncludeAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "abs.md")
	if err := os.WriteFile(mdPath, []byte("absolute"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := []byte(fmt.Sprintf(`markdown include=%q`, mdPath))
	// baseDir intentionally empty — absolute paths must work without it.
	cfg, errs := ParsePageWithBase(src, "")
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || !strings.Contains(md.HTML, "absolute") {
		t.Errorf("expected absolute include resolved, got %+v", md)
	}
}

func TestParsePageMarkdownIncludeTildePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	mdPath := filepath.Join(home, ".subspace_markdown_include_test.md")
	if err := os.WriteFile(mdPath, []byte("tilde works"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(mdPath)

	src := []byte(`markdown include="~/.subspace_markdown_include_test.md"`)
	cfg, errs := ParsePageWithBase(src, "")
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || !strings.Contains(md.HTML, "tilde works") {
		t.Errorf("expected tilde include resolved, got %+v", md)
	}
}

func TestParsePageMarkdownIncludeWatchedPaths(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "a.md")
	if err := os.WriteFile(mdPath, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := []byte(fmt.Sprintf(`markdown include="./a.md"
markdown include="./missing.md" "fallback"
`))
	cfg, _ := ParsePageWithBase(src, dir)
	abs, _ := filepath.Abs(mdPath)
	want := abs
	gotFound := false
	for _, item := range cfg.Items {
		if item.Markdown != nil && item.Markdown.IncludePath == want {
			gotFound = true
		}
	}
	if !gotFound {
		t.Errorf("expected IncludePath=%q on the resolved include", want)
	}
	// The missing include's resolved path should also be exposed so
	// the watcher can subscribe and reload when the file appears.
	missingFound := false
	for _, item := range cfg.Items {
		if item.Markdown != nil && strings.HasSuffix(item.Markdown.IncludePath, "missing.md") {
			missingFound = true
		}
	}
	if !missingFound {
		t.Errorf("expected IncludePath set even when include is missing")
	}
}

func TestParsePageMarkdownTopLevelColor(t *testing.T) {
	cfg, errs := ParsePage([]byte(`markdown columns=2 color="#ff6b6b" "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Color != "#ff6b6b" {
		t.Errorf("expected Color=#ff6b6b, got %+v", md)
	}
}

func TestParsePageMarkdownTopLevelColorOmitted(t *testing.T) {
	cfg, errs := ParsePage([]byte(`markdown columns=2 "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Color != "" {
		t.Errorf("expected empty Color when omitted, got %+v", md)
	}
}

func TestParsePageMarkdownInsideListIgnoresColor(t *testing.T) {
	// color= on an in-list markdown is silently ignored — the row is
	// inline prose with no card chrome to tint, so accepting the
	// property without erroring keeps configs forwards-compatible
	// when a row is later promoted to a top-level grid card.
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com"
	markdown color="#ff0000" "_inline note_"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Errorf("expected no errors for in-list markdown color, got %v", errs)
	}
	if len(cfg.Sections) != 1 {
		t.Fatalf("expected one section, got %+v", cfg)
	}
	// ListItem doesn't carry a Color field — the data model already
	// enforces that there's nothing to apply the color to.
	if len(cfg.Sections[0].Items) != 2 || cfg.Sections[0].Items[1].Kind != "markdown" {
		t.Fatalf("expected link + markdown items, got %+v", cfg.Sections[0].Items)
	}
}

func TestParsePageMarkdownRowsAuto(t *testing.T) {
	cfg, errs := ParsePage([]byte(`markdown columns=2 rows="auto" "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil {
		t.Fatal("expected a markdown item")
	}
	if !md.RowsAuto {
		t.Errorf("expected RowsAuto = true, got %+v", md)
	}
	if md.Rows != 0 {
		t.Errorf("expected Rows = 0 when RowsAuto is set, got %d", md.Rows)
	}
	if md.Columns != 2 {
		t.Errorf("expected Columns = 2, got %d", md.Columns)
	}
}

func TestParsePageMarkdownRowsAutoCaseInsensitive(t *testing.T) {
	for _, v := range []string{"auto", "AUTO", "Auto"} {
		src := []byte(fmt.Sprintf(`markdown rows=%q "x"`, v))
		cfg, errs := ParsePage(src)
		if len(errs) > 0 {
			t.Fatalf("rows=%q: errors: %v", v, errs)
		}
		if md := cfg.Items[0].Markdown; md == nil || !md.RowsAuto {
			t.Errorf("rows=%q: expected RowsAuto, got %+v", v, md)
		}
	}
}

func TestParsePageMarkdownRowsAutoAlonePromotesToCard(t *testing.T) {
	// rows="auto" by itself is a positioning property — it should
	// turn the markdown into a grid card (Columns=1) rather than a
	// page-spanning band.
	cfg, errs := ParsePage([]byte(`markdown rows="auto" "x"`))
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	md := cfg.Items[0].Markdown
	if md == nil || md.Columns != 1 || md.Rows != 0 || !md.RowsAuto {
		t.Errorf("expected 1-col grid card with RowsAuto, got %+v", md)
	}
}

func TestParsePageMarkdownRowsAutoSerialised(t *testing.T) {
	// Frontend Vitest tests assume a stable JSON shape: when RowsAuto
	// is true, Rows should be omitted (omitempty).
	cfg, _ := ParsePage([]byte(`markdown rows="auto" "x"`))
	md := cfg.Items[0].Markdown
	b, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, `"RowsAuto":true`) {
		t.Errorf("missing RowsAuto:true in %q", got)
	}
	if strings.Contains(got, `"Rows":`) {
		t.Errorf("Rows should be omitted when zero, got %q", got)
	}
}

func TestParsePageMarkdownInsideListIgnoresGridProps(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com"
	markdown columns=2 rows="auto" float="right" "_inline note_"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	s := cfg.Sections[0]
	if len(s.Items) != 2 {
		t.Fatalf("Items length = %d, want 2", len(s.Items))
	}
	if s.Items[1].Kind != "markdown" {
		t.Errorf("Items[1].Kind = %q, want \"markdown\"", s.Items[1].Kind)
	}
	// Inside a list, markdown is a row — column/row hints are
	// dropped (ListItem doesn't carry them).
	if !strings.Contains(s.Items[1].HTML, "<em>inline note</em>") {
		t.Errorf("inline markdown HTML = %q", s.Items[1].HTML)
	}
}

func TestParsePageMarkdownInsideList(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub" url="https://github.com"
	markdown "_See banner above_"
	link "Wiki" url="https://wiki.example.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	s := cfg.Sections[0]
	if len(s.Items) != 3 {
		t.Fatalf("Items length = %d, want 3", len(s.Items))
	}
	wantKinds := []string{"link", "markdown", "link"}
	for i, want := range wantKinds {
		if s.Items[i].Kind != want {
			t.Errorf("Items[%d].Kind = %q, want %q", i, s.Items[i].Kind, want)
		}
	}
	if !strings.Contains(s.Items[1].HTML, "<em>See banner above</em>") {
		t.Errorf("inline markdown HTML = %q", s.Items[1].HTML)
	}
	// Links view stays links-only — markdown is excluded.
	if len(s.Links) != 2 {
		t.Errorf("Links length = %d, want 2 (markdown excluded)", len(s.Links))
	}
}

func TestParsePageItemsPreserveOrder(t *testing.T) {
	input := []byte(`
list "A" {
	link "x" url="https://x.example"
}
markdown "first"
list "B" {
	link "y" url="https://y.example"
}
markdown columns=1 "second"
list "C" {
	link "z" url="https://z.example"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) > 0 {
		t.Fatalf("ParsePage() errors: %v", errs)
	}
	if len(cfg.Items) != 5 {
		t.Fatalf("Items length = %d, want 5", len(cfg.Items))
	}
	wantKinds := []string{"list", "markdown", "list", "markdown", "list"}
	for i, want := range wantKinds {
		if cfg.Items[i].Kind != want {
			t.Errorf("Items[%d].Kind = %q, want %q", i, cfg.Items[i].Kind, want)
		}
	}
	// Sections is the derived flat list view — markdown items don't appear.
	if len(cfg.Sections) != 3 {
		t.Errorf("Sections length = %d, want 3 (lists only)", len(cfg.Sections))
	}
}

func TestParsePageMarkdownMissingArgument(t *testing.T) {
	input := []byte(`
markdown
list "Dev" {
	link "GitHub" url="https://github.com"
}
`)
	cfg, errs := ParsePage(input)
	if len(errs) == 0 {
		t.Fatal("expected error for markdown with no argument")
	}
	// The markdown is dropped, the list survives.
	if len(cfg.Items) != 1 || cfg.Items[0].Kind != "list" {
		t.Errorf("surrounding list should survive, got Items=%+v", cfg.Items)
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
