package route

import (
	"net"
	"path/filepath"
	"strings"
)

// Rule defines a routing rule that maps a hostname pattern to an upstream name.
type Rule struct {
	Pattern  string
	Upstream string
	Fallback string
	File     string // source config file (for diagnostics)
}

type patternKind int

const (
	kindExact  patternKind = iota
	kindDomain             // ".example.com"
	kindGlob               // contains * or ?
	kindCIDR               // "10.0.0.0/8"
)

type compiledRule struct {
	pattern  string
	upstream string
	fallback string
	file     string
	kind     patternKind
	cidr     *net.IPNet
}

// Matcher holds an ordered list of routing rules and matches hostnames against them.
type Matcher struct {
	rules []compiledRule
}

// NewMatcher creates a Matcher from the given rules. Rules are evaluated in order,
// and the last matching rule wins. Patterns are pre-compiled at construction time.
func NewMatcher(rules []Rule) *Matcher {
	compiled := make([]compiledRule, len(rules))
	for i, r := range rules {
		compiled[i] = compileRule(r)
	}
	return &Matcher{rules: compiled}
}

func compileRule(r Rule) compiledRule {
	pattern := strings.ToLower(r.Pattern)

	base := compiledRule{
		pattern:  pattern,
		upstream: r.Upstream,
		fallback: r.Fallback,
		file:     r.File,
	}

	// CIDR: contains "/" and parses as a network
	if strings.Contains(pattern, "/") {
		_, cidr, err := net.ParseCIDR(pattern)
		if err == nil {
			base.kind = kindCIDR
			base.cidr = cidr
			return base
		}
	}

	// Glob: contains * or ?
	if strings.ContainsAny(pattern, "*?") {
		base.kind = kindGlob
		return base
	}

	// Domain suffix: starts with "."
	if strings.HasPrefix(pattern, ".") {
		base.kind = kindDomain
		return base
	}

	// Exact match
	base.kind = kindExact
	return base
}

// Match returns the upstream name for the given hostname. If no rule matches,
// it returns an empty string (meaning direct connection).
// The hostname may include a port, which is stripped before matching.
func (m *Matcher) Match(hostname string) string {
	host := stripPort(hostname)

	var result string
	for _, rule := range m.rules {
		if rule.match(host) {
			result = rule.upstream
		}
	}
	return result
}

// Resolve returns the last matching rule for the given hostname, or nil
// if no rule matches. The hostname may include a port, which is stripped
// before matching.
func (m *Matcher) Resolve(hostname string) *Rule {
	host := stripPort(hostname)

	var result *Rule
	for i := range m.rules {
		if m.rules[i].match(host) {
			result = ruleFromCompiled(&m.rules[i])
		}
	}
	return result
}

// ResolveAll returns all matching rules for the given hostname, in order.
// The last element is the active (winning) rule.
func (m *Matcher) ResolveAll(hostname string) []Rule {
	host := stripPort(hostname)

	var results []Rule
	for i := range m.rules {
		if m.rules[i].match(host) {
			results = append(results, *ruleFromCompiled(&m.rules[i]))
		}
	}
	return results
}

func ruleFromCompiled(cr *compiledRule) *Rule {
	return &Rule{
		Pattern:  cr.pattern,
		Upstream: cr.upstream,
		Fallback: cr.fallback,
		File:     cr.file,
	}
}

func (r *compiledRule) match(host string) bool {
	switch r.kind {
	case kindCIDR:
		ip := net.ParseIP(host)
		return ip != nil && r.cidr.Contains(ip)
	case kindGlob:
		matched, _ := filepath.Match(r.pattern, host)
		return matched
	case kindDomain:
		return strings.HasSuffix(host, r.pattern)
	default:
		return host == r.pattern
	}
}

func stripPort(hostname string) string {
	host, _, err := net.SplitHostPort(hostname)
	if err != nil {
		host = hostname
	}
	return strings.ToLower(host)
}
