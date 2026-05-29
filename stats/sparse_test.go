package stats

import (
	"fmt"
	"testing"
	"time"
)

// countDomainRows returns the raw number of rows in snapshot_domains.
// The sparse-write behaviour is defined in terms of which Record calls
// actually persist a per-domain row, so counting rows directly is what
// makes each decision observable.
func countDomainRows(t *testing.T, store *Store) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshot_domains").Scan(&n); err != nil {
		t.Fatalf("count snapshot_domains: %v", err)
	}
	return n
}

func countRows(t *testing.T, store *Store, table string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestRecordSkipsUnchangedDomainRows pins the core of the sparse-write
// fix: a domain whose counters did not change since the last persisted
// row produces no new row — except on a keyframe tick, where every live
// domain is re-written so the windowed-delta query always finds a seed.
func TestRecordSkipsUnchangedDomainRows(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	t0 := time.Now().Truncate(time.Second)
	dom := func(s int64) Snapshot {
		return Snapshot{Domains: map[string]UpstreamStats{
			"example.com": {Success: s, BytesIn: s * 10},
		}}
	}

	// The first record is always a keyframe: it establishes the baseline.
	if err := store.Record(t0, dom(3)); err != nil {
		t.Fatal(err)
	}
	if got := countDomainRows(t, store); got != 1 {
		t.Fatalf("after first record: got %d domain rows, want 1", got)
	}

	// Unchanged counters, well within the keyframe interval → no new row.
	if err := store.Record(t0.Add(5*time.Second), dom(3)); err != nil {
		t.Fatal(err)
	}
	if got := countDomainRows(t, store); got != 1 {
		t.Fatalf("unchanged domain was re-written: got %d domain rows, want 1", got)
	}

	// A changed counter is always written, immediately.
	if err := store.Record(t0.Add(10*time.Second), dom(4)); err != nil {
		t.Fatal(err)
	}
	if got := countDomainRows(t, store); got != 2 {
		t.Fatalf("changed domain was not written: got %d domain rows, want 2", got)
	}

	// After the keyframe interval elapses, even an unchanged domain is
	// re-written so a window starting here still has a seed sample.
	if err := store.Record(t0.Add(keyframeInterval), dom(4)); err != nil {
		t.Fatal(err)
	}
	if got := countDomainRows(t, store); got != 3 {
		t.Fatalf("keyframe did not re-write unchanged domain: got %d domain rows, want 3", got)
	}
}

// TestDenseTablesAlwaysWritten guards the scope of the fix: the low-
// cardinality, line-graphed series stay dense (one row per Record), so
// the charts never develop gaps. Only domains/routes go sparse.
func TestDenseTablesAlwaysWritten(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	t0 := time.Now().Truncate(time.Second)
	snap := Snapshot{
		Connections: 100,
		Active:      5,
		Protocols:   map[string]int64{"HTTP": 60},
		Errors:      map[string]int64{"dial_failed": 1},
		Upstreams:   map[string]UpstreamStats{"direct": {Success: 99}},
	}

	// Two identical records within the keyframe interval.
	if err := store.Record(t0, snap); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(t0.Add(5*time.Second), snap); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"snapshots", "snapshot_protocols", "snapshot_errors", "snapshot_upstreams"} {
		if got := countRows(t, store, table); got != 2 {
			t.Errorf("%s: got %d rows, want 2 (dense series must write every tick)", table, got)
		}
	}
}

// recordDense inserts a row for every domain unconditionally — the
// pre-sparse behaviour — so a sparse store can be compared against it.
func recordDense(t *testing.T, store *Store, ts time.Time, domains map[string]UpstreamStats) {
	t.Helper()
	for host, ds := range domains {
		if _, err := store.db.Exec(
			"INSERT INTO snapshot_domains (timestamp, domain, success, failures, bytes_in, bytes_out) VALUES (?, ?, ?, ?, ?, ?)",
			ts.Unix(), host, ds.Success, ds.Failures, ds.BytesIn, ds.BytesOut,
		); err != nil {
			t.Fatalf("dense insert: %v", err)
		}
	}
}

// TestTopDomainsLosslessUnderSparseWrites is the correctness capstone:
// dropping unchanged per-domain rows (and seeding idle domains only on
// keyframes) must not change what TopDomains reports for any window —
// the windowed-delta query sums positive consecutive deltas, to which an
// unchanged row contributes exactly zero. Replays an identical sequence
// into a dense store and a sparse store and demands equal results,
// including a window that opens while a domain sits idle.
func TestTopDomainsLosslessUnderSparseWrites(t *testing.T) {
	dense, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer dense.Close()
	sparse, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sparse.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	// 20 minutes of 5s ticks. "a" climbs for the first 5 minutes then
	// goes idle; "b" stays idle for 10 minutes then climbs. A cumulative
	// (monotonic) counter, like the real per-domain Success counter.
	var aSucc, bSucc int64
	for i := 0; i*5 <= 20*60; i++ {
		ts := base.Add(time.Duration(i*5) * time.Second)
		elapsed := time.Duration(i*5) * time.Second
		if elapsed < 5*time.Minute {
			aSucc++
		}
		if elapsed >= 10*time.Minute {
			bSucc += 2
		}
		domains := map[string]UpstreamStats{
			"a.example": {Success: aSucc},
			"b.example": {Success: bSucc},
		}
		recordDense(t, dense, ts, domains)
		if err := sparse.Record(ts, Snapshot{Domains: domains}); err != nil {
			t.Fatalf("sparse record: %v", err)
		}
	}

	// Sparse must drop a large fraction of the dense rows, or the test
	// isn't exercising the optimisation at all.
	denseRows := countDomainRows(t, dense)
	sparseRows := countDomainRows(t, sparse)
	if sparseRows >= denseRows/2 {
		t.Fatalf("sparse store wrote %d rows vs dense %d — not sparse enough to be meaningful", sparseRows, denseRows)
	}

	windows := []struct {
		name     string
		from, to time.Time
	}{
		{"full", base, base.Add(20 * time.Minute)},
		{"a-active", base, base.Add(5 * time.Minute)},
		{"a-idle-b-active", base.Add(10 * time.Minute), base.Add(15 * time.Minute)},
		{"tail-idle-a", base.Add(6 * time.Minute), base.Add(9 * time.Minute)},
	}
	for _, w := range windows {
		t.Run(w.name, func(t *testing.T) {
			want, err := dense.TopDomains(w.from, w.to, "success", 10)
			if err != nil {
				t.Fatalf("dense TopDomains: %v", err)
			}
			got, err := sparse.TopDomains(w.from, w.to, "success", 10)
			if err != nil {
				t.Fatalf("sparse TopDomains: %v", err)
			}
			if len(got) != len(want) {
				t.Fatalf("entry count: sparse=%v dense=%v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("entry %d: sparse=%+v dense=%+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestRecordBatchesManyDomains exercises the prepared-statement batch
// path: a single keyframe record with many distinct domains must persist
// exactly one correct row per domain.
func TestRecordBatchesManyDomains(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	domains := make(map[string]UpstreamStats, 200)
	for i := 0; i < 200; i++ {
		host := fmt.Sprintf("host-%03d.example", i)
		domains[host] = UpstreamStats{Success: int64(i), BytesIn: int64(i) * 7}
	}

	t0 := time.Now().Truncate(time.Second)
	if err := store.Record(t0, Snapshot{Domains: domains}); err != nil {
		t.Fatal(err)
	}

	if got := countDomainRows(t, store); got != len(domains) {
		t.Fatalf("got %d domain rows, want %d", got, len(domains))
	}

	// Spot-check that values round-tripped intact through the batch.
	var succ, bin int64
	if err := store.db.QueryRow(
		"SELECT success, bytes_in FROM snapshot_domains WHERE domain = ?", "host-042.example",
	).Scan(&succ, &bin); err != nil {
		t.Fatal(err)
	}
	if succ != 42 || bin != 42*7 {
		t.Errorf("host-042: success=%d bytes_in=%d, want 42 and %d", succ, bin, 42*7)
	}
}

// TestKeyframeIntervalWithinLookback locks the correctness invariant:
// the keyframe interval must be no larger than the windowed-delta query's
// pre-window lookback, or an idle-then-active domain loses its seed and
// the query over-counts its entire cumulative total as in-window growth.
func TestKeyframeIntervalWithinLookback(t *testing.T) {
	if int64(keyframeInterval/time.Second) > preWindowLookback {
		t.Fatalf("keyframeInterval (%s) exceeds preWindowLookback (%ds): top-N delta would over-count idle-then-active domains",
			keyframeInterval, preWindowLookback)
	}
}
