package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupThemeExport creates a temp config dir and returns its path along
// with a runner that exercises the export subcommand with the given args.
func setupThemeExport(t *testing.T) (configDir string, run func(args ...string) (string, string, error)) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	configDir = filepath.Join(tmp, "subspace")

	run = func(args ...string) (string, string, error) {
		var outBuf, errBuf bytes.Buffer
		cmd := newThemeCommand()
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs(append([]string{"export"}, args...))
		err := cmd.Execute()
		return outBuf.String(), errBuf.String(), err
	}
	return configDir, run
}

func TestThemeExport_FromDark(t *testing.T) {
	configDir, run := setupThemeExport(t)
	if _, _, err := run("--from", "dark", "mydark"); err != nil {
		t.Fatalf("export: %v", err)
	}
	path := filepath.Join(configDir, "themes", "mydark.kdl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	// Sanity: the dark heading is neon cyan #00ffea.
	if !strings.Contains(string(body), "#00ffea") {
		t.Errorf("dark export missing neon cyan; got:\n%s", body)
	}
}

func TestThemeExport_FromLight(t *testing.T) {
	configDir, run := setupThemeExport(t)
	if _, _, err := run("--from", "light", "mylight"); err != nil {
		t.Fatalf("export: %v", err)
	}
	path := filepath.Join(configDir, "themes", "mylight.kdl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	// Sanity: the light heading is deep teal #0a7178.
	if !strings.Contains(string(body), "#0a7178") {
		t.Errorf("light export missing #0a7178; got:\n%s", body)
	}
}

func TestThemeExport_DefaultsFromActive(t *testing.T) {
	// When --from is omitted, export should use whatever palette is
	// currently active. Set the active palette to "light" before
	// running so the export reflects that.
	t.Cleanup(func() { activePaletteName = "" })
	activePaletteName = "light"

	configDir, run := setupThemeExport(t)
	if _, _, err := run("auto"); err != nil {
		t.Fatalf("export: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(configDir, "themes", "auto.kdl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "#0a7178") {
		t.Errorf("default --from should follow active palette (light); body:\n%s", body)
	}
}

func TestThemeExport_RefusesOverwriteWithoutForce(t *testing.T) {
	configDir, run := setupThemeExport(t)
	if _, _, err := run("--from", "dark", "twice"); err != nil {
		t.Fatalf("first export: %v", err)
	}
	_, _, err := run("--from", "light", "twice")
	if err == nil {
		t.Fatalf("expected refusal to overwrite, got nil")
	}
	// File should still hold the dark palette.
	body, _ := os.ReadFile(filepath.Join(configDir, "themes", "twice.kdl"))
	if !strings.Contains(string(body), "#00ffea") {
		t.Errorf("file should still hold dark palette after refused overwrite")
	}
}

func TestThemeExport_ForceOverwrites(t *testing.T) {
	configDir, run := setupThemeExport(t)
	if _, _, err := run("--from", "dark", "swap"); err != nil {
		t.Fatalf("first export: %v", err)
	}
	if _, _, err := run("--from", "light", "--force", "swap"); err != nil {
		t.Fatalf("force overwrite: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(configDir, "themes", "swap.kdl"))
	if !strings.Contains(string(body), "#0a7178") {
		t.Errorf("file should now hold light palette; body:\n%s", body)
	}
}

func TestThemeExport_AutoCreatesThemesDir(t *testing.T) {
	configDir, run := setupThemeExport(t)
	// themes/ doesn't exist yet
	if _, err := os.Stat(filepath.Join(configDir, "themes")); !os.IsNotExist(err) {
		t.Fatalf("themes dir should not exist yet: %v", err)
	}
	if _, _, err := run("--from", "dark", "fresh"); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "themes", "fresh.kdl")); err != nil {
		t.Errorf("expected file to be created: %v", err)
	}
}

func TestThemeExport_RejectsInvalidFrom(t *testing.T) {
	_, run := setupThemeExport(t)
	_, _, err := run("--from", "neon", "invalid")
	if err == nil {
		t.Fatalf("expected error for unknown --from")
	}
	if !strings.Contains(err.Error(), "dark or light") {
		t.Errorf("error should mention valid options; got %v", err)
	}
}
