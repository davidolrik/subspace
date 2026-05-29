package stats

import (
	"database/sql"
	"testing"
	"time"
)

func mustOpenRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	return db
}

// seedDenseDomains writes per-domain rows the pre-sparse way — one row
// per tick regardless of whether the counters changed — straight into a
// freshly-schema'd database without running migrations, simulating a
// database created by an older build. "a" changes for a few ticks then
// sits idle; "b" is idle for the first half then climbs. Both idle runs
// span several keyframe buckets, which is what the compaction must thin.
func seedDenseDomains(t *testing.T, path string, base time.Time) {
	t.Helper()
	raw := mustOpenRaw(t, path)
	defer raw.Close()
	if _, err := raw.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}

	insert := func(name string, ts time.Time, succ int64) {
		if _, err := raw.Exec(
			"INSERT INTO snapshot_domains (timestamp, domain, success, failures, bytes_in, bytes_out) VALUES (?, ?, ?, ?, ?, ?)",
			ts.Unix(), name, succ, 0, succ*10, 0,
		); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	for i := 0; i <= 180; i++ { // 15 minutes of 5s ticks
		ts := base.Add(time.Duration(i*5) * time.Second)
		a := int64(i)
		if a > 3 {
			a = 3
		}
		var b int64
		if i >= 90 {
			b = int64(i-90) * 2
		}
		insert("a.example", ts, a)
		insert("b.example", ts, b)
	}
}

func sameTopEntries(a, b []TopEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCompactionMigrationIsLosslessForTopDomains drives the one-time
// compaction migration end to end: a legacy dense database is opened via
// OpenStore (which runs the migration), and TopDomains must return
// exactly what it returned on the un-compacted data — for the full range
// and for a window that opens during an idle run — while the row count
// drops sharply.
func TestCompactionMigrationIsLosslessForTopDomains(t *testing.T) {
	path := tempDB(t)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	seedDenseDomains(t, path, base)

	from, to := base, base.Add(15*time.Minute)
	subFrom, subTo := base.Add(10*time.Minute), base.Add(15*time.Minute)

	pre := &Store{db: mustOpenRaw(t, path)}
	wantFull, err := pre.TopDomains(from, to, "success", 10)
	if err != nil {
		t.Fatalf("pre TopDomains: %v", err)
	}
	wantSub, err := pre.TopDomains(subFrom, subTo, "success", 10)
	if err != nil {
		t.Fatalf("pre TopDomains (sub): %v", err)
	}
	preRows := countDomainRows(t, pre)
	pre.db.Close()

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	if err := store.Maintain(); err != nil { // runs the compaction migration
		t.Fatalf("Maintain: %v", err)
	}

	postRows := countDomainRows(t, store)
	if postRows >= preRows {
		t.Fatalf("migration did not reduce rows: pre=%d post=%d", preRows, postRows)
	}

	gotFull, err := store.TopDomains(from, to, "success", 10)
	if err != nil {
		t.Fatalf("post TopDomains: %v", err)
	}
	if !sameTopEntries(gotFull, wantFull) {
		t.Errorf("full window changed by compaction:\n got=%+v\nwant=%+v", gotFull, wantFull)
	}
	gotSub, err := store.TopDomains(subFrom, subTo, "success", 10)
	if err != nil {
		t.Fatalf("post TopDomains (sub): %v", err)
	}
	if !sameTopEntries(gotSub, wantSub) {
		t.Errorf("idle-boundary window changed by compaction:\n got=%+v\nwant=%+v", gotSub, wantSub)
	}
}

// TestOpenStoreDoesNotMigrate pins the startup contract: opening the
// store must be cheap and must NOT run the compaction migration, because
// OpenStore sits on the path the proxy waits on before it starts
// accepting connections. The heavy work happens only in Maintain, which
// the daemon runs off the startup path.
func TestOpenStoreDoesNotMigrate(t *testing.T) {
	path := tempDB(t)
	seedDenseDomains(t, path, time.Now().Add(-time.Hour).Truncate(time.Second))

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	before := countDomainRows(t, store)

	// OpenStore must leave the legacy dense rows untouched...
	var migRecorded int
	_ = store.db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE id = ?", migrations[0].id,
	).Scan(&migRecorded)
	if migRecorded != 0 {
		t.Errorf("OpenStore recorded the migration; it should defer to Maintain")
	}

	// ...and Maintain must then compact them.
	if err := store.Maintain(); err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if after := countDomainRows(t, store); after >= before {
		t.Errorf("Maintain did not compact: before=%d after=%d", before, after)
	}
}

// TestMigrationRunsExactlyOnce confirms the migration is recorded and not
// re-applied: reopening the same database leaves the (already compacted)
// row count untouched.
func TestMigrationRunsExactlyOnce(t *testing.T) {
	path := tempDB(t)
	seedDenseDomains(t, path, time.Now().Add(-time.Hour).Truncate(time.Second))

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Maintain(); err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	first := countDomainRows(t, store)
	store.Close()

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Maintain(); err != nil { // must be a no-op the second time
		t.Fatalf("Maintain (reopen): %v", err)
	}
	if again := countDomainRows(t, reopened); again != first {
		t.Errorf("migration re-ran on reopen: %d -> %d rows", first, again)
	}

	var applied int
	if err := reopened.db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE id = ?", migrations[0].id,
	).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Errorf("migration record count = %d, want 1", applied)
	}
}
