// Package env captures the operator's shell environment on a slow
// timer and exposes it to the markdown renderer for `${NAME}`
// substitution. The Refresher mirrors stats.Recorder's ticker-loop
// shape so the existing pattern stays consistent across the codebase.
//
// The Snapshot's referenced-vars-only change detection is the
// optimization that keeps idle CPU near zero: a vanilla shell spawn
// produces a different env every time (`$RANDOM`, `$SHLVL`, `$$`),
// but Replace only reports `changed=true` when a variable that's
// actually used in some markdown card differs from the previous
// capture.
package env

import (
	"sync"
)

// Snapshot stores the most recent captured environment along with
// the set of variable names that markdown cards actually reference.
// Lookup is read-mostly (one call per `${NAME}` token rendered);
// Replace runs once per refresher tick. A single sync.RWMutex is
// enough — readers never block each other and writers are rare.
type Snapshot struct {
	mu   sync.RWMutex
	vars map[string]string
	refs map[string]struct{}
}

// NewSnapshot returns an empty snapshot. The first Replace call
// establishes the baseline; before that, Lookup always reports a
// missing var.
func NewSnapshot() *Snapshot {
	return &Snapshot{
		vars: make(map[string]string),
		refs: make(map[string]struct{}),
	}
}

// Lookup returns the value of name and whether it's defined. An
// empty value with ok=true means "var is set, value is the empty
// string"; ok=false means "not in the captured env at all". The
// markdown renderer's expandVars uses this two-result form to leave
// `${NAME}` as the literal token when the var is truly missing,
// rather than substituting a silent blank.
func (s *Snapshot) Lookup(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.vars[name]
	return v, ok
}

// SetReferences records the union of `${NAME}` tokens used across
// every markdown card on every loaded page. Called by cmd/serve.go
// after each (re)load. Only keys present in this set count toward
// the change-detection in Replace.
func (s *Snapshot) SetReferences(refs map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if refs == nil {
		s.refs = make(map[string]struct{})
		return
	}
	// Copy so the caller can mutate its map without affecting us.
	cp := make(map[string]struct{}, len(refs))
	for k := range refs {
		cp[k] = struct{}{}
	}
	s.refs = cp
}

// Replace swaps the captured environment for a freshly captured one
// and reports whether any *referenced* variable changed value or
// presence. Returns false when no references are registered, even
// if every value differs — the renderer has nothing to act on, so
// triggering a re-render would be wasted work.
func (s *Snapshot) Replace(newVars map[string]string) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name := range s.refs {
		oldVal, oldOK := s.vars[name]
		newVal, newOK := newVars[name]
		if oldOK != newOK || oldVal != newVal {
			changed = true
			break
		}
	}

	if newVars == nil {
		s.vars = make(map[string]string)
	} else {
		s.vars = newVars
	}
	return changed
}
