package pages

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// PageConfig holds the parsed content of a page KDL file.
type PageConfig struct {
	Title    string        `json:"Title,omitempty"`
	Footer   string        `json:"Footer,omitempty"`
	Sections []ListSection `json:"Sections"`
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
func ParsePageFile(path string) (*PageConfig, []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return &PageConfig{}, []error{fmt.Errorf("reading page file: %w", err)}
	}
	return ParsePage(data)
}

// ParsePage parses a page configuration from KDL data. See
// ParsePageFile for the error-collection contract.
func ParsePage(data []byte) (*PageConfig, []error) {
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return &PageConfig{}, []error{fmt.Errorf("parsing KDL: %w", err)}
	}

	cfg := &PageConfig{}
	var errs []error

	for _, node := range doc.Nodes {
		switch node.Name.ValueString() {
		case "title":
			if len(node.Arguments) < 1 {
				errs = append(errs, fmt.Errorf("title requires a value argument"))
				continue
			}
			cfg.Title = node.Arguments[0].ValueString()
		case "footer":
			if len(node.Arguments) < 1 {
				errs = append(errs, fmt.Errorf("footer requires a value argument"))
				continue
			}
			cfg.Footer = node.Arguments[0].ValueString()
		case "list":
			s, sectionErrs := parseListSection(node)
			errs = append(errs, sectionErrs...)
			// Drop sections whose name was missing — there's nothing
			// to render and no way to address it from the search
			// palette. Other malformed sub-nodes are tolerated and
			// the partial section is kept.
			if s.Name != "" {
				cfg.Sections = append(cfg.Sections, s)
			}
		default:
			errs = append(errs, fmt.Errorf("unknown node: %q", node.Name.ValueString()))
		}
	}

	return cfg, errs
}

func parseListSection(node *document.Node) (ListSection, []error) {
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
