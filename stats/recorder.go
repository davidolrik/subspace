package stats

import (
	"log/slog"
	"sync"
	"time"
)

// RecorderConfig controls the periodic snapshot and downsample intervals.
type RecorderConfig struct {
	SnapshotInterval time.Duration // how often to snapshot (default: 5s)
	DownsampleRules  []DownsampleRule
	// Retention controls how long historical data is kept. Zero or
	// negative disables pruning entirely (data accumulates forever).
	Retention time.Duration
	// BurstThreshold logs a warning when more than this many new
	// connections arrive within a single snapshot interval — a sign of a
	// connection storm (e.g. a client reconnecting in a tight loop).
	// Zero or negative disables the check.
	BurstThreshold int64
}

// DownsampleRule defines when and how to aggregate old data.
type DownsampleRule struct {
	OlderThan time.Duration // aggregate data older than this
	Bucket    time.Duration // into buckets of this size
}

// DefaultRecorderConfig returns the default 5s → 1m → 1h scheme with
// a 365-day retention window so the historical-charts UI has data to
// show without unbounded growth.
func DefaultRecorderConfig() RecorderConfig {
	return RecorderConfig{
		SnapshotInterval: 5 * time.Second,
		DownsampleRules: []DownsampleRule{
			{OlderThan: time.Hour, Bucket: time.Minute},
			{OlderThan: 7 * 24 * time.Hour, Bucket: time.Hour},
		},
		Retention: 365 * 24 * time.Hour,
		// ~200 new connections/second sustained over a 5s interval: far
		// above ordinary personal-daemon traffic, low enough to surface a
		// genuine connection storm.
		BurstThreshold: 1000,
	}
}

// Recorder periodically snapshots a Collector into a Store and
// runs downsampling on a schedule.
type Recorder struct {
	collector *Collector
	store     *Store
	config    RecorderConfig
	stop      chan struct{}
	done      chan struct{} // closed by Run on exit so Stop can wait for it
	stopOnce  sync.Once

	// Burst detection state. Run is the only caller of recordSnapshot, so
	// these need no synchronisation.
	lastConnections int64
	haveLast        bool
}

// NewRecorder creates a recorder that will periodically persist stats.
// Call Run to start recording.
func NewRecorder(collector *Collector, store *Store, config RecorderConfig) *Recorder {
	if config.SnapshotInterval <= 0 {
		config.SnapshotInterval = 5 * time.Second
	}
	return &Recorder{
		collector: collector,
		store:     store,
		config:    config,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Run starts the periodic snapshot and downsample loops. Blocks until
// Stop is called.
func (r *Recorder) Run() {
	defer close(r.done)
	snapTicker := time.NewTicker(r.config.SnapshotInterval)
	defer snapTicker.Stop()

	// Run downsample once per minute
	dsampleTicker := time.NewTicker(time.Minute)
	defer dsampleTicker.Stop()

	for {
		select {
		case <-r.stop:
			return
		case <-snapTicker.C:
			r.recordSnapshot(time.Now())
		case <-dsampleTicker.C:
			for _, rule := range r.config.DownsampleRules {
				if err := r.store.Downsample(rule.OlderThan, rule.Bucket); err != nil {
					slog.Error("stats downsample failed", "error", err)
				}
			}
			if r.config.Retention > 0 {
				if err := r.store.Prune(r.config.Retention); err != nil {
					slog.Error("stats prune failed", "error", err)
				}
			}
		}
	}
}

// recordSnapshot captures the collector, persists it, and checks for a
// connection burst. Called only from Run's snapshot tick.
func (r *Recorder) recordSnapshot(now time.Time) {
	snap := r.collector.Snapshot()
	if err := r.store.Record(now, snap); err != nil {
		slog.Error("stats record failed", "error", err)
	}
	r.detectBurst(snap)
}

// detectBurst logs a warning when the number of new connections since the
// previous snapshot exceeds the configured threshold. The cumulative
// connections counter is used (not the active gauge), so a storm that
// opens and drains within one interval is still caught. The first
// snapshot only establishes the baseline — there's nothing to diff
// against — and a counter reset (process restart) yields a negative
// delta that can't exceed the threshold, so neither false-positives.
func (r *Recorder) detectBurst(snap Snapshot) {
	defer func() {
		r.lastConnections = snap.Connections
		r.haveLast = true
	}()
	if r.config.BurstThreshold <= 0 || !r.haveLast {
		return
	}
	newConns := snap.Connections - r.lastConnections
	if newConns >= r.config.BurstThreshold {
		slog.Warn("connection burst detected",
			"new_connections", newConns,
			"interval", r.config.SnapshotInterval,
			"active", snap.Active,
		)
	}
}

// Stop signals the recorder to stop, blocks until the Run loop has
// exited, then writes a final snapshot. Waiting for the loop first
// means the flush is genuinely the last write — it can't race with a
// final snapshot still in flight inside Run. Idempotent: safe to call
// multiple times; the final flush happens exactly once.
func (r *Recorder) Stop() {
	r.stopOnce.Do(func() {
		close(r.stop)
		<-r.done
		r.Flush()
	})
}

// Flush writes one final snapshot to the store.
func (r *Recorder) Flush() {
	snap := r.collector.Snapshot()
	if err := r.store.Record(time.Now(), snap); err != nil {
		slog.Error("stats flush failed", "error", err)
	}
}
