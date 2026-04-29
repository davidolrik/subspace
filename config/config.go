package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sblinch/kdl-go"
	"github.com/sblinch/kdl-go/document"
)

// Upstream defines a proxy that traffic can be routed through.
type Upstream struct {
	Type     string
	Address  string
	Username string
	Password string

	// WireGuard-specific fields
	Endpoint   string
	PrivateKey string
	PublicKey  string
	DNS        string
}

// Route maps a hostname pattern to an upstream proxy.
type Route struct {
	Pattern  string
	Via      string
	Fallback string
	File     string // absolute path to the config file containing this route
}

// Page describes an internal page served at pages.subspace.pub/{name}.
type Page struct {
	File  string // absolute path to the KDL file
	Name  string // primary page name (path segment)
	Alias string // optional alias page name
}

// Tag is a globally defined label that pages may attach to links and
// list sections. Each tag has its own color and is rendered as a small
// pill in the page UI. The Name is the unique reference key used in
// page KDL files; the Alias is the text shown on the pill (defaults to
// Name) and may be repeated across tags so that multiple uniquely-named
// tags can render with the same display label but different colors.
type Tag struct {
	Name  string
	Alias string
	Color string
}

// Config is the top-level configuration for subspace.
type Config struct {
	Listen        string
	ControlSocket string
	Pages         []Page
	Upstreams     map[string]Upstream
	Routes        []Route
	Tags          map[string]Tag
	IncludedFiles []string // absolute paths of all files parsed (main + includes)
	// Errors holds non-fatal config problems collected during parsing
	// and finalization (e.g. a route that refers to an unknown
	// upstream). Subspace skips the offending item and continues so the
	// rest of the config can still take effect; the operator sees the
	// list at startup logs and on the internal pages banner.
	Errors []string
}

var validUpstreamTypes = map[string]bool{
	"http":      true,
	"socks5":    true,
	"wireguard": true,
}

// ParseFile parses a config file, resolving include directives relative
// to the file's directory. This is the primary entry point for loading
// config from disk.
func ParseFile(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}

	p := &parser{
		cfg: &Config{
			Upstreams: make(map[string]Upstream),
			Tags:      make(map[string]Tag),
		},
		seen: make(map[string]bool),
	}

	if err := p.parseFile(absPath); err != nil {
		return nil, err
	}

	return p.finalize()
}

// Parse parses a KDL configuration document and returns a Config.
// Include directives are not supported — use ParseFile for that.
func Parse(data []byte) (*Config, error) {
	p := &parser{
		cfg: &Config{
			Upstreams: make(map[string]Upstream),
			Tags:      make(map[string]Tag),
		},
		seen:         make(map[string]bool),
		noIncludes:   true,
	}

	if err := p.parseData(data, ""); err != nil {
		return nil, err
	}

	return p.finalize()
}

// parser holds state during recursive config parsing.
type parser struct {
	cfg        *Config
	seen       map[string]bool // absolute paths already parsed (circular include detection)
	noIncludes bool            // true when using Parse() without file context
}

func (p *parser) parseFile(absPath string) error {
	if p.seen[absPath] {
		return fmt.Errorf("circular include: %s", absPath)
	}
	p.seen[absPath] = true
	p.cfg.IncludedFiles = append(p.cfg.IncludedFiles, absPath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", absPath, err)
	}

	return p.parseData(data, filepath.Dir(absPath), absPath)
}

func (p *parser) parseData(data []byte, baseDir string, filePath ...string) error {
	var currentFile string
	if len(filePath) > 0 {
		currentFile = filePath[0]
	}
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parsing KDL: %w", err)
	}

	for _, node := range doc.Nodes {
		switch node.Name.ValueString() {
		case "listen":
			if len(node.Arguments) < 1 {
				p.collect(currentFile, "listen requires an address argument")
				continue
			}
			p.cfg.Listen = node.Arguments[0].ValueString()

		case "upstream":
			if len(node.Arguments) < 1 {
				p.collect(currentFile, "upstream requires a name argument")
				continue
			}
			name := node.Arguments[0].ValueString()
			u, err := parseUpstream(node)
			if err != nil {
				p.collect(currentFile, fmt.Sprintf("upstream %q: %v", name, err))
				continue
			}
			p.cfg.Upstreams[name] = u

		case "control_socket":
			if len(node.Arguments) < 1 {
				p.collect(currentFile, "control_socket requires a path argument")
				continue
			}
			p.cfg.ControlSocket = node.Arguments[0].ValueString()

		case "page":
			pg, err := parsePage(node, baseDir)
			if err != nil {
				p.collect(currentFile, err.Error())
				continue
			}
			if baseDir != "" {
				p.cfg.IncludedFiles = append(p.cfg.IncludedFiles, pg.File)
			}
			p.cfg.Pages = append(p.cfg.Pages, pg)

		case "route":
			r, err := parseRoute(node)
			if err != nil {
				p.collect(currentFile, err.Error())
				continue
			}
			r.File = currentFile
			p.cfg.Routes = append(p.cfg.Routes, r)

		case "tags":
			tagErrs := parseTagsBlock(node, p.cfg.Tags)
			for _, msg := range tagErrs {
				p.collect(currentFile, msg)
			}

		case "include":
			// Includes still propagate fatal errors (missing file,
			// circular reference, glob errors). Per-item problems
			// inside the included file go through the same collect
			// path as the main file.
			if err := p.handleInclude(node, baseDir); err != nil {
				return err
			}

		default:
			p.collect(currentFile, fmt.Sprintf("unknown config node: %q", node.Name.ValueString()))
		}
	}

	return nil
}

// collect appends a non-fatal config error, prefixed with the source
// file when known.
func (p *parser) collect(file, msg string) {
	if file != "" {
		p.cfg.Errors = append(p.cfg.Errors, fmt.Sprintf("%s: %s", file, msg))
		return
	}
	p.cfg.Errors = append(p.cfg.Errors, msg)
}

func (p *parser) handleInclude(node *document.Node, baseDir string) error {
	if p.noIncludes {
		return fmt.Errorf("include directives require ParseFile (not Parse)")
	}

	if len(node.Arguments) < 1 {
		return fmt.Errorf("include requires a path argument")
	}

	pattern := node.Arguments[0].ValueString()
	absPattern := filepath.Join(baseDir, pattern)

	matches, err := filepath.Glob(absPattern)
	if err != nil {
		return fmt.Errorf("include glob %q: %w", pattern, err)
	}

	// If the pattern has no glob meta characters, it's an exact path —
	// error if the file doesn't exist rather than silently skipping.
	if len(matches) == 0 && !hasGlobMeta(pattern) {
		return fmt.Errorf("include file not found: %s", pattern)
	}

	sort.Strings(matches)

	for _, match := range matches {
		if err := p.parseFile(match); err != nil {
			return err
		}
	}

	return nil
}

func (p *parser) finalize() (*Config, error) {
	cfg := p.cfg

	// Apply defaults
	if cfg.ControlSocket == "" {
		cfg.ControlSocket = defaultControlSocket()
	}

	// Validate route references. "direct" is a built-in that bypasses
	// all upstreams, so it doesn't need to exist in the Upstreams map.
	// Routes with an unknown via are dropped; routes with a bad
	// fallback keep working but lose the fallback. Errors are
	// collected so the operator can see all of them at once.
	kept := cfg.Routes[:0]
	for _, r := range cfg.Routes {
		if r.Via != "direct" {
			if _, ok := cfg.Upstreams[r.Via]; !ok {
				cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q references unknown upstream %q (route dropped)", r.Pattern, r.Via))
				continue
			}
		}
		if r.Fallback != "" {
			if r.Fallback == r.Via {
				cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q: fallback must differ from via (%q) (fallback cleared)", r.Pattern, r.Via))
				r.Fallback = ""
			} else if r.Fallback != "direct" {
				if _, ok := cfg.Upstreams[r.Fallback]; !ok {
					cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q references unknown fallback upstream %q (fallback cleared)", r.Pattern, r.Fallback))
					r.Fallback = ""
				}
			}
		}
		kept = append(kept, r)
	}
	cfg.Routes = kept

	return cfg, nil
}

func parseUpstream(node *document.Node) (Upstream, error) {
	var u Upstream

	if len(node.Children) == 0 {
		return u, fmt.Errorf("upstream block requires children (type, address)")
	}

	for _, child := range node.Children {
		if len(child.Arguments) < 1 {
			return u, fmt.Errorf("%q requires a value", child.Name.ValueString())
		}
		val := child.Arguments[0].ValueString()

		switch child.Name.ValueString() {
		case "type":
			u.Type = val
		case "address":
			u.Address = val
		case "username":
			u.Username = val
		case "password":
			u.Password = val
		case "endpoint":
			u.Endpoint = val
		case "private-key":
			u.PrivateKey = val
		case "public-key":
			u.PublicKey = val
		case "dns":
			u.DNS = val
		default:
			return u, fmt.Errorf("unknown upstream property: %q", child.Name.ValueString())
		}
	}

	if u.Type == "" {
		return u, fmt.Errorf("missing required property: type")
	}
	if !validUpstreamTypes[u.Type] {
		return u, fmt.Errorf("invalid upstream type %q (must be http, socks5, or wireguard)", u.Type)
	}

	switch u.Type {
	case "wireguard":
		if u.Endpoint == "" {
			return u, fmt.Errorf("missing required property: endpoint")
		}
		if u.PrivateKey == "" {
			return u, fmt.Errorf("missing required property: private-key")
		}
		if u.PublicKey == "" {
			return u, fmt.Errorf("missing required property: public-key")
		}
		if u.Address == "" {
			return u, fmt.Errorf("missing required property: address")
		}
	default:
		if u.Address == "" {
			return u, fmt.Errorf("missing required property: address")
		}
	}

	return u, nil
}

func parsePage(node *document.Node, baseDir string) (Page, error) {
	if len(node.Arguments) < 1 {
		return Page{}, fmt.Errorf("page requires a path argument")
	}

	filePath := node.Arguments[0].ValueString()

	// Derive page name from filename by default
	name := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	// Optional name= override (also accepts host= for backwards compatibility)
	if nameVal, ok := node.Properties.Get("name"); ok && nameVal != nil {
		name = nameVal.ValueString()
	} else if hostVal, ok := node.Properties.Get("host"); ok && hostVal != nil {
		name = hostVal.ValueString()
	}

	// Optional alias=
	var alias string
	if aliasVal, ok := node.Properties.Get("alias"); ok && aliasVal != nil {
		alias = aliasVal.ValueString()
	}

	// Resolve file path
	if baseDir != "" {
		filePath = filepath.Join(baseDir, filePath)
	}

	return Page{
		File:  filePath,
		Name:  name,
		Alias: alias,
	}, nil
}

// parseTagsBlock walks the children of a `tags { ... }` node and adds
// each successfully parsed tag to the supplied map. Per-child errors
// are returned as a slice so the caller can collect them; the bad tag
// is skipped and parsing continues.
func parseTagsBlock(node *document.Node, tags map[string]Tag) []string {
	var errs []string
	for _, child := range node.Children {
		if child.Name.ValueString() != "tag" {
			errs = append(errs, fmt.Sprintf("tags block: unknown node %q", child.Name.ValueString()))
			continue
		}
		if len(child.Arguments) < 1 {
			errs = append(errs, "tag requires a name argument")
			continue
		}
		name := child.Arguments[0].ValueString()
		if name == "" {
			errs = append(errs, "tag requires a non-empty name")
			continue
		}
		if _, exists := tags[name]; exists {
			errs = append(errs, fmt.Sprintf("duplicate tag name %q", name))
			continue
		}

		colorVal, ok := child.Properties.Get("color")
		if !ok || colorVal == nil {
			errs = append(errs, fmt.Sprintf("tag %q requires color property", name))
			continue
		}
		color := colorVal.ValueString()
		if color == "" {
			errs = append(errs, fmt.Sprintf("tag %q requires non-empty color property", name))
			continue
		}

		alias := name
		if aliasVal, ok := child.Properties.Get("alias"); ok && aliasVal != nil {
			if v := aliasVal.ValueString(); v != "" {
				alias = v
			}
		}

		tags[name] = Tag{Name: name, Alias: alias, Color: color}
	}
	return errs
}

func parseRoute(node *document.Node) (Route, error) {
	if len(node.Arguments) < 1 {
		return Route{}, fmt.Errorf("route requires a pattern argument")
	}

	pattern := node.Arguments[0].ValueString()

	viaVal, ok := node.Properties.Get("via")
	if !ok || viaVal == nil {
		return Route{}, fmt.Errorf("route %q requires via property", pattern)
	}
	via := viaVal.ValueString()
	if via == "" {
		return Route{}, fmt.Errorf("route %q requires via property", pattern)
	}

	var fallback string
	if fbVal, ok := node.Properties.Get("fallback"); ok && fbVal != nil {
		fallback = fbVal.ValueString()
	}

	return Route{
		Pattern:  pattern,
		Via:      via,
		Fallback: fallback,
	}, nil
}

func hasGlobMeta(pattern string) bool {
	for _, c := range pattern {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}

func defaultControlSocket() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "subspace", "control.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "subspace", "control.sock")
}
