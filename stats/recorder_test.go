package stats

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// captureWarnLogs redirects slog to a buffer for the duration of the test
// so assertions can inspect emitted warnings.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestRecorderDetectsConnectionBurst checks that a jump in new
// connections between two snapshots beyond the configured threshold logs
// a warning, while the establishing (first) snapshot never does — there's
// no prior sample to diff against.
func TestRecorderDetectsConnectionBurst(t *testing.T) {
	buf := captureWarnLogs(t)

	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	col := New()
	rec := NewRecorder(col, store, RecorderConfig{SnapshotInterval: time.Second, BurstThreshold: 100})

	now := time.Now().Truncate(time.Second)

	// Baseline snapshot: a handful of connections, no prior sample.
	for i := 0; i < 5; i++ {
		col.IncProtocol("TLS")
	}
	rec.recordSnapshot(now)
	if strings.Contains(buf.String(), "burst") {
		t.Fatalf("baseline snapshot logged a burst: %q", buf.String())
	}

	// Storm: 500 new connections in one interval, over the 100 threshold.
	for i := 0; i < 500; i++ {
		col.IncProtocol("TLS")
	}
	rec.recordSnapshot(now.Add(time.Second))

	if !strings.Contains(buf.String(), "connection burst") {
		t.Errorf("expected a connection burst warning, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "new_connections=500") {
		t.Errorf("burst log should report the new-connection count, got: %q", buf.String())
	}
}

// TestRecorderNoBurstUnderThreshold guards against noise: ordinary
// per-interval growth below the threshold must not warn.
func TestRecorderNoBurstUnderThreshold(t *testing.T) {
	buf := captureWarnLogs(t)

	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	col := New()
	rec := NewRecorder(col, store, RecorderConfig{SnapshotInterval: time.Second, BurstThreshold: 100})

	now := time.Now().Truncate(time.Second)
	rec.recordSnapshot(now)
	for i := 0; i < 50; i++ { // well under the threshold
		col.IncProtocol("TLS")
	}
	rec.recordSnapshot(now.Add(time.Second))

	if strings.Contains(buf.String(), "burst") {
		t.Errorf("threshold should suppress small growth, got: %q", buf.String())
	}
}

// countSnapshotRows returns the raw number of rows in the snapshots
// table. Counting rows (rather than Query's per-second DataPoints)
// makes every Record call observable, which is what lets the test
// detect a loop that keeps recording after Stop.
func countSnapshotRows(t *testing.T, store *Store) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshots").Scan(&n); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	return n
}

func TestRecorderStopIdempotent(t *testing.T) {
	// Stop must be safe to call more than once: no panic on the closed
	// channel, and the one-shot final flush must not write twice.
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	rec := NewRecorder(New(), store, RecorderConfig{SnapshotInterval: 5 * time.Millisecond})
	go rec.Run()
	time.Sleep(20 * time.Millisecond)

	rec.Stop()
	after1 := countSnapshotRows(t, store)

	rec.Stop() // must not panic and must not flush a second time
	after2 := countSnapshotRows(t, store)

	if after2 != after1 {
		t.Errorf("second Stop wrote extra rows: %d -> %d", after1, after2)
	}
}

func TestRecorderStopHaltsLoop(t *testing.T) {
	// Stop is synchronous: once it returns, the Run loop has exited and
	// the final flush has been written, so no further rows can appear.
	// Reading the count immediately after Stop and again later must give
	// the same number — a loop still ticking would record more.
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	rec := NewRecorder(New(), store, RecorderConfig{SnapshotInterval: 5 * time.Millisecond})
	go rec.Run()
	time.Sleep(30 * time.Millisecond) // let several snapshots land

	rec.Stop()

	before := countSnapshotRows(t, store)
	time.Sleep(40 * time.Millisecond) // ~8 more ticks if the loop were still alive
	after := countSnapshotRows(t, store)
	if after != before {
		t.Errorf("recorder kept recording after Stop: before=%d after=%d", before, after)
	}
}
