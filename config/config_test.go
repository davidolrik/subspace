package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `
listen ":8080"

upstream "corporate" {
	type "http"
	address "proxy.corp.com:3128"
	username "user"
	password "pass"
}

upstream "tunnel" {
	type "socks5"
	address "socks.example.com:1080"
}

route ".corp.internal" via="corporate"
route "specific.host.com" via="tunnel"
`

func TestParseConfig(t *testing.T) {
	cfg, err := Parse([]byte(testConfig))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}

	if len(cfg.Upstreams) != 2 {
		t.Fatalf("got %d upstreams, want 2", len(cfg.Upstreams))
	}

	corp, ok := cfg.Upstreams["corporate"]
	if !ok {
		t.Fatal("missing upstream 'corporate'")
	}
	if corp.Type != "http" {
		t.Errorf("corporate.Type = %q, want %q", corp.Type, "http")
	}
	if corp.Address != "proxy.corp.com:3128" {
		t.Errorf("corporate.Address = %q, want %q", corp.Address, "proxy.corp.com:3128")
	}
	if corp.Username != "user" {
		t.Errorf("corporate.Username = %q, want %q", corp.Username, "user")
	}
	if corp.Password != "pass" {
		t.Errorf("corporate.Password = %q, want %q", corp.Password, "pass")
	}

	tunnel, ok := cfg.Upstreams["tunnel"]
	if !ok {
		t.Fatal("missing upstream 'tunnel'")
	}
	if tunnel.Type != "socks5" {
		t.Errorf("tunnel.Type = %q, want %q", tunnel.Type, "socks5")
	}
	if tunnel.Address != "socks.example.com:1080" {
		t.Errorf("tunnel.Address = %q, want %q", tunnel.Address, "socks.example.com:1080")
	}
	if tunnel.Username != "" {
		t.Errorf("tunnel.Username = %q, want empty", tunnel.Username)
	}

	if len(cfg.Routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(cfg.Routes))
	}

	// Routes must preserve order
	if cfg.Routes[0].Pattern != ".corp.internal" {
		t.Errorf("Routes[0].Pattern = %q, want %q", cfg.Routes[0].Pattern, ".corp.internal")
	}
	if cfg.Routes[0].Via != "corporate" {
		t.Errorf("Routes[0].Via = %q, want %q", cfg.Routes[0].Via, "corporate")
	}
	if cfg.Routes[1].Pattern != "specific.host.com" {
		t.Errorf("Routes[1].Pattern = %q, want %q", cfg.Routes[1].Pattern, "specific.host.com")
	}
	if cfg.Routes[1].Via != "tunnel" {
		t.Errorf("Routes[1].Via = %q, want %q", cfg.Routes[1].Via, "tunnel")
	}
}

func TestParseConfigValidatesUpstreamReferences(t *testing.T) {
	input := `
listen ":8080"

upstream "real" {
	type "http"
	address "proxy:3128"
}

route ".example.com" via="nonexistent"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if len(cfg.Routes) != 0 {
		t.Errorf("bad route should be dropped, got %d routes", len(cfg.Routes))
	}
	if !hasErrorContaining(cfg.Errors, "nonexistent") {
		t.Errorf("Errors = %v, want one mentioning the missing upstream", cfg.Errors)
	}
}

func TestParseConfigDirectRouteAllowed(t *testing.T) {
	input := `
listen ":8080"

upstream "corp" {
	type "http"
	address "proxy:3128"
}

route ".corp.com" via="corp"
route "bypass.corp.com" via="direct"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v (via=\"direct\" should be allowed)", err)
	}
	if cfg.Routes[1].Via != "direct" {
		t.Errorf("Routes[1].Via = %q, want %q", cfg.Routes[1].Via, "direct")
	}
}

func TestParseConfigMinimal(t *testing.T) {
	input := `listen ":9090"`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9090")
	}
	if len(cfg.Upstreams) != 0 {
		t.Errorf("got %d upstreams, want 0", len(cfg.Upstreams))
	}
	if len(cfg.Routes) != 0 {
		t.Errorf("got %d routes, want 0", len(cfg.Routes))
	}
}

func TestParseConfigControlSocket(t *testing.T) {
	input := `
listen ":8080"
control_socket "/tmp/subspace.sock"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.ControlSocket != "/tmp/subspace.sock" {
		t.Errorf("ControlSocket = %q, want %q", cfg.ControlSocket, "/tmp/subspace.sock")
	}
}

func TestParseConfigControlSocketDefault(t *testing.T) {
	input := `listen ":8080"`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Should default to ~/.config/subspace/control.sock
	if cfg.ControlSocket == "" {
		t.Error("ControlSocket should have a default value")
	}
	if !strings.HasSuffix(cfg.ControlSocket, "subspace/control.sock") {
		t.Errorf("ControlSocket = %q, want it to end with subspace/control.sock", cfg.ControlSocket)
	}
}

func TestParseConfigPageDerivedHost(t *testing.T) {
	input := `
listen ":8080"
page "dev.kdl"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Pages) != 1 {
		t.Fatalf("got %d links pages, want 1", len(cfg.Pages))
	}
	if cfg.Pages[0].Name != "dev" {
		t.Errorf("Host = %q, want %q", cfg.Pages[0].Name, "dev")
	}
}

func TestParseConfigPageExplicitName(t *testing.T) {
	input := `
listen ":8080"
page "my-file.kdl" name="internal"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Pages[0].Name != "internal" {
		t.Errorf("Host = %q, want %q", cfg.Pages[0].Name, "internal")
	}
}

func TestParseConfigPageAlias(t *testing.T) {
	input := `
listen ":8080"
page "dashboard.kdl" alias="dash"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	lp := cfg.Pages[0]
	if lp.Name != "dashboard" {
		t.Errorf("Host = %q, want %q", lp.Name, "dashboard")
	}
	if lp.Alias != "dash" {
		t.Errorf("Alias = %q, want %q", lp.Alias, "dash")
	}
}

func TestParseConfigMultipleLinks(t *testing.T) {
	input := `
listen ":8080"
page "dev.kdl"
page "ops.kdl"
page "tools.kdl" name="internal" alias="int"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Pages) != 3 {
		t.Fatalf("got %d links pages, want 3", len(cfg.Pages))
	}
	if cfg.Pages[0].Name != "dev" {
		t.Errorf("[0].Name = %q, want %q", cfg.Pages[0].Name, "dev")
	}
	if cfg.Pages[1].Name != "ops" {
		t.Errorf("[1].Name = %q, want %q", cfg.Pages[1].Name, "ops")
	}
	if cfg.Pages[2].Name != "internal" || cfg.Pages[2].Alias != "int" {
		t.Errorf("[2] = %+v", cfg.Pages[2])
	}
}

func TestParseConfigPageNoPages(t *testing.T) {
	input := `listen ":8080"`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Pages) != 0 {
		t.Errorf("got %d links pages, want 0", len(cfg.Pages))
	}
}



func TestParseFilePageResolvesPath(t *testing.T) {
	dir := t.TempDir()

	linksPath := filepath.Join(dir, "dev.kdl")
	writeFile(t, linksPath, `group "Test" { link "Ex" url="https://example.com" }`)

	configPath := filepath.Join(dir, "config.kdl")
	writeFile(t, configPath, `
listen ":8080"
page "dev.kdl"
`)

	cfg, err := ParseFile(configPath)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(cfg.Pages) != 1 {
		t.Fatalf("got %d links pages, want 1", len(cfg.Pages))
	}
	if cfg.Pages[0].File != linksPath {
		t.Errorf("File = %q, want %q", cfg.Pages[0].File, linksPath)
	}

	found := false
	for _, f := range cfg.IncludedFiles {
		if f == linksPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("IncludedFiles = %v, want it to contain %q", cfg.IncludedFiles, linksPath)
	}
}

// --- include tests ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFileInclude(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "upstreams.kdl"), `
upstream "corporate" {
	type "http"
	address "proxy.corp.com:3128"
}
`)
	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "upstreams.kdl"
route ".corp.internal" via="corporate"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if _, ok := cfg.Upstreams["corporate"]; !ok {
		t.Error("expected upstream 'corporate' from included file")
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].Via != "corporate" {
		t.Error("route should reference included upstream")
	}
}

func TestParseFileIncludeGlob(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "upstreams")

	writeFile(t, filepath.Join(sub, "a.kdl"), `
upstream "alpha" {
	type "http"
	address "alpha:3128"
}
`)
	writeFile(t, filepath.Join(sub, "b.kdl"), `
upstream "beta" {
	type "socks5"
	address "beta:1080"
}
`)
	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "upstreams/*.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(cfg.Upstreams) != 2 {
		t.Fatalf("got %d upstreams, want 2", len(cfg.Upstreams))
	}
	if _, ok := cfg.Upstreams["alpha"]; !ok {
		t.Error("missing upstream 'alpha'")
	}
	if _, ok := cfg.Upstreams["beta"]; !ok {
		t.Error("missing upstream 'beta'")
	}
}

func TestParseFileIncludeNested(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "c.kdl"), `
upstream "deep" {
	type "http"
	address "deep:3128"
}
`)
	writeFile(t, filepath.Join(dir, "b.kdl"), `include "c.kdl"`)
	writeFile(t, filepath.Join(dir, "a.kdl"), `
listen ":8080"
include "b.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "a.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if _, ok := cfg.Upstreams["deep"]; !ok {
		t.Error("expected upstream 'deep' from nested include")
	}
}

func TestParseFileIncludeCircular(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "a.kdl"), `
listen ":8080"
include "b.kdl"
`)
	writeFile(t, filepath.Join(dir, "b.kdl"), `include "a.kdl"`)

	_, err := ParseFile(filepath.Join(dir, "a.kdl"))
	if err == nil {
		t.Fatal("expected error for circular include")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %q, want it to mention 'circular'", err)
	}
}

func TestParseFileIncludeNotFound(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "nonexistent.kdl"
`)

	_, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err == nil {
		t.Fatal("expected error for missing include file")
	}
}

func TestParseFileIncludeGlobNoMatch(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "nothing/*.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}
}

func TestParseFileIncludeGlobEmptyDir(t *testing.T) {
	dir := t.TempDir()

	// Create the directory but put no .kdl files in it
	os.MkdirAll(filepath.Join(dir, "upstreams"), 0755)

	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "upstreams/*.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}
}

func TestParseFileIncludedFilesTracked(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "extra.kdl"), `
upstream "extra" {
	type "http"
	address "extra:3128"
}
`)
	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "extra.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if len(cfg.IncludedFiles) != 2 {
		t.Fatalf("IncludedFiles has %d entries, want 2 (main + extra)", len(cfg.IncludedFiles))
	}

	// Both files should be absolute paths
	for _, f := range cfg.IncludedFiles {
		if !filepath.IsAbs(f) {
			t.Errorf("IncludedFiles entry %q is not absolute", f)
		}
	}
}

func TestParseFileIncludeRelativePath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")

	// sub/inner.kdl includes "../upstream.kdl" — relative to sub/, not CWD
	writeFile(t, filepath.Join(dir, "upstream.kdl"), `
upstream "relative" {
	type "http"
	address "relative:3128"
}
`)
	writeFile(t, filepath.Join(sub, "inner.kdl"), `include "../upstream.kdl"`)
	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
include "sub/inner.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if _, ok := cfg.Upstreams["relative"]; !ok {
		t.Error("expected upstream 'relative' from relatively-included file")
	}
}

func TestParseIncludeInDataErrors(t *testing.T) {
	input := `
listen ":8080"
include "some/file.kdl"
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error when include is used with Parse()")
	}
	if !strings.Contains(err.Error(), "include") {
		t.Errorf("error = %q, want it to mention 'include'", err)
	}
}

func TestParseConfigRouteFallback(t *testing.T) {
	input := `
listen ":8080"

upstream "primary" {
	type "http"
	address "proxy1:3128"
}

upstream "backup" {
	type "http"
	address "proxy2:3128"
}

route ".example.com" via="primary" fallback="backup"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(cfg.Routes))
	}
	if cfg.Routes[0].Fallback != "backup" {
		t.Errorf("Fallback = %q, want %q", cfg.Routes[0].Fallback, "backup")
	}
}

func TestParseConfigRouteFallbackOptional(t *testing.T) {
	input := `
listen ":8080"

upstream "corp" {
	type "http"
	address "proxy:3128"
}

route ".example.com" via="corp"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Routes[0].Fallback != "" {
		t.Errorf("Fallback = %q, want empty", cfg.Routes[0].Fallback)
	}
}

func TestParseConfigRouteFallbackDirect(t *testing.T) {
	input := `
listen ":8080"

upstream "corp" {
	type "http"
	address "proxy:3128"
}

route ".example.com" via="corp" fallback="direct"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Routes[0].Fallback != "direct" {
		t.Errorf("Fallback = %q, want %q", cfg.Routes[0].Fallback, "direct")
	}
}

func TestParseConfigRouteFallbackValidation(t *testing.T) {
	input := `
listen ":8080"

upstream "corp" {
	type "http"
	address "proxy:3128"
}

route ".example.com" via="corp" fallback="nonexistent"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("route should be kept (only fallback dropped), got %d routes", len(cfg.Routes))
	}
	if cfg.Routes[0].Fallback != "" {
		t.Errorf("Fallback = %q, want empty (cleared because target was unknown)", cfg.Routes[0].Fallback)
	}
	if !hasErrorContaining(cfg.Errors, "nonexistent") {
		t.Errorf("Errors = %v, want one mentioning the missing fallback", cfg.Errors)
	}
}

func TestParseConfigRouteFallbackSameAsVia(t *testing.T) {
	input := `
listen ":8080"

upstream "corp" {
	type "http"
	address "proxy:3128"
}

route ".example.com" via="corp" fallback="corp"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("route should be kept (only fallback cleared), got %d routes", len(cfg.Routes))
	}
	if cfg.Routes[0].Fallback != "" {
		t.Errorf("Fallback = %q, want empty (cleared because it matched via)", cfg.Routes[0].Fallback)
	}
	if !hasErrorContaining(cfg.Errors, "fallback") {
		t.Errorf("Errors = %v, want one mentioning the fallback conflict", cfg.Errors)
	}
}

func TestParseFileRouteTracksSourceFile(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "routes.kdl"), `
route ".example.com" via="direct"
`)
	writeFile(t, filepath.Join(dir, "main.kdl"), `
listen ":8080"
route "specific.com" via="direct"
include "routes.kdl"
`)

	cfg, err := ParseFile(filepath.Join(dir, "main.kdl"))
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(cfg.Routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(cfg.Routes))
	}

	mainFile := filepath.Join(dir, "main.kdl")
	routesFile := filepath.Join(dir, "routes.kdl")

	if cfg.Routes[0].File != mainFile {
		t.Errorf("Routes[0].File = %q, want %q", cfg.Routes[0].File, mainFile)
	}
	if cfg.Routes[1].File != routesFile {
		t.Errorf("Routes[1].File = %q, want %q", cfg.Routes[1].File, routesFile)
	}
}

func TestParseConfigWireGuardUpstream(t *testing.T) {
	input := `
listen ":8080"

upstream "home" {
	type "wireguard"
	endpoint "vpn.example.com:51820"
	private-key "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	public-key "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	address "10.0.0.2/32"
	dns "1.1.1.1"
}

route ".home.lan" via="home"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	home, ok := cfg.Upstreams["home"]
	if !ok {
		t.Fatal("missing upstream 'home'")
	}
	if home.Type != "wireguard" {
		t.Errorf("Type = %q, want %q", home.Type, "wireguard")
	}
	if home.Endpoint != "vpn.example.com:51820" {
		t.Errorf("Endpoint = %q, want %q", home.Endpoint, "vpn.example.com:51820")
	}
	if home.PrivateKey != "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=" {
		t.Errorf("PrivateKey = %q", home.PrivateKey)
	}
	if home.PublicKey != "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=" {
		t.Errorf("PublicKey = %q", home.PublicKey)
	}
	if home.Address != "10.0.0.2/32" {
		t.Errorf("Address = %q, want %q", home.Address, "10.0.0.2/32")
	}
	if home.DNS != "1.1.1.1" {
		t.Errorf("DNS = %q, want %q", home.DNS, "1.1.1.1")
	}
}

func TestParseConfigWireGuardMissingEndpoint(t *testing.T) {
	input := `
listen ":8080"

upstream "bad" {
	type "wireguard"
	private-key "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	public-key "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	address "10.0.0.2/32"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.Upstreams["bad"]; ok {
		t.Error("invalid upstream should be skipped, but was added")
	}
	if !hasErrorContaining(cfg.Errors, "endpoint") {
		t.Errorf("Errors = %v, want one mentioning the missing endpoint", cfg.Errors)
	}
}

func TestParseConfigWireGuardMissingKeys(t *testing.T) {
	input := `
listen ":8080"

upstream "bad" {
	type "wireguard"
	endpoint "vpn:51820"
	address "10.0.0.2/32"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.Upstreams["bad"]; ok {
		t.Error("invalid upstream should be skipped, but was added")
	}
	if len(cfg.Errors) == 0 {
		t.Error("expected at least one collected error")
	}
}

func TestParseConfigWireGuardDNSOptional(t *testing.T) {
	input := `
listen ":8080"

upstream "home" {
	type "wireguard"
	endpoint "vpn:51820"
	private-key "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	public-key "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	address "10.0.0.2/32"
}

route ".home.lan" via="home"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Upstreams["home"].DNS != "" {
		t.Errorf("DNS = %q, want empty", cfg.Upstreams["home"].DNS)
	}
}

func TestParseConfigValidatesUpstreamType(t *testing.T) {
	input := `
listen ":8080"

upstream "bad" {
	type "ftp"
	address "proxy:21"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.Upstreams["bad"]; ok {
		t.Error("invalid upstream should be skipped, but was added")
	}
	if !hasErrorContaining(cfg.Errors, "ftp") {
		t.Errorf("Errors = %v, want one mentioning the bad type", cfg.Errors)
	}
}

func TestParseTagsBlock(t *testing.T) {
	input := `
listen ":8080"

tags {
	tag "prod" color="#00ff88"
	tag "internal" color="#ff6b6b"
	tag "wip" color="#ffaa00"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(cfg.Tags) != 3 {
		t.Fatalf("got %d tags, want 3", len(cfg.Tags))
	}

	prod, ok := cfg.Tags["prod"]
	if !ok {
		t.Fatal("missing tag 'prod'")
	}
	if prod.Name != "prod" {
		t.Errorf("prod.Name = %q, want %q", prod.Name, "prod")
	}
	if prod.Color != "#00ff88" {
		t.Errorf("prod.Color = %q, want %q", prod.Color, "#00ff88")
	}

	if cfg.Tags["internal"].Color != "#ff6b6b" {
		t.Errorf("internal.Color = %q, want %q", cfg.Tags["internal"].Color, "#ff6b6b")
	}
	if cfg.Tags["wip"].Color != "#ffaa00" {
		t.Errorf("wip.Color = %q, want %q", cfg.Tags["wip"].Color, "#ffaa00")
	}
}

func TestParseTagsEmptyBlock(t *testing.T) {
	input := `
listen ":8080"

tags {
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Tags) != 0 {
		t.Errorf("got %d tags, want 0", len(cfg.Tags))
	}
}

func TestParseTagsNoBlock(t *testing.T) {
	cfg, err := Parse([]byte(`listen ":8080"`))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Tags == nil {
		t.Fatal("Tags map should be initialized, got nil")
	}
	if len(cfg.Tags) != 0 {
		t.Errorf("got %d tags, want 0", len(cfg.Tags))
	}
}

func TestParseTagsDuplicateName(t *testing.T) {
	input := `
tags {
	tag "prod" color="#00ff88"
	tag "prod" color="#ff0000"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	// First definition wins; the duplicate is dropped.
	if cfg.Tags["prod"].Color != "#00ff88" {
		t.Errorf("prod.Color = %q, want first definition kept", cfg.Tags["prod"].Color)
	}
	if !hasErrorContaining(cfg.Errors, "prod") {
		t.Errorf("Errors = %v, want one mentioning duplicate tag", cfg.Errors)
	}
}

func TestParseTagsMissingColor(t *testing.T) {
	input := `
tags {
	tag "prod"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.Tags["prod"]; ok {
		t.Error("tag without color should be skipped, but was added")
	}
	if !hasErrorContaining(cfg.Errors, "color") {
		t.Errorf("Errors = %v, want one mentioning missing color", cfg.Errors)
	}
}

func TestParseTagsMissingName(t *testing.T) {
	input := `
tags {
	tag color="#00ff88"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if len(cfg.Errors) == 0 {
		t.Error("expected at least one collected error")
	}
}

func TestParseTagsUnknownChild(t *testing.T) {
	input := `
tags {
	widget "prod" color="#00ff88"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if !hasErrorContaining(cfg.Errors, "widget") {
		t.Errorf("Errors = %v, want one mentioning unknown child", cfg.Errors)
	}
}

func TestParseTagsAlias(t *testing.T) {
	input := `
tags {
	tag "services"         color="#00ff88"
	tag "olrikit_services" color="#ff0088" alias="services"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	plain := cfg.Tags["services"]
	if plain.Alias != "services" {
		t.Errorf("plain services.Alias = %q, want %q (default to name)", plain.Alias, "services")
	}

	olrikit := cfg.Tags["olrikit_services"]
	if olrikit.Alias != "services" {
		t.Errorf("olrikit_services.Alias = %q, want %q", olrikit.Alias, "services")
	}
	if olrikit.Color != "#ff0088" {
		t.Errorf("olrikit_services.Color = %q, want %q", olrikit.Color, "#ff0088")
	}
}

func TestParseTagsAliasDefaultsToName(t *testing.T) {
	input := `
tags {
	tag "prod" color="#00ff88"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Tags["prod"].Alias != "prod" {
		t.Errorf("prod.Alias = %q, want %q (default to name)", cfg.Tags["prod"].Alias, "prod")
	}
}

func TestParseTagsEmptyColor(t *testing.T) {
	input := `
tags {
	tag "prod" color=""
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.Tags["prod"]; ok {
		t.Error("tag with empty color should be skipped, but was added")
	}
	if !hasErrorContaining(cfg.Errors, "color") {
		t.Errorf("Errors = %v, want one mentioning empty color", cfg.Errors)
	}
}

func TestParseSearchEnginesBlock(t *testing.T) {
	input := `
listen ":8080"

search-engines {
	engine "google"   url="https://www.google.com/search?q={query}" icon="si-google" alias="g"
	engine "metacpan" url="https://metacpan.org/search?q={query}"   icon="fa-cube"   alias="cpan"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(cfg.SearchEngines) != 2 {
		t.Fatalf("got %d search engines, want 2", len(cfg.SearchEngines))
	}

	g, ok := cfg.SearchEngines["google"]
	if !ok {
		t.Fatal("missing search engine 'google'")
	}
	if g.Name != "google" {
		t.Errorf("google.Name = %q, want %q", g.Name, "google")
	}
	if g.URL != "https://www.google.com/search?q={query}" {
		t.Errorf("google.URL = %q, want the templated URL", g.URL)
	}
	if g.Icon != "si-google" {
		t.Errorf("google.Icon = %q, want %q", g.Icon, "si-google")
	}
	if g.Alias != "g" {
		t.Errorf("google.Alias = %q, want %q", g.Alias, "g")
	}

	if cfg.SearchEngines["metacpan"].Alias != "cpan" {
		t.Errorf("metacpan.Alias = %q, want %q", cfg.SearchEngines["metacpan"].Alias, "cpan")
	}
}

func TestParseSearchEnginesDefault(t *testing.T) {
	input := `
search-engines default="google" {
	engine "google" url="https://www.google.com/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.DefaultSearchEngine != "google" {
		t.Errorf("DefaultSearchEngine = %q, want %q", cfg.DefaultSearchEngine, "google")
	}
}

func TestParseSearchEnginesUnknownDefault(t *testing.T) {
	input := `
search-engines default="missing" {
	engine "google" url="https://www.google.com/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if cfg.DefaultSearchEngine != "" {
		t.Errorf("DefaultSearchEngine = %q, want empty after unknown reference", cfg.DefaultSearchEngine)
	}
	if !hasErrorContaining(cfg.Errors, "missing") {
		t.Errorf("Errors = %v, want one mentioning the unknown default", cfg.Errors)
	}
}

func TestParseSearchEnginesNoDefault(t *testing.T) {
	input := `
search-engines {
	engine "google" url="https://www.google.com/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.DefaultSearchEngine != "" {
		t.Errorf("DefaultSearchEngine = %q, want empty when not configured", cfg.DefaultSearchEngine)
	}
	if len(cfg.SearchEngines) != 1 {
		t.Errorf("got %d search engines, want 1", len(cfg.SearchEngines))
	}
}

func TestParseSearchEnginesEmptyBlock(t *testing.T) {
	input := `
search-engines {
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.SearchEngines) != 0 {
		t.Errorf("got %d search engines, want 0", len(cfg.SearchEngines))
	}
}

func TestParseSearchEnginesNoBlock(t *testing.T) {
	cfg, err := Parse([]byte(`listen ":8080"`))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.SearchEngines == nil {
		t.Fatal("SearchEngines map should be initialized, got nil")
	}
	if len(cfg.SearchEngines) != 0 {
		t.Errorf("got %d search engines, want 0", len(cfg.SearchEngines))
	}
}

func TestParseSearchEnginesDuplicateName(t *testing.T) {
	input := `
search-engines {
	engine "google" url="https://www.google.com/search?q={query}"
	engine "google" url="https://other.example.com/?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	// First definition wins; the duplicate is dropped.
	if cfg.SearchEngines["google"].URL != "https://www.google.com/search?q={query}" {
		t.Errorf("google.URL = %q, want first definition kept", cfg.SearchEngines["google"].URL)
	}
	if !hasErrorContaining(cfg.Errors, "google") {
		t.Errorf("Errors = %v, want one mentioning duplicate engine", cfg.Errors)
	}
}

func TestParseSearchEnginesMissingURL(t *testing.T) {
	input := `
search-engines {
	engine "google"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.SearchEngines["google"]; ok {
		t.Error("engine without url should be skipped, but was added")
	}
	if !hasErrorContaining(cfg.Errors, "url") {
		t.Errorf("Errors = %v, want one mentioning missing url", cfg.Errors)
	}
}

func TestParseSearchEnginesMissingPlaceholder(t *testing.T) {
	input := `
search-engines {
	engine "google" url="https://www.google.com/search"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if _, ok := cfg.SearchEngines["google"]; ok {
		t.Error("engine with url missing {query} should be skipped, but was added")
	}
	if !hasErrorContaining(cfg.Errors, "{query}") {
		t.Errorf("Errors = %v, want one mentioning the missing placeholder", cfg.Errors)
	}
}

func TestParseSearchEnginesMissingName(t *testing.T) {
	input := `
search-engines {
	engine url="https://www.google.com/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if len(cfg.Errors) == 0 {
		t.Error("expected at least one collected error")
	}
}

func TestParseSearchEnginesUnknownChild(t *testing.T) {
	input := `
search-engines {
	widget "google" url="https://www.google.com/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if !hasErrorContaining(cfg.Errors, "widget") {
		t.Errorf("Errors = %v, want one mentioning unknown child", cfg.Errors)
	}
}

func TestParseSearchEnginesPreservesNameCase(t *testing.T) {
	input := `
search-engines {
	engine "MetaCPAN" url="https://metacpan.org/search?q={query}" alias="cpan"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Map is keyed by the lowercase name so all lookups are
	// case-insensitive, but the original casing is preserved on the
	// Name field for display.
	e, ok := cfg.SearchEngines["metacpan"]
	if !ok {
		t.Fatal("engine not found by lowercase key")
	}
	if e.Name != "MetaCPAN" {
		t.Errorf("Name = %q, want %q (original casing preserved)", e.Name, "MetaCPAN")
	}
	if _, ok := cfg.SearchEngines["MetaCPAN"]; ok {
		t.Error("engine should not be reachable by its original-case key")
	}
}

func TestParseSearchEnginesDuplicateNameCaseInsensitive(t *testing.T) {
	input := `
search-engines {
	engine "Google" url="https://www.google.com/search?q={query}"
	engine "google" url="https://other.example.com/?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect the error, got: %v", err)
	}
	if len(cfg.SearchEngines) != 1 {
		t.Errorf("got %d engines, want 1 (second is a case-insensitive duplicate)", len(cfg.SearchEngines))
	}
	if cfg.SearchEngines["google"].Name != "Google" {
		t.Errorf("Name = %q, want first definition kept", cfg.SearchEngines["google"].Name)
	}
	if !hasErrorContaining(cfg.Errors, "google") {
		t.Errorf("Errors = %v, want one mentioning the duplicate", cfg.Errors)
	}
}

func TestParseSearchEnginesDefaultCaseInsensitive(t *testing.T) {
	input := `
search-engines default="MetaCPAN" {
	engine "metacpan" url="https://metacpan.org/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.DefaultSearchEngine != "metacpan" {
		t.Errorf("DefaultSearchEngine = %q, want %q (lowercased)", cfg.DefaultSearchEngine, "metacpan")
	}
}

func TestParseSearchEnginesAliasDefaultsEmpty(t *testing.T) {
	input := `
search-engines {
	engine "google" url="https://www.google.com/search?q={query}"
}
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.SearchEngines["google"].Alias != "" {
		t.Errorf("google.Alias = %q, want empty when not configured", cfg.SearchEngines["google"].Alias)
	}
}

func hasErrorContaining(errs []string, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

func TestParseCollectsMultipleErrors(t *testing.T) {
	input := `
listen ":8080"

upstream "good" {
	type "http"
	address "proxy:3128"
}

upstream "badtype" {
	type "ftp"
	address "x:21"
}

route ".ok.com" via="good"
route ".bad.com" via="missing"
route ".half.com" via="good" fallback="missing"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse should succeed and collect errors, got: %v", err)
	}

	// One good upstream remains.
	if _, ok := cfg.Upstreams["good"]; !ok {
		t.Error("good upstream missing")
	}
	if _, ok := cfg.Upstreams["badtype"]; ok {
		t.Error("badtype upstream should be skipped")
	}

	// Two routes survive: the good one and the half-broken one (fallback cleared).
	if len(cfg.Routes) != 2 {
		t.Fatalf("got %d routes, want 2 (the bad-via one is dropped)", len(cfg.Routes))
	}
	for _, r := range cfg.Routes {
		if r.Pattern == ".half.com" && r.Fallback != "" {
			t.Errorf(".half.com fallback should be cleared, got %q", r.Fallback)
		}
	}

	// At least three errors collected: bad upstream type, bad via, bad fallback.
	if len(cfg.Errors) < 3 {
		t.Errorf("got %d collected errors, want at least 3: %v", len(cfg.Errors), cfg.Errors)
	}
}

func TestParseFatalOnKDLSyntaxError(t *testing.T) {
	// Unclosed brace — KDL parse should fail and we return a real error.
	input := `
listen ":8080"
upstream "x" {
	type "http"
	address "y:1"
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected fatal error for KDL syntax error")
	}
}
