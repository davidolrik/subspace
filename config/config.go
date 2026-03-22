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
}

// Route maps a hostname pattern to an upstream proxy.
type Route struct {
	Pattern string
	Via     string
}

// Page describes an internal page served on a *.subspace hostname.
type Page struct {
	File  string // absolute path to the KDL file
	Host  string // primary hostname (without .subspace suffix)
	Alias string // optional alias hostname (without .subspace suffix)
}

// Config is the top-level configuration for subspace.
type Config struct {
	Listen        string
	ControlSocket string
	Pages         []Page
	Upstreams     map[string]Upstream
	Routes        []Route
	IncludedFiles []string // absolute paths of all files parsed (main + includes)
}

var reservedHosts = map[string]bool{
	"stats":      true,
	"statistics": true,
}

var validUpstreamTypes = map[string]bool{
	"http":   true,
	"socks5": true,
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

	return p.parseData(data, filepath.Dir(absPath))
}

func (p *parser) parseData(data []byte, baseDir string) error {
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parsing KDL: %w", err)
	}

	for _, node := range doc.Nodes {
		switch node.Name.ValueString() {
		case "listen":
			if len(node.Arguments) < 1 {
				return fmt.Errorf("listen requires an address argument")
			}
			p.cfg.Listen = node.Arguments[0].ValueString()

		case "upstream":
			if len(node.Arguments) < 1 {
				return fmt.Errorf("upstream requires a name argument")
			}
			name := node.Arguments[0].ValueString()
			u, err := parseUpstream(node)
			if err != nil {
				return fmt.Errorf("upstream %q: %w", name, err)
			}
			p.cfg.Upstreams[name] = u

		case "control_socket":
			if len(node.Arguments) < 1 {
				return fmt.Errorf("control_socket requires a path argument")
			}
			p.cfg.ControlSocket = node.Arguments[0].ValueString()

		case "page":
			pg, err := parsePage(node, baseDir)
			if err != nil {
				return err
			}
			if baseDir != "" {
				p.cfg.IncludedFiles = append(p.cfg.IncludedFiles, pg.File)
			}
			p.cfg.Pages = append(p.cfg.Pages, pg)

		case "route":
			r, err := parseRoute(node)
			if err != nil {
				return err
			}
			p.cfg.Routes = append(p.cfg.Routes, r)

		case "include":
			if err := p.handleInclude(node, baseDir); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unknown config node: %q", node.Name.ValueString())
		}
	}

	return nil
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
	for _, r := range cfg.Routes {
		if r.Via == "direct" {
			continue
		}
		if _, ok := cfg.Upstreams[r.Via]; !ok {
			return nil, fmt.Errorf("route %q references unknown upstream %q", r.Pattern, r.Via)
		}
	}

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
		default:
			return u, fmt.Errorf("unknown upstream property: %q", child.Name.ValueString())
		}
	}

	if u.Type == "" {
		return u, fmt.Errorf("missing required property: type")
	}
	if !validUpstreamTypes[u.Type] {
		return u, fmt.Errorf("invalid upstream type %q (must be http or socks5)", u.Type)
	}
	if u.Address == "" {
		return u, fmt.Errorf("missing required property: address")
	}

	return u, nil
}

func parsePage(node *document.Node, baseDir string) (Page, error) {
	if len(node.Arguments) < 1 {
		return Page{}, fmt.Errorf("page requires a path argument")
	}

	filePath := node.Arguments[0].ValueString()

	// Derive hostname from filename by default
	host := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))

	// Optional host= override
	if hostVal, ok := node.Properties.Get("host"); ok && hostVal != nil {
		host = hostVal.ValueString()
	}

	if reservedHosts[host] {
		return Page{}, fmt.Errorf("page host %q is reserved for the statistics page", host)
	}

	// Optional alias=
	var alias string
	if aliasVal, ok := node.Properties.Get("alias"); ok && aliasVal != nil {
		alias = aliasVal.ValueString()
		if reservedHosts[alias] {
			return Page{}, fmt.Errorf("page alias %q is reserved for the statistics page", alias)
		}
	}

	// Resolve file path
	if baseDir != "" {
		filePath = filepath.Join(baseDir, filePath)
	}

	return Page{
		File:  filePath,
		Host:  host,
		Alias: alias,
	}, nil
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

	return Route{
		Pattern: pattern,
		Via:     via,
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
