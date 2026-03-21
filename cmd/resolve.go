package cmd

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/route"
)

var resolveCmd = &cobra.Command{
	Use:   "resolve <url>",
	Short: "Show which route applies to a given URL",
	Long:  `Resolves a URL against the configured routes and shows which upstream proxy (if any) would handle the traffic.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ParseFile(configFile)
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
			rules[i] = route.Rule{Pattern: r.Pattern, Upstream: r.Via}
		}
		matcher := route.NewMatcher(rules)

		matched := matcher.Resolve(hostname)

		lbl := func(s string) string { return style.Colorize(style.Cyan, s) }

		fmt.Println()
		fmt.Printf("%s %s\n", lbl("url      "), style.Colorize(style.Steel, rawURL))
		fmt.Printf("%s %s\n", lbl("hostname "), style.Colorize(style.Green, hostname))

		if matched == nil {
			fmt.Printf("%s %s\n", lbl("route    "), style.Colorize(style.Smoke, "no matching route"))
			fmt.Printf("%s %s\n", lbl("action   "), style.Colorize(style.Green, "direct connection"))
			fmt.Println()
			return nil
		}

		fmt.Printf("%s %s %s %s\n",
			lbl("route    "),
			style.Colorize(style.Steel, matched.Pattern),
			style.Colorize(style.Smoke, "→"),
			style.Colorize(style.UpstreamColor(matched.Upstream), matched.Upstream),
		)

		u, ok := cfg.Upstreams[matched.Upstream]
		if !ok {
			fmt.Printf("%s %s %s\n",
				lbl("upstream "),
				style.Colorize(style.UpstreamColor(matched.Upstream), matched.Upstream),
				style.BoldC(style.Red, "(not found in config!)"),
			)
			fmt.Println()
			return nil
		}

		fmt.Printf("%s %s %s %s\n",
			lbl("upstream "),
			style.Colorize(style.UpstreamColor(matched.Upstream), matched.Upstream),
			style.Colorize(style.Smoke, u.Type),
			style.Colorize(style.Green, u.Address),
		)
		if u.Username != "" {
			fmt.Printf("%s %s\n", lbl("auth     "), style.Colorize(style.Green, u.Username))
		}
		fmt.Println()

		return nil
	},
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

func init() {
	rootCmd.AddCommand(resolveCmd)
}
