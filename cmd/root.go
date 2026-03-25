package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"go.olrik.dev/subspace/internal/style"
)

// Version is set at build time via ldflags.
var Version = "dev"

func printBanner() {
	name := style.BoldC(style.White, "sub") + style.BoldC(style.Cyan, "space")
	ver := style.Colorize(style.Ghost, Version)
	tagline := style.Colorize(style.Steel, "transparent proxy with upstream routing")
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

// NewRootCommand creates the root command with all subcommands registered.
func NewRootCommand() *cobra.Command {
	var configFile string

	rootCmd := &cobra.Command{
		Use:   "subspace",
		Short: "Subspace - transparent proxy with upstream routing",
		Long: `Subspace is a transparent proxy that supports HTTP, HTTPS, WebSocket, and WSS.
It routes traffic based on hostnames through configurable upstream proxies
(HTTP CONNECT, SOCKS5, etc.) without terminating TLS.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			printBanner()
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	defaultConfig := filepath.Join(ConfigDir(), "config.kdl")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", defaultConfig, "config file path")

	// Register color functions for help templates
	cobra.AddTemplateFunc("cyan", func(s string) string { return style.BoldC(style.Cyan, s) })
	cobra.AddTemplateFunc("green", func(s string) string { return style.Colorize(style.Green, s) })
	cobra.AddTemplateFunc("smoke", func(s string) string { return style.Colorize(style.Smoke, s) })
	cobra.AddTemplateFunc("ghost", func(s string) string { return style.Colorize(style.Ghost, s) })
	cobra.AddTemplateFunc("steel", func(s string) string { return style.Colorize(style.Steel, s) })

	rootCmd.SetHelpTemplate(helpTemplate)
	rootCmd.SetUsageTemplate(usageTemplate)

	rootCmd.AddCommand(newServeCommand(&configFile))
	rootCmd.AddCommand(newLogsCommand(&configFile))
	rootCmd.AddCommand(newStatusCommand(&configFile))
	rootCmd.AddCommand(newResolveCommand(&configFile))

	return rootCmd
}

var helpTemplate = `
{{if .Long}}{{steel .Long}}{{else}}{{steel .Short}}{{end}}

{{cyan "Usage:"}}{{if .Runnable}}
  {{green .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{green (printf "%s [command]" .CommandPath)}}{{end}}
{{if .HasAvailableSubCommands}}
{{cyan "Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{green (rpad .Name .NamePadding)}}  {{smoke .Short}}{{end}}{{end}}{{end}}
{{if .HasAvailableLocalFlags}}
{{cyan "Flags:"}}
{{steel (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}
{{if .HasAvailableInheritedFlags}}
{{cyan "Global Flags:"}}
{{steel (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableSubCommands}}

{{ghost (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`

var usageTemplate = `{{cyan "Usage:"}}{{if .Runnable}}
  {{green .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{green (printf "%s [command]" .CommandPath)}}{{end}}
{{if .HasAvailableSubCommands}}
{{cyan "Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{green (rpad .Name .NamePadding)}}  {{smoke .Short}}{{end}}{{end}}{{end}}
{{if .HasAvailableLocalFlags}}
{{cyan "Flags:"}}
{{steel (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}
{{if .HasAvailableInheritedFlags}}
{{cyan "Global Flags:"}}
{{steel (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableSubCommands}}

{{ghost (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`
