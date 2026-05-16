package stats

import (
	"fmt"
	"strings"
	"time"
)

// TopEntry is one row in a Top-N report: a name (upstream / domain /
// route pattern) plus the activity value over the queried window
// under the requested metric.
type TopEntry struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// upstreamMetricExpr maps a metric name to the SQL expression used to
// compute the per-row value before aggregation. Per-upstream counters
// are cumulative (they only ever increase between resets), so we
// derive the activity over a window as MAX(expr) - MIN(expr).
func upstreamMetricExpr(metric string) (string, error) {
	switch metric {
	case "success":
		return "success", nil
	case "failures":
		return "failures", nil
	case "bytes_in":
		return "bytes_in", nil
	case "bytes_out":
		return "bytes_out", nil
	case "bytes_total", "":
		return "(bytes_in + bytes_out)", nil
	default:
		return "", fmt.Errorf("unknown upstream metric %q (want success, failures, bytes_in, bytes_out, or bytes_total)", metric)
	}
}

// TopUpstreams returns the most-active upstreams within [from, to],
// ranked by the given metric ("success", "failures", "bytes_in",
// "bytes_out", or "bytes_total" / ""). At most `limit` entries are
// returned, ordered by descending activity.
//
// Counters are cumulative since process start, so activity within a
// window is computed as MAX(metric) - MIN(metric) per upstream over
// the window's rows. Subspace restarts (which reset counters to zero)
// can cause a small undercount when the window straddles a restart;
// for the dashboard's intended use that's acceptable.
func (s *Store) TopUpstreams(from, to time.Time, metric string, limit int) ([]TopEntry, error) {
	return s.topByName(from, to, metric, limit, "snapshot_upstreams", "upstream", nil)
}

// TopDomains returns the most-active destination hostnames in the
// window, ranked by the given metric. Same shape and semantics as
// TopUpstreams.
func (s *Store) TopDomains(from, to time.Time, metric string, limit int) ([]TopEntry, error) {
	return s.topByName(from, to, metric, limit, "snapshot_domains", "domain", nil)
}

// TopRoutes returns the most-active route patterns in the window,
// ranked by the given metric. Same shape and semantics as
// TopUpstreams.
func (s *Store) TopRoutes(from, to time.Time, metric string, limit int) ([]TopEntry, error) {
	return s.topByName(from, to, metric, limit, "snapshot_routes", "route", nil)
}

// TopRoutesIn behaves like TopRoutes but only considers routes whose
// pattern is in the given set. Used by the dashboard's "Top Blocked"
// card so the numbers come from the same time-windowed differential
// data the regular Top Routes card uses, but filtered to just the
// patterns currently routed at blackhole. Passing an empty list
// returns no rows (rather than degrading to TopRoutes).
func (s *Store) TopRoutesIn(from, to time.Time, metric string, limit int, names []string) ([]TopEntry, error) {
	if len(names) == 0 {
		return nil, nil
	}
	return s.topByName(from, to, metric, limit, "snapshot_routes", "route", names)
}

// preWindowLookback is how far before window-start the SQL pulls
// samples to seed the LAG window function. The recorder's coarsest
// downsample bucket is one hour, so a 2-hour lookback always picks up
// the immediately-prior sample for each name even on the oldest data.
// A daemon that has been silent for longer than this loses the seed
// for that period; its first in-window sample gracefully falls back to
// "growth from zero". The cost of larger lookback is more rows
// scanned per query, so 2h is the smallest value that covers the
// recorder's downsample schedule.
const preWindowLookback = 2 * 60 * 60 // seconds

func (s *Store) topByName(from, to time.Time, metric string, limit int, table, column string, includeOnly []string) ([]TopEntry, error) {
	expr, err := upstreamMetricExpr(metric)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}

	// Windowed delta as the sum of positive consecutive-sample
	// differences per name. The query pulls samples from
	// [from-lookback, to] so LAG sees the immediately-prior
	// pre-window sample naturally; pre-window rows are excluded from
	// the SUM via the `timestamp < from` CASE arm so they only serve
	// to seed LAG. This:
	//
	//   * Counts every in-window monotonic increase, including the
	//     step from the pre-window seed into the first in-window
	//     sample.
	//   * Skips the negative jump at a counter reset (restart), so a
	//     restart inside the window doesn't inflate the row.
	//   * Attributes both pre-restart-in-window growth AND post-
	//     restart growth in the same window — neither over-counted
	//     (the pre-restart peak doesn't propagate beyond the restart
	//     step) nor under-counted (the pre-restart growth still
	//     telescopes through the LAG sum).
	//   * When a name has no pre-window seed within the lookback,
	//     the first in-window sample's prev is NULL and is treated as
	//     growth-from-zero — the right answer for a freshly-started
	//     daemon, where all of the counter's value is recent.
	//
	// The earlier CTE-heavy implementation (separate seeds and
	// seed_vals CTEs JOINing back to the table) was correct but slow
	// (~500ms on a 1M-row table for a 5-min window). This single-CTE
	// form benchmarks at ~80ms for the same window. See
	// idx_snap_X_ts; the lookback range is found by a single index
	// seek.
	fromUnix := from.Unix()
	toUnix := to.Unix()

	filterClause := ""
	var inArgs []any
	if len(includeOnly) > 0 {
		// Parameterised IN clause so route patterns aren't
		// interpolated into the SQL string. SQLite tops out around
		// 999 bound variables per statement — well above any
		// realistic blackhole pattern count.
		placeholders := make([]string, len(includeOnly))
		for i, n := range includeOnly {
			placeholders[i] = "?"
			inArgs = append(inArgs, n)
		}
		filterClause = fmt.Sprintf(" AND %s IN (%s)", column, strings.Join(placeholders, ","))
	}

	args := []any{fromUnix - preWindowLookback, toUnix}
	args = append(args, inArgs...)
	args = append(args, fromUnix, limit)

	query := fmt.Sprintf(`
		WITH samples AS (
		    SELECT %s AS name, timestamp, %s AS v
		    FROM %s
		    WHERE timestamp >= ? AND timestamp <= ?%s
		),
		with_lag AS (
		    SELECT name, timestamp, v,
		           LAG(v) OVER (PARTITION BY name ORDER BY timestamp) AS prev
		    FROM samples
		)
		SELECT name,
		       SUM(
		           CASE
		               WHEN timestamp < ? THEN 0          -- pre-window: seed only, no delta contribution
		               WHEN prev IS NULL  THEN v          -- in-window first row with no seed → growth from zero
		               WHEN v > prev      THEN v - prev   -- monotonic in-window growth
		               ELSE 0                             -- restart (negative jump) or flat
		           END
		       ) AS delta
		FROM with_lag
		GROUP BY name
		HAVING delta > 0
		ORDER BY delta DESC
		LIMIT ?
	`, column, expr, table, filterClause)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying top %s: %w", table, err)
	}
	defer rows.Close()

	var out []TopEntry
	for rows.Next() {
		var name string
		var value int64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		out = append(out, TopEntry{Name: name, Value: value})
	}
	return out, rows.Err()
}
