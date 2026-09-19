package cmd

import (
	"fmt"

	"github.com/gotcli/got/internal/generator"
	"github.com/spf13/cobra"
)

func newAPICommand(cfg *config) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "api",
		Short: "Scaffold a new API feature in the current project",
		Long: `Scaffold entity, schema, repository, service, handler, and route files in
the current generated project. The API name must be lowercase snake_case.

Existing files are never overwritten. Repository and service operations in
this command are intentional TODO scaffolds; use "got generate crud" when
working implementations should be generated from a database schema.`,
		Example: `  cd my_service
  got api --name users
  got api --name order_items`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := (generator.APIGenerator{}).Generate(cfg.cwd, name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "generated API %q\n", name)
			return nil
		},
	}
	command.Flags().StringVarP(&name, "name", "n", "", "feature name in lowercase snake_case, for example order_items")
	_ = command.MarkFlagRequired("name")
	return command
}
