package stats

import (
	"testing"
	"time"
)

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
