package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/control"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/stats"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show health and status of upstream proxies",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ParseFile(configFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", cfg.ControlSocket)
				},
			},
		}

		resp, err := client.Get("http://subspace/status")
		if err != nil {
			return fmt.Errorf("connecting to control socket %s: %w\n(is subspace serve running?)", cfg.ControlSocket, err)
		}
		defer resp.Body.Close()

		var status control.StatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return fmt.Errorf("decoding status: %w", err)
		}

		if statusJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(status)
		}

		printStatusOutput(status)
		return nil
	},
}

func printStatusOutput(status control.StatusResponse) {
	// Upstreams — sort alphabetically with "direct" last
	section("Upstreams")
	names := sortedUpstreamNames(status.Upstreams)

	if len(names) > 0 {
		for _, name := range names {
			us := status.Upstreams[name]

			var badge string
			if name == "direct" {
				badge = style.Badge(style.Steel, style.BgOk, " -- ")
			} else if us.Healthy {
				badge = style.Badge(style.Green, style.BgOk, " OK ")
			} else {
				badge = style.Badge(style.Red, style.BgErr, "FAIL")
			}

			if us.Address != "" {
				detail := fmt.Sprintf("%s, %s, %s", us.Type, us.Address, us.Latency)
				fmt.Printf("  %s %s (%s)\n",
					badge,
					style.Colorize(style.UpstreamColor(name), name),
					style.Colorize(style.Steel, detail),
				)
			} else {
				fmt.Printf("  %s %s\n",
					badge,
					style.Colorize(style.UpstreamColor(name), name),
				)
			}

			var s stats.UpstreamStats
			if us.Stats != nil {
				s = *us.Stats
			}
			fmt.Printf("         %s ok, %s fail, %s in, %s out\n",
				style.Colorize(style.Green, fmt.Sprintf("%d", s.Success)),
				colorFailures(s.Failures),
				style.Colorize(style.Cyan, formatBytes(s.BytesIn)),
				style.Colorize(style.Cyan, formatBytes(s.BytesOut)),
			)
		}
	} else {
		fmt.Printf("  %s\n", style.Colorize(style.Ghost, "(none)"))
	}

	// Connections
	section("Connections")
	kv("total", fmt.Sprintf("%d", status.Connections.Total))
	kv("active", fmt.Sprintf("%d", status.Connections.Active))

	// Pool
	if status.Pool != nil {
		section("Pool")
		kv("hits", fmt.Sprintf("%d", status.Pool.Hits))
		kv("misses", fmt.Sprintf("%d", status.Pool.Misses))
		if len(status.Pool.IdleConns) > 0 {
			for name, count := range status.Pool.IdleConns {
				kv("idle/"+name, fmt.Sprintf("%d", count))
			}
		}
	}

	fmt.Println()
}

func sortedUpstreamNames(m map[string]control.UpstreamStatus) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "direct" {
			return false
		}
		if names[j] == "direct" {
			return true
		}
		return names[i] < names[j]
	})
	return names
}

func section(name string) {
	fmt.Printf("\n%s\n", style.BoldC(style.Cyan, name+":"))
}

func kv(key, val string) {
	fmt.Printf("  %s: %s\n", style.Colorize(style.Steel, key), val)
}

func colorFailures(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n > 0 {
		return style.Colorize(style.Red, s)
	}
	return style.Colorize(style.Ghost, s)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func init() {
	statusCmd.Flags().BoolVarP(&statusJSON, "json", "J", false, "output raw JSON")
	rootCmd.AddCommand(statusCmd)
}
