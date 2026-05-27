package env

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CaptureFn captures the current environment as a name→value map.
// The real implementation calls the operator's shell with the login
// flag so per-user login init files run; tests inject a fake.
// Returns an error only when there's no usable environment to hand
// back at all.
type CaptureFn func(ctx context.Context) (map[string]string, error)

// Capture spawns `<shell> -lc env`, captures stdout, and parses it
// into a name→value map. Falls back to os.Environ() when shell == ""
// or the spawn fails for any reason — we never want a transient
// shell error to leave the dashboard with a blank environment.
//
// `-lc env` means: login shell (sources per-user login init files
// like `.zprofile` / `.zlogin` / `.bash_profile`), then run `env` to
// print the resulting environment. That's where operators should
// put `PUBLIC_IP=$(curl …)` so it's visible to subspace. We
// deliberately do NOT pass `-i`: an interactive shell tries to
// take the controlling terminal (job control, /dev/tty, terminal
// mode setup), which leaves subspace's tty in a state where ctrl-c
// no longer reaches the parent.
func Capture(ctx context.Context, shell string) (map[string]string, error) {
	if shell == "" {
		return osEnvironMap(), nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, shell, "-lc", "env")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Discard stderr — login init files sometimes print motds or
	// completion-loading chatter to stderr that would otherwise
	// pollute subspace's logs every tick.
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		slog.Warn("env capture: shell spawn failed; falling back to os.Environ", "shell", shell, "error", err)
		return osEnvironMap(), nil
	}
	return parseEnvOutput(stdout.Bytes()), nil
}

// osEnvironMap converts os.Environ() into the map form. Used as the
// startup-bootstrap and as the fallback when the shell spawn fails.
func osEnvironMap() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// parseEnvOutput parses the output of `env` into a map. `env`
// prints one `KEY=VALUE` pair per line. Values can contain newlines
// (rare but valid) — when a line doesn't start with a `KEY=` prefix
// it's appended to the previous variable's value. Lines without an
// `=` are skipped silently (no useful diagnostic to surface).
func parseEnvOutput(data []byte) map[string]string {
	out := make(map[string]string)
	var lastKey string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if i := indexOfKeyDelimiter(line); i > 0 {
			key := line[:i]
			val := line[i+1:]
			out[key] = val
			lastKey = key
			continue
		}
		// Continuation of a multi-line value.
		if lastKey != "" {
			out[lastKey] += "\n" + line
		}
	}
	return out
}

// indexOfKeyDelimiter returns the index of the `=` that separates a
// shell-identifier key from its value, or -1 if the line doesn't
// look like a `KEY=VALUE` header. We require the prefix to be a
// valid shell identifier so a line like `path with =sign in middle`
// from a multi-line value continuation isn't mistaken for a new var.
func indexOfKeyDelimiter(line string) int {
	if line == "" {
		return -1
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '=' {
			if i == 0 {
				return -1
			}
			return i
		}
		if !isIdentChar(c, i == 0) {
			return -1
		}
	}
	return -1
}

func isIdentChar(c byte, first bool) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c == '_':
		return true
	case !first && c >= '0' && c <= '9':
		return true
	}
	return false
}

// Refresher periodically calls capture and applies the result to the
// snapshot, calling onChange iff a *referenced* variable's value
// changed. Mirrors the stats.Recorder shape so the goroutine pattern
// stays consistent across the codebase.
type Refresher struct {
	snap     *Snapshot
	interval time.Duration
	capture  CaptureFn
	onChange func()
	stop     chan struct{}
	done     chan struct{} // closed by Run on exit so Stop can wait for it
	stopOnce sync.Once
}

// NewRefresher constructs a refresher. The cmd/ layer enforces the
// documented 10s minimum interval at config-parse time; this
// constructor trusts whatever's passed in so tests can drive the
// loop at millisecond cadence.
func NewRefresher(snap *Snapshot, interval time.Duration, capture CaptureFn, onChange func()) *Refresher {
	return &Refresher{
		snap:     snap,
		interval: interval,
		capture:  capture,
		onChange: onChange,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Run blocks until Stop is called. Each tick calls capture, applies
// the result via Snapshot.Replace, and invokes onChange when (and
// only when) a referenced variable actually changed.
func (r *Refresher) Run() {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.tickOnce()
		}
	}
}

func (r *Refresher) tickOnce() {
	// Nothing references the environment → nothing to refresh. Skip the
	// shell spawn entirely; this is what keeps idle CPU at zero for
	// configs that don't use ${NAME} substitution. Checked per tick (not
	// once at startup) so a config reload that adds an env-referencing
	// card resumes capturing without a daemon restart.
	if !r.snap.HasReferences() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	vars, err := r.capture(ctx)
	if err != nil {
		slog.Warn("env capture failed", "error", err)
		return
	}
	if r.snap.Replace(vars) {
		if r.onChange != nil {
			r.onChange()
		}
	}
}

// Stop signals Run to return and blocks until it has. After Stop
// returns, no further capture can run — callers (and tests) can rely
// on the loop being fully halted. Idempotent: safe to call multiple
// times and from multiple goroutines.
func (r *Refresher) Stop() {
	r.stopOnce.Do(func() {
		close(r.stop)
		<-r.done
	})
}

// ResolveShell picks the shell to invoke for env capture. Order:
//  1. configured (operator's `env { shell ... }`),
//  2. $SHELL,
//  3. /bin/sh as a last-resort default.
//
// Returns "" only if all three are empty, which on a real system
// shouldn't happen — but Capture treats "" as "use os.Environ()" so
// the dashboard still has a sane environment either way.
func ResolveShell(configured string) string {
	if configured != "" {
		return configured
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if _, err := os.Stat("/bin/sh"); err == nil {
		return "/bin/sh"
	}
	return ""
}

// CaptureWith returns a CaptureFn bound to a specific shell. The
// refresher uses this so the shell name is captured once at startup
// and not re-resolved on every tick.
func CaptureWith(shell string) CaptureFn {
	return func(ctx context.Context) (map[string]string, error) {
		return Capture(ctx, shell)
	}
}
