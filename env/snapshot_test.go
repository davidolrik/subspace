package env

import (
	"sync"
	"testing"
)

func TestSnapshotLookup(t *testing.T) {
	s := NewSnapshot()
	s.Replace(map[string]string{"USER": "mandse", "EMPTY": ""})

	if v, ok := s.Lookup("USER"); !ok || v != "mandse" {
		t.Errorf("Lookup(USER) = %q,%v; want \"mandse\",true", v, ok)
	}
	// An empty value is still a defined variable.
	if v, ok := s.Lookup("EMPTY"); !ok || v != "" {
		t.Errorf("Lookup(EMPTY) = %q,%v; want \"\",true", v, ok)
	}
	if _, ok := s.Lookup("MISSING"); ok {
		t.Errorf("Lookup(MISSING) returned ok=true; want false")
	}
}

func TestSnapshotReplaceWithNoReferences(t *testing.T) {
	// With no references registered, Replace must always report
	// changed=false — there's nothing the caller cares about, so
	// a re-render would be wasted work.
	s := NewSnapshot()
	if changed := s.Replace(map[string]string{"A": "1"}); changed {
		t.Errorf("first Replace with empty refs: changed=%v, want false", changed)
	}
	if changed := s.Replace(map[string]string{"A": "2"}); changed {
		t.Errorf("second Replace with empty refs: changed=%v, want false", changed)
	}
}

func TestSnapshotReplaceReferencedChange(t *testing.T) {
	s := NewSnapshot()
	s.SetReferences(map[string]struct{}{"USER": {}, "PUBLIC_IP": {}})

	// First Replace establishes the baseline. Treated as no-op-from-
	// the-renderer's-perspective: the initial Capture in cmd/serve.go
	// is the bootstrap; we don't want it to fire onChange before the
	// first render has even happened.
	s.Replace(map[string]string{"USER": "mandse", "PUBLIC_IP": "1.1.1.1"})

	// Same values → no change.
	if changed := s.Replace(map[string]string{"USER": "mandse", "PUBLIC_IP": "1.1.1.1", "RANDOM": "noise"}); changed {
		t.Errorf("identical referenced vars: changed=%v, want false", changed)
	}

	// PUBLIC_IP changed → change.
	if changed := s.Replace(map[string]string{"USER": "mandse", "PUBLIC_IP": "2.2.2.2"}); !changed {
		t.Error("PUBLIC_IP changed: changed=false, want true")
	}

	// USER vanished entirely → change (presence differs).
	if changed := s.Replace(map[string]string{"PUBLIC_IP": "2.2.2.2"}); !changed {
		t.Error("USER removed: changed=false, want true")
	}
}

func TestSnapshotReplaceUnreferencedChurnIgnored(t *testing.T) {
	// $RANDOM, $$, etc. churn on every shell spawn. If they're not in
	// the reference set, Replace must report no change — this is the
	// optimization that keeps idle CPU near zero.
	s := NewSnapshot()
	s.SetReferences(map[string]struct{}{"PUBLIC_IP": {}})
	s.Replace(map[string]string{"PUBLIC_IP": "1.1.1.1", "RANDOM": "111"})

	for i := 0; i < 5; i++ {
		newVars := map[string]string{"PUBLIC_IP": "1.1.1.1", "RANDOM": "different-each-time"}
		if changed := s.Replace(newVars); changed {
			t.Errorf("iter %d: only $RANDOM changed, but Replace reported changed=true", i)
		}
	}
}

func TestSnapshotConcurrent(t *testing.T) {
	// -race must stay clean under concurrent reads while writes are
	// happening. Mirrors the runtime shape: many goroutines reading
	// (one per markdown card render across all dashboard requests),
	// occasional writer (the env refresher).
	s := NewSnapshot()
	s.SetReferences(map[string]struct{}{"X": {}})
	s.Replace(map[string]string{"X": "0"})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					s.Lookup("X")
				}
			}
		}()
	}

	for i := 0; i < 100; i++ {
		s.Replace(map[string]string{"X": "v"})
	}
	close(stop)
	wg.Wait()
}
