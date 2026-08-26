package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func newTestWatcher(t *testing.T) *fsnotify.Watcher {
	t.Helper()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("fsnotify.NewWatcher: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// waitFor polls cond until it returns true or the deadline passes.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestWatchSetMissingDirIsPending covers the reboot race: an include
// whose parent directory doesn't exist yet (a volume that hasn't
// mounted, a folder not created yet) must not be treated as watched.
// It stays pending, and once the directory appears retryPending
// subscribes to it and reports the change.
func TestWatchSetMissingDirIsPending(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "later", "notes.md")

	ws := newWatchSet(newTestWatcher(t))
	ws.set([]string{missing})

	if !ws.hasPending() {
		t.Fatal("expected missing include dir to be pending")
	}
	if ws.retryPending() {
		t.Fatal("retryPending reported success while dir still missing")
	}

	if err := os.MkdirAll(filepath.Dir(missing), 0o755); err != nil {
		t.Fatal(err)
	}
	if !ws.retryPending() {
		t.Fatal("retryPending did not pick up the newly created dir")
	}
	if ws.hasPending() {
		t.Fatal("dir still pending after successful retry")
	}
}

// TestWatchSetSetReplacesDirs checks that updating the file set drops
// subscriptions for directories no longer referenced and keeps the
// pending set in sync with the new file list.
func TestWatchSetSetReplacesDirs(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a", "x.kdl")
	b := filepath.Join(root, "b", "y.kdl")
	for _, f := range []string{a, b} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(root, "gone", "z.md")

	ws := newWatchSet(newTestWatcher(t))
	ws.set([]string{a, missing})
	if !ws.hasPending() {
		t.Fatal("expected pending dir")
	}

	ws.set([]string{b})
	if ws.hasPending() {
		t.Fatal("pending dir should be dropped when no longer referenced")
	}
	if ws.dirs[filepath.Dir(a)] {
		t.Fatal("old dir still watched")
	}
	if !ws.dirs[filepath.Dir(b)] {
		t.Fatal("new dir not watched")
	}
}

// TestRunConfigWatchReloadsWhenIncludeAppears is the end-to-end
// version of the reboot race: the watch loop is started while an
// include's directory is missing, the directory and file appear
// later, and the loop must call reload on its own (no config edit
// needed) and then watch the new directory normally.
func TestRunConfigWatchReloadsWhenIncludeAppears(t *testing.T) {
	root := t.TempDir()
	mainCfg := filepath.Join(root, "subspace.kdl")
	if err := os.WriteFile(mainCfg, []byte("// main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	include := filepath.Join(root, "vol", "TODO.md")

	reloads := make(chan struct{}, 10)
	reload := func() []string {
		reloads <- struct{}{}
		return []string{mainCfg, include}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runConfigWatch(ctx, newTestWatcher(t), []string{mainCfg, include}, reload, 20*time.Millisecond)
	}()

	// Nothing should reload while the directory is still missing.
	select {
	case <-reloads:
		t.Fatal("reload fired before include dir existed")
	case <-time.After(100 * time.Millisecond):
	}

	if err := os.MkdirAll(filepath.Dir(include), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(include, []byte("# todo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloads:
	case <-time.After(2 * time.Second):
		t.Fatal("reload did not fire after include dir appeared")
	}

	// The new directory is now a normal watch: editing the include
	// triggers a reload via fsnotify.
	drain(reloads)
	if !waitFor(t, 2*time.Second, func() bool {
		if err := os.WriteFile(include, []byte("# todo edited\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-reloads:
			return true
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}) {
		t.Fatal("editing the include did not trigger a reload")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watch loop did not stop on cancel")
	}
}

// TestRunConfigWatchIgnoresUnrelatedFiles ensures the poll/retry path
// didn't loosen the existing filter: a non-KDL file appearing in a
// watched directory must not reload.
func TestRunConfigWatchIgnoresUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	mainCfg := filepath.Join(root, "subspace.kdl")
	if err := os.WriteFile(mainCfg, []byte("// main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reloads := make(chan struct{}, 10)
	reload := func() []string {
		reloads <- struct{}{}
		return []string{mainCfg}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runConfigWatch(ctx, newTestWatcher(t), []string{mainCfg}, reload, 20*time.Millisecond)

	if err := os.WriteFile(filepath.Join(root, "stats.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloads:
		t.Fatal("unrelated file triggered a reload")
	case <-time.After(200 * time.Millisecond):
	}

	if err := os.WriteFile(filepath.Join(root, "extra.kdl"), []byte("// x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloads:
	case <-time.After(2 * time.Second):
		t.Fatal("new .kdl file in watched dir did not trigger a reload")
	}
}

func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
