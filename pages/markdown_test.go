package pages

import (
	"strings"
	"testing"
)

func TestRenderMarkdownBasic(t *testing.T) {
	html, err := RenderMarkdown("**bold** and *italic*")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, "<strong>bold</strong>") {
		t.Errorf("expected <strong>bold</strong> in output, got: %q", html)
	}
	if !strings.Contains(html, "<em>italic</em>") {
		t.Errorf("expected <em>italic</em> in output, got: %q", html)
	}
}

func TestRenderMarkdownEscapesScript(t *testing.T) {
	html, err := RenderMarkdown(`<script>alert(1)</script>`)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Errorf("rendered output must not contain a <script> tag, got: %q", html)
	}
}

func TestRenderMarkdownStripsEventHandlers(t *testing.T) {
	html, err := RenderMarkdown(`<a href="https://example.com" onclick="alert(1)">x</a>`)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if strings.Contains(strings.ToLower(html), "onclick") {
		t.Errorf("event handlers must be stripped, got: %q", html)
	}
}

func TestRenderMarkdownLinksGetTargetBlank(t *testing.T) {
	html, err := RenderMarkdown(`[example](https://example.com)`)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, `target="_blank"`) {
		t.Errorf("expected target=\"_blank\" on rendered link, got: %q", html)
	}
	if !strings.Contains(html, `rel=`) || !strings.Contains(html, "noopener") || !strings.Contains(html, "noreferrer") {
		t.Errorf("expected rel=\"noopener noreferrer\" on rendered link, got: %q", html)
	}
}

func TestRenderMarkdownGFMTable(t *testing.T) {
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	html, err := RenderMarkdown(src)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, "<table>") {
		t.Errorf("expected <table> in output for GFM table, got: %q", html)
	}
}

func TestRenderMarkdownHeadings(t *testing.T) {
	html, err := RenderMarkdown("## Hello world\n")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, "<h2") || !strings.Contains(html, "Hello world</h2>") {
		t.Errorf("expected an <h2> containing \"Hello world\", got: %q", html)
	}
}

func TestRenderMarkdownGitHubAlertNote(t *testing.T) {
	html, err := RenderMarkdown("> [!NOTE]\n> This is a note\n")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, `class="md-alert md-alert-note"`) {
		t.Errorf("expected blockquote class \"md-alert md-alert-note\", got: %q", html)
	}
	if !strings.Contains(html, `class="md-alert-title"`) {
		t.Errorf("expected title with class \"md-alert-title\", got: %q", html)
	}
	if !strings.Contains(html, "Note</p>") {
		t.Errorf("expected default title \"Note\", got: %q", html)
	}
	if !strings.Contains(html, "This is a note") {
		t.Errorf("expected body content preserved, got: %q", html)
	}
}

func TestRenderMarkdownGitHubAlertCustomTitle(t *testing.T) {
	html, err := RenderMarkdown("> [!NOTE] Heads up\n> Body line\n")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, "Heads up</p>") {
		t.Errorf("expected custom title \"Heads up\", got: %q", html)
	}
}

func TestRenderMarkdownGitHubAlertAllTypes(t *testing.T) {
	for _, kind := range []string{"note", "tip", "warning", "caution", "important"} {
		src := "> [!" + strings.ToUpper(kind) + "]\n> body\n"
		html, err := RenderMarkdown(src)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		want := `class="md-alert md-alert-` + kind + `"`
		if !strings.Contains(html, want) {
			t.Errorf("%s: expected %q in output, got: %q", kind, want, html)
		}
	}
}

func TestRenderMarkdownPlainBlockquoteUnchanged(t *testing.T) {
	html, err := RenderMarkdown("> just a quote\n")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if strings.Contains(html, "md-alert") {
		t.Errorf("plain blockquote should not get the alert classes, got: %q", html)
	}
	if !strings.Contains(html, "<blockquote>") {
		t.Errorf("expected an unchanged <blockquote>, got: %q", html)
	}
}

func TestRenderMarkdownEmpty(t *testing.T) {
	html, err := RenderMarkdown("")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if html != "" {
		t.Errorf("empty input should produce empty output, got: %q", html)
	}
}
