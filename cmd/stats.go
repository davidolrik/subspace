package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/control"
)

func newStatsCommand(configFile *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Inspect and manage the statistics database",
	}
	cmd.AddCommand(newStatsPurgeCommand(configFile))
	return cmd
}

func newStatsPurgeCommand(configFile *string) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "purge <domain>",
		Short: "Remove all historical samples for a domain from the stats database",
		Long: strings.TrimSpace(`
Remove every recorded sample for a specific destination host from the
per-domain stats table. Use this when something landed in stats that
should have been browsed through a private listener.

Only the per-domain table is touched — route, upstream, protocol and
connection rollups are intentionally left intact. The live in-memory
counter for the host is cleared too, so the dashboard stops showing it
immediately.
`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.ParseFile(*configFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			domain := args[0]
			if domain == "" {
				return fmt.Errorf("domain must not be empty")
			}

			client := &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						return net.Dial("unix", cfg.ControlSocket)
					},
				},
			}

			u := "http://subspace/stats/purge?domain=" + url.QueryEscape(domain)
			resp, err := client.Post(u, "", nil)
			if err != nil {
				return fmt.Errorf("connecting to control socket %s: %w\n(is subspace serve running?)", cfg.ControlSocket, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := readBodySnippet(resp)
				return fmt.Errorf("daemon refused purge (%s): %s", resp.Status, body)
			}

			var got control.PurgeResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				return fmt.Errorf("decoding response: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(got)
			}

			fmt.Printf("purged %d sample(s) for %s\n", got.Removed, got.Domain)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "J", false, "output raw JSON")
	return cmd
}

// readBodySnippet reads up to 1 KiB of the response body for inclusion
// in error messages. Used when the control socket returns a non-200
// status so the operator sees the daemon's reason without us swallowing
// it.
func readBodySnippet(resp *http.Response) (string, error) {
	const limit = 1024
	buf := make([]byte, limit)
	n, _ := resp.Body.Read(buf)
	return strings.TrimSpace(string(buf[:n])), nil
}
