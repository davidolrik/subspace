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

	cfg, err := ParsePage(input)
	if err != nil {
		t.Fatalf("ParsePage() error: %v", err)
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

func TestParsePageTitleAndFooter(t *testing.T) {
	input := []byte(`
title "My Links"
footer "Acme Corp — Internal Use Only"

list "Dev" {
	link "GitHub" url="https://github.com"
}
`)
	cfg, err := ParsePage(input)
	if err != nil {
		t.Fatalf("ParsePage() error: %v", err)
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
	cfg, err := ParsePage(input)
	if err != nil {
		t.Fatalf("ParsePage() error: %v", err)
	}
	if cfg.Title != "" {
		t.Errorf("Title = %q, want empty (default applied by frontend)", cfg.Title)
	}
	if cfg.Footer != "" {
		t.Errorf("Footer = %q, want empty", cfg.Footer)
	}
}

func TestParsePageEmpty(t *testing.T) {
	cfg, err := ParsePage([]byte(""))
	if err != nil {
		t.Fatalf("ParsePage() error: %v", err)
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
	_, err := ParsePage(input)
	if err == nil {
		t.Fatal("expected error for list section without name")
	}
}

func TestParsePageLinkMissingName(t *testing.T) {
	input := []byte(`
list "Dev" {
	link url="https://github.com"
}
`)
	_, err := ParsePage(input)
	if err == nil {
		t.Fatal("expected error for link without name")
	}
}

func TestParsePageLinkMissingURL(t *testing.T) {
	input := []byte(`
list "Dev" {
	link "GitHub"
}
`)
	_, err := ParsePage(input)
	if err == nil {
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
	cfg, err := ParsePage(input)
	if err != nil {
		t.Fatalf("ParsePage() error: %v", err)
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

func TestParsePageUnknownTopLevel(t *testing.T) {
	input := []byte(`
something "foo"
`)
	_, err := ParsePage(input)
	if err == nil {
		t.Fatal("expected error for unknown top-level node")
	}
}

func TestParsePageUnknownChild(t *testing.T) {
	input := []byte(`
list "Dev" {
	widget "foo" url="https://example.com"
}
`)
	_, err := ParsePage(input)
	if err == nil {
		t.Fatal("expected error for unknown child node in list section")
	}
}
