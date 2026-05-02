package stats

import (
	"fmt"
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
	return s.topByName(from, to, metric, limit, "snapshot_upstreams", "upstream")
}

// TopDomains returns the most-active destination hostnames in the
// window, ranked by the given metric. Same shape and semantics as
// TopUpstreams.
func (s *Store) TopDomains(from, to time.Time, metric string, limit int) ([]TopEntry, error) {
	return s.topByName(from, to, metric, limit, "snapshot_domains", "domain")
}

// TopRoutes returns the most-active route patterns in the window,
// ranked by the given metric. Same shape and semantics as
// TopUpstreams.
func (s *Store) TopRoutes(from, to time.Time, metric string, limit int) ([]TopEntry, error) {
	return s.topByName(from, to, metric, limit, "snapshot_routes", "route")
}

func (s *Store) topByName(from, to time.Time, metric string, limit int, table, column string) ([]TopEntry, error) {
	expr, err := upstreamMetricExpr(metric)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}

	query := fmt.Sprintf(`
		SELECT %s, MAX(%s) - MIN(%s) AS delta
		FROM %s
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY %s
		HAVING delta > 0
		ORDER BY delta DESC
		LIMIT ?
	`, column, expr, expr, table, column)

	rows, err := s.db.Query(query, from.Unix(), to.Unix(), limit)
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
