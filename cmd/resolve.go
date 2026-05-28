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

			lbl := func(s string) string { return style.Colorize(style.Heading, s) }

			fmt.Println()
			fmt.Printf("  %s  %s\n", lbl("url     "), style.Colorize(style.Body, rawURL))
			fmt.Printf("  %s  %s\n", lbl("hostname"), style.Colorize(style.Success, hostname))

			matches := matcher.ResolveAll(hostname)

			if len(matches) == 0 {
				fmt.Println()
				fmt.Printf("  %s  %s\n", lbl("route   "), style.Colorize(style.Muted, "no matching route"))
				fmt.Printf("  %s  %s\n", lbl("action  "), style.Colorize(style.Success, "direct connection"))
				fmt.Println()
				return nil
			}

			// Print all matching rules
			fmt.Println()
			fmt.Printf("  %s\n", style.BoldC(style.Heading, "rules"))

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
					fb = "  " + style.Colorize(style.Muted, "fallback=") + style.Colorize(style.UpstreamColor(m.Fallback), fbText)
				} else if maxFallback > 0 {
					fb = "  " + strings.Repeat(" ", maxFallback+len("fallback="))
				}

				file := includeChain(m.File, cfg)

				marker := "  "
				if isActive {
					marker = style.Colorize(style.Success, "→ ")
				}

				fmt.Printf("    %s%s %s %s%s  %s\n",
					marker,
					style.Colorize(style.Body, pattern),
					style.Colorize(style.Muted, "→"),
					style.Colorize(style.UpstreamColor(m.Upstream), upstream),
					fb,
					style.Colorize(style.Faint, file),
				)
			}

			// Determine the effective upstream, considering health and fallback
			active := matches[activeIdx]
			effectiveUpstream := active.Upstream
			fellBack := false

			if health != nil && !isPseudoUpstream(active.Upstream) {
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
				fallbackNote = " " + style.Colorize(style.Warning, "(fallback — "+active.Upstream+" is down)")
			}

			if isPseudoUpstream(effectiveUpstream) {
				note := ""
				if effectiveUpstream == "blackhole" {
					note = " " + style.Colorize(style.Muted, "(traffic dropped — HTTP 451 / SOCKS5 0x02)")
				}
				fmt.Printf("  %s  %s%s%s\n",
					lbl("upstream"),
					style.Colorize(style.UpstreamColor(effectiveUpstream), effectiveUpstream),
					fallbackNote,
					note,
				)
				fmt.Println()
				return nil
			}

			u, ok := cfg.Upstreams[effectiveUpstream]
			if !ok {
				fmt.Printf("  %s  %s %s\n",
					lbl("upstream"),
					style.Colorize(style.UpstreamColor(effectiveUpstream), effectiveUpstream),
					style.BoldC(style.Error, "(not found in config!)"),
				)
				fmt.Println()
				return nil
			}

			fmt.Printf("  %s  %s %s %s%s\n",
				lbl("upstream"),
				style.Colorize(style.UpstreamColor(effectiveUpstream), effectiveUpstream),
				style.Colorize(style.Muted, u.Type),
				style.Colorize(style.Success, u.Address),
				fallbackNote,
			)
			if u.Username != "" {
				fmt.Printf("  %s  %s\n", lbl("auth    "), style.Colorize(style.Success, u.Username))
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

// includeChain renders the include path leading to file, with each
// segment formatted relative to the main config's directory and joined
// with " › ". The root config is the common ancestor of every included
// file, so it's dropped from the chain — except for a rule that lives
// in the root itself, where it's the only segment and is kept. Returns
// "" for an empty file, matching formatFile.
func includeChain(file string, cfg *config.Config) string {
	if file == "" {
		return ""
	}
	// Walk parents from the rule's file up to the root. The seen guard
	// is belt-and-suspenders: circular includes are already rejected at
	// parse time, so the chain is always finite.
	var chain []string
	seen := make(map[string]bool)
	for f := file; f != "" && !seen[f]; f = cfg.IncludedBy[f] {
		seen[f] = true
		chain = append(chain, formatFile(f, cfg))
	}
	// chain is leaf→root. Drop the root (last element) unless it's the
	// only one, then reverse so the remaining path renders root-first.
	if len(chain) > 1 {
		chain = chain[:len(chain)-1]
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return strings.Join(chain, " › ")
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
		// url.Parse("example.com") puts the whole input in Path, not
		// Host — including any trailing slash or path ("host/a/b").
		// Keep only the host segment so the slash doesn't leak into
		// route matching.
		host := u.Path
		if i := strings.IndexByte(host, '/'); i >= 0 {
			host = host[:i]
		}
		return host, nil
	}

	// Bare "host:port" with no scheme parses as scheme:opaque (e.g.
	// "host:8080" → scheme="host", opaque="8080"), so neither branch
	// above matches. Re-parse with a "//" authority prefix to force the
	// host:port to be recognised as the URL authority, then drop the port.
	if u2, err := url.Parse("//" + rawURL); err == nil && u2.Host != "" {
		return u2.Hostname(), nil
	}

	return "", fmt.Errorf("cannot extract hostname from %q", rawURL)
}
