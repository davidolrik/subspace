package pages

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// PageConfig holds the parsed content of a page KDL file.
//
// Items preserves the original document order so the frontend can
// interleave full-width markdown bands with grids of lists. Sections
// is a derived links-only view kept for callers that don't care about
// markdown blocks (search palette flattening, tag validation).
type PageConfig struct {
	Title    string        `json:"Title,omitempty"`
	Footer   string        `json:"Footer,omitempty"`
	Items    []TopItem     `json:"Items"`
	Sections []ListSection `json:"Sections"`
}

// TopItem is one top-level entry in a page. Kind is "list" or
// "markdown"; Section / Markdown carry the per-kind payload.
type TopItem struct {
	Kind     string       `json:"Kind"`
	Section  *ListSection `json:"Section,omitempty"`
	Markdown *MarkdownDoc `json:"Markdown,omitempty"`
}

// MarkdownDoc carries the rendered, sanitized HTML and the requested
// grid span. When both Columns and Rows are zero the markdown is a
// full-width band that breaks the surrounding grid. When either is
// non-zero the markdown sits in the grid as a card spanning that many
// columns × rows; the omitted dimension defaults to 1. Columns is
// clamped to the current grid width by CSS at narrow viewports;
// Rows is unclamped because the grid's row count is open-ended.
//
// Float controls horizontal placement of grid cards. "" / "left"
// follow the natural left-to-right grid flow; "right" pins the card
// to the right edge of the grid (clamped along with Columns at
// narrow viewports). Float is ignored when the markdown is a band.
//
// IncludePath is the absolute path of the file the markdown was
// loaded from when `include="..."` was used (regardless of whether
// the file actually existed at parse time — the path is recorded so
// the config watcher can reload the page if the file is created or
// modified later). Empty when the source is inline.
type MarkdownDoc struct {
	HTML        string `json:"HTML"`
	Columns     int    `json:"Columns,omitempty"`
	Rows        int    `json:"Rows,omitempty"`
	Float       string `json:"Float,omitempty"`
	Color       string `json:"Color,omitempty"`
	IncludePath string `json:"IncludePath,omitempty"`
}

// ListSection is a named section within a page that contains an
// ordered mix of links and inline subtitles. Items preserves the
// original KDL order — subtitles are rendered as small headings
// between groups of links. Links is a flat link-only view kept for
// callers (search palette flattening, tag validation) that only care
// about navigable items.
type ListSection struct {
	Name  string     `json:"Name"`
	Color string     `json:"Color,omitempty"`
	Icon  string     `json:"Icon,omitempty"`
	Tags  []string   `json:"Tags,omitempty"`
	Items []ListItem `json:"Items"`
	Links []Link     `json:"Links"`
}

// ListItem is one entry in a ListSection. Kind is "link", "subtitle",
// or "markdown"; the per-kind fields are mutually exclusive — links
// use Name/URL/Icon/Description/Tags, subtitles use Name only,
// markdown uses HTML only.
type ListItem struct {
	Kind        string   `json:"Kind"`
	Name        string   `json:"Name,omitempty"`
	URL         string   `json:"URL,omitempty"`
	Icon        string   `json:"Icon,omitempty"`
	Description string   `json:"Description,omitempty"`
	Tags        []string `json:"Tags,omitempty"`
	HTML        string   `json:"HTML,omitempty"`
}

// Link is a single page link.
type Link struct {
	Name        string   `json:"Name"`
	URL         string   `json:"URL"`
	Icon        string   `json:"Icon,omitempty"`
	Description string   `json:"Description,omitempty"`
	Tags        []string `json:"Tags,omitempty"`
}

// ParsePageFile parses a page KDL file from disk.
//
// Always returns a non-nil PageConfig so callers can keep the page
// registered even when the file is unreadable or the KDL is
// syntactically broken — the operator gets the error in the
// dashboard banner instead of being redirected to the troubleshooting
// docs. When the KDL is well-formed but individual nodes are
// malformed, the bad nodes are skipped and the rest is kept.
//
// Markdown `include="..."` paths are resolved relative to the
// directory of `path`.
func ParsePageFile(path string) (*PageConfig, []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return &PageConfig{}, []error{fmt.Errorf("reading page file: %w", err)}
	}
	return ParsePageWithBase(data, filepath.Dir(path))
}

// ParsePage parses a page configuration from KDL data. See
// ParsePageFile for the error-collection contract. Equivalent to
// ParsePageWithBase with no base directory: relative include paths
// are resolved against the current working directory.
func ParsePage(data []byte) (*PageConfig, []error) {
	return ParsePageWithBase(data, "")
}

// ParsePageWithBase parses a page configuration from KDL data and
// resolves any `markdown include="..."` paths relative to baseDir.
// Absolute paths and paths starting with `~/` ignore baseDir.
func ParsePageWithBase(data []byte, baseDir string) (*PageConfig, []error) {
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return &PageConfig{}, []error{fmt.Errorf("parsing KDL: %w", err)}
	}

	cfg := &PageConfig{}
	var errs []error

	for _, node := range doc.Nodes {
		switch node.Name.ValueString() {
		case "title":
			validateKnownProps(node, "title", nil, &errs)
			if len(node.Arguments) < 1 {
				errs = append(errs, fmt.Errorf("title requires a value argument"))
				continue
			}
			cfg.Title = node.Arguments[0].ValueString()
		case "footer":
			validateKnownProps(node, "footer", nil, &errs)
			if len(node.Arguments) < 1 {
				errs = append(errs, fmt.Errorf("footer requires a value argument"))
				continue
			}
			cfg.Footer = node.Arguments[0].ValueString()
		case "list":
			validateKnownProps(node, "list", []string{"tags", "color", "icon"}, &errs)
			s, sectionErrs := parseListSection(node, baseDir)
			errs = append(errs, sectionErrs...)
			// Drop sections whose name was missing — there's nothing
			// to render and no way to address it from the search
			// palette. Other malformed sub-nodes are tolerated and
			// the partial section is kept.
			if s.Name != "" {
				cfg.Sections = append(cfg.Sections, s)
				section := s
				cfg.Items = append(cfg.Items, TopItem{Kind: "list", Section: &section})
			}
		case "markdown":
			validateKnownProps(node, "markdown", []string{"columns", "rows", "float", "color", "include"}, &errs)
			md, mdErrs := parseMarkdownNode(node, true, baseDir)
			errs = append(errs, mdErrs...)
			if md != nil {
				cfg.Items = append(cfg.Items, TopItem{Kind: "markdown", Markdown: md})
			}
		default:
			errs = append(errs, fmt.Errorf("unknown node: %q", node.Name.ValueString()))
		}
	}

	return cfg, errs
}

// parseMarkdownNode handles a `markdown` element at either the top
// level (allowGrid=true: respects optional columns=N / rows=N
// properties) or inside a list (allowGrid=false: both properties
// are silently ignored since the markdown becomes a list row, not a
// grid cell). baseDir is used to resolve relative `include="..."`
// paths. Returns nil for the doc only when the node is too malformed
// to render anything; a missing include with no fallback still
// returns a placeholder doc so the page renders.
func parseMarkdownNode(node *document.Node, allowGrid bool, baseDir string) (*MarkdownDoc, []error) {
	var errs []error
	columns, rows := 0, 0
	float := ""
	color := ""
	if allowGrid {
		columns = readPositiveIntProp(node, "columns", &errs)
		rows = readPositiveIntProp(node, "rows", &errs)
		float = readFloatProp(node, &errs)
		if v, ok := node.Properties.Get("color"); ok && v != nil {
			color = v.ValueString()
		}

		// Either positioning property present implies "this is a
		// grid card, not a band". Fill in the omitted dimension
		// with 1 so the frontend bands() walker can treat it
		// uniformly. A float= alone (without columns/rows) also
		// becomes a 1×1 card at the requested edge.
		if columns > 0 || rows > 0 || float != "" {
			if columns == 0 {
				columns = 1
			}
			if rows == 0 {
				rows = 1
			}
		}
	}

	// Source resolution:
	//   include="..." → try the file. If it loads, that's the source.
	//   If it fails AND there's an inline value arg, fall back to it.
	//   If it fails with no fallback, render a placeholder card with
	//   the error message so the operator sees what's wrong.
	src := ""
	includePath := ""
	if v, ok := node.Properties.Get("include"); ok && v != nil {
		raw := v.ValueString()
		resolved, rerr := resolveIncludePath(raw, baseDir)
		includePath = resolved // record for the watcher even on failure
		if rerr != nil {
			errs = append(errs, fmt.Errorf("markdown include=%q: %w", raw, rerr))
		} else if data, ferr := os.ReadFile(resolved); ferr == nil {
			src = string(data)
		} else {
			errs = append(errs, fmt.Errorf("markdown include=%q: %w", raw, ferr))
			// Try the inline value as a fallback. Otherwise we'll
			// render a placeholder below.
			if len(node.Arguments) >= 1 {
				src = node.Arguments[0].ValueString()
			}
		}
	} else {
		if len(node.Arguments) < 1 {
			return nil, []error{fmt.Errorf("markdown requires a value argument or include=")}
		}
		src = node.Arguments[0].ValueString()
	}

	if src == "" && includePath != "" {
		// Include failed and there was no usable fallback — show a
		// visible placeholder instead of silently rendering an empty
		// card the operator might miss.
		return &MarkdownDoc{
			HTML:        includePlaceholderHTML(includePath),
			Columns:     columns,
			Rows:        rows,
			Float:       float,
			Color:       color,
			IncludePath: includePath,
		}, errs
	}

	rendered, err := RenderMarkdown(src)
	if err != nil {
		errs = append(errs, fmt.Errorf("rendering markdown: %w", err))
		return nil, errs
	}
	return &MarkdownDoc{
		HTML:        rendered,
		Columns:     columns,
		Rows:        rows,
		Float:       float,
		Color:       color,
		IncludePath: includePath,
	}, errs
}

// resolveIncludePath turns the raw include= value into an absolute
// path. Absolute paths and ~/-prefixed paths ignore baseDir; everything
// else is resolved relative to baseDir. Returns the cleaned absolute
// path even when the file doesn't exist (so the watcher can subscribe
// in case it appears later).
func resolveIncludePath(raw, baseDir string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty path")
	}
	p := raw
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding ~: %w", err)
		}
		if p == "~" {
			p = home
		} else {
			p = filepath.Join(home, p[2:])
		}
	} else if !filepath.IsAbs(p) {
		if baseDir == "" {
			abs, err := filepath.Abs(p)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
		p = filepath.Join(baseDir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// includePlaceholderHTML produces a "this include couldn't be loaded"
// alert that fits the dashboard's existing markdown alert style. The
// path is HTML-escaped so a malicious filename can't inject markup.
func includePlaceholderHTML(path string) string {
	return `<blockquote class="md-alert md-alert-caution">` +
		`<p class="md-alert-title">Include failed</p>` +
		`<p>Could not load <code>` + html.EscapeString(path) + `</code>.</p>` +
		`</blockquote>`
}

// readFloatProp reads the optional float="left|right" property and
// returns "" / "left" (default) or "right". Invalid values are
// reported as a non-fatal error and treated as if the property was
// absent (no float).
func readFloatProp(node *document.Node, errs *[]error) string {
	v, ok := node.Properties.Get("float")
	if !ok || v == nil {
		return ""
	}
	s := v.ValueString()
	switch s {
	case "left":
		return "" // explicit "left" is the same as omitting the prop
	case "right":
		return "right"
	default:
		*errs = append(*errs, fmt.Errorf("markdown float=%q invalid (want \"left\" or \"right\"); ignoring", s))
		return ""
	}
}

// readPositiveIntProp returns the value of a positive-integer KDL
// property, or 0 if the property is absent. Non-integer values and
// values < 1 append a non-fatal error and return 0 so the caller can
// treat them as if the property was omitted.
func readPositiveIntProp(node *document.Node, name string, errs *[]error) int {
	v, ok := node.Properties.Get(name)
	if !ok || v == nil {
		return 0
	}
	n, err := valueToInt(v)
	switch {
	case err != nil:
		*errs = append(*errs, fmt.Errorf("markdown %s=%q invalid (want a positive integer); ignoring", name, v.ValueString()))
		return 0
	case n < 1:
		*errs = append(*errs, fmt.Errorf("markdown %s=%d invalid (want >= 1); ignoring", name, n))
		return 0
	default:
		return n
	}
}

// valueToInt extracts a Go int from a kdl-go *Value via its
// ResolvedValue() pathway. Non-numeric and non-integer numeric values
// (floats, big.Float, strings, bools) all return an error so the
// caller can build the right diagnostic.
func valueToInt(v *document.Value) (int, error) {
	switch n := v.ResolvedValue().(type) {
	case int64:
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, fmt.Errorf("not an integer (got %T)", n)
	}
}

func parseListSection(node *document.Node, baseDir string) (ListSection, []error) {
	if len(node.Arguments) < 1 {
		return ListSection{}, []error{fmt.Errorf("list requires a name argument")}
	}

	s := ListSection{
		Name: node.Arguments[0].ValueString(),
	}

	if colorVal, ok := node.Properties.Get("color"); ok && colorVal != nil {
		s.Color = colorVal.ValueString()
	}
	if iconVal, ok := node.Properties.Get("icon"); ok && iconVal != nil {
		s.Icon = iconVal.ValueString()
	}
	s.Tags = parseTagsProperty(node)

	var errs []error
	for _, child := range node.Children {
		switch child.Name.ValueString() {
		case "link":
			l, err := parseLink(child)
			if err != nil {
				errs = append(errs, fmt.Errorf("list %q: %w", s.Name, err))
				continue
			}
			s.Links = append(s.Links, l)
			s.Items = append(s.Items, ListItem{
				Kind:        "link",
				Name:        l.Name,
				URL:         l.URL,
				Icon:        l.Icon,
				Description: l.Description,
				Tags:        l.Tags,
			})
		case "title":
			if len(child.Arguments) < 1 {
				errs = append(errs, fmt.Errorf("list %q: title requires a value argument", s.Name))
				continue
			}
			name := child.Arguments[0].ValueString()
			if name == "" {
				errs = append(errs, fmt.Errorf("list %q: title requires a non-empty value", s.Name))
				continue
			}
			s.Items = append(s.Items, ListItem{
				Kind: "subtitle",
				Name: name,
			})
		case "markdown":
			md, mdErrs := parseMarkdownNode(child, false, baseDir)
			for _, e := range mdErrs {
				errs = append(errs, fmt.Errorf("list %q: %w", s.Name, e))
			}
			if md != nil {
				s.Items = append(s.Items, ListItem{Kind: "markdown", HTML: md.HTML})
			}
		default:
			errs = append(errs, fmt.Errorf("list %q: unknown node %q", s.Name, child.Name.ValueString()))
		}
	}

	return s, errs
}

func parseLink(node *document.Node) (Link, error) {
	if len(node.Arguments) < 1 {
		return Link{}, fmt.Errorf("link requires a name argument")
	}

	l := Link{
		Name: node.Arguments[0].ValueString(),
	}

	urlVal, ok := node.Properties.Get("url")
	if !ok || urlVal == nil {
		return Link{}, fmt.Errorf("link %q requires url property", l.Name)
	}
	l.URL = urlVal.ValueString()
	if l.URL == "" {
		return Link{}, fmt.Errorf("link %q requires url property", l.Name)
	}

	if iconVal, ok := node.Properties.Get("icon"); ok && iconVal != nil {
		l.Icon = iconVal.ValueString()
	}
	if descVal, ok := node.Properties.Get("description"); ok && descVal != nil {
		l.Description = descVal.ValueString()
	}
	l.Tags = parseTagsProperty(node)

	return l, nil
}

// validateKnownProps emits a non-fatal error for every property on
// `node` whose name is not in `allowed`. Used at the top level where
// typos are worth flagging — inside a list we deliberately stay
// lenient so an operator's experimental `link "x" url="..." note="..."`
// or `markdown badge="ship" "..."` doesn't get rejected. `allowed`
// may be nil, meaning the node accepts no properties.
func validateKnownProps(node *document.Node, kind string, allowed []string, errs *[]error) {
	for name := range node.Properties.Unordered() {
		known := false
		for _, a := range allowed {
			if a == name {
				known = true
				break
			}
		}
		if known {
			continue
		}
		label := kind
		if kind == "list" || kind == "markdown" {
			if len(node.Arguments) > 0 {
				label = fmt.Sprintf("%s %q", kind, node.Arguments[0].ValueString())
			}
		}
		*errs = append(*errs, fmt.Errorf("%s: unknown property %q", label, name))
	}
}

// parseTagsProperty reads the optional "tags" property and splits it
// on whitespace. Returns nil when the property is absent or empty so
// that JSON serialization with omitempty works as expected.
func parseTagsProperty(node *document.Node) []string {
	val, ok := node.Properties.Get("tags")
	if !ok || val == nil {
		return nil
	}
	fields := strings.Fields(val.ValueString())
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	return fields
}
