package route

import (
	"testing"
)

func TestMatcherExactMatch(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "specific.host.com", Upstream: "tunnel"},
	})

	if got := m.Match("specific.host.com"); got != "tunnel" {
		t.Errorf("Match(specific.host.com) = %q, want %q", got, "tunnel")
	}

	if got := m.Match("other.host.com"); got != "" {
		t.Errorf("Match(other.host.com) = %q, want empty", got)
	}
}

func TestMatcherDomainMatch(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: ".corp.internal", Upstream: "corporate"},
	})

	if got := m.Match("foo.corp.internal"); got != "corporate" {
		t.Errorf("Match(foo.corp.internal) = %q, want %q", got, "corporate")
	}

	if got := m.Match("bar.baz.corp.internal"); got != "corporate" {
		t.Errorf("Match(bar.baz.corp.internal) = %q, want %q", got, "corporate")
	}

	// The domain itself should not match a dot-prefix rule
	if got := m.Match("corp.internal"); got != "" {
		t.Errorf("Match(corp.internal) = %q, want empty", got)
	}
}

func TestMatcherLastMatchWins(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: ".example.com", Upstream: "first"},
		{Pattern: ".example.com", Upstream: "second"},
	})

	if got := m.Match("foo.example.com"); got != "second" {
		t.Errorf("Match(foo.example.com) = %q, want %q", got, "second")
	}
}

func TestMatcherNoMatch(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: ".corp.internal", Upstream: "corporate"},
		{Pattern: "specific.host.com", Upstream: "tunnel"},
	})

	if got := m.Match("unrelated.com"); got != "" {
		t.Errorf("Match(unrelated.com) = %q, want empty", got)
	}
}

func TestMatcherEmptyRules(t *testing.T) {
	m := NewMatcher(nil)

	if got := m.Match("anything.com"); got != "" {
		t.Errorf("Match(anything.com) = %q, want empty", got)
	}
}

func TestMatcherMixedRules(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: ".internal.example.com", Upstream: "internal"},
		{Pattern: "api.internal.example.com", Upstream: "api-specific"},
	})

	// Exact match should win when it's last
	if got := m.Match("api.internal.example.com"); got != "api-specific" {
		t.Errorf("Match(api.internal.example.com) = %q, want %q", got, "api-specific")
	}

	// Domain match still works for other hosts
	if got := m.Match("web.internal.example.com"); got != "internal" {
		t.Errorf("Match(web.internal.example.com) = %q, want %q", got, "internal")
	}
}

func TestMatcherStripsPort(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "example.com", Upstream: "tunnel"},
	})

	if got := m.Match("example.com:8080"); got != "tunnel" {
		t.Errorf("Match(example.com:8080) = %q, want %q", got, "tunnel")
	}
}

func TestResolveReturnsMatchingRule(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: ".corp.internal", Upstream: "corporate"},
		{Pattern: "api.corp.internal", Upstream: "api-proxy"},
	})

	result := m.Resolve("api.corp.internal")
	if result == nil {
		t.Fatal("Resolve returned nil, want a match")
	}
	// Last match wins: api.corp.internal matches both rules, last one wins
	if result.Pattern != "api.corp.internal" {
		t.Errorf("Pattern = %q, want %q", result.Pattern, "api.corp.internal")
	}
	if result.Upstream != "api-proxy" {
		t.Errorf("Upstream = %q, want %q", result.Upstream, "api-proxy")
	}
}

func TestResolveReturnsNilOnNoMatch(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: ".corp.internal", Upstream: "corporate"},
	})

	result := m.Resolve("example.com")
	if result != nil {
		t.Errorf("Resolve returned %+v, want nil", result)
	}
}

// --- glob tests ---

func TestMatcherGlob(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "192.168.*.*", Upstream: "lan"},
	})

	if got := m.Match("192.168.1.1"); got != "lan" {
		t.Errorf("Match(192.168.1.1) = %q, want %q", got, "lan")
	}
	if got := m.Match("10.0.0.1"); got != "" {
		t.Errorf("Match(10.0.0.1) = %q, want empty", got)
	}
}

func TestMatcherGlobHostname(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "*.cdn.example.com", Upstream: "cdn"},
	})

	if got := m.Match("a.cdn.example.com"); got != "cdn" {
		t.Errorf("Match(a.cdn.example.com) = %q, want %q", got, "cdn")
	}
	if got := m.Match("cdn.example.com"); got != "" {
		t.Errorf("Match(cdn.example.com) = %q, want empty", got)
	}
}

// --- CIDR tests ---

func TestMatcherCIDR(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "10.0.0.0/8", Upstream: "internal"},
	})

	if got := m.Match("10.1.2.3"); got != "internal" {
		t.Errorf("Match(10.1.2.3) = %q, want %q", got, "internal")
	}
	if got := m.Match("192.168.1.1"); got != "" {
		t.Errorf("Match(192.168.1.1) = %q, want empty", got)
	}
}

func TestMatcherCIDR6(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "fd00::/8", Upstream: "private6"},
	})

	if got := m.Match("fd12::1"); got != "private6" {
		t.Errorf("Match(fd12::1) = %q, want %q", got, "private6")
	}
	if got := m.Match("2001:db8::1"); got != "" {
		t.Errorf("Match(2001:db8::1) = %q, want empty", got)
	}
}

func TestMatcherCIDRWithPort(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "192.168.0.0/16", Upstream: "lan"},
	})

	if got := m.Match("192.168.1.1:8080"); got != "lan" {
		t.Errorf("Match(192.168.1.1:8080) = %q, want %q", got, "lan")
	}
}

func TestMatcherMixedPatternTypes(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "10.0.0.0/8", Upstream: "cidr-match"},
		{Pattern: ".example.com", Upstream: "domain-match"},
		{Pattern: "special.example.com", Upstream: "exact-match"},
	})

	if got := m.Match("10.1.2.3"); got != "cidr-match" {
		t.Errorf("Match(10.1.2.3) = %q, want %q", got, "cidr-match")
	}
	if got := m.Match("foo.example.com"); got != "domain-match" {
		t.Errorf("Match(foo.example.com) = %q, want %q", got, "domain-match")
	}
	// Last match wins — exact after domain
	if got := m.Match("special.example.com"); got != "exact-match" {
		t.Errorf("Match(special.example.com) = %q, want %q", got, "exact-match")
	}
}

func TestResolveStripsPort(t *testing.T) {
	m := NewMatcher([]Rule{
		{Pattern: "example.com", Upstream: "tunnel"},
	})

	result := m.Resolve("example.com:443")
	if result == nil {
		t.Fatal("Resolve returned nil, want a match")
	}
	if result.Upstream != "tunnel" {
		t.Errorf("Upstream = %q, want %q", result.Upstream, "tunnel")
	}
}
