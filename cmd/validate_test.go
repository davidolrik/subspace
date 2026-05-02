package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runValidate parses the given main config and returns the captured
// stdout/stderr plus the exit error from the validate command.
func runValidate(t *testing.T, configPath string) (stdout, stderr string, err error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := newValidateCommand(&configPath)
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(nil)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func writeConfig(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "config.kdl")
}

func TestValidateCleanConfigSucceeds(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, map[string]string{
		"config.kdl": `
listen ":8080"

upstream "corp" {
    type "http"
    address "proxy.corp.example:3128"
}

route "*.corp.example" via="corp"

tags {
    tag "prod" color="#00ff88"
}

search-engines default="google" {
    engine "google" url="https://www.google.com/search?q={query}"
}
`,
	})

	stdout, stderr, err := runValidate(t, configPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "OK") {
		t.Errorf("stdout should report success, got:\n%s", stdout)
	}
	for _, want := range []string{"upstreams", "routes", "tags", "search engines"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q section\n%s", want, stdout)
		}
	}
}

func TestValidateReportsKDLSyntaxErrorAndFails(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, map[string]string{
		"config.kdl": `this is { not valid kdl`,
	})

	_, stderr, err := runValidate(t, configPath)
	if err == nil {
		t.Fatal("expected validate to return an error for syntactically broken KDL")
	}
	if !strings.Contains(stderr, "config.kdl") && !strings.Contains(err.Error(), "config.kdl") {
		t.Errorf("error should mention the offending file, got stderr=%q err=%v", stderr, err)
	}
}

func TestValidateReportsCollectedErrorsAndFails(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, map[string]string{
		"config.kdl": `
listen ":8080"

// Route references an undefined upstream — non-fatal at parse time
// (collected as a config error) but should still fail validation.
route "*.example.com" via="missing-upstream"
`,
	})

	stdout, _, err := runValidate(t, configPath)
	if err == nil {
		t.Fatal("expected validate to fail when cfg.Errors is non-empty")
	}
	if !strings.Contains(stdout, "missing-upstream") {
		t.Errorf("stdout should list the unknown-upstream error, got:\n%s", stdout)
	}
}

func TestValidateReportsBrokenPageFile(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, map[string]string{
		"config.kdl": `
listen ":8080"
page "broken.kdl"
`,
		"broken.kdl": `this is { not valid kdl`,
	})

	stdout, _, err := runValidate(t, configPath)
	if err == nil {
		t.Fatal("expected validate to fail when a page KDL file fails to parse")
	}
	if !strings.Contains(stdout, "broken.kdl") {
		t.Errorf("stdout should mention the broken page file, got:\n%s", stdout)
	}
}

func TestValidateReportsUnknownTagReferenceFromPage(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir, map[string]string{
		"config.kdl": `
listen ":8080"
tags {
    tag "prod" color="#00ff88"
}
page "dev.kdl"
`,
		"dev.kdl": `
title "Development"
list "Repos" {
    link "GitHub" url="https://github.com" tags="undefined-tag"
}
`,
	})

	stdout, _, err := runValidate(t, configPath)
	if err == nil {
		t.Fatal("expected validate to fail when a page references an unknown tag")
	}
	if !strings.Contains(stdout, "undefined-tag") {
		t.Errorf("stdout should mention the unknown tag, got:\n%s", stdout)
	}
}

func TestValidateMissingFileFails(t *testing.T) {
	_, _, err := runValidate(t, "/path/that/does/not/exist.kdl")
	if err == nil {
		t.Fatal("expected validate to fail when the config file is missing")
	}
}
