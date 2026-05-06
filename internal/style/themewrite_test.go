package style

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteThemeFile_RoundTripDark(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "exported.kdl")

	if err := WriteThemeFile(path, DarkPalette(), false); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, warns := ResolveTheme("exported", dir)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings on reload: %v", warns)
	}
	if loaded != DarkPalette() {
		t.Errorf("round-trip mismatch")
	}
}

func TestWriteThemeFile_RoundTripLight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "lite.kdl")

	if err := WriteThemeFile(path, LightPalette(), false); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, warns := ResolveTheme("lite", dir)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings on reload: %v", warns)
	}
	if loaded != LightPalette() {
		t.Errorf("light round-trip mismatch")
	}
}

func TestWriteThemeFile_AutoCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	// themes/ subdir does not exist yet
	path := filepath.Join(dir, "themes", "auto.kdl")

	if err := WriteThemeFile(path, DarkPalette(), false); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "themes")); err != nil {
		t.Errorf("themes/ subdir was not created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("output file not written: %v", err)
	}
}

func TestWriteThemeFile_RefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "conflict.kdl")

	if err := WriteThemeFile(path, DarkPalette(), false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	err := WriteThemeFile(path, LightPalette(), false)
	if err == nil {
		t.Fatalf("expected refusal to overwrite, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention existence; got %v", err)
	}

	// File contents must be the original (dark).
	loaded, _ := ResolveTheme("conflict", dir)
	if loaded != DarkPalette() {
		t.Errorf("file should still hold dark palette after refused overwrite")
	}
}

func TestWriteThemeFile_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "swap.kdl")

	if err := WriteThemeFile(path, DarkPalette(), false); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteThemeFile(path, LightPalette(), true); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
	loaded, _ := ResolveTheme("swap", dir)
	if loaded != LightPalette() {
		t.Errorf("file should now hold light palette")
	}
}

func TestWriteThemeFile_ContainsAllKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "full.kdl")
	if err := WriteThemeFile(path, DarkPalette(), false); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range PaletteKeys() {
		if !strings.Contains(string(body), k) {
			t.Errorf("output missing key %q", k)
		}
	}
}
