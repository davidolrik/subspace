package stats

import (
	"testing"
	"time"
)

// recordUpstreamSeries inserts a sequence of cumulative samples for the
// given upstream so the per-window MAX-MIN delta can be tested.
func recordUpstreamSeries(t *testing.T, store *Store, name string, base time.Time, samples []UpstreamStats) {
	t.Helper()
	for i, s := range samples {
		ts := base.Add(time.Duration(i) * time.Second)
		snap := Snapshot{
			Upstreams: map[string]UpstreamStats{name: s},
		}
		if err := store.Record(ts, snap); err != nil {
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
	record := func(name string, samples []UpstreamStats) {
		for i, s := range samples {
			ts := base.Add(time.Duration(i) * time.Second)
			snap := Snapshot{Domains: map[string]UpstreamStats{name: s}}
			if err := store.Record(ts, snap); err != nil {
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
