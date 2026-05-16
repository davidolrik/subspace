package stats

import (
	"testing"
	"time"
)

// recordUpstreamSeries inserts a sequence of cumulative samples for the
// given upstream so the per-window delta can be tested. The first
// sample lands one second *before* `base` so it acts as the
// pre-window baseline; the remaining samples land at `base`, `base+1s`,
// etc. so queries that start at `base` see the inside-the-window
// activity attributed against that prior baseline.
func recordUpstreamSeries(t *testing.T, store *Store, name string, base time.Time, samples []UpstreamStats) {
	t.Helper()
	if len(samples) == 0 {
		return
	}
	// Baseline lives strictly before the window so the delta math
	// can subtract it from the in-window values.
	if err := store.Record(base.Add(-time.Second), Snapshot{
		Upstreams: map[string]UpstreamStats{name: samples[0]},
	}); err != nil {
		t.Fatal(err)
	}
	for i, s := range samples[1:] {
		ts := base.Add(time.Duration(i) * time.Second)
		if err := store.Record(ts, Snapshot{
			Upstreams: map[string]UpstreamStats{name: s},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTopUpstreamsByBytesTotal(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	// Two cumulative samples per upstream — Top-N should return
	// (latest - earliest) per upstream, summed across bytes_in and
	// bytes_out, descending.
	recordUpstreamSeries(t, store, "corp", base, []UpstreamStats{
		{BytesIn: 100, BytesOut: 50},
		{BytesIn: 600, BytesOut: 350}, // delta: 800 total
	})
	recordUpstreamSeries(t, store, "direct", base, []UpstreamStats{
		{BytesIn: 0, BytesOut: 0},
		{BytesIn: 1500, BytesOut: 500}, // delta: 2000 total
	})
	recordUpstreamSeries(t, store, "wg", base, []UpstreamStats{
		{BytesIn: 50, BytesOut: 50},
		{BytesIn: 100, BytesOut: 100}, // delta: 100 total
	})

	top, err := store.TopUpstreams(base, base.Add(time.Hour), "bytes_total", 10)
	if err != nil {
		t.Fatalf("TopUpstreams failed: %v", err)
	}

	if len(top) != 3 {
		t.Fatalf("got %d entries, want 3", len(top))
	}
	if top[0].Name != "direct" || top[0].Value != 2000 {
		t.Errorf("rank 0 = %+v, want direct=2000", top[0])
	}
	if top[1].Name != "corp" || top[1].Value != 800 {
		t.Errorf("rank 1 = %+v, want corp=800", top[1])
	}
	if top[2].Name != "wg" || top[2].Value != 100 {
		t.Errorf("rank 2 = %+v, want wg=100", top[2])
	}
}

func TestTopUpstreamsRespectsLimit(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i, n := range []string{"a", "b", "c", "d", "e"} {
		recordUpstreamSeries(t, store, n, base, []UpstreamStats{
			{BytesIn: 0},
			{BytesIn: int64((i + 1) * 100)},
		})
	}

	top, err := store.TopUpstreams(base, base.Add(time.Hour), "bytes_in", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 {
		t.Fatalf("got %d entries, want 3 (limit)", len(top))
	}
	wantOrder := []string{"e", "d", "c"}
	for i, w := range wantOrder {
		if top[i].Name != w {
			t.Errorf("rank %d = %q, want %q", i, top[i].Name, w)
		}
	}
}

func TestTopUpstreamsByMetric(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	recordUpstreamSeries(t, store, "corp", base, []UpstreamStats{
		{Success: 0, Failures: 0, BytesIn: 100, BytesOut: 100},
		{Success: 50, Failures: 5, BytesIn: 500, BytesOut: 600},
	})
	recordUpstreamSeries(t, store, "wg", base, []UpstreamStats{
		{Success: 0, Failures: 0, BytesIn: 0, BytesOut: 0},
		{Success: 80, Failures: 20, BytesIn: 200, BytesOut: 50},
	})

	cases := []struct {
		metric    string
		topName   string
		topValue  int64
	}{
		{"success", "wg", 80},      // wg has more successful conns
		{"failures", "wg", 20},     // wg has more failures
		{"bytes_in", "corp", 400},  // corp wins on bytes_in delta
		{"bytes_out", "corp", 500}, // corp wins on bytes_out delta
	}
	for _, c := range cases {
		top, err := store.TopUpstreams(base, base.Add(time.Hour), c.metric, 5)
		if err != nil {
			t.Fatalf("metric %q: %v", c.metric, err)
		}
		if len(top) == 0 {
			t.Fatalf("metric %q: no entries returned", c.metric)
		}
		if top[0].Name != c.topName || top[0].Value != c.topValue {
			t.Errorf("metric %q: top = %+v, want %s=%d", c.metric, top[0], c.topName, c.topValue)
		}
	}
}

func TestTopUpstreamsRejectsUnknownMetric(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := store.TopUpstreams(time.Now().Add(-time.Hour), time.Now(), "nonsense", 5); err == nil {
		t.Error("expected error for unknown metric")
	}
}

func TestTopUpstreamsEmptyDatabase(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	top, err := store.TopUpstreams(time.Now().Add(-time.Hour), time.Now(), "bytes_total", 10)
	if err != nil {
		t.Fatalf("TopUpstreams on empty DB failed: %v", err)
	}
	if len(top) != 0 {
		t.Errorf("got %d entries, want 0", len(top))
	}
}

func TestTopDomains(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	// First sample lands strictly before `base` as the pre-window
	// baseline; subsequent samples land inside the window so the delta
	// math has something to subtract from.
	record := func(name string, samples []UpstreamStats) {
		if len(samples) == 0 {
			return
		}
		if err := store.Record(base.Add(-time.Second), Snapshot{
			Domains: map[string]UpstreamStats{name: samples[0]},
		}); err != nil {
			t.Fatal(err)
		}
		for i, s := range samples[1:] {
			ts := base.Add(time.Duration(i) * time.Second)
			if err := store.Record(ts, Snapshot{
				Domains: map[string]UpstreamStats{name: s},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	record("github.com", []UpstreamStats{{BytesIn: 0, BytesOut: 0}, {BytesIn: 1000, BytesOut: 500}})
	record("example.com", []UpstreamStats{{BytesIn: 100, BytesOut: 100}, {BytesIn: 600, BytesOut: 400}})
	record("internal", []UpstreamStats{{Success: 0}, {Success: 50}})

	top, err := store.TopDomains(base, base.Add(time.Hour), "bytes_total", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Name != "github.com" || top[0].Value != 1500 {
		t.Errorf("top domains by bytes_total = %+v, want github.com=1500 first", top)
	}
	if top[1].Name != "example.com" || top[1].Value != 800 {
		t.Errorf("top[1] = %+v, want example.com=800", top[1])
	}

	// success metric
	top, err = store.TopDomains(base, base.Add(time.Hour), "success", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].Name != "internal" || top[0].Value != 50 {
		t.Errorf("top domains by success = %+v, want internal=50 only", top)
	}
}

func TestTopRoutes(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	record := func(name string, samples []UpstreamStats) {
		for i, s := range samples {
			ts := base.Add(time.Duration(i) * time.Second)
			snap := Snapshot{Routes: map[string]UpstreamStats{name: s}}
			if err := store.Record(ts, snap); err != nil {
				t.Fatal(err)
			}
		}
	}

	record("*.corp.example", []UpstreamStats{{BytesIn: 0}, {BytesIn: 5000}})
	record("direct", []UpstreamStats{{BytesIn: 0}, {BytesIn: 2000}})

	top, err := store.TopRoutes(base, base.Add(time.Hour), "bytes_in", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || top[0].Name != "*.corp.example" || top[0].Value != 5000 {
		t.Errorf("top routes = %+v, want *.corp.example=5000 first", top)
	}
}

func TestTopRoutesIn(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	record := func(name string, samples []UpstreamStats) {
		for i, s := range samples {
			ts := base.Add(time.Duration(i) * time.Second)
			snap := Snapshot{Routes: map[string]UpstreamStats{name: s}}
			if err := store.Record(ts, snap); err != nil {
				t.Fatal(err)
			}
		}
	}
	record(".ads.example", []UpstreamStats{{BytesIn: 0}, {BytesIn: 5000}})
	record(".tracker.io", []UpstreamStats{{BytesIn: 0}, {BytesIn: 1000}})
	record(".corp.internal", []UpstreamStats{{BytesIn: 0}, {BytesIn: 9999}}) // not in filter

	patterns := []string{".ads.example", ".tracker.io"}
	top, err := store.TopRoutesIn(base, base.Add(time.Hour), "bytes_in", 10, patterns)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 {
		t.Fatalf("got %d entries, want 2 (filtered to blackhole patterns): %+v", len(top), top)
	}
	if top[0].Name != ".ads.example" || top[0].Value != 5000 {
		t.Errorf("rank 0 = %+v, want .ads.example=5000", top[0])
	}
	if top[1].Name != ".tracker.io" || top[1].Value != 1000 {
		t.Errorf("rank 1 = %+v, want .tracker.io=1000", top[1])
	}
	for _, e := range top {
		if e.Name == ".corp.internal" {
			t.Errorf("filtered-out route .corp.internal leaked into results")
		}
	}
}

func TestTopRoutesInEmptyFilterReturnsNothing(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	snap := Snapshot{Routes: map[string]UpstreamStats{".x": {BytesIn: 100}}}
	_ = store.Record(base, snap)
	_ = store.Record(base.Add(time.Second), Snapshot{Routes: map[string]UpstreamStats{".x": {BytesIn: 200}}})

	top, err := store.TopRoutesIn(base, base.Add(time.Hour), "bytes_in", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 0 {
		t.Errorf("got %d entries, want 0 when filter is empty (must NOT degrade to unfiltered): %+v", len(top), top)
	}
}

// TestTopWindowCountsPreSnapshotActivity verifies the delta includes
// activity that landed in the very first snapshot of the domain. The
// previous SQL used MAX(window) - MIN(window), which silently dropped
// any cumulative value present in the first row — visible to the user
// as "Top Activity shows the same numbers regardless of the period
// selector" on a fresh daemon or a recently-active domain.
func TestTopWindowCountsPreSnapshotActivity(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-10 * time.Minute).Truncate(time.Second)

	// Domain's very first snapshot already holds 1000 bytes (the
	// request happened before the first 5s recorder tick). Then a
	// later request brings it to 2500.
	record := func(ts time.Time, bytes int64) {
		snap := Snapshot{Domains: map[string]UpstreamStats{
			"fresh.example": {BytesIn: bytes},
		}}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}
	record(base, 1000)
	record(base.Add(30*time.Second), 1000)
	record(base.Add(time.Minute), 2500)
	record(base.Add(2*time.Minute), 2500)

	// Window starts before the first snapshot → all activity should
	// be attributed, since nothing for this domain pre-existed the
	// window.
	top, err := store.TopDomains(base.Add(-time.Hour), base.Add(time.Hour), "bytes_in", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 {
		t.Fatalf("got %d entries, want 1", len(top))
	}
	if got := top[0].Value; got != 2500 {
		t.Errorf("delta = %d, want 2500 (all activity, including pre-first-snapshot 1000)", got)
	}
}

// TestTopWindowExcludesPreWindowActivity verifies the inverse — that
// activity recorded BEFORE the window is not double-counted into the
// window's delta. Caught a previous fix attempt that read the
// pre-window value with the wrong sign.
func TestTopWindowExcludesPreWindowActivity(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	record := func(ts time.Time, bytes int64) {
		snap := Snapshot{Domains: map[string]UpstreamStats{
			"long-lived.example": {BytesIn: bytes},
		}}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}
	// Pre-window activity reached 5000.
	record(base, 5000)
	record(base.Add(10*time.Minute), 5000)
	// Window starts here.
	windowStart := base.Add(30 * time.Minute)
	// Inside the window, an additional 1500 bytes accumulate.
	record(windowStart.Add(5*time.Minute), 6500)
	record(windowStart.Add(20*time.Minute), 6500)

	top, err := store.TopDomains(windowStart, windowStart.Add(time.Hour), "bytes_in", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 {
		t.Fatalf("got %d entries, want 1", len(top))
	}
	if got := top[0].Value; got != 1500 {
		t.Errorf("delta = %d, want 1500 (only the activity inside the window)", got)
	}
}

// TestTopWindowSurvivesRestart verifies a counter reset inside the
// window doesn't make the delta go negative (which the HAVING > 0
// filter would otherwise silently exclude). Restarts are an
// acknowledged best-effort case — we want to surface the post-restart
// activity at minimum, not lose the row entirely.
func TestTopWindowSurvivesRestart(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	record := func(ts time.Time, bytes int64) {
		snap := Snapshot{Domains: map[string]UpstreamStats{
			"restarted.example": {BytesIn: bytes},
		}}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}
	// Pre-window cumulative ran to 10000.
	record(base, 10000)
	windowStart := base.Add(10 * time.Minute)
	// Inside the window the daemon restarts: counters drop to 0
	// then climb back up to 300.
	record(windowStart.Add(time.Minute), 0)
	record(windowStart.Add(5*time.Minute), 300)

	top, err := store.TopDomains(windowStart, windowStart.Add(time.Hour), "bytes_in", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 {
		t.Fatalf("got %d entries, want 1 (post-restart activity should still surface)", len(top))
	}
	if got := top[0].Value; got != 300 {
		t.Errorf("delta = %d, want 300 (post-restart growth)", got)
	}
}

// TestTopWindowStraddlingRestartCountsBothSides verifies that a window
// crossing a daemon restart attributes both the in-window growth that
// happened pre-restart AND the post-restart growth — without picking
// up the pre-restart historical peak (which would inflate) or the
// post-restart cumulative alone (which would under-count).
func TestTopWindowStraddlingRestartCountsBothSides(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	base := time.Now().Add(-15 * time.Minute).Truncate(time.Second)
	record := func(ts time.Time, bytes int64) {
		snap := Snapshot{Upstreams: map[string]UpstreamStats{
			"b1": {BytesIn: bytes},
		}}
		if err := store.Record(ts, snap); err != nil {
			t.Fatal(err)
		}
	}

	// Window will cover [base+10min, base+15min] (a 5-minute window).
	windowStart := base.Add(10 * time.Minute)
	windowEnd := base.Add(15 * time.Minute)

	// Pre-window: counter reached 200 right before the window.
	record(base, 100)
	record(base.Add(5*time.Minute), 200)
	// Immediately before window start — this is the seed value the
	// new SQL must read to attribute the first chunk of in-window
	// growth correctly.
	record(windowStart.Add(-5*time.Second), 200)

	// In-window pre-restart: 200 → 250 (50 of in-window growth).
	record(windowStart, 220)
	record(windowStart.Add(2*time.Minute), 250)

	// Restart inside the window at +2min30s. Counter drops to 0,
	// then climbs to 6.
	restart := windowStart.Add(2*time.Minute + 30*time.Second)
	record(restart, 0)
	record(restart.Add(time.Minute), 4)
	record(windowEnd, 6)

	top, err := store.TopUpstreams(windowStart, windowEnd, "bytes_in", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(top), top)
	}
	if top[0].Name != "b1" {
		t.Errorf("name = %q, want %q", top[0].Name, "b1")
	}
	// Expected:
	//   pre-restart in-window growth: 250 - 200 (seed) = 50
	//   post-restart in-window growth: 6 - 0           = 6
	//   total                                          = 56
	if top[0].Value != 56 {
		t.Errorf("value = %d, want 56 (pre-restart 50 + post-restart 6)", top[0].Value)
	}
}

func TestTopUpstreamsWindowFilters(t *testing.T) {
	store, err := OpenStore(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)

	// Old activity: 2 hours ago, should NOT appear in a 1-hour window.
	recordUpstreamSeries(t, store, "old", now.Add(-2*time.Hour), []UpstreamStats{
		{BytesIn: 0}, {BytesIn: 9999},
	})
	// Recent activity: within the window.
	recordUpstreamSeries(t, store, "recent", now.Add(-30*time.Minute), []UpstreamStats{
		{BytesIn: 0}, {BytesIn: 100},
	})

	top, err := store.TopUpstreams(now.Add(-time.Hour), now, "bytes_in", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].Name != "recent" {
		t.Errorf("got %+v, want a single 'recent' entry", top)
	}
}
