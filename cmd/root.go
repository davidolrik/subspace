package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/config"
	"go.olrik.dev/subspace/internal/style"
)

// Version is set at build time via ldflags.
var Version = "dev"

func printBanner() {
	name := style.BoldC(style.Strong, "sub") + style.BoldC(style.Heading, "space")
	ver := style.Colorize(style.Faint, Version)
	tagline := style.Colorize(style.Body, "transparent proxy with upstream routing")
	fmt.Fprintf(os.Stderr, "%s %s - %s\n", name, ver, tagline)
}

// Execute runs the root command.
func Execute() {
	// Subspace manages its own proxy routing via config, so system proxy
	// env vars must not influence any HTTP clients within the process.
	clearProxyEnv()

	if err := NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

// clearProxyEnv removes proxy-related environment variables so that
// no part of subspace is influenced by the system's proxy settings.
func clearProxyEnv() {
	for _, key := range []string{
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"NO_PROXY", "no_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		os.Unsetenv(key)
	}
}

// ConfigDir returns the default subspace config directory.
// Respects $XDG_CONFIG_HOME, falling back to ~/.config/subspace.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "subspace")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "subspace")
}

// activePaletteName is the resolved theme name applied by applyTheme.
// `theme export` reads this so its --from flag can default to whatever
// the user is currently running. Empty means "dark".
var activePaletteName string

// NewRootCommand creates the root command with all subcommands registered.
func NewRootCommand() *cobra.Command {
	var configFile string
	var themeFlag string

	rootCmd := &cobra.Command{
		Use:   "subspace",
		Short: "Subspace - transparent proxy with upstream routing",
		Long: `Subspace is a transparent proxy that supports HTTP, HTTPS, WebSocket, and WSS.
It routes traffic based on hostnames through configurable upstream proxies
(HTTP CONNECT, SOCKS5, etc.) without terminating TLS.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			warns := applyTheme(configFile, themeFlag)
			printBanner()
			for _, w := range warns {
				fmt.Fprintln(os.Stderr, style.Colorize(style.Muted, "theme: "+w))
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	defaultConfig := filepath.Join(ConfigDir(), "config.kdl")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", defaultConfig, "config file path")
	rootCmd.PersistentFlags().StringVar(&themeFlag, "theme", "", "color theme override (built-ins: dark, light; or a custom theme name from "+filepath.Join(ConfigDir(), "themes")+"/<name>.kdl)")

	// Register color functions for help templates. Names match the
	// semantic palette roles in internal/style.
	cobra.AddTemplateFunc("heading", func(s string) string { return style.BoldC(style.Heading, s) })
	cobra.AddTemplateFunc("success", func(s string) string { return style.Colorize(style.Success, s) })
	cobra.AddTemplateFunc("muted", func(s string) string { return style.Colorize(style.Muted, s) })
	cobra.AddTemplateFunc("faint", func(s string) string { return style.Colorize(style.Faint, s) })
	cobra.AddTemplateFunc("body", func(s string) string { return style.Colorize(style.Body, s) })

	rootCmd.SetHelpTemplate(helpTemplate)
	rootCmd.SetUsageTemplate(usageTemplate)

	rootCmd.AddCommand(newServeCommand(&configFile))
	rootCmd.AddCommand(newLogsCommand(&configFile))
	rootCmd.AddCommand(newStatusCommand(&configFile))
	rootCmd.AddCommand(newResolveCommand(&configFile))
	rootCmd.AddCommand(newValidateCommand(&configFile))
	rootCmd.AddCommand(newTopCommand(&configFile))
	rootCmd.AddCommand(newSchemaCommand())
	rootCmd.AddCommand(newThemeCommand())
	rootCmd.AddCommand(newVersionCommand())

	return rootCmd
}

// applyTheme resolves the active theme name (precedence: override flag
// → config-file `theme` key → "dark"), applies it to the style package,
// and records the resolved name in activePaletteName. It returns
// human-readable warnings for any non-fatal problems encountered while
// resolving — these are printed under the banner. A missing or broken
// config file silently falls through to dark; the full config parse
// (later, inside subcommands) is the place real config errors surface.
func applyTheme(configFile, override string) []string {
	name := override
	if name == "" {
		name = themeFromConfigFile(configFile)
	}
	palette, warns := style.ResolveTheme(name, ConfigDir())
	style.ApplyPalette(palette)
	activePaletteName = canonicalThemeName(name)
	return warns
}

// themeFromConfigFile reads just the theme key from a config file using
// a tolerant parser. Returns "" if the file can't be opened or parsed —
// in that case the caller falls back to the dark default.
func themeFromConfigFile(path string) string {
	cfg, err := config.ParseFile(path)
	if err != nil {
		return ""
	}
	return cfg.Theme
}

// canonicalThemeName normalizes the resolved theme name. Empty resolves
// to "dark" so theme export can pick a sensible default --from.
func canonicalThemeName(name string) string {
	if name == "" {
		return "dark"
	}
	return name
}

var helpTemplate = `
{{if .Long}}{{body .Long}}{{else}}{{body .Short}}{{end}}

{{heading "Usage:"}}{{if .Runnable}}
  {{success .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{success (printf "%s [command]" .CommandPath)}}{{end}}
{{if .HasAvailableSubCommands}}
{{heading "Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{success (rpad .Name .NamePadding)}}  {{muted .Short}}{{end}}{{end}}{{end}}
{{if .HasAvailableLocalFlags}}
{{heading "Flags:"}}
{{body (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}
{{if .HasAvailableInheritedFlags}}
{{heading "Global Flags:"}}
{{body (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableSubCommands}}

{{faint (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`

var usageTemplate = `{{heading "Usage:"}}{{if .Runnable}}
  {{success .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{success (printf "%s [command]" .CommandPath)}}{{end}}
{{if .HasAvailableSubCommands}}
{{heading "Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{success (rpad .Name .NamePadding)}}  {{muted .Short}}{{end}}{{end}}{{end}}
{{if .HasAvailableLocalFlags}}
{{heading "Flags:"}}
{{body (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}
{{if .HasAvailableInheritedFlags}}
{{heading "Global Flags:"}}
{{body (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableSubCommands}}

{{faint (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`
