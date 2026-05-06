package cmd

import (
	"reflect"
	"testing"

	"go.olrik.dev/subspace/control"
)

func TestSortedUpstreamNames(t *testing.T) {
	// Operator-declared upstreams come first (alphabetical), then the
	// built-in pseudo-upstreams ("blackhole" before "direct"). The
	// pseudo-upstreams stay grouped at the bottom regardless of their
	// alphabetical position relative to declared ones.
	in := map[string]control.UpstreamStatus{
		"zebra":     {},
		"direct":    {},
		"alpha":     {},
		"blackhole": {},
		"middle":    {},
	}
	got := sortedUpstreamNames(in)
	want := []string{"alpha", "middle", "zebra", "blackhole", "direct"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedUpstreamNames = %v, want %v", got, want)
	}
}

func TestIsPseudoUpstream(t *testing.T) {
	for _, name := range []string{"direct", "blackhole"} {
		if !isPseudoUpstream(name) {
			t.Errorf("isPseudoUpstream(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"corp", "tunnel", "", "direct-but-not"} {
		if isPseudoUpstream(name) {
			t.Errorf("isPseudoUpstream(%q) = true, want false", name)
		}
	}
}
