package cmd

import (
	"testing"

	"go.olrik.dev/subspace/config"
)

// TestBuildRoutingKeepsBlackholeRoutes is a regression test for the
// runtime drop where any route whose `via` had no dialer was discarded
// before the matcher was built. blackhole is a pseudo-upstream with no
// dialer by design, so the check must skip it (and direct) the same
// way the config-layer validator does.
func TestBuildRoutingKeepsBlackholeRoutes(t *testing.T) {
	cfg := &config.Config{
		Upstreams: map[string]config.Upstream{},
		Routes: []config.Route{
			{Pattern: ".", Via: "blackhole"},
		},
	}

	matcher, _ := buildRouting(cfg)

	rule := matcher.Resolve("fitnessengros.dk")
	if rule == nil {
		t.Fatalf("blackhole catch-all route was dropped at build time; cfg.Errors=%v", cfg.Errors)
	}
	if rule.Upstream != "blackhole" {
		t.Errorf("Resolve(\"fitnessengros.dk\").Upstream = %q, want %q", rule.Upstream, "blackhole")
	}
	for _, msg := range cfg.Errors {
		if msg != "" && (rule.Upstream == "blackhole") {
			// Any error message generated for the blackhole route is a bug.
			if contains(msg, "blackhole") {
				t.Errorf("unexpected error mentioning blackhole: %q", msg)
			}
		}
	}
}

// TestBuildRoutingKeepsBlackholeFallback covers the fallback="blackhole"
// path through buildRouting — the fallback must survive the
// dialer-existence check so the runtime can short-circuit on dial
// failure.
func TestBuildRoutingKeepsBlackholeFallback(t *testing.T) {
	cfg := &config.Config{
		Upstreams: map[string]config.Upstream{
			"corp": {Type: "http", Address: "127.0.0.1:1"},
		},
		Routes: []config.Route{
			{Pattern: ".risky.example", Via: "corp", Fallback: "blackhole"},
		},
	}

	_, _ = buildRouting(cfg)

	if len(cfg.Routes) != 1 {
		t.Fatalf("got %d routes after build, want 1; cfg.Errors=%v", len(cfg.Routes), cfg.Errors)
	}
	if cfg.Routes[0].Fallback != "blackhole" {
		t.Errorf("Fallback = %q, want %q (must not be cleared by buildRouting)", cfg.Routes[0].Fallback, "blackhole")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
