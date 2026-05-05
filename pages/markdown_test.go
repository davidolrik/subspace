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

func TestStripCommonIndentTabs(t *testing.T) {
	// Operator-facing scenario: a markdown node nested inside a
	// `list "..." { markdown r#" ... "# }` block in a tab-indented
	// config file. The leading indentation on the first content line
	// (one tab here) should be stripped from every following line so
	// the rendered markdown is flush-left.
	in := "\t## Heading\n" +
		"\tBody line 1.\n" +
		"\n" + // blank line stays blank
		"\t- item one\n" +
		"\t- item two\n"
	want := "## Heading\n" +
		"Body line 1.\n" +
		"\n" +
		"- item one\n" +
		"- item two\n"
	if got := stripCommonIndent(in); got != want {
		t.Errorf("stripCommonIndent\n got:\n%q\nwant:\n%q", got, want)
	}
}

func TestStripCommonIndentSpaces(t *testing.T) {
	in := "    Line 1\n" +
		"    Line 2\n"
	want := "Line 1\nLine 2\n"
	if got := stripCommonIndent(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestStripCommonIndentLeadingBlankLine(t *testing.T) {
	// Heredoc-style sources often start with a newline immediately
	// after the opening quote; the indent of the SECOND line (the
	// first non-blank) should determine the prefix.
	in := "\n" +
		"        Line 1\n" +
		"        Line 2\n"
	want := "Line 1\nLine 2\n"
	if got := stripCommonIndent(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestStripCommonIndentMixedDeeperLines(t *testing.T) {
	// Lines indented MORE than the first line keep their extra
	// indentation — markdown's own list/code semantics rely on it.
	in := "  - top item\n" +
		"    - nested item\n" +
		"  - back to top\n"
	want := "- top item\n" +
		"  - nested item\n" +
		"- back to top\n"
	if got := stripCommonIndent(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestStripCommonIndentSingleLineUnchanged(t *testing.T) {
	in := "no newline here"
	if got := stripCommonIndent(in); got != in {
		t.Errorf("single-line input must round-trip, got %q", got)
	}
}

func TestStripCommonIndentNoIndent(t *testing.T) {
	in := "Line 1\nLine 2\n"
	if got := stripCommonIndent(in); got != in {
		t.Errorf("uninidented input must round-trip, got %q", got)
	}
}

func TestStripCommonIndentBlankLinesIgnored(t *testing.T) {
	// Blank or whitespace-only lines never tighten the common
	// prefix — otherwise a stray blank line at zero indent would
	// disable the strip.
	in := "    Line 1\n\n    Line 2\n   \n    Line 3\n"
	want := "Line 1\n\nLine 2\n\nLine 3\n"
	if got := stripCommonIndent(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRenderMarkdownStripsIndent(t *testing.T) {
	// End-to-end: indented multi-line source survives RenderMarkdown
	// as if it had been written flush-left.
	src := "\t## Heading\n\tBody line\n"
	html, err := RenderMarkdown(src)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	if !strings.Contains(html, "<h2") || !strings.Contains(html, "Heading</h2>") {
		t.Errorf("indented heading should still render as <h2>, got: %q", html)
	}
	// If indentation weren't stripped the body would render as a
	// 4-space code block instead of a paragraph.
	if strings.Contains(html, "<pre") {
		t.Errorf("body should be a paragraph, not a code block: %q", html)
	}
}

func TestRenderMarkdownTaskListInteractive(t *testing.T) {
	html, err := RenderMarkdown("- [ ] todo\n- [x] done\n")
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	// Both checkboxes survive sanitisation.
	if strings.Count(html, "<input") != 2 {
		t.Fatalf("expected two <input> elements, got: %q", html)
	}
	// `disabled` is removed so the checkboxes are clickable.
	if strings.Contains(html, "disabled") {
		t.Errorf("rendered task list must not carry disabled, got: %q", html)
	}
	// The checked state on `[x]` survives.
	if !strings.Contains(html, "checked") {
		t.Errorf("checked state should be preserved for [x] items, got: %q", html)
	}
	// The frontend uses .md-task on the <li> to address task rows.
	if !strings.Contains(html, "md-task") {
		t.Errorf("task <li> should be marked with the md-task class, got: %q", html)
	}
}

func TestRenderMarkdownFencedCodeWithLanguage(t *testing.T) {
	src := "```go\nfunc Hello() string { return \"hi\" }\n```\n"
	html, err := RenderMarkdown(src)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	// Chroma class-based highlighting wraps tokens in <span class="...">.
	// Specific class names depend on the lexer, but the wrapper always
	// has a `chroma` class so the dashboard's CSS can target it.
	if !strings.Contains(html, "chroma") {
		t.Errorf("expected chroma class on rendered code block, got: %q", html)
	}
	// Keyword tokens for Go ("func", "return") should be emitted as
	// <span> elements (not plain text), so the class survives sanitisation.
	if strings.Count(html, "<span") < 2 {
		t.Errorf("expected several <span> tokens for highlighted Go code, got: %q", html)
	}
}

func TestRenderMarkdownFencedCodeUnknownLanguage(t *testing.T) {
	// An unknown language must not break rendering — chroma falls back
	// to a plain-text lexer.
	src := "```not-a-real-lang\nhello\n```\n"
	html, err := RenderMarkdown(src)
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, "hello") {
		t.Errorf("body content lost: %q", html)
	}
}

func TestRenderMarkdownInlineCodeUnchanged(t *testing.T) {
	// Inline `code` should not be highlighted (chroma only fires on
	// fenced blocks). Sanity check so we don't accidentally syntax-
	// highlight prose.
	html, err := RenderMarkdown("Use `make build` to compile.")
	if err != nil {
		t.Fatalf("RenderMarkdown error: %v", err)
	}
	if !strings.Contains(html, "<code>make build</code>") {
		t.Errorf("inline code should render as a plain <code>, got: %q", html)
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

func TestExpandVarsCases(t *testing.T) {
	lookup := func(name string) (string, bool) {
		switch name {
		case "USER":
			return "mandse", true
		case "PUBLIC_IP":
			return "1.2.3.4", true
		case "EMPTY":
			return "", true
		}
		return "", false
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single substitution", "Hi ${USER}", "Hi mandse"},
		{"multiple substitutions on one line", "${USER}@${PUBLIC_IP}", "mandse@1.2.3.4"},
		{"bare dollar untouched", "It costs $50.", "It costs $50."},
		{"dollar before letters untouched", "Use $PATH like a shell pro.", "Use $PATH like a shell pro."},
		{"escape leaves literal token", "Literal: $${USER}", "Literal: ${USER}"},
		{"undefined left as token", "Greetings ${MISSING}", "Greetings ${MISSING}"},
		{"empty value substitutes empty string", "Value=[${EMPTY}]", "Value=[]"},
		{"empty braces left as-is", "${} should not match", "${} should not match"},
		{"name starting with digit not matched", "${1FOO}", "${1FOO}"},
		{"underscore-only name allowed", "${_X}", "${_X}"}, // _X undefined → token preserved
		{"adjacent substitutions", "${USER}${USER}", "mandsemandse"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandVars(tc.in, lookup)
			if got != tc.want {
				t.Errorf("expandVars(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandVarsNilLookup(t *testing.T) {
	// A nil lookup means "no env wired up" — every token should be
	// left as-is so the rendered card visibly shows the missing
	// reference instead of silently blanking out.
	got := expandVars("Hi ${USER}!", nil)
	if got != "Hi ${USER}!" {
		t.Errorf("nil lookup should preserve tokens, got %q", got)
	}
}

func TestCollectVarRefs(t *testing.T) {
	src := "Hi ${USER} from ${HOST}, again ${USER}. Cost: $50. Escaped: $${SKIP}."
	refs := collectVarRefs(src)
	for _, want := range []string{"USER", "HOST"} {
		if _, ok := refs[want]; !ok {
			t.Errorf("expected %q in refs, got %v", want, refs)
		}
	}
	if _, ok := refs["SKIP"]; ok {
		t.Errorf("$${SKIP} is escaped, must not appear in refs: %v", refs)
	}
	if len(refs) != 2 {
		t.Errorf("expected exactly 2 refs (USER, HOST), got %v", refs)
	}
}

func TestRenderMarkdownWithEnvSubstitutes(t *testing.T) {
	lookup := func(name string) (string, bool) {
		if name == "USER" {
			return "mandse", true
		}
		return "", false
	}
	html, err := RenderMarkdownWithEnv("Hello, **${USER}**!", lookup)
	if err != nil {
		t.Fatalf("RenderMarkdownWithEnv error: %v", err)
	}
	if !strings.Contains(html, "<strong>mandse</strong>") {
		t.Errorf("expected substituted bold value, got: %q", html)
	}
}

func TestRenderMarkdownWithEnvCodeFence(t *testing.T) {
	// Inside fenced code blocks, ${VAR} should still expand —
	// otherwise users couldn't show, say, a current PUBLIC_IP value
	// in a `code` callout. Chroma highlighting should still apply.
	lookup := func(name string) (string, bool) {
		if name == "PUBLIC_IP" {
			return "10.0.0.1", true
		}
		return "", false
	}
	src := "```\nip=${PUBLIC_IP}\n```"
	html, err := RenderMarkdownWithEnv(src, lookup)
	if err != nil {
		t.Fatalf("RenderMarkdownWithEnv error: %v", err)
	}
	if !strings.Contains(html, "10.0.0.1") {
		t.Errorf("expected substituted IP inside code block, got: %q", html)
	}
}

func TestRenderMarkdownWithEnvNilLookupMatchesPlain(t *testing.T) {
	// Confirms the wrapper degrades to RenderMarkdown's behaviour when
	// no env is wired. The unsubstituted token should land in the
	// rendered output verbatim (HTML-escaped where goldmark sees fit).
	html, err := RenderMarkdownWithEnv("token: ${USER}", nil)
	if err != nil {
		t.Fatalf("RenderMarkdownWithEnv error: %v", err)
	}
	if !strings.Contains(html, "${USER}") {
		t.Errorf("nil lookup should preserve tokens in rendered output, got: %q", html)
	}
}
