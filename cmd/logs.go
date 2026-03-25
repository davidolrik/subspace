package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"golang.org/x/term"
)

func newLogsCommand(configFile *string) *cobra.Command {
	var (
		lines  int
		level  string
		follow bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream log output from a running subspace server",
		Long: `Connects to the control socket of a running subspace server and streams log lines.
Shows the last N lines first, then streams new lines as they arrive.

Levels: debug, info, warn, error`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.ParseFile(*configFile)
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

			followStr := "true"
			if !follow {
				followStr = "false"
			}
			colorStr := "false"
			if term.IsTerminal(int(os.Stdout.Fd())) {
				colorStr = "true"
			}
			url := fmt.Sprintf("http://subspace/logs?n=%d&level=%s&follow=%s&color=%s", lines, level, followStr, colorStr)
			resp, err := client.Get(url)
			if err != nil {
				return fmt.Errorf("connecting to control socket %s: %w\n(is subspace serve running?)", cfg.ControlSocket, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("server returned %d", resp.StatusCode)
			}

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}

			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading logs: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "N", 10, "number of historical log lines to show")
	cmd.Flags().StringVarP(&level, "level", "L", "info", "minimum log level (debug, info, warn, error)")
	cmd.Flags().BoolVarP(&follow, "follow", "F", false, "follow live log output after showing history")

	return cmd
}
