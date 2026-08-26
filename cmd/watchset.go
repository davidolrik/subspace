package cmd

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// missingIncludePoll is how often the config watcher re-checks
// directories it couldn't subscribe to. fsnotify can't observe a
// volume being mounted or a parent folder being created, so a
// `markdown include=` whose directory doesn't exist at startup (for
// example a file on an encrypted volume that mounts after login)
// would otherwise stay broken until the next config edit.
const missingIncludePoll = 15 * time.Second

// watchSet tracks the files the config watcher cares about and the
// directories it has subscribed to on their behalf. Directories that
// can't be watched because they don't exist yet are kept in pending
// so they can be retried instead of being silently forgotten.
type watchSet struct {
	watcher *fsnotify.Watcher
	files   map[string]bool
	dirs    map[string]bool // successfully subscribed
	pending map[string]bool // subscription failed; retried by retryPending
}

func newWatchSet(w *fsnotify.Watcher) *watchSet {
	return &watchSet{
		watcher: w,
		files:   make(map[string]bool),
		dirs:    make(map[string]bool),
		pending: make(map[string]bool),
	}
}

// set replaces the watched file list. Directories of new files are
// subscribed (or marked pending when that fails), and directories no
// longer referenced by any file are unsubscribed.
func (ws *watchSet) set(files []string) {
	newFiles := make(map[string]bool, len(files))
	newDirs := make(map[string]bool, len(files))
	for _, f := range files {
		newFiles[f] = true
		newDirs[filepath.Dir(f)] = true
	}

	for dir := range newDirs {
		if ws.dirs[dir] {
			continue
		}
		ws.subscribe(dir)
	}
	for dir := range ws.dirs {
		if !newDirs[dir] {
			ws.watcher.Remove(dir)
			delete(ws.dirs, dir)
		}
	}
	for dir := range ws.pending {
		if !newDirs[dir] {
			delete(ws.pending, dir)
		}
	}
	ws.files = newFiles
}

// subscribe adds dir to the fsnotify watcher, recording it as either
// watched or pending depending on the outcome.
func (ws *watchSet) subscribe(dir string) bool {
	if err := ws.watcher.Add(dir); err != nil {
		if !ws.pending[dir] {
			slog.Warn("config watcher: directory not available yet, will retry", "path", dir, "error", err)
		}
		ws.pending[dir] = true
		return false
	}
	delete(ws.pending, dir)
	ws.dirs[dir] = true
	return true
}

// hasPending reports whether any directory is still waiting to be
// subscribed.
func (ws *watchSet) hasPending() bool {
	return len(ws.pending) > 0
}

// retryPending re-attempts every pending directory and reports
// whether at least one of them became watchable. A true result means
// files that were missing may now exist, so the caller should reload.
func (ws *watchSet) retryPending() bool {
	appeared := false
	for dir := range ws.pending {
		if ws.subscribe(dir) {
			slog.Info("config watcher: directory appeared", "path", dir)
			appeared = true
		}
	}
	return appeared
}

// wants reports whether an fsnotify event for path should trigger a
// reload: it must be a watched file, or a new .kdl file in a watched
// directory (it may match an existing glob include). Everything else
// in a watched directory — stats.db, WAL/SHM files, the operator's
// editor swap files — is ignored.
func (ws *watchSet) wants(path string) bool {
	if ws.files[path] {
		return true
	}
	if !ws.dirs[filepath.Dir(path)] {
		return false
	}
	return filepath.Ext(path) == ".kdl"
}

// runConfigWatch is the config watcher's event loop. It subscribes to
// the directories of files, calls reload whenever one of them changes,
// and replaces the watched set with reload's return value (nil means
// the reload failed and the current set is kept). Directories that
// couldn't be subscribed are polled every pollInterval; when one
// appears, reload runs so content that was missing gets picked up.
// The loop returns when ctx is cancelled or the watcher is closed.
func runConfigWatch(ctx context.Context, watcher *fsnotify.Watcher, files []string, reload func() []string, pollInterval time.Duration) {
	ws := newWatchSet(watcher)
	ws.set(files)
	slog.Info("watching config for changes", "files", len(ws.files))

	doReload := func() {
		if newFiles := reload(); newFiles != nil {
			ws.set(newFiles)
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			eventAbs, _ := filepath.Abs(event.Name)
			if !ws.wants(eventAbs) {
				continue
			}
			doReload()

		case <-ticker.C:
			if ws.hasPending() && ws.retryPending() {
				doReload()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "error", err)
		}
	}
}
