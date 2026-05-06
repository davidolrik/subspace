package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/internal/style"
)

func newThemeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Manage CLI color themes",
		Long: `Inspect and manage the CLI color theme.

The active theme is selected by the 'theme' key in your config file or
the --theme global flag. Built-in themes are "dark" (default) and
"light". Any other name resolves to a custom theme file at
<configdir>/themes/<name>.kdl.`,
	}
	cmd.AddCommand(newThemeExportCommand())
	return cmd
}

func newThemeExportCommand() *cobra.Command {
	var fromFlag string
	var force bool

	cmd := &cobra.Command{
		Use:   "export [--from dark|light] [--force] <name>",
		Short: "Write a starter theme file to the config directory",
		Long: `Write a full palette KDL file to <configdir>/themes/<name>.kdl as a
starting point for a custom theme. Edit the file and reference it from
your config with 'theme "<name>"'.

The --from flag selects the base palette. When omitted, the currently
active palette is used.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			base := fromFlag
			if base == "" {
				base = activePaletteName
				if base == "" {
					base = "dark"
				}
			}
			var palette style.Palette
			switch base {
			case "dark":
				palette = style.DarkPalette()
			case "light":
				palette = style.LightPalette()
			default:
				return fmt.Errorf("--from must be dark or light, got %q", base)
			}

			path := filepath.Join(ConfigDir(), "themes", name+".kdl")
			if err := style.WriteThemeFile(path, palette, force); err != nil {
				return err
			}

			out := cmd.ErrOrStderr()
			fmt.Fprintln(out, style.Colorize(style.Success, "wrote ")+style.Colorize(style.Body, path))
			fmt.Fprintln(out, style.Colorize(style.Muted, fmt.Sprintf("add 'theme %q' to your config to use it", name)))
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFlag, "from", "", "base palette to copy from (dark or light); defaults to the active theme")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")
	return cmd
}
