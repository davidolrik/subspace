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
