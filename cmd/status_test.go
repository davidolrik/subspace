package cmd

import (
	"reflect"
	"testing"

	"go.olrik.dev/subspace/control"
)

func TestSortedUpstreamNames(t *testing.T) {
	// Three buckets, alphabetical within each:
	//   1. healthy declared upstreams
	//   2. pseudo-upstreams ("blackhole", "direct", "ignore" — always healthy)
	//   3. unhealthy declared upstreams (pushed to the bottom)
	in := map[string]control.UpstreamStatus{
		"zebra":     {Healthy: true},
		"direct":    {Healthy: true},
		"alpha":     {Healthy: true},
		"blackhole": {Healthy: true},
		"ignore":    {Healthy: true},
		"sick":      {Healthy: false},
		"middle":    {Healthy: true},
		"broken":    {Healthy: false},
	}
	got := sortedUpstreamNames(in)
	want := []string{
		"alpha", "middle", "zebra", // healthy declared
		"blackhole", "direct", "ignore", // pseudo (alphabetical)
		"broken", "sick", // unhealthy declared
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedUpstreamNames = %v, want %v", got, want)
	}
}

func TestIsPseudoUpstream(t *testing.T) {
	for _, name := range []string{"direct", "blackhole", "ignore"} {
		if !isPseudoUpstream(name) {
			t.Errorf("isPseudoUpstream(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"corp", "tunnel", "", "direct-but-not", "ignored"} {
		if isPseudoUpstream(name) {
			t.Errorf("isPseudoUpstream(%q) = true, want false", name)
		}
	}
}
