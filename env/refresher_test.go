package env

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefresherFiresOnChangeOnlyWhenReferencedChanges(t *testing.T) {
	// Mirrors the cmd/serve.go bootstrap flow: an initial Replace
	// happens before SetReferences (so it never reports a change),
	// then the refresher starts. This means the first refresher tick
	// is comparing against a known baseline, not against an empty
	// snapshot — otherwise every initial capture would flag as
	// "PUBLIC_IP went from missing to set" and fire onChange.
	snap := NewSnapshot()
	snap.Replace(map[string]string{"PUBLIC_IP": "1.1.1.1", "RANDOM": "a"})
	snap.SetReferences(map[string]struct{}{"PUBLIC_IP": {}})

	changes := []map[string]string{
		{"PUBLIC_IP": "1.1.1.1", "RANDOM": "b"}, // unreferenced churn
		{"PUBLIC_IP": "1.1.1.1", "RANDOM": "c"}, // unreferenced churn
		{"PUBLIC_IP": "2.2.2.2", "RANDOM": "d"}, // real change
	}
	var (
		mu  sync.Mutex
		idx int
	)
	readIdx := func() int { mu.Lock(); defer mu.Unlock(); return idx }
	capture := func(_ context.Context) (map[string]string, error) {
		mu.Lock()
		defer mu.Unlock()
		if idx >= len(changes) {
			// After the script is exhausted, keep returning the
			// final state so the loop is well-defined.
			return changes[len(changes)-1], nil
		}
		out := changes[idx]
		idx++
		return out, nil
	}

	var fired int32
	r := NewRefresher(snap, 5*time.Millisecond, capture, func() {
		atomic.AddInt32(&fired, 1)
	})

	go r.Run()
	defer r.Stop()

	// Wait until we've consumed all three scripted captures.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && readIdx() < len(changes) {
		time.Sleep(2 * time.Millisecond)
	}
	// Brief settle so the post-capture branch has a chance to call onChange.
	time.Sleep(20 * time.Millisecond)

	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Errorf("onChange fired %d times, want exactly 1 (only the real PUBLIC_IP change)", got)
	}
}

func TestRefresherSkipsCaptureWhenNoReferences(t *testing.T) {
	// With no `${NAME}` references registered, there's nothing for the
	// renderer to substitute, so the refresher must not spawn the
	// capture shell at all. This is what keeps idle CPU at zero for
	// configs that don't use env substitution — spawning a login shell
	// every tick is pure waste otherwise.
	snap := NewSnapshot() // no SetReferences → empty reference set

	var captures int32
	capture := func(_ context.Context) (map[string]string, error) {
		atomic.AddInt32(&captures, 1)
		return map[string]string{"X": "v"}, nil
	}
	r := NewRefresher(snap, 5*time.Millisecond, capture, func() {})
	go r.Run()
	defer r.Stop()

	// Several tick intervals — plenty of chances to (wrongly) capture.
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&captures); got != 0 {
		t.Errorf("capture ran %d times with no references; want 0", got)
	}
}

func TestRefresherResumesCaptureWhenReferencesAdded(t *testing.T) {
	// References can appear after a config reload adds a markdown card
	// that uses ${NAME}. The watcher updates the reference set in place
	// but never restarts the refresher, so the guard must be evaluated
	// per tick — capturing resumes without a daemon restart.
	snap := NewSnapshot()

	var captures int32
	capture := func(_ context.Context) (map[string]string, error) {
		atomic.AddInt32(&captures, 1)
		return map[string]string{"X": "v"}, nil
	}
	r := NewRefresher(snap, 5*time.Millisecond, capture, func() {})
	go r.Run()
	defer r.Stop()

	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&captures); got != 0 {
		t.Fatalf("capture ran %d times before any references; want 0", got)
	}

	// A reload adds an env-referencing card.
	snap.SetReferences(map[string]struct{}{"X": {}})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&captures) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&captures) == 0 {
		t.Error("capture never ran after references were added")
	}
}

func TestRefresherStopIdempotent(t *testing.T) {
	// Stop must be safe to call more than once — a second call should
	// not panic on an already-closed channel.
	snap := NewSnapshot()
	snap.SetReferences(map[string]struct{}{"X": {}})
	capture := func(_ context.Context) (map[string]string, error) {
		return map[string]string{"X": "v"}, nil
	}
	r := NewRefresher(snap, 5*time.Millisecond, capture, func() {})
	go r.Run()
	time.Sleep(15 * time.Millisecond)

	r.Stop()
	r.Stop() // must not panic
}

func TestRefresherStopHaltsTicker(t *testing.T) {
	snap := NewSnapshot()
	snap.SetReferences(map[string]struct{}{"X": {}})

	var captures int32
	capture := func(_ context.Context) (map[string]string, error) {
		atomic.AddInt32(&captures, 1)
		return map[string]string{"X": "v"}, nil
	}
	r := NewRefresher(snap, 5*time.Millisecond, capture, func() {})
	go r.Run()

	time.Sleep(30 * time.Millisecond)
	r.Stop()

	before := atomic.LoadInt32(&captures)
	time.Sleep(40 * time.Millisecond)
	after := atomic.LoadInt32(&captures)
	if after != before {
		t.Errorf("Refresher kept capturing after Stop: before=%d after=%d", before, after)
	}
}

func TestCaptureFallbackToOSEnvironWhenShellEmpty(t *testing.T) {
	// When no shell is configured, Capture returns the parent
	// process's environment. Use an env var the test sets itself so
	// the assertion doesn't depend on the host.
	t.Setenv("SUBSPACE_TEST_VAR", "hello")
	got, err := Capture(context.Background(), "")
	if err != nil {
		t.Fatalf("Capture(\"\") error: %v", err)
	}
	if v, ok := got["SUBSPACE_TEST_VAR"]; !ok || v != "hello" {
		t.Errorf("os.Environ fallback missing SUBSPACE_TEST_VAR: got %q,%v", v, ok)
	}
}

func TestCaptureWithRealShell(t *testing.T) {
	// Smoke-test the actual /bin/sh path. We export a unique var and
	// check it round-trips. -lc is the real refresher path; -c is
	// enough for /bin/sh to evaluate the inline export.
	t.Setenv("SUBSPACE_TEST_VAR_2", "world")
	got, err := Capture(context.Background(), "/bin/sh")
	if err != nil {
		t.Fatalf("Capture(/bin/sh) error: %v", err)
	}
	if v, ok := got["SUBSPACE_TEST_VAR_2"]; !ok || v != "world" {
		t.Errorf("/bin/sh capture missing SUBSPACE_TEST_VAR_2: got %q,%v", v, ok)
	}
}
