package cmd

import (
	"path/filepath"
	"testing"

	"go.olrik.dev/subspace/config"
)

func TestExtractHostname(t *testing.T) {
	cases := map[string]string{
		// Bare host with a trailing slash or path must yield just the
		// host — otherwise the slash leaks into route matching and a
		// suffix rule like ".olrik.cloud" stops matching.
		"unifi.hq.olrik.cloud/":     "unifi.hq.olrik.cloud",
		"unifi.hq.olrik.cloud/a/b":  "unifi.hq.olrik.cloud",
		"unifi.hq.olrik.cloud":      "unifi.hq.olrik.cloud",
		// Full URLs go through the Host branch.
		"https://unifi.hq.olrik.cloud/path": "unifi.hq.olrik.cloud",
		"http://host:8080/x":                "host",
		// Bare host:port (no scheme) — url.Parse mis-reads this as
		// scheme:opaque, so it needs the "//" reparse fallback.
		"host:8080":                  "host",
		"unifi.hq.olrik.cloud:25000": "unifi.hq.olrik.cloud",
		"host:8080/path":             "host",
	}
	for in, want := range cases {
		got, err := extractHostname(in)
		if err != nil {
			t.Errorf("extractHostname(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("extractHostname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIncludeChain(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.kdl")
	mid := filepath.Join(dir, "config.d", "10-load.kdl")
	leaf := filepath.Join(dir, "contexts", "client.kdl")

	// IncludedFiles[0] is the root; formatFile renders paths relative to
	// its directory. IncludedBy records the parent of each included file.
	cfg := &config.Config{
		IncludedFiles: []string{root, mid, leaf},
		IncludedBy: map[string]string{
			mid:  root,
			leaf: mid,
		},
	}

	// The root config is the common ancestor of every included file, so
	// it's omitted from the chain — except for a rule that lives in the
	// root itself, where it's the only useful thing to show.
	cases := map[string]string{
		"":   "",
		root: "config.kdl",
		mid:  "config.d/10-load.kdl",
		leaf: "config.d/10-load.kdl › contexts/client.kdl",
	}
	for file, want := range cases {
		if got := includeChain(file, cfg); got != want {
			t.Errorf("includeChain(%q) = %q, want %q", file, got, want)
		}
	}
}
