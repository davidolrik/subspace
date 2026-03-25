package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Skip the banner
		},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
}
