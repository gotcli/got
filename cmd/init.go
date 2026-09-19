package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/gotcli/got/internal/dbschema"
	"github.com/gotcli/got/internal/generator"
	"github.com/gotcli/got/internal/postgres"
	"github.com/gotcli/got/internal/sqlserver"
	"github.com/gotcli/got/pkg/promtui"
	"github.com/spf13/cobra"
)

func newInitCommand(cfg *config) *cobra.Command {
	command := newServiceInitCommand(cfg)
	command.Use = "init"
	command.AddCommand(newServiceInitSubcommand(cfg), newWorkspaceInitCommand(cfg))
	return command
}

func newServiceInitSubcommand(cfg *config) *cobra.Command {
	command := newServiceInitCommand(cfg)
	command.Use = "service"
	command.Short = "Create one independently deployable service"
	command.Long = "Create one independently deployable service.\n\n" + command.Long
	return command
}

func newServiceInitCommand(cfg *config) *cobra.Command {
	var projectName, moduleName, database, architecture string
	var generateCRUD bool
	var dbHost, dbName, dbUser, dbSchema string
	var dbPort uint16
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a standard API or microservice project",
		Long: `Create a new Go/Fiber project in a child directory of the current path.

When project, module, architecture, or database flags are omitted, GOT asks for
them interactively. Standard mode generates the layered API foundation.
Microservice mode additionally generates health endpoints, graceful shutdown,
HTTP timeouts, Dockerfile, and .dockerignore.

Use --generate-crud to inspect an existing PostgreSQL or SQL Server schema and
generate entity, repository, service, handler, and route layers. Missing host,
database name, and username values are prompted. The database password is
always read from a masked prompt; it is never accepted as a flag or saved.
For a clearer two-step workflow, initialize first and run "got generate crud"
from the generated service directory. The combined flag remains supported for
backward compatibility.`,
		Example: `  # Fully interactive setup
  got init

  # Standard PostgreSQL API
  got init --name catalog --module example.com/catalog \
    --architecture standard --db pg

  # Microservice with CRUD generated from PostgreSQL
  got init --name ordering --module example.com/ordering \
    --architecture microservice --db pg --generate-crud \
    --db-host localhost --db-port 5432 --db-name ordering \
    --db-user postgres --db-schema public

  # SQL Server uses port 1433 and schema dbo by default
  got init --name billing --module example.com/billing \
    --architecture microservice --db mssql --generate-crud \
    --db-host sql.internal --db-name billing --db-user app_user`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ui := promtui.NewPromptUI()
			stepOutput := cmd.ErrOrStderr()
			var err error
			if architecture == "" {
				architecture, err = ui.AskForSelect("Choose project architecture", []huh.Option[string]{
					huh.NewOption("Standard API", "standard"),
					huh.NewOption("Microservice", "microservice"),
				}, true)
				if err != nil {
					return err
				}
			}
			if projectName == "" {
				projectName, err = ui.AskForInput("What is your project name?", true)
				if err != nil {
					return err
				}
			}
			if moduleName == "" {
				defaultModule := fmt.Sprintf("github.com/yourname/%s", projectName)
				moduleName, err = ui.AskForSelect("Choose your module name", []huh.Option[string]{
					huh.NewOption(defaultModule, defaultModule),
					huh.NewOption(projectName, projectName),
					huh.NewOption("Custom name", "custom"),
				}, true)
				if err != nil {
					return err
				}
				if moduleName == "custom" {
					moduleName, err = ui.AskForInput("Name your module", true)
					if err != nil {
						return err
					}
				}
			}
			if database == "" {
				database, err = ui.AskForSelect("Choose a database", []huh.Option[string]{
					huh.NewOption("PostgreSQL", "pg"),
					huh.NewOption("MySQL", "mysql"),
					huh.NewOption("SQL Server", "mssql"),
				}, true)
				if err != nil {
					return err
				}
			}
			if dbPort == 0 {
				switch database {
				case "mssql":
					dbPort = 1433
				case "mysql":
					dbPort = 3306
				default:
					dbPort = 5432
				}
			}
			if dbSchema == "" {
				if database == "mssql" {
					dbSchema = "dbo"
				} else {
					dbSchema = "public"
				}
			}

			var schema *dbschema.Database
			if generateCRUD {
				if database != "pg" && database != "mssql" {
					return fmt.Errorf("--generate-crud does not support database %q", database)
				}
				dbHost, err = askDatabaseHost(ui, dbHost)
				if err != nil {
					return err
				}
				if dbName == "" {
					dbName, err = ui.AskForInput("Database name", true)
					if err != nil {
						return err
					}
				}
				if dbUser == "" {
					dbUser, err = ui.AskForInput("Database user", true)
					if err != nil {
						return err
					}
				}
				fmt.Fprintf(stepOutput, "[step] connecting to %s and reading metadata\n", database)
				password, err := ui.AskForPassword("Database password", true)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
				defer cancel()
				var inspected dbschema.Database
				switch database {
				case "pg":
					inspected, err = postgres.Introspect(ctx, postgres.Config{
						Host: dbHost, Port: dbPort, Database: dbName, User: dbUser, Password: password, Schema: dbSchema,
					})
				case "mssql":
					inspected, err = sqlserver.Introspect(ctx, sqlserver.Config{
						Host: dbHost, Port: dbPort, Database: dbName, User: dbUser, Password: password, Schema: dbSchema,
					})
				}
				password = ""
				if err != nil {
					return err
				}
				if len(inspected.Tables) == 0 {
					return fmt.Errorf("schema %q contains no tables", dbSchema)
				}
				fmt.Fprintf(stepOutput, "[step] discovered %d database objects\n", len(inspected.Tables))
				schema = &inspected
			}

			options := generator.ProjectOptions{
				Name: projectName, Module: moduleName, Architecture: architecture, Database: database, ParentPath: cfg.cwd,
				DBHost: dbHost, DBPort: dbPort, DBName: dbName, DBUser: dbUser, Schema: schema,
			}
			if err := (generator.ProjectGenerator{Output: stepOutput}).Generate(cmd.Context(), options); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "project %q is ready\n", projectName)
			if generateCRUD {
				fmt.Fprintf(cmd.OutOrStdout(), "set %s_DB_PASSWORD before starting the generated service\n", generatedEnvPrefix(moduleName, projectName))
			}
			return nil
		},
	}
	command.Flags().StringVarP(&projectName, "name", "n", "", "destination directory name using lowercase letters, hyphens, or underscores")
	command.Flags().StringVarP(&moduleName, "module", "m", "", "Go module import path, for example example.com/ordering")
	command.Flags().StringVarP(&database, "db", "d", "", "database driver: pg, mysql, or mssql")
	command.Flags().StringVarP(&architecture, "architecture", "a", "", "project mode: standard or microservice")
	command.Flags().BoolVar(&generateCRUD, "generate-crud", false, "inspect a PostgreSQL or SQL Server schema and generate executable CRUD")
	command.Flags().StringVar(&dbHost, "db-host", "", "database host (prompted when --generate-crud is enabled)")
	command.Flags().Uint16Var(&dbPort, "db-port", 0, "database port (default: 5432 pg, 1433 mssql, 3306 mysql)")
	command.Flags().StringVar(&dbName, "db-name", "", "database name (prompted during CRUD generation when omitted)")
	command.Flags().StringVar(&dbUser, "db-user", "", "database username (prompted during CRUD generation when omitted)")
	command.Flags().StringVar(&dbSchema, "db-schema", "", "database schema (default: public for pg, dbo for mssql)")
	return command
}

func newWorkspaceInitCommand(cfg *config) *cobra.Command {
	var name, module, database, serviceList string
	command := &cobra.Command{
		Use: "workspace", Short: "Create a workspace containing multiple microservices",
		Long: `Create a multi-module microservice workspace. Each service receives its own
go.mod, configuration, health checks, Dockerfile, and deployment lifecycle.
The workspace root contains go.work, compose.yml, Makefile, README, and a GOT
manifest used by "got add service". Services never share entities or database
repositories through the generated workspace.`,
		Example: `  got init workspace --name restaurant-platform \
    --module example.com/restaurant-platform \
    --services order,pos,auth --db pg`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ui := promtui.NewPromptUI()
			var err error
			if name == "" {
				name, err = ui.AskForInput("Workspace name", true)
				if err != nil {
					return err
				}
			}
			if module == "" {
				module, err = ui.AskForInput("Workspace module prefix", true)
				if err != nil {
					return err
				}
			}
			if serviceList == "" {
				serviceList, err = ui.AskForInput("Services (comma separated, for example order,pos,auth)", true)
				if err != nil {
					return err
				}
			}
			if database == "" {
				database, err = ui.AskForSelect("Choose a database", []huh.Option[string]{huh.NewOption("PostgreSQL", "pg"), huh.NewOption("MySQL", "mysql"), huh.NewOption("SQL Server", "mssql")}, true)
				if err != nil {
					return err
				}
			}
			options := generator.WorkspaceOptions{Name: name, Module: module, Database: database, ParentPath: cfg.cwd, Services: strings.Split(serviceList, ",")}
			if err := (generator.WorkspaceGenerator{Output: cmd.ErrOrStderr()}).Generate(cmd.Context(), options); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "workspace %q is ready with services: %s\n", name, serviceList)
			return nil
		},
	}
	command.Flags().StringVarP(&name, "name", "n", "", "workspace directory name")
	command.Flags().StringVarP(&module, "module", "m", "", "module prefix for child services")
	command.Flags().StringVar(&serviceList, "services", "", "comma-separated services, for example order,pos,auth")
	command.Flags().StringVarP(&database, "db", "d", "", "database driver for generated services: pg, mysql, or mssql")
	return command
}

type inputPrompter interface {
	AskForInput(title string, isRequired bool) (string, error)
}

func askDatabaseHost(ui inputPrompter, host string) (string, error) {
	if strings.TrimSpace(host) != "" {
		return host, nil
	}
	host, err := ui.AskForInput("Database host (for example localhost)", true)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(host), nil
}

func generatedEnvPrefix(moduleName, projectName string) string {
	value := moduleName
	if value == "" {
		value = projectName
	}
	parts := strings.Split(value, "/")
	value = parts[len(parts)-1]
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(value))
}
