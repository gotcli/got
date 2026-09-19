package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the got version",
		Long:    "Print the version embedded in the installed GOT binary.",
		Example: `  got version`,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}
