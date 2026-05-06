package style

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTheme_BuiltinNames(t *testing.T) {
	dark, warns := ResolveTheme("dark", t.TempDir())
	if len(warns) != 0 {
		t.Errorf("dark: unexpected warnings %v", warns)
	}
	if dark != DarkPalette() {
		t.Errorf("dark: palette mismatch")
	}

	light, warns := ResolveTheme("light", t.TempDir())
	if len(warns) != 0 {
		t.Errorf("light: unexpected warnings %v", warns)
	}
	if light != LightPalette() {
		t.Errorf("light: palette mismatch")
	}
}

func TestResolveTheme_EmptyNameDefaultsToDark(t *testing.T) {
	p, warns := ResolveTheme("", t.TempDir())
	if len(warns) != 0 {
		t.Errorf("unexpected warnings %v", warns)
	}
	if p != DarkPalette() {
		t.Errorf("empty name should resolve to dark")
	}
}

func TestResolveTheme_LoadsFromThemesSubdir(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `heading "#112233"
success "#445566"
`
	if err := os.WriteFile(filepath.Join(themes, "mytheme.kdl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, warns := ResolveTheme("mytheme", dir)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings %v", warns)
	}
	wantHeading := fg(0x11, 0x22, 0x33)
	wantSuccess := fg(0x44, 0x55, 0x66)
	if p.Heading != wantHeading {
		t.Errorf("Heading = %q, want %q", p.Heading, wantHeading)
	}
	if p.Success != wantSuccess {
		t.Errorf("Success = %q, want %q", p.Success, wantSuccess)
	}
	// Untouched keys must inherit from dark.
	if p.Error != DarkPalette().Error {
		t.Errorf("Error did not inherit from dark; got %q want %q", p.Error, DarkPalette().Error)
	}
}

func TestResolveTheme_MissingFileFallsBackWithWarning(t *testing.T) {
	dir := t.TempDir()
	p, warns := ResolveTheme("doesnotexist", dir)
	if p != DarkPalette() {
		t.Errorf("missing file should fall back to dark")
	}
	if len(warns) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(warns), warns)
	}
	if !strings.Contains(warns[0], "doesnotexist") {
		t.Errorf("warning should mention theme name; got %q", warns[0])
	}
}

func TestResolveTheme_MalformedKDLFallsBackWithWarning(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.Mkdir(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "broken.kdl"), []byte("this is not valid {{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, warns := ResolveTheme("broken", dir)
	if p != DarkPalette() {
		t.Errorf("malformed KDL should fall back to dark")
	}
	if len(warns) == 0 {
		t.Errorf("expected at least one warning")
	}
}

func TestResolveTheme_UnknownKeyWarnsButLoads(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	_ = os.Mkdir(themes, 0o755)
	body := `heading "#112233"
nonsense "#000000"
`
	_ = os.WriteFile(filepath.Join(themes, "weird.kdl"), []byte(body), 0o644)

	p, warns := ResolveTheme("weird", dir)
	if p.Heading != fg(0x11, 0x22, 0x33) {
		t.Errorf("Heading should still load: %q", p.Heading)
	}
	if len(warns) != 1 {
		t.Fatalf("want 1 warning for unknown key, got %d: %v", len(warns), warns)
	}
	if !strings.Contains(warns[0], "nonsense") {
		t.Errorf("warning should mention unknown key; got %q", warns[0])
	}
}

func TestResolveTheme_BadHexWarnsButOtherKeysLoad(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	_ = os.Mkdir(themes, 0o755)
	body := `heading "#112233"
success "not-a-color"
`
	_ = os.WriteFile(filepath.Join(themes, "partial.kdl"), []byte(body), 0o644)

	p, warns := ResolveTheme("partial", dir)
	if p.Heading != fg(0x11, 0x22, 0x33) {
		t.Errorf("Heading should still load")
	}
	if p.Success != DarkPalette().Success {
		t.Errorf("Success should inherit from dark when its hex is bad")
	}
	if len(warns) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(warns), warns)
	}
	if !strings.Contains(warns[0], "success") {
		t.Errorf("warning should mention key 'success'; got %q", warns[0])
	}
}

func TestResolveTheme_BackgroundKeysWork(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	_ = os.Mkdir(themes, 0o755)
	body := `bg-success "#dff5e8"
bg-error   "#fde0e6"
`
	_ = os.WriteFile(filepath.Join(themes, "bgtest.kdl"), []byte(body), 0o644)

	p, warns := ResolveTheme("bgtest", dir)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if p.BgSuccess != bg(0xdf, 0xf5, 0xe8) {
		t.Errorf("BgSuccess = %q, want bg(223,245,232)", p.BgSuccess)
	}
	if p.BgError != bg(0xfd, 0xe0, 0xe6) {
		t.Errorf("BgError = %q", p.BgError)
	}
}
