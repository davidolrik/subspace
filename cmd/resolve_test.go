package cmd

import (
	"path/filepath"
	"testing"

	"go.olrik.dev/subspace/config"
)

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
