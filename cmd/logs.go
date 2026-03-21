package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
)

var (
	logsLines  int
	logsLevel  string
	logsFollow bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream log output from a running subspace server",
	Long: `Connects to the control socket of a running subspace server and streams log lines.
Shows the last N lines first, then streams new lines as they arrive.

Levels: debug, info, warn, error`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {},
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

		followStr := "true"
		if !logsFollow {
			followStr = "false"
		}
		url := fmt.Sprintf("http://subspace/logs?n=%d&level=%s&follow=%s", logsLines, logsLevel, followStr)
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

func init() {
	logsCmd.Flags().IntVarP(&logsLines, "lines", "N", 10, "number of historical log lines to show")
	logsCmd.Flags().StringVarP(&logsLevel, "level", "L", "info", "minimum log level (debug, info, warn, error)")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "F", false, "follow live log output after showing history")
	rootCmd.AddCommand(logsCmd)
}
