package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// Private suppresses domain-identifying stats writes (per-domain and
	// per-route counters) for traffic that matches this route. Rollups —
	// total connections, protocol breakdown, per-upstream bytes — still
	// record so totals reconcile with reality. Set via `private=#true`.
	Private bool
	File    string // absolute path to the config file containing this route
}

// Listener describes one bind address the proxy accepts connections on.
// More than one listener may be configured; each carries its own
// `private` and `label` flags so an operator can point an "incognito"
// browser profile at a dedicated port whose traffic never lands in
// the per-domain stats tables.
type Listener struct {
	Address string
	// Private suppresses domain-identifying stats writes (per-domain and
	// per-route counters) for any connection accepted on this listener.
	// Rollups still record.
	Private bool
	// Label is a cosmetic name used in logs and status output to
	// disambiguate listeners (e.g. "incognito"). Empty by default.
	Label string
}

// Page describes an internal page served at pages.subspace.pub/{name}.
type Page struct {
	File  string // absolute path to the KDL file
	Name  string // primary page name (path segment)
	Alias string // optional alias page name
}

// SearchEngine is a globally defined external search target that the
// pages dashboard search palette can route queries to. The Name is the
// primary keyword used to invoke the engine (e.g. typing "google foo"
// searches Google for "foo"); Alias provides an optional second
// keyword for the same engine. URL must contain the literal substring
// "{query}", which is replaced with the URL-encoded query at navigation
// time. Fallback opts the engine into the no-match fallback list shown
// when a query has no other matches; the engine pointed at by the
// block-level default= property is shown in the same list whether or
// not Fallback is set.
type SearchEngine struct {
	Name        string
	Alias       string
	URL         string
	Icon        string
	Description string
	Fallback    bool
	// URLEncode controls how the user's query is encoded when
	// substituted into URL. One of "", "component", "form", or "raw":
	//   - "" / "component" — encodeURIComponent (spaces → %20). Default.
	//   - "form"           — same, but spaces → "+" (form-style).
	//   - "raw"            — passthrough; the query is inserted as-is.
	URLEncode string
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
	// Listeners are the bind addresses the proxy accepts connections on.
	// At least one is required; declaring more than one is supported so
	// an operator can dedicate a port to incognito-style private traffic.
	Listeners     []Listener
	ControlSocket string
	Pages         []Page
	Upstreams     map[string]Upstream
	Routes        []Route
	Tags          map[string]Tag
	// SearchEngines maps engine name → definition. The DefaultSearchEngine
	// (if set) names the engine used for the no-match fallback row in the
	// pages dashboard search palette and must reference an entry in this
	// map; an unknown reference is downgraded to a non-fatal error during
	// finalize and the field is cleared.
	SearchEngines       map[string]SearchEngine
	DefaultSearchEngine string
	// StatsRetention controls how long the SQLite stats database keeps
	// historical samples. Zero means "no automatic pruning" (the
	// configured default in cmd/serve.go applies when the user hasn't
	// set a value).
	StatsRetention time.Duration
	// EnvShell is the shell to spawn for env capture. Empty means
	// cmd/serve.go falls back to $SHELL (or /bin/sh as a last resort).
	EnvShell string
	// EnvRefreshInterval is how often subspace re-reads the operator's
	// environment so markdown cards can pick up new values for tokens
	// like ${PUBLIC_IP}. Zero means cmd/serve.go applies its default.
	// The parser enforces a 10s minimum so a config typo can't turn
	// the refresher into a fork bomb.
	EnvRefreshInterval time.Duration
	// Theme names the CLI color theme. Empty means "dark" (the default).
	// Built-in values "dark" and "light" short-circuit; any other name
	// resolves to <configdir>/themes/<name>.kdl. The actual resolution
	// and palette application happens in cmd/, not here — config just
	// surfaces the operator's choice.
	Theme              string
	IncludedFiles      []string // absolute paths of all files parsed (main + includes)
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

// isBuiltinUpstream reports whether name is a reserved pseudo-upstream
// that needs no `upstream` block. "direct" connects without a proxy;
// "blackhole" drops traffic with a protocol-appropriate refusal.
func isBuiltinUpstream(name string) bool {
	return name == "direct" || name == "blackhole"
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
			Upstreams:     make(map[string]Upstream),
			Tags:          make(map[string]Tag),
			SearchEngines: make(map[string]SearchEngine),
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
			Upstreams:     make(map[string]Upstream),
			Tags:          make(map[string]Tag),
			SearchEngines: make(map[string]SearchEngine),
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
			l, errs := parseListen(node)
			for _, msg := range errs {
				p.collect(currentFile, msg)
			}
			p.cfg.Listeners = append(p.cfg.Listeners, l)

		case "upstream":
			if len(node.Arguments) < 1 {
				p.collect(currentFile, "upstream requires a name argument")
				continue
			}
			name := node.Arguments[0].ValueString()
			if isBuiltinUpstream(name) {
				p.collect(currentFile, fmt.Sprintf("upstream %q: %q is a reserved built-in name", name, name))
				continue
			}
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

		case "theme":
			if len(node.Arguments) < 1 {
				p.collect(currentFile, "theme requires a name argument")
				continue
			}
			p.cfg.Theme = node.Arguments[0].ValueString()

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

		case "search-engines":
			engineErrs := parseSearchEnginesBlock(node, p.cfg.SearchEngines, &p.cfg.DefaultSearchEngine)
			for _, msg := range engineErrs {
				p.collect(currentFile, msg)
			}

		case "stats":
			for _, msg := range parseStatsBlock(node, &p.cfg.StatsRetention) {
				p.collect(currentFile, msg)
			}

		case "env":
			for _, msg := range parseEnvBlock(node, &p.cfg.EnvShell, &p.cfg.EnvRefreshInterval) {
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

	// Validate route references. Built-in pseudo-upstreams ("direct",
	// "blackhole") bypass the Upstreams map — direct connects without a
	// proxy, blackhole drops the traffic. Routes with an unknown via
	// are dropped; routes with a bad fallback keep working but lose the
	// fallback. Errors are collected so the operator can see all of
	// them at once.
	kept := cfg.Routes[:0]
	for _, r := range cfg.Routes {
		if !isBuiltinUpstream(r.Via) {
			if _, ok := cfg.Upstreams[r.Via]; !ok {
				cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q references unknown upstream %q (route dropped)", r.Pattern, r.Via))
				continue
			}
		}
		if r.Fallback != "" {
			if r.Fallback == r.Via {
				cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q: fallback must differ from via (%q) (fallback cleared)", r.Pattern, r.Via))
				r.Fallback = ""
			} else if !isBuiltinUpstream(r.Fallback) {
				if _, ok := cfg.Upstreams[r.Fallback]; !ok {
					cfg.Errors = append(cfg.Errors, fmt.Sprintf("route %q references unknown fallback upstream %q (fallback cleared)", r.Pattern, r.Fallback))
					r.Fallback = ""
				}
			}
		}
		kept = append(kept, r)
	}
	cfg.Routes = kept

	// Validate the configured default search engine resolves to one of
	// the parsed engines. An unknown reference is downgraded to a
	// non-fatal error and the field is cleared so the pages search
	// palette simply renders no fallback row instead of pointing at
	// nothing.
	if cfg.DefaultSearchEngine != "" {
		if _, ok := cfg.SearchEngines[cfg.DefaultSearchEngine]; !ok {
			cfg.Errors = append(cfg.Errors, fmt.Sprintf("search-engines: default %q does not match any configured engine (default cleared)", cfg.DefaultSearchEngine))
			cfg.DefaultSearchEngine = ""
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

// parseSearchEnginesBlock walks the children of a `search-engines { ... }`
// node and adds each successfully parsed engine to the supplied map.
// Engine names are stored under their lowercase form so duplicate
// detection and the default-engine cross-reference are case-insensitive,
// while the original casing is preserved on the SearchEngine.Name field
// for display in the search palette. The block-level `default=`
// property names the engine used as the no-match fallback; it is
// recorded in *defaultName (lowercased) and validated later in
// finalize. Per-child errors are returned as a slice so the caller
// can collect them; the bad engine is skipped and parsing continues.
func parseSearchEnginesBlock(node *document.Node, engines map[string]SearchEngine, defaultName *string) []string {
	var errs []string

	if defVal, ok := node.Properties.Get("default"); ok && defVal != nil {
		if v := defVal.ValueString(); v != "" {
			*defaultName = strings.ToLower(v)
		}
	}

	for _, child := range node.Children {
		if child.Name.ValueString() != "engine" {
			errs = append(errs, fmt.Sprintf("search-engines block: unknown node %q", child.Name.ValueString()))
			continue
		}
		if len(child.Arguments) < 1 {
			errs = append(errs, "engine requires a name argument")
			continue
		}
		name := child.Arguments[0].ValueString()
		if name == "" {
			errs = append(errs, "engine requires a non-empty name")
			continue
		}
		key := strings.ToLower(name)
		if _, exists := engines[key]; exists {
			errs = append(errs, fmt.Sprintf("duplicate engine name %q", name))
			continue
		}

		urlVal, ok := child.Properties.Get("url")
		if !ok || urlVal == nil {
			errs = append(errs, fmt.Sprintf("engine %q requires url property", name))
			continue
		}
		urlStr := urlVal.ValueString()
		if urlStr == "" {
			errs = append(errs, fmt.Sprintf("engine %q requires non-empty url property", name))
			continue
		}
		if !strings.Contains(urlStr, "{query}") {
			errs = append(errs, fmt.Sprintf("engine %q url must contain {query} placeholder", name))
			continue
		}

		var alias string
		if aliasVal, ok := child.Properties.Get("alias"); ok && aliasVal != nil {
			alias = aliasVal.ValueString()
		}

		var icon string
		if iconVal, ok := child.Properties.Get("icon"); ok && iconVal != nil {
			icon = iconVal.ValueString()
		}

		var description string
		if descVal, ok := child.Properties.Get("description"); ok && descVal != nil {
			description = descVal.ValueString()
		}

		var urlEncode string
		if encVal, ok := child.Properties.Get("url-encode"); ok && encVal != nil {
			urlEncode = encVal.ValueString()
			switch urlEncode {
			case "", "component", "form", "raw":
				// ok
			default:
				errs = append(errs, fmt.Sprintf("engine %q has unknown url-encode %q (want \"component\", \"form\", or \"raw\")", name, urlEncode))
				continue
			}
		}

		var fallback bool
		if fbVal, ok := child.Properties.Get("fallback"); ok && fbVal != nil {
			// Accept three syntaxes so operators can pick whichever
			// reads best in their KDL: the v2 keyword form
			// `fallback=#true`, the bare bool `fallback=true`, and
			// the quoted string `fallback="true"`.
			switch v := fbVal.ResolvedValue().(type) {
			case bool:
				fallback = v
			case string:
				fallback = strings.EqualFold(v, "true") || strings.EqualFold(v, "#true")
			}
		}

		engines[key] = SearchEngine{
			Name:        name,
			Alias:       alias,
			URL:         urlStr,
			Icon:        icon,
			Description: description,
			Fallback:    fallback,
			URLEncode:   urlEncode,
		}
	}
	return errs
}

// parseStatsBlock walks `stats { ... }` nodes. Today only `retention`
// is recognised; unknown children are reported as non-fatal errors
// so we can grow the block without breaking older configs.
func parseStatsBlock(node *document.Node, retention *time.Duration) []string {
	var errs []string
	for _, child := range node.Children {
		switch child.Name.ValueString() {
		case "retention":
			if len(child.Arguments) < 1 {
				errs = append(errs, "stats retention requires a duration argument")
				continue
			}
			val := child.Arguments[0].ValueString()
			d, err := parseRetentionDuration(val)
			if err != nil {
				errs = append(errs, fmt.Sprintf("stats retention %q: %v", val, err))
				continue
			}
			*retention = d
		default:
			errs = append(errs, fmt.Sprintf("stats block: unknown node %q", child.Name.ValueString()))
		}
	}
	return errs
}

// EnvMinimumRefresh is the lowest refresh interval the parser will
// accept. Below this we'd be re-spawning the operator's shell often
// enough to be noticeable; the value matches what's documented in
// the operator-facing reference.
const EnvMinimumRefresh = 10 * time.Second

// parseEnvBlock walks `env { ... }` nodes. Recognised children are
// `shell "<path>"` and `refresh "<duration>"`; anything else is a
// non-fatal error so the block can grow without breaking older
// configs. Refresh values that fail to parse, or that fall below
// EnvMinimumRefresh, are reported and the field is left at zero so
// cmd/serve.go applies its own default.
func parseEnvBlock(node *document.Node, shell *string, refresh *time.Duration) []string {
	var errs []string
	for _, child := range node.Children {
		switch child.Name.ValueString() {
		case "shell":
			if len(child.Arguments) < 1 {
				errs = append(errs, "env shell requires a path argument")
				continue
			}
			*shell = child.Arguments[0].ValueString()
		case "refresh":
			if len(child.Arguments) < 1 {
				errs = append(errs, "env refresh requires a duration argument")
				continue
			}
			val := child.Arguments[0].ValueString()
			d, err := time.ParseDuration(val)
			if err != nil {
				errs = append(errs, fmt.Sprintf("env refresh %q: %v", val, err))
				continue
			}
			if d < EnvMinimumRefresh {
				errs = append(errs, fmt.Sprintf("env refresh %q below %s minimum; using default", val, EnvMinimumRefresh))
				continue
			}
			*refresh = d
		default:
			errs = append(errs, fmt.Sprintf("env block: unknown node %q", child.Name.ValueString()))
		}
	}
	return errs
}

// RetentionForever is the sentinel value the parser writes when the
// operator explicitly opts out of stats pruning ("forever" or "0").
// Distinguishing this from a zero-valued (i.e. "not configured")
// field lets cmd/serve.go apply its own default when the user hasn't
// specified anything, while still honouring an explicit "keep
// everything" request.
const RetentionForever = time.Duration(-1)

// parseRetentionDuration extends time.ParseDuration with day suffixes
// ("30d") and the explicit sentinels "forever" / "0" for "never
// prune" — both return RetentionForever. Anything else delegates to
// time.ParseDuration so the standard "12h30m" / "168h" forms keep
// working.
func parseRetentionDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "forever" || s == "0" {
		return RetentionForever, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day count: %s", strings.TrimSuffix(s, "d"))
		}
		if n < 0 {
			return 0, fmt.Errorf("retention must be non-negative")
		}
		if n == 0 {
			return RetentionForever, nil
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("retention must be non-negative")
	}
	if d == 0 {
		return RetentionForever, nil
	}
	return d, nil
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

	var private bool
	if privVal, ok := node.Properties.Get("private"); ok && privVal != nil {
		b, perr := asBool(privVal)
		if perr != nil {
			return Route{}, fmt.Errorf("route %q: %w", pattern, perr)
		}
		private = b
	}

	return Route{
		Pattern:  pattern,
		Via:      via,
		Fallback: fallback,
		Private:  private,
	}, nil
}

// parseListen walks an optional `listen "addr" { ... }` block and
// returns the constructed Listener plus a slice of non-fatal child-node
// errors. Children are optional — the plain `listen "addr"` form parses
// cleanly with no children. Per-child errors are surfaced via the
// returned slice so the rest of the listener still applies; this
// matches how upstream/tags/env blocks behave.
func parseListen(node *document.Node) (Listener, []string) {
	l := Listener{Address: node.Arguments[0].ValueString()}
	var errs []string
	for _, child := range node.Children {
		switch child.Name.ValueString() {
		case "private":
			if len(child.Arguments) < 1 {
				errs = append(errs, fmt.Sprintf("listen %q: private requires a boolean argument", l.Address))
				continue
			}
			b, perr := asBool(child.Arguments[0])
			if perr != nil {
				errs = append(errs, fmt.Sprintf("listen %q: %v", l.Address, perr))
				continue
			}
			l.Private = b
		case "label":
			if len(child.Arguments) < 1 {
				errs = append(errs, fmt.Sprintf("listen %q: label requires a string argument", l.Address))
				continue
			}
			l.Label = child.Arguments[0].ValueString()
		default:
			errs = append(errs, fmt.Sprintf("listen %q: unknown child node %q", l.Address, child.Name.ValueString()))
		}
	}
	return l, errs
}

// asBool resolves a KDL value to a Go bool. Subspace uses KDL v1, which
// spells booleans as bare `true` / `false` keywords; the parser may
// surface them either as a Go bool through ResolvedValue() or as the
// literal string "true"/"false" through ValueString() depending on how
// the value was tokenised. Both forms are accepted.
func asBool(v *document.Value) (bool, error) {
	if v == nil {
		return false, fmt.Errorf("expected boolean, got nil")
	}
	switch x := v.ResolvedValue().(type) {
	case bool:
		return x, nil
	case string:
		switch x {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected boolean (true or false), got %q", v.ValueString())
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
