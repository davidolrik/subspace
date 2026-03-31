package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/control"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/route"
)

func newResolveCommand(configFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <url>",
		Short: "Show which route applies to a given URL",
		Long:  `Resolves a URL against the configured routes and shows which upstream proxy (if any) would handle the traffic.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.ParseFile(*configFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			rawURL := args[0]
			hostname, err := extractHostname(rawURL)
			if err != nil {
				return err
			}

			rules := make([]route.Rule, len(cfg.Routes))
			for i, r := range cfg.Routes {
				rules[i] = route.Rule{Pattern: r.Pattern, Upstream: r.Via, Fallback: r.Fallback, File: r.File}
			}
			matcher := route.NewMatcher(rules)

			// Try to fetch live health data from the running server
			health := fetchHealth(cfg.ControlSocket)

			lbl := func(s string) string { return style.Colorize(style.Cyan, s) }

			fmt.Println()
			fmt.Printf("  %s  %s\n", lbl("url     "), style.Colorize(style.Steel, rawURL))
			fmt.Printf("  %s  %s\n", lbl("hostname"), style.Colorize(style.Green, hostname))

			matches := matcher.ResolveAll(hostname)

			if len(matches) == 0 {
				fmt.Println()
				fmt.Printf("  %s  %s\n", lbl("route   "), style.Colorize(style.Smoke, "no matching route"))
				fmt.Printf("  %s  %s\n", lbl("action  "), style.Colorize(style.Green, "direct connection"))
				fmt.Println()
				return nil
			}

			// Print all matching rules
			fmt.Println()
			fmt.Printf("  %s\n", style.BoldC(style.Cyan, "rules"))

			// Calculate column widths for alignment
			var maxPattern, maxUpstream, maxFallback int
			for _, m := range matches {
				if len(m.Pattern) > maxPattern {
					maxPattern = len(m.Pattern)
				}
				if len(m.Upstream) > maxUpstream {
					maxUpstream = len(m.Upstream)
				}
				if len(m.Fallback) > maxFallback {
					maxFallback = len(m.Fallback)
				}
			}

			activeIdx := len(matches) - 1
			for i, m := range matches {
				isActive := i == activeIdx

				pattern := fmt.Sprintf("%-*s", maxPattern, m.Pattern)
				upstream := fmt.Sprintf("%-*s", maxUpstream, m.Upstream)

				var fb string
				if m.Fallback != "" {
					fbText := fmt.Sprintf("%-*s", maxFallback, m.Fallback)
					fb = "  " + style.Colorize(style.Smoke, "fallback=") + style.Colorize(style.UpstreamColor(m.Fallback), fbText)
				} else if maxFallback > 0 {
					fb = "  " + strings.Repeat(" ", maxFallback+len("fallback="))
				}

				file := formatFile(m.File, cfg)

				marker := "  "
				if isActive {
					marker = style.Colorize(style.Green, "→ ")
				}

				fmt.Printf("    %s%s %s %s%s  %s\n",
					marker,
					style.Colorize(style.Steel, pattern),
					style.Colorize(style.Smoke, "→"),
					style.Colorize(style.UpstreamColor(m.Upstream), upstream),
					fb,
					style.Colorize(style.Ghost, file),
				)
			}

			// Determine the effective upstream, considering health and fallback
			active := matches[activeIdx]
			effectiveUpstream := active.Upstream
			fellBack := false

			if health != nil && active.Upstream != "direct" {
				if us, ok := health[active.Upstream]; ok && !us.Healthy {
					if active.Fallback != "" {
						effectiveUpstream = active.Fallback
						fellBack = true
					}
				}
			}

			// Print upstream details
			fmt.Println()

			fallbackNote := ""
			if fellBack {
				fallbackNote = " " + style.Colorize(style.Amber, "(fallback — "+active.Upstream+" is down)")
			}

			if effectiveUpstream == "direct" {
				fmt.Printf("  %s  %s%s\n",
					lbl("upstream"),
					style.Colorize(style.UpstreamColor("direct"), "direct"),
					fallbackNote,
				)
				fmt.Println()
				return nil
			}

			u, ok := cfg.Upstreams[effectiveUpstream]
			if !ok {
				fmt.Printf("  %s  %s %s\n",
					lbl("upstream"),
					style.Colorize(style.UpstreamColor(effectiveUpstream), effectiveUpstream),
					style.BoldC(style.Red, "(not found in config!)"),
				)
				fmt.Println()
				return nil
			}

			fmt.Printf("  %s  %s %s %s%s\n",
				lbl("upstream"),
				style.Colorize(style.UpstreamColor(effectiveUpstream), effectiveUpstream),
				style.Colorize(style.Smoke, u.Type),
				style.Colorize(style.Green, u.Address),
				fallbackNote,
			)
			if u.Username != "" {
				fmt.Printf("  %s  %s\n", lbl("auth    "), style.Colorize(style.Green, u.Username))
			}
			fmt.Println()

			return nil
		},
	}
}

// fetchHealth tries to get upstream health status from the running server.
// Returns nil if the server is not running or unreachable.
func fetchHealth(controlSocket string) map[string]control.UpstreamStatus {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", controlSocket)
			},
		},
	}

	resp, err := client.Get("http://subspace/status")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var status control.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil
	}

	return status.Upstreams
}

// formatFile returns a short display path for a config file, relative to the
// main config's directory when possible.
func formatFile(file string, cfg *config.Config) string {
	if file == "" {
		return ""
	}
	if len(cfg.IncludedFiles) > 0 {
		baseDir := filepath.Dir(cfg.IncludedFiles[0])
		if rel, err := filepath.Rel(baseDir, file); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(file)
}

func extractHostname(rawURL string) (string, error) {
	// Try parsing as a URL first
	u, err := url.Parse(rawURL)
	if err == nil && u.Host != "" {
		return u.Hostname(), nil
	}

	// Maybe it's just a bare hostname or hostname:port
	if u != nil && u.Scheme == "" && u.Host == "" && u.Path != "" {
		// url.Parse("example.com") puts it in Path, not Host
		return u.Path, nil
	}

	return "", fmt.Errorf("cannot extract hostname from %q", rawURL)
}
