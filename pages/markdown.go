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
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}
	clean := markdownPolicy.SanitizeBytes(buf.Bytes())
	out := transformAlerts(string(clean))
	out = addLinkTarget(out)
	return strings.TrimRight(out, "\n"), nil
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
