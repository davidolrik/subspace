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
