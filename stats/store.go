package stats

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists time-series statistics snapshots to a SQLite database.
type Store struct {
	db *sql.DB
}

// TimeSeries holds a sequence of data points returned by a query.
type TimeSeries struct {
	Points []DataPoint `json:"points"`
}

// DataPoint is a single time-series entry with all metrics at that instant.
type DataPoint struct {
	Timestamp   time.Time                  `json:"timestamp"`
	Connections int64                      `json:"connections"`
	Active      int64                      `json:"active"`
	Protocols   map[string]int64           `json:"protocols"`
	Errors      map[string]int64           `json:"errors"`
	Upstreams   map[string]UpstreamStats   `json:"upstreams"`
}

const schema = `
CREATE TABLE IF NOT EXISTS snapshots (
	timestamp INTEGER NOT NULL,
	connections INTEGER NOT NULL,
	active INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snapshots_ts ON snapshots(timestamp);

CREATE TABLE IF NOT EXISTS snapshot_protocols (
	timestamp INTEGER NOT NULL,
	protocol TEXT NOT NULL,
	count INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snap_proto_ts ON snapshot_protocols(timestamp);

CREATE TABLE IF NOT EXISTS snapshot_errors (
	timestamp INTEGER NOT NULL,
	error_type TEXT NOT NULL,
	count INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snap_err_ts ON snapshot_errors(timestamp);

CREATE TABLE IF NOT EXISTS snapshot_upstreams (
	timestamp INTEGER NOT NULL,
	upstream TEXT NOT NULL,
	success INTEGER NOT NULL,
	failures INTEGER NOT NULL,
	bytes_in INTEGER NOT NULL,
	bytes_out INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snap_up_ts ON snapshot_upstreams(timestamp);
`

// OpenStore opens or creates a SQLite database at the given path.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening stats database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating stats schema: %w", err)
	}

	// WAL mode for better concurrent read/write performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Record persists a snapshot at the given timestamp.
func (s *Store) Record(ts time.Time, snap Snapshot) error {
	unix := ts.Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"INSERT INTO snapshots (timestamp, connections, active) VALUES (?, ?, ?)",
		unix, snap.Connections, snap.Active,
	); err != nil {
		return err
	}

	for proto, count := range snap.Protocols {
		if _, err := tx.Exec(
			"INSERT INTO snapshot_protocols (timestamp, protocol, count) VALUES (?, ?, ?)",
			unix, proto, count,
		); err != nil {
			return err
		}
	}

	for errType, count := range snap.Errors {
		if _, err := tx.Exec(
			"INSERT INTO snapshot_errors (timestamp, error_type, count) VALUES (?, ?, ?)",
			unix, errType, count,
		); err != nil {
			return err
		}
	}

	for name, us := range snap.Upstreams {
		if _, err := tx.Exec(
			"INSERT INTO snapshot_upstreams (timestamp, upstream, success, failures, bytes_in, bytes_out) VALUES (?, ?, ?, ?, ?, ?)",
			unix, name, us.Success, us.Failures, us.BytesIn, us.BytesOut,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Query returns all data points within the given time range, ordered by timestamp.
func (s *Store) Query(from, to time.Time) (*TimeSeries, error) {
	fromUnix := from.Unix()
	toUnix := to.Unix()

	// Fetch snapshot rows
	rows, err := s.db.Query(
		"SELECT timestamp, connections, active FROM snapshots WHERE timestamp >= ? AND timestamp <= ? ORDER BY timestamp",
		fromUnix, toUnix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var timestamps []int64
	pointsByTS := make(map[int64]*DataPoint)

	for rows.Next() {
		var ts, conn, active int64
		if err := rows.Scan(&ts, &conn, &active); err != nil {
			return nil, err
		}
		p := &DataPoint{
			Timestamp:   time.Unix(ts, 0),
			Connections: conn,
			Active:      active,
			Protocols:   make(map[string]int64),
			Errors:      make(map[string]int64),
			Upstreams:   make(map[string]UpstreamStats),
		}
		pointsByTS[ts] = p
		timestamps = append(timestamps, ts)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(timestamps) == 0 {
		return &TimeSeries{}, nil
	}

	// Fetch protocols
	protoRows, err := s.db.Query(
		"SELECT timestamp, protocol, count FROM snapshot_protocols WHERE timestamp >= ? AND timestamp <= ?",
		fromUnix, toUnix,
	)
	if err != nil {
		return nil, err
	}
	defer protoRows.Close()

	for protoRows.Next() {
		var ts, count int64
		var proto string
		if err := protoRows.Scan(&ts, &proto, &count); err != nil {
			return nil, err
		}
		if p, ok := pointsByTS[ts]; ok {
			p.Protocols[proto] = count
		}
	}

	// Fetch errors
	errRows, err := s.db.Query(
		"SELECT timestamp, error_type, count FROM snapshot_errors WHERE timestamp >= ? AND timestamp <= ?",
		fromUnix, toUnix,
	)
	if err != nil {
		return nil, err
	}
	defer errRows.Close()

	for errRows.Next() {
		var ts, count int64
		var errType string
		if err := errRows.Scan(&ts, &errType, &count); err != nil {
			return nil, err
		}
		if p, ok := pointsByTS[ts]; ok {
			p.Errors[errType] = count
		}
	}

	// Fetch upstreams
	upRows, err := s.db.Query(
		"SELECT timestamp, upstream, success, failures, bytes_in, bytes_out FROM snapshot_upstreams WHERE timestamp >= ? AND timestamp <= ?",
		fromUnix, toUnix,
	)
	if err != nil {
		return nil, err
	}
	defer upRows.Close()

	for upRows.Next() {
		var ts int64
		var name string
		var us UpstreamStats
		if err := upRows.Scan(&ts, &name, &us.Success, &us.Failures, &us.BytesIn, &us.BytesOut); err != nil {
			return nil, err
		}
		if p, ok := pointsByTS[ts]; ok {
			p.Upstreams[name] = us
		}
	}

	// Build ordered result
	points := make([]DataPoint, len(timestamps))
	for i, ts := range timestamps {
		points[i] = *pointsByTS[ts]
	}

	return &TimeSeries{Points: points}, nil
}

// Downsample aggregates fine-grained data older than `olderThan` into
// buckets of the given duration. Within each bucket, cumulative counters
// (connections, success, bytes) keep the max value and gauges (active)
// keep the average.
func (s *Store) Downsample(olderThan time.Duration, bucket time.Duration) error {
	cutoff := time.Now().Add(-olderThan).Unix()
	bucketSec := int64(bucket.Seconds())

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// For each table: create a temp table with aggregated data, delete
	// old rows, insert aggregated rows back.

	// --- snapshots ---
	if _, err := tx.Exec(`
		CREATE TEMP TABLE tmp_snap AS
		SELECT (timestamp / ?) * ? AS ts, MAX(connections) AS connections, CAST(AVG(active) AS INTEGER) AS active
		FROM snapshots WHERE timestamp < ?
		GROUP BY ts
	`, bucketSec, bucketSec, cutoff); err != nil {
		return fmt.Errorf("aggregate snapshots: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM snapshots WHERE timestamp < ?", cutoff); err != nil {
		return fmt.Errorf("delete old snapshots: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO snapshots SELECT * FROM tmp_snap"); err != nil {
		return fmt.Errorf("insert aggregated snapshots: %w", err)
	}
	tx.Exec("DROP TABLE tmp_snap")

	// --- protocols ---
	if _, err := tx.Exec(`
		CREATE TEMP TABLE tmp_proto AS
		SELECT (timestamp / ?) * ? AS ts, protocol, MAX(count) AS count
		FROM snapshot_protocols WHERE timestamp < ?
		GROUP BY ts, protocol
	`, bucketSec, bucketSec, cutoff); err != nil {
		return fmt.Errorf("aggregate protocols: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM snapshot_protocols WHERE timestamp < ?", cutoff); err != nil {
		return fmt.Errorf("delete old protocols: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO snapshot_protocols SELECT * FROM tmp_proto"); err != nil {
		return fmt.Errorf("insert aggregated protocols: %w", err)
	}
	tx.Exec("DROP TABLE tmp_proto")

	// --- errors ---
	if _, err := tx.Exec(`
		CREATE TEMP TABLE tmp_err AS
		SELECT (timestamp / ?) * ? AS ts, error_type, MAX(count) AS count
		FROM snapshot_errors WHERE timestamp < ?
		GROUP BY ts, error_type
	`, bucketSec, bucketSec, cutoff); err != nil {
		return fmt.Errorf("aggregate errors: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM snapshot_errors WHERE timestamp < ?", cutoff); err != nil {
		return fmt.Errorf("delete old errors: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO snapshot_errors SELECT * FROM tmp_err"); err != nil {
		return fmt.Errorf("insert aggregated errors: %w", err)
	}
	tx.Exec("DROP TABLE tmp_err")

	// --- upstreams ---
	if _, err := tx.Exec(`
		CREATE TEMP TABLE tmp_up AS
		SELECT (timestamp / ?) * ? AS ts, upstream, MAX(success) AS success, MAX(failures) AS failures,
		       MAX(bytes_in) AS bytes_in, MAX(bytes_out) AS bytes_out
		FROM snapshot_upstreams WHERE timestamp < ?
		GROUP BY ts, upstream
	`, bucketSec, bucketSec, cutoff); err != nil {
		return fmt.Errorf("aggregate upstreams: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM snapshot_upstreams WHERE timestamp < ?", cutoff); err != nil {
		return fmt.Errorf("delete old upstreams: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO snapshot_upstreams SELECT * FROM tmp_up"); err != nil {
		return fmt.Errorf("insert aggregated upstreams: %w", err)
	}
	tx.Exec("DROP TABLE tmp_up")

	return tx.Commit()
}
