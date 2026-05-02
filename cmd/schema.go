package cmd

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed subspace.kdl-schema
var kdlSchemaContent []byte

// newSchemaCommand returns `subspace schema`, which prints the
// embedded KDL schema describing the configuration file format.
//
// Editors with kdl-schema support can use the schema for completion
// and validation. Even editors without schema support get a useful
// machine-readable reference for every block, property, and child.
func newSchemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the KDL schema describing the subspace config file",
		Long: `Schema prints the embedded KDL schema describing every node,
property, and child the subspace config file accepts. The output is
intended to be redirected into a schema file your editor can use for
completion and validation, e.g.:

    subspace schema > ~/.config/subspace/subspace.kdl-schema

Most KDL editor extensions look for a kdl-schema directive at the top
of the document; add a comment like:

    // kdl-schema "./subspace.kdl-schema"

at the top of your config.kdl to wire it up.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), string(kdlSchemaContent))
			return err
		},
	}
}
