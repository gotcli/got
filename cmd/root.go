package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

type config struct {
	cwd string
}

func NewRootCommand(version string) *cobra.Command {
	cfg := &config{}
	root := &cobra.Command{
		Use:   "got",
		Short: "Generate maintainable Go services",
		Long: `GOT generates maintainable Go/Fiber services from consistent templates.

Create one standard API or microservice, or a workspace containing multiple
independently deployable services. GOT can inspect a supported database to
generate CRUD and extend generated projects with methods, JWT, or uploads.
Run a command with --help to see its workflow and examples.`,
		Example: `  # Start an interactive project setup
  got init

  # Create a multi-service workspace
  got init workspace --name platform --module example.com/platform \
    --services order,pos,auth --db pg

  # Create a microservice without interactive project prompts
  got init --name ordering --module example.com/ordering \
    --architecture microservice --db pg

  # Extend a generated project
  cd ordering
  got generate crud
  got add method --folder orders --name Approve --http-method PATCH --path /:id/approve`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg.cwd = cwd
			return nil
		},
	}
	root.AddCommand(newInitCommand(cfg), newAPICommand(cfg), newAddCommand(cfg), newDoctorCommand(cfg, version), newGenerateCommand(cfg), newVersionCommand(version))
	return root
}

func Execute(version string) error {
	return NewRootCommand(version).Execute()
}
