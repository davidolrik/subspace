package stats

import (
	"log/slog"
	"time"
)

// RecorderConfig controls the periodic snapshot and downsample intervals.
type RecorderConfig struct {
	SnapshotInterval time.Duration // how often to snapshot (default: 5s)
	DownsampleRules  []DownsampleRule
}

// DownsampleRule defines when and how to aggregate old data.
type DownsampleRule struct {
	OlderThan time.Duration // aggregate data older than this
	Bucket    time.Duration // into buckets of this size
}

// DefaultRecorderConfig returns the default 5s → 1m → 1h scheme.
func DefaultRecorderConfig() RecorderConfig {
	return RecorderConfig{
		SnapshotInterval: 5 * time.Second,
		DownsampleRules: []DownsampleRule{
			{OlderThan: time.Hour, Bucket: time.Minute},
			{OlderThan: 7 * 24 * time.Hour, Bucket: time.Hour},
		},
	}
}

// Recorder periodically snapshots a Collector into a Store and
// runs downsampling on a schedule.
type Recorder struct {
	collector *Collector
	store     *Store
	config    RecorderConfig
	stop      chan struct{}
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
	}
}

// Run starts the periodic snapshot and downsample loops. Blocks until
// Stop is called.
func (r *Recorder) Run() {
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
			snap := r.collector.Snapshot()
			if err := r.store.Record(time.Now(), snap); err != nil {
				slog.Error("stats record failed", "error", err)
			}
		case <-dsampleTicker.C:
			for _, rule := range r.config.DownsampleRules {
				if err := r.store.Downsample(rule.OlderThan, rule.Bucket); err != nil {
					slog.Error("stats downsample failed", "error", err)
				}
			}
		}
	}
}

// Stop signals the recorder to stop and writes a final snapshot.
func (r *Recorder) Stop() {
	close(r.stop)
	r.Flush()
}

// Flush writes one final snapshot to the store.
func (r *Recorder) Flush() {
	snap := r.collector.Snapshot()
	if err := r.store.Record(time.Now(), snap); err != nil {
		slog.Error("stats flush failed", "error", err)
	}
}
