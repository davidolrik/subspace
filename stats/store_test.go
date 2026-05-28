package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test-stats.db")
}

func TestStoreOpenClose(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestStoreCreatesFile(t *testing.T) {
	path := tempDB(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("database file was not created")
	}
}

func TestStoreRecordAndQuery(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)

	snap := Snapshot{
		Connections: 100,
		Active:      5,
		Protocols:   map[string]int64{"HTTP": 60, "TLS": 40},
		Errors:      map[string]int64{"dial_failed": 2},
		Upstreams: map[string]UpstreamStats{
			"corporate": {Success: 80, Failures: 1, BytesIn: 1024, BytesOut: 2048},
			"direct":    {Success: 19, Failures: 1, BytesIn: 512, BytesOut: 256},
		},
	}

	if err := store.Record(now, snap); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Query the data back
	series, err := store.Query(now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(series.Points) != 1 {
		t.Fatalf("got %d points, want 1", len(series.Points))
	}

	p := series.Points[0]
	if p.Connections != 100 {
		t.Errorf("Connections = %d, want 100", p.Connections)
	}
	if p.Active != 5 {
		t.Errorf("Active = %d, want 5", p.Active)
	}
	if p.Protocols["HTTP"] != 60 {
		t.Errorf("Protocols[HTTP] = %d, want 60", p.Protocols["HTTP"])
	}
	if p.Protocols["TLS"] != 40 {
		t.Errorf("Protocols[TLS] = %d, want 40", p.Protocols["TLS"])
	}
	if p.Errors["dial_failed"] != 2 {
		t.Errorf("Errors[dial_failed] = %d, want 2", p.Errors["dial_failed"])
	}

	corp := p.Upstreams["corporate"]
	if corp.Success != 80 || corp.BytesIn != 1024 {
		t.Errorf("corporate = %+v", corp)
	}
}

func TestStoreQueryTimeRange(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Truncate(time.Second)

	// Record 3 snapshots at different times
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		snap := Snapshot{
			Connections: int64(i + 1),
			Protocols:   map[string]int64{},
			Errors:      map[string]int64{},
			Upstreams:   map[string]UpstreamStats{},
		}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}

	// Query only the middle point
	series, err := store.Query(
		base.Add(30*time.Second),
		base.Add(90*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(series.Points) != 1 {
		t.Fatalf("got %d points, want 1", len(series.Points))
	}
	if series.Points[0].Connections != 2 {
		t.Errorf("Connections = %d, want 2", series.Points[0].Connections)
	}
}

func TestStoreDownsample(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Insert 12 points, 5 seconds apart (1 minute of data)
	// Align base to a minute boundary so all points fall in one bucket
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	for i := 0; i < 12; i++ {
		ts := base.Add(time.Duration(i) * 5 * time.Second)
		snap := Snapshot{
			Connections: int64(i + 1),
			Active:      int64(i % 3),
			Protocols:   map[string]int64{"HTTP": int64(i + 1)},
			Errors:      map[string]int64{},
			Upstreams: map[string]UpstreamStats{
				"direct": {Success: int64(i + 1), BytesIn: int64((i + 1) * 100)},
			},
		}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}

	// Downsample to 1-minute buckets for data older than 1 hour
	if err := store.Downsample(time.Hour, time.Minute); err != nil {
		t.Fatalf("Downsample failed: %v", err)
	}

	// Should have been compressed into 1 bucket
	series, err := store.Query(base.Add(-time.Minute), base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if len(series.Points) != 1 {
		t.Fatalf("got %d points after downsample, want 1", len(series.Points))
	}

	// The downsampled point should have the max connections (12)
	// and summed bytes
	p := series.Points[0]
	if p.Connections != 12 {
		t.Errorf("downsampled Connections = %d, want 12 (max)", p.Connections)
	}
}

// TestStoreDownsampleDoesNotReprocess verifies downsampling is incremental:
// once a time range has been aggregated into buckets, a later run must not
// re-aggregate it. We simulate the (production-impossible, since Record is
// monotonic) case of a stray raw row landing inside an already-bucketed
// minute and assert the existing bucket is left untouched. With a
// reprocess-everything implementation the stray row's value folds into the
// bucket via MAX(); the incremental implementation skips it.
func TestStoreDownsampleDoesNotReprocess(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)
	for i := 0; i < 12; i++ {
		ts := base.Add(time.Duration(i) * 5 * time.Second)
		snap := Snapshot{
			Protocols: map[string]int64{},
			Errors:    map[string]int64{},
			Upstreams: map[string]UpstreamStats{},
			Domains:   map[string]UpstreamStats{"a.example": {Success: int64(i + 1)}},
		}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Downsample(time.Hour, time.Minute); err != nil {
		t.Fatalf("first Downsample failed: %v", err)
	}

	var got int64
	if err := store.db.QueryRow(
		"SELECT success FROM snapshot_domains WHERE timestamp = ? AND domain = ?",
		base.Unix(), "a.example",
	).Scan(&got); err != nil {
		t.Fatalf("reading bucket after first downsample: %v", err)
	}
	if got != 12 {
		t.Fatalf("bucket success after first downsample = %d, want 12", got)
	}

	// Stray raw row inside the already-bucketed minute, below the watermark.
	if _, err := store.db.Exec(
		"INSERT INTO snapshot_domains (timestamp, domain, success, failures, bytes_in, bytes_out) VALUES (?, ?, ?, 0, 0, 0)",
		base.Add(2*time.Second).Unix(), "a.example", 9999,
	); err != nil {
		t.Fatal(err)
	}

	if err := store.Downsample(time.Hour, time.Minute); err != nil {
		t.Fatalf("second Downsample failed: %v", err)
	}

	if err := store.db.QueryRow(
		"SELECT success FROM snapshot_domains WHERE timestamp = ? AND domain = ?",
		base.Unix(), "a.example",
	).Scan(&got); err != nil {
		t.Fatalf("reading bucket after second downsample: %v", err)
	}
	if got != 12 {
		t.Errorf("bucket was reprocessed: success = %d, want 12 (stray row must be ignored)", got)
	}
}

func TestStorePruneDeletesOldRows(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	// Three points: 10 days old, 1 day old, "now". Pruning at 7 days
	// must keep the latter two and drop the 10-day-old row across all
	// four tables.
	points := []time.Time{
		now.Add(-10 * 24 * time.Hour),
		now.Add(-1 * 24 * time.Hour),
		now,
	}
	for i, ts := range points {
		snap := Snapshot{
			Connections: int64(i + 1),
			Protocols:   map[string]int64{"HTTP": 1},
			Errors:      map[string]int64{"dial_failed": 1},
			Upstreams:   map[string]UpstreamStats{"direct": {Success: 1, BytesIn: 1}},
		}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Prune(7 * 24 * time.Hour); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	for _, table := range []string{"snapshots", "snapshot_protocols", "snapshot_errors", "snapshot_upstreams"} {
		var count int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if count != 2 {
			t.Errorf("%s row count after prune = %d, want 2 (kept 1d-old + now)", table, count)
		}
	}
}

func TestStorePurgeDomain(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now()
	for i := 0; i < 3; i++ {
		snap := Snapshot{
			Domains: map[string]UpstreamStats{
				"private.example.com": {Success: int64(i + 1), BytesIn: 100, BytesOut: 200},
				"other.example.com":   {Success: int64(i + 1), BytesIn: 50, BytesOut: 75},
			},
			Routes: map[string]UpstreamStats{
				".example.com": {Success: int64(i + 1)},
			},
			Upstreams: map[string]UpstreamStats{
				"direct": {Success: int64(i + 1), BytesIn: 150, BytesOut: 275},
			},
		}
		if err := store.Record(now.Add(-time.Duration(i)*time.Minute), snap); err != nil {
			t.Fatal(err)
		}
	}

	n, err := store.PurgeDomain("private.example.com")
	if err != nil {
		t.Fatalf("PurgeDomain failed: %v", err)
	}
	if n != 3 {
		t.Errorf("PurgeDomain returned %d, want 3", n)
	}

	// The target domain should be gone.
	var got int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshot_domains WHERE domain = ?", "private.example.com").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("private.example.com rows after purge = %d, want 0", got)
	}

	// Other domains keep their rows.
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshot_domains WHERE domain = ?", "other.example.com").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("other.example.com rows after purge = %d, want 3", got)
	}

	// Routes and upstreams are intentionally left alone.
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshot_routes").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("snapshot_routes rows after purge = %d, want 3 (untouched)", got)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshot_upstreams").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("snapshot_upstreams rows after purge = %d, want 3 (untouched)", got)
	}
}

func TestStorePurgeDomainRequiresDomain(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.PurgeDomain(""); err == nil {
		t.Error("PurgeDomain(\"\") should return an error")
	}
}

func TestStorePurgeDomainNoMatchIsZero(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	n, err := store.PurgeDomain("never.recorded.example")
	if err != nil {
		t.Fatalf("PurgeDomain failed: %v", err)
	}
	if n != 0 {
		t.Errorf("PurgeDomain on empty DB returned %d, want 0", n)
	}
}

func TestStorePruneZeroIsNoOp(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := store.Record(now.Add(-time.Duration(i)*time.Hour), Snapshot{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Prune(0); err != nil {
		t.Fatalf("Prune(0) failed: %v", err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshots").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("Prune(0) deleted rows: count = %d, want 5 (no-op)", count)
	}
}

// TestStorePragmas verifies the connection-level pragmas that protect
// the stats DB from unbounded WAL growth and busy-failures. These are
// set via the DSN so every pooled connection inherits them; query each
// one back through database/sql to confirm.
func TestStorePragmas(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cases := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"synchronous", "1"}, // NORMAL
		{"busy_timeout", "5000"},
		{"journal_size_limit", "67108864"}, // 64 MiB
	}
	for _, tc := range cases {
		var got string
		if err := store.db.QueryRow("PRAGMA " + tc.pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", tc.pragma, err)
		}
		if got != tc.want {
			t.Errorf("PRAGMA %s = %q, want %q", tc.pragma, got, tc.want)
		}
	}
}

// walSize returns the current size of the *-wal sidecar for the given
// database file, or 0 if it doesn't exist.
func walSize(t *testing.T, dbPath string) int64 {
	t.Helper()
	info, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return 0
	}
	return info.Size()
}

// fillForWALGrowth writes enough snapshots through `store` to push the
// WAL past `minBytes`. Fails the test if it can't get there in a
// reasonable number of iterations — which would mean the test below is
// vacuous.
func fillForWALGrowth(t *testing.T, store *Store, path string, minBytes int64) {
	t.Helper()
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 5000; i++ {
		snap := Snapshot{
			Connections: int64(i),
			Protocols:   map[string]int64{"HTTP": int64(i)},
			Upstreams:   map[string]UpstreamStats{"direct": {Success: int64(i), BytesIn: int64(i * 1000)}},
		}
		if err := store.Record(base.Add(time.Duration(i)*time.Second), snap); err != nil {
			t.Fatal(err)
		}
		if walSize(t, path) >= minBytes {
			return
		}
	}
	t.Fatalf("WAL did not reach %d bytes after 5000 records (size=%d)", minBytes, walSize(t, path))
}

// TestStorePruneTruncatesWAL verifies that Prune flushes the WAL after
// it commits, so a daemon doing regular retention sweeps doesn't carry
// a multi-GB WAL between restarts.
func TestStorePruneTruncatesWAL(t *testing.T) {
	path := tempDB(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fillForWALGrowth(t, store, path, 256*1024)

	if err := store.Prune(time.Hour); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	if got := walSize(t, path); got != 0 {
		t.Errorf("WAL size after Prune = %d, want 0", got)
	}
}

// TestStoreDownsampleTruncatesWAL verifies that Downsample flushes the
// WAL after it commits. Downsample rewrites every old row across six
// tables, so it's one of the heaviest WAL producers in the system.
func TestStoreDownsampleTruncatesWAL(t *testing.T) {
	path := tempDB(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fillForWALGrowth(t, store, path, 256*1024)

	if err := store.Downsample(time.Hour, time.Minute); err != nil {
		t.Fatalf("Downsample failed: %v", err)
	}

	if got := walSize(t, path); got != 0 {
		t.Errorf("WAL size after Downsample = %d, want 0", got)
	}
}

// TestStoreCloseTruncatesWAL writes enough data to grow the WAL, then
// verifies Close() runs a TRUNCATE checkpoint so the *-wal sidecar
// doesn't stick around at gigabyte sizes between daemon restarts.
func TestStoreCloseTruncatesWAL(t *testing.T) {
	path := tempDB(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Truncate(time.Second)
	for i := 0; i < 2000; i++ {
		snap := Snapshot{
			Connections: int64(i),
			Protocols:   map[string]int64{"HTTP": int64(i)},
			Upstreams:   map[string]UpstreamStats{"direct": {Success: int64(i), BytesIn: int64(i * 1000)}},
		}
		if err := store.Record(base.Add(time.Duration(i)*time.Second), snap); err != nil {
			t.Fatal(err)
		}
	}

	// Confirm the WAL actually grew — otherwise the test below is vacuous.
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("expected WAL to grow during writes; stat err=%v size=%d", err, info.Size())
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After a clean Close that runs PRAGMA wal_checkpoint(TRUNCATE),
	// the WAL should be empty (truncated to 0) or removed entirely.
	info, err := os.Stat(path + "-wal")
	if err != nil {
		return // absent is fine
	}
	if info.Size() != 0 {
		t.Errorf("WAL size after Close = %d, want 0", info.Size())
	}
}
