package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gotcli/got-community/internal/dbschema"
	"github.com/gotcli/got-community/internal/generator"
	"github.com/gotcli/got-community/internal/postgres"
	"github.com/gotcli/got-community/internal/sqlserver"
	"github.com/gotcli/got-community/pkg/promtui"
	"github.com/spf13/cobra"
)

type crudConnection struct {
	Driver, Host, Database, User, Password, Schema string
	Port                                           uint16
}

type crudInspector func(context.Context, crudConnection) (dbschema.Database, error)

type passwordPrompter interface {
	AskForPassword(title string, isRequired bool) (string, error)
}

type generateDependencies struct {
	prompt    passwordPrompter
	inspector crudInspector
}

func defaultGenerateDependencies() generateDependencies {
	return generateDependencies{
		prompt: promtui.NewPromptUI(),
		inspector: func(ctx context.Context, input crudConnection) (dbschema.Database, error) {
			switch input.Driver {
			case "pg":
				return postgres.Introspect(ctx, postgres.Config{
					Host: input.Host, Port: input.Port, Database: input.Database,
					User: input.User, Password: input.Password, Schema: input.Schema,
				})
			case "mssql":
				return sqlserver.Introspect(ctx, sqlserver.Config{
					Host: input.Host, Port: input.Port, Database: input.Database,
					User: input.User, Password: input.Password, Schema: input.Schema,
				})
			default:
				return dbschema.Database{}, fmt.Errorf("CRUD generation does not support database driver %q", input.Driver)
			}
		},
	}
}

func newGenerateCommand(cfg *config) *cobra.Command {
	return newGenerateCommandWithDependencies(cfg, defaultGenerateDependencies())
}

func newGenerateCommandWithDependencies(cfg *config, dependencies generateDependencies) *cobra.Command {
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate code inside the current project",
		Long: `Generate code from resources associated with the current GOT project.

Run this command from a generated service directory. Project settings are read
from go.mod and config.yml; command flags can override database connection
values for one run. Sensitive passwords are never accepted as flags or saved.`,
	}
	generate.AddCommand(newGenerateCRUDCommand(cfg, dependencies))
	return generate
}

func newGenerateCRUDCommand(cfg *config, dependencies generateDependencies) *cobra.Command {
	var host, database, user, schema string
	var port uint16
	command := &cobra.Command{
		Use:   "crud",
		Short: "Generate CRUD from the database configured by this project",
		Long: `Read the current project's database configuration and inspect its schema.

GOT generates entity, repository, service, handler, and route layers for each
supported table. PostgreSQL and SQL Server are supported. Inspection is
read-only. The database password is requested through a masked prompt and is
never accepted as a flag, printed, or written to the project.

For a microservice workspace, run this command inside each child service, not
from the workspace root. Each service should generate CRUD only from the
database or schema it owns.`,
		Example: `  # Single service: values come from config.yml
  cd ordering-api
  got generate crud

  # Override selected config values for this run
  got generate crud --db-host localhost --db-schema public

  # Workspace: enter one child service first
  cd commerce-platform/order-service
  got generate crud --db-name commerce --db-schema ordering

  cd ../payment-service
  got generate crud --db-name commerce --db-schema payment`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			connection, err := loadProjectCRUDConnection(cfg.cwd)
			if err != nil {
				return err
			}
			if host != "" {
				connection.Host = strings.TrimSpace(host)
			}
			if port != 0 {
				connection.Port = port
			}
			if database != "" {
				connection.Database = strings.TrimSpace(database)
			}
			if user != "" {
				connection.User = strings.TrimSpace(user)
			}
			if schema != "" {
				connection.Schema = strings.TrimSpace(schema)
			}
			if err := validateCRUDConnection(connection); err != nil {
				return err
			}
			connection.Password, err = dependencies.prompt.AskForPassword("Database password", true)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "[step] connecting to %s at %s:%d and reading schema %s\n", connection.Driver, connection.Host, connection.Port, connection.Schema)
			inspectCtx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			inspected, err := dependencies.inspector(inspectCtx, connection)
			connection.Password = ""
			if err != nil {
				return err
			}
			if len(inspected.Tables) == 0 {
				return fmt.Errorf("schema %q contains no tables", connection.Schema)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "[step] discovered %d database objects\n", len(inspected.Tables))
			if err := (generator.APIGenerator{}).GenerateCRUD(cfg.cwd, inspected); err != nil {
				return fmt.Errorf("generate CRUD: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "generated CRUD for %d database objects\n", len(inspected.Tables))
			return nil
		},
	}
	command.Flags().StringVar(&host, "db-host", "", "override the database host from config.yml")
	command.Flags().Uint16Var(&port, "db-port", 0, "override the database port from config.yml")
	command.Flags().StringVar(&database, "db-name", "", "override the database name from config.yml")
	command.Flags().StringVar(&user, "db-user", "", "override the database username from config.yml")
	command.Flags().StringVar(&schema, "db-schema", "", "database schema (default: public for PostgreSQL, dbo for SQL Server)")
	return command
}

func loadProjectCRUDConnection(projectPath string) (crudConnection, error) {
	module, err := projectModule(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return crudConnection{}, fmt.Errorf("run from a generated service directory: %w", err)
	}
	values, err := readSimpleYAML(filepath.Join(projectPath, "config.yml"))
	if err != nil {
		return crudConnection{}, err
	}
	prefix := generatedEnvPrefix(module, filepath.Base(projectPath))
	driver := normalizeProjectDriver(configValue(values, prefix, "DB_DRIVER"))
	portValue := configValue(values, prefix, "DB_PORT")
	parsedPort, err := strconv.ParseUint(portValue, 10, 16)
	if err != nil {
		return crudConnection{}, fmt.Errorf("invalid DB_PORT %q in project configuration", portValue)
	}
	connection := crudConnection{
		Driver: driver, Host: configValue(values, prefix, "DB_HOST"), Port: uint16(parsedPort),
		Database: configValue(values, prefix, "DB_NAME"), User: configValue(values, prefix, "DB_USERNAME"),
		Schema: configValue(values, prefix, "DB_SCHEMA"),
	}
	if strings.TrimSpace(connection.Schema) == "" {
		switch driver {
		case "pg":
			connection.Schema = "public"
		case "mssql":
			connection.Schema = "dbo"
		}
	}
	return connection, nil
}

func projectModule(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("go.mod has no module directive")
}

func normalizeProjectDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "pg", "postgres", "postgresql":
		return "pg"
	case "mssql", "sqlserver":
		return "mssql"
	case "mysql":
		return "mysql"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

func validateCRUDConnection(connection crudConnection) error {
	if connection.Driver != "pg" && connection.Driver != "mssql" {
		return fmt.Errorf("CRUD generation does not support database driver %q (supported: pg, mssql)", connection.Driver)
	}
	missing := make([]string, 0, 4)
	for name, value := range map[string]string{"DB_HOST": connection.Host, "DB_NAME": connection.Database, "DB_USERNAME": connection.User, "DB_SCHEMA": connection.Schema} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if connection.Port == 0 {
		missing = append(missing, "DB_PORT")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("project database configuration is incomplete: %s", strings.Join(missing, ", "))
	}
	return nil
}
