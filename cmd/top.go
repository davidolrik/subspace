package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/stats"
)

// newTopCommand returns the `subspace top` parent. Three kinds are
// supported: upstreams, domains, routes. Each reads from the
// persistent stats database and ranks entries by an activity metric
// over a time window.
func newTopCommand(configFile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top <kind>",
		Short: "Show the most-active upstreams, routes, or domains over a window",
		Long: `Top renders a ranked summary of activity from the persistent
statistics database, without needing the proxy to be running.

Available kinds: upstreams, domains, routes.`,
	}
	cmd.AddCommand(
		newTopKindCommand(configFile, "upstreams", "Rank upstreams by activity over a window"),
		newTopKindCommand(configFile, "domains", "Rank destination hostnames by activity over a window"),
		newTopKindCommand(configFile, "routes", "Rank route patterns by activity over a window"),
	)
	return cmd
}

// newTopKindCommand creates a `subspace top <kind>` subcommand.
// Each subcommand shares the same flag set and renderer; only the
// kind name and the underlying Store query method differ.
func newTopKindCommand(configFile *string, kind, short string) *cobra.Command {
	var (
		windowStr  string
		metric     string
		limit      int
		jsonOutput bool
	)

	c := &cobra.Command{
		Use:   kind,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			window, err := time.ParseDuration(windowStr)
			if err != nil {
				return fmt.Errorf("invalid window %q: %w", windowStr, err)
			}
			if window <= 0 {
				return fmt.Errorf("window must be positive, got %v", window)
			}

			if _, err := config.ParseFile(*configFile); err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dbPath := filepath.Join(filepath.Dir(*configFile), "stats.db")
			store, err := stats.OpenStore(dbPath)
			if err != nil {
				return fmt.Errorf("opening stats database %s: %w", dbPath, err)
			}
			defer store.Close()

			to := time.Now()
			from := to.Add(-window)
			top, err := topByKind(store, kind, from, to, metric, limit)
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"kind":   kind,
					"metric": metric,
					"window": window.String(),
					"limit":  limit,
					"top":    top,
				})
			}

			printTopReport(kind, top, metric, window)
			return nil
		},
	}

	c.Flags().StringVarP(&windowStr, "window", "w", "24h", "duration window (e.g. 1h, 24h, 168h)")
	c.Flags().StringVarP(&metric, "metric", "m", "bytes_total", "metric to rank by: success, failures, bytes_in, bytes_out, bytes_total")
	c.Flags().IntVarP(&limit, "limit", "n", 10, "maximum number of entries to return")
	c.Flags().BoolVarP(&jsonOutput, "json", "J", false, "output raw JSON")

	return c
}

func topByKind(store *stats.Store, kind string, from, to time.Time, metric string, limit int) ([]stats.TopEntry, error) {
	switch kind {
	case "upstreams":
		return store.TopUpstreams(from, to, metric, limit)
	case "domains":
		return store.TopDomains(from, to, metric, limit)
	case "routes":
		return store.TopRoutes(from, to, metric, limit)
	default:
		return nil, fmt.Errorf("unknown top kind %q", kind)
	}
}

func printTopReport(kind string, top []stats.TopEntry, metric string, window time.Duration) {
	header := fmt.Sprintf("Top %d %s by %s over %s", len(top), kind, metric, window)
	fmt.Println()
	fmt.Printf("  %s\n", style.BoldC(style.Heading, header))
	fmt.Println()

	if len(top) == 0 {
		fmt.Printf("  %s\n\n", style.Colorize(style.Faint, "(no activity recorded in this window)"))
		return
	}

	// Calculate column width for alignment.
	maxName := 0
	for _, e := range top {
		if n := len(e.Name); n > maxName {
			maxName = n
		}
	}

	for i, e := range top {
		rank := fmt.Sprintf("%2d.", i+1)
		name := fmt.Sprintf("%-*s", maxName, e.Name)
		value := formatMetricValue(metric, e.Value)
		nameColor := style.Body
		if kind == "upstreams" {
			nameColor = style.UpstreamColor(e.Name)
		}
		fmt.Printf("  %s  %s  %s\n",
			style.Colorize(style.Faint, rank),
			style.Colorize(nameColor, name),
			style.Colorize(style.Success, value),
		)
	}
	fmt.Println()
}

func formatMetricValue(metric string, v int64) string {
	if strings.HasPrefix(metric, "bytes") {
		return formatBytes(v)
	}
	return fmt.Sprintf("%d", v)
}
