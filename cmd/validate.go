package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/internal/style"
	"go.olrik.dev/subspace/pages"
)

// newValidateCommand returns the `subspace validate` subcommand.
//
// validate runs the same parsing pipeline as `serve`, but stops short
// of starting any listeners or watchers — it only reports on the
// configuration. It is intended for CI on a config repo: zero exit
// status iff the config parses cleanly, has no collected non-fatal
// errors, no broken page files, and no page references to undefined
// tags.
func newValidateCommand(configFile *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Parse the config and report errors without starting the server",
		Long: `Validate runs the same parsing pipeline as the serve command — main
config, included files, page KDL files, and tag references — and
reports any errors it finds. It exits with a non-zero status when
anything is wrong, so it's safe to wire into CI for a config repo.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			cfg, err := config.ParseFile(*configFile)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", *configFile, err)
			}

			// Mirror the parts of serve.go that surface non-fatal
			// errors so the CI signal matches what the running server
			// would log on startup.
			pageInfos := loadPages(cfg)
			h := pages.New(pageInfos, nil, nil)
			h.SetTags(tagDefs(cfg))
			cfg.Errors = append(cfg.Errors, h.ValidateTagReferences()...)

			fmt.Fprintln(out)
			fmt.Fprintf(out, "  %s   %s\n", style.Colorize(style.Cyan, "config       "), style.Colorize(style.Steel, *configFile))
			summarise(out, "files included", len(cfg.IncludedFiles))
			summarise(out, "upstreams     ", len(cfg.Upstreams))
			summarise(out, "routes        ", len(cfg.Routes))
			summarise(out, "pages         ", len(pageInfos))
			summarise(out, "tags          ", len(cfg.Tags))
			summarise(out, "search engines", len(cfg.SearchEngines))

			if len(cfg.Errors) == 0 {
				fmt.Fprintln(out)
				fmt.Fprintf(out, "  %s\n", style.BoldC(style.Green, "OK"))
				fmt.Fprintln(out)
				return nil
			}

			fmt.Fprintln(out)
			fmt.Fprintf(out, "  %s\n", style.BoldC(style.Red, fmt.Sprintf("%d error(s):", len(cfg.Errors))))
			for _, e := range cfg.Errors {
				fmt.Fprintf(out, "    %s %s\n", style.Colorize(style.Red, "•"), style.Colorize(style.Steel, e))
			}
			fmt.Fprintln(out)

			return fmt.Errorf("config validation failed: %d error(s)", len(cfg.Errors))
		},
	}
}

func summarise(w interface{ Write([]byte) (int, error) }, label string, n int) {
	fmt.Fprintf(w, "  %s   %s\n",
		style.Colorize(style.Cyan, label),
		style.Colorize(style.Green, fmt.Sprintf("%d", n)),
	)
}
