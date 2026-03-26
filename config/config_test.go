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
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for route referencing nonexistent upstream")
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

func TestParseConfigValidatesUpstreamType(t *testing.T) {
	input := `
listen ":8080"

upstream "bad" {
	type "ftp"
	address "proxy:21"
}
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for invalid upstream type")
	}
}
