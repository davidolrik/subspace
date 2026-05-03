package pages

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// markdownRenderer is the package-wide goldmark instance. GFM gives us
// tables, strikethrough, autolinks, and task lists on top of CommonMark.
// html.WithUnsafe lets raw HTML in the source pass through goldmark; the
// bluemonday sanitizer downstream is what actually keeps us safe.
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// markdownPolicy is bluemonday's UGC profile, which permits the elements
// CommonMark + GFM produce (headings, lists, tables, code, blockquote,
// links, basic inline formatting) and strips scripts, event handlers,
// dangerous URI schemes, etc. We disable bluemonday's automatic
// rel="nofollow" so addLinkTarget can write the rel value we want, and
// allow id on headings so goldmark's auto-heading-ids survive.
var markdownPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.RequireNoFollowOnLinks(false)
	p.RequireNoFollowOnFullyQualifiedLinks(false)
	p.AllowAttrs("target").OnElements("a")
	p.AllowAttrs("rel").OnElements("a")
	p.AllowAttrs("id").Matching(bluemonday.SpaceSeparatedTokens).OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	// GFM task-list checkboxes are clickable in the dashboard so
	// state can be toggled and persisted client-side.
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^md-task$`)).OnElements("li")
	return p
}()

// RenderMarkdown parses CommonMark + GFM markdown and returns sanitized
// HTML safe to inject via x-html. All anchors are forced to open in a
// new tab with rel="noopener noreferrer" to match the rest of the
// dashboard's link behaviour and to prevent reverse-tabnabbing.
//
// GitHub-flavored alerts are recognised — a blockquote whose first
// paragraph starts with `[!NOTE]`, `[!TIP]`, `[!WARNING]`, `[!CAUTION]`,
// or `[!IMPORTANT]` is rewritten with `md-alert` classes so the dashboard
// can style it as a callout. The marker may optionally be followed by
// a custom title (`> [!NOTE] Custom title`); otherwise the type's name
// is used as the title.
func RenderMarkdown(src string) (string, error) {
	if strings.TrimSpace(src) == "" {
		return "", nil
	}
	src = stripCommonIndent(src)
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}
	// Tag GFM task-list <li>s and drop the `disabled` attribute
	// BEFORE sanitising so the policy's class allowlist actually
	// sees the class on the <li>. (Sanitising first would strip
	// the attribute we're about to add.)
	pre := transformTaskLists(buf.String())
	clean := markdownPolicy.SanitizeBytes([]byte(pre))
	out := transformAlerts(string(clean))
	out = addLinkTarget(out)
	return strings.TrimRight(out, "\n"), nil
}

// taskLiRegex finds a <li> whose first child is a task-list checkbox
// (i.e. a goldmark-rendered `- [ ]` / `- [x]` item). Group 1 is the
// existing space between `<li` and the rest of the tag (zero or
// more characters); we use it to slot a `class="md-task"` in.
var taskLiRegex = regexp.MustCompile(`(?s)<li(\s*)>(\s*<input[^>]*type="checkbox"[^>]*>)`)

// taskInputRegex matches a task-list checkbox <input> tag in any
// attribute order. Group 1 is the inside of the tag (every
// attribute); we rebuild the tag without `disabled` so the checkbox
// becomes clickable.
var taskInputRegex = regexp.MustCompile(`<input([^>]*type="checkbox"[^>]*)>`)
var disabledAttrRegex = regexp.MustCompile(`\s*disabled(="[^"]*")?`)

func transformTaskLists(html string) string {
	html = taskLiRegex.ReplaceAllString(html, `<li class="md-task">$2`)
	html = taskInputRegex.ReplaceAllStringFunc(html, func(m string) string {
		sub := taskInputRegex.FindStringSubmatch(m)
		attrs := disabledAttrRegex.ReplaceAllString(sub[1], "")
		return "<input" + attrs + ">"
	})
	return html
}

// stripCommonIndent trims a leading-whitespace prefix from every line
// of src so operators can write `markdown r#" ... "#` nodes indented
// to match the surrounding KDL block without that indentation leaking
// into the rendered markdown (where four leading spaces would turn
// the line into a code block, list bullets would lose their meaning,
// etc.).
//
// The prefix is taken from the first non-blank line — that's the
// "intended" indentation for the block. Blank or whitespace-only
// lines are passed through as empty strings, so they don't tighten
// the prefix when the source has stray blank lines at zero indent.
// Lines indented more than the prefix keep their extra leading
// whitespace; lines indented less are left untouched (we only strip
// the exact prefix, never partial whitespace). A heredoc-style
// leading blank line — common with `r#"\n...content...\n"#` — is
// also trimmed so it doesn't render as an extra paragraph break.
func stripCommonIndent(src string) string {
	if !strings.Contains(src, "\n") {
		return src
	}
	// Drop a single leading blank line introduced by the
	// `r#"\n...` heredoc convention. Multiple leading blanks (rare,
	// usually intentional) are preserved so they remain authorial.
	if strings.HasPrefix(src, "\n") {
		src = src[1:]
	}
	lines := strings.Split(src, "\n")
	prefix := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		i := 0
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		prefix = line[:i]
		break
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = strings.TrimPrefix(line, prefix)
	}
	return strings.Join(lines, "\n")
}

// alertRegex matches a blockquote whose first paragraph begins with a
// GitHub-style alert marker. Group 1 is the alert type (uppercase),
// group 2 is anything between the marker and the first newline (the
// optional custom title), and group 3 is the rest of the first
// paragraph (the body up to </p>).
var alertRegex = regexp.MustCompile(`(?s)<blockquote>\s*<p>\[!(NOTE|TIP|WARNING|CAUTION|IMPORTANT)\]([^\n<]*)(?:\n(.*?))?</p>`)

// defaultAlertTitles maps the marker type to its capitalised default
// title used when the author writes `> [!NOTE]` with no explicit
// title after the marker.
var defaultAlertTitles = map[string]string{
	"NOTE":      "Note",
	"TIP":       "Tip",
	"WARNING":   "Warning",
	"CAUTION":   "Caution",
	"IMPORTANT": "Important",
}

// transformAlerts rewrites GitHub-style alert blockquotes in the
// already-sanitised HTML, adding md-alert classes and splitting the
// marker line off into its own title paragraph. Runs after the
// sanitiser so the class attributes don't need a bluemonday whitelist.
func transformAlerts(html string) string {
	return alertRegex.ReplaceAllStringFunc(html, func(m string) string {
		sub := alertRegex.FindStringSubmatch(m)
		kind := sub[1]
		title := strings.TrimSpace(sub[2])
		if title == "" {
			title = defaultAlertTitles[kind]
		}
		body := ""
		if len(sub) > 3 {
			body = strings.TrimSpace(sub[3])
		}
		var sb strings.Builder
		sb.WriteString(`<blockquote class="md-alert md-alert-`)
		sb.WriteString(strings.ToLower(kind))
		sb.WriteString(`"><p class="md-alert-title">`)
		sb.WriteString(title)
		sb.WriteString(`</p>`)
		if body != "" {
			sb.WriteString(`<p>`)
			sb.WriteString(body)
			sb.WriteString(`</p>`)
		}
		return sb.String()
	})
}

// addLinkTarget rewrites every <a href="..."> emitted by goldmark to
// open in a new tab. We do this with a simple string scan rather than
// HTML parsing because the sanitizer guarantees the input is well-formed
// and only contains the small subset of tags UGCPolicy allows.
func addLinkTarget(html string) string {
	var b strings.Builder
	b.Grow(len(html) + 64)

	i := 0
	for {
		idx := strings.Index(html[i:], "<a ")
		if idx < 0 {
			b.WriteString(html[i:])
			return b.String()
		}
		idx += i
		end := strings.Index(html[idx:], ">")
		if end < 0 {
			b.WriteString(html[i:])
			return b.String()
		}
		end += idx
		b.WriteString(html[i:idx])

		tag := html[idx : end+1]
		// Only inject when href is present and target/rel aren't
		// already set — preserves any explicit author choice.
		if strings.Contains(tag, "href=") {
			if !strings.Contains(tag, "target=") {
				tag = strings.Replace(tag, "<a ", `<a target="_blank" `, 1)
			}
			if !strings.Contains(tag, "rel=") {
				tag = strings.Replace(tag, "<a ", `<a rel="noopener noreferrer" `, 1)
			}
		}
		b.WriteString(tag)
		i = end + 1
	}
}
