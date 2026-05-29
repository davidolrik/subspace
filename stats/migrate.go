package stats

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// keyframeBucketSeconds is the compaction migration's coarse stand-in for
// the runtime keyframe cadence: when thinning historical per-name rows it
// keeps at least one row per name per this many seconds, so even a long-
// idle key retains a seed sample within preWindowLookback (see topn.go).
const keyframeBucketSeconds = int64(keyframeInterval / time.Second)

// migration is a one-time change applied to a database exactly once and
// recorded in schema_migrations. Migrations run in slice order.
type migration struct {
	id  string
	run func(*sql.DB) error
}

var migrations = []migration{
	{id: "0001_compact_sparse_per_name_rows", run: compactSparsePerNameRows},
}

// applyMigrations runs every migration not yet recorded in
// schema_migrations, in order, recording each only after it succeeds so a
// migration runs exactly once per database and a failure leaves it
// pending for the next start rather than silently skipped.
func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		id TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	for _, m := range migrations {
		var applied int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE id = ?", m.id).Scan(&applied); err != nil {
			return fmt.Errorf("checking migration %s: %w", m.id, err)
		}
		if applied > 0 {
			continue
		}
		if err := m.run(db); err != nil {
			return fmt.Errorf("migration %s: %w", m.id, err)
		}
		if _, err := db.Exec("INSERT INTO schema_migrations (id, applied_at) VALUES (?, ?)", m.id, time.Now().Unix()); err != nil {
			return fmt.Errorf("recording migration %s: %w", m.id, err)
		}
	}
	return nil
}

// compactSparsePerNameRows retroactively applies the sparse-write scheme
// (see Record) to existing per-domain and per-route history. Databases
// written by older builds re-stored every live counter on every snapshot,
// so an idle domain accrued a row every few seconds forever — in the
// field this grew snapshot_domains into millions of rows and a multi-
// hundred-MB file that re-spiked CPU on every snapshot write.
//
// The migration deletes any row whose counters equal the immediately
// preceding row for the same name, except the first row in each keyframe-
// sized bucket. That keeps every change point plus one seed per bucket —
// exactly what the windowed-delta top-N query (topByName) needs — so the
// result of TopDomains/TopRoutes is unchanged for every window while the
// dominant source of bloat collapses. VACUUM then returns the freed pages
// to the filesystem; without it SQLite keeps the file at its peak size.
func compactSparsePerNameRows(db *sql.DB) error {
	var deleted int64
	for _, tbl := range []struct{ table, col string }{
		{"snapshot_domains", "domain"},
		{"snapshot_routes", "route"},
	} {
		// A row is redundant when it equals the previous sample for the
		// same name (zero delta, so invisible to the top-N query) AND it
		// is not the first sample in its keyframe bucket (which is kept as
		// the seed). LAG over a NULL previous (the first row of a name)
		// compares unequal, so a name's earliest row is always retained.
		query := fmt.Sprintf(`
			DELETE FROM %[1]s
			WHERE rowid IN (
				SELECT rowid FROM (
					SELECT rowid,
						success, failures, bytes_in, bytes_out,
						LAG(success)   OVER (PARTITION BY %[2]s ORDER BY timestamp) AS p_success,
						LAG(failures)  OVER (PARTITION BY %[2]s ORDER BY timestamp) AS p_failures,
						LAG(bytes_in)  OVER (PARTITION BY %[2]s ORDER BY timestamp) AS p_bytes_in,
						LAG(bytes_out) OVER (PARTITION BY %[2]s ORDER BY timestamp) AS p_bytes_out,
						ROW_NUMBER() OVER (
							PARTITION BY %[2]s, timestamp / %[3]d
							ORDER BY timestamp
						) AS bucket_rn
					FROM %[1]s
				)
				WHERE bucket_rn > 1
				  AND success   = p_success
				  AND failures  = p_failures
				  AND bytes_in  = p_bytes_in
				  AND bytes_out = p_bytes_out
			)`, tbl.table, tbl.col, keyframeBucketSeconds)

		res, err := db.Exec(query)
		if err != nil {
			return fmt.Errorf("compacting %s: %w", tbl.table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("compacting %s: %w", tbl.table, err)
		}
		deleted += n
	}

	if deleted > 0 {
		slog.Info("compacting stats database: removed redundant per-name rows, reclaiming disk", "rows_removed", deleted)
		// VACUUM cannot run inside a transaction and rewrites the whole
		// file, so on a large legacy database this can pause startup for a
		// few seconds — a one-time cost the size reclaim is worth.
		if _, err := db.Exec("VACUUM"); err != nil {
			return fmt.Errorf("vacuum after compaction: %w", err)
		}
	}
	return nil
}
