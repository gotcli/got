package generator

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/gotcli/got-community/internal/dbschema"
	"github.com/gotcli/blueprints"
)

type ProjectOptions struct {
	Name         string
	Module       string
	Architecture string
	Database     string
	ParentPath   string
	DBHost       string
	DBPort       uint16
	DBName       string
	DBUser       string
	Schema       *dbschema.Database
}

type CommandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

type ProjectGenerator struct {
	Runner CommandRunner
	Output io.Writer
}

func (g ProjectGenerator) logStep(message string, args ...any) {
	if g.Output == nil {
		return
	}
	fmt.Fprintf(g.Output, "[step] "+message+"\n", args...)
}

var databases = map[string]struct {
	driver     string
	module     string
	dependency string
}{
	"mysql": {driver: "mysql", module: "gorm.io/driver/mysql", dependency: "gorm.io/driver/mysql@v1.5.7"},
	"pg":    {driver: "postgres", module: "gorm.io/driver/postgres", dependency: "gorm.io/driver/postgres@v1.5.11"},
	"mssql": {driver: "sqlserver", module: "gorm.io/driver/sqlserver", dependency: "gorm.io/driver/sqlserver@v1.5.3"},
}

func (g ProjectGenerator) Generate(ctx context.Context, options ProjectOptions) error {
	g.logStep("validating project configuration")
	if err := validateProjectName(options.Name); err != nil {
		return err
	}
	if options.Module == "" {
		options.Module = options.Name
	}
	if options.Architecture == "" {
		options.Architecture = "standard"
	}
	if options.Architecture != "standard" && options.Architecture != "microservice" {
		return fmt.Errorf("unsupported architecture %q (supported: standard, microservice)", options.Architecture)
	}
	database, ok := databases[options.Database]
	if !ok {
		return fmt.Errorf("unsupported database %q (supported: mysql, pg, mssql)", options.Database)
	}
	if options.DBHost == "" {
		options.DBHost = "localhost"
	}
	if options.DBPort == 0 {
		options.DBPort = map[string]uint16{"mysql": 3306, "pg": 5432, "mssql": 1433}[options.Database]
	}
	if options.DBName == "" {
		options.DBName = "app_db"
	}
	if options.DBUser == "" {
		options.DBUser = "app_user"
	}
	projectPath := filepath.Join(options.ParentPath, options.Name)
	if _, err := os.Stat(projectPath); err == nil {
		return fmt.Errorf("destination already exists: %s", projectPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination: %w", err)
	}

	g.logStep("creating project files in %s", projectPath)
	data := templateData{
		ModuleName: options.Module, DBName: database.driver, DBUrl: database.module,
		DBHost: options.DBHost, DBPort: options.DBPort, DatabaseName: options.DBName, DBUser: options.DBUser,
	}
	files := map[string]string{
		".gitignore":                    blueprints.GitIgnore,
		"config.yml":                    blueprints.ConfigYml,
		"configs/viper.go":              blueprints.ConfigViper,
		"pkg/logger/logger.go":          blueprints.StructuredLogger,
		"pkg/response/response.go":      blueprints.HTTPResponse,
		"middlewares/request_logger.go": blueprints.RequestLoggerMiddleware,
		filepath.Join("databases", options.Database+".go"): blueprints.ORMInterface,
		"main.go":        blueprints.MainFile,
		"routes/root.go": blueprints.RouteRoot,
		"routes/v1.go":   blueprints.RouteV1,
	}
	if options.Architecture == "microservice" {
		files["main.go"] = blueprints.MicroserviceMain
		files["routes/health.go"] = blueprints.MicroserviceHealth
		files["Dockerfile"] = blueprints.MicroserviceDockerfile
		files[".dockerignore"] = blueprints.MicroserviceDockerIgnore
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, relativePath := range paths {
		content, err := render(relativePath, files[relativePath], data)
		if err != nil {
			return err
		}
		if err := writeNewFile(filepath.Join(projectPath, relativePath), content); err != nil {
			return err
		}
	}

	runner := g.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	commands := [][]string{
		{"go", "mod", "init", options.Module},
		{"go", "mod", "edit", "-go=1.22", "-toolchain=go1.22.1"},
		{"go", "get", "github.com/gofiber/fiber/v2@v2.52.5", "github.com/spf13/viper@v1.19.0", "gorm.io/gorm@v1.25.12", database.dependency, "github.com/shopspring/decimal@v1.4.0"},
	}
	commandSteps := []string{"initializing Go module", "setting Go toolchain", "installing project dependencies"}
	for index, command := range commands {
		g.logStep(commandSteps[index])
		if err := runner.Run(ctx, projectPath, command[0], command[1:]...); err != nil {
			return fmt.Errorf("run %q: %w", command, err)
		}
	}
	if options.Schema != nil {
		g.logStep("generating CRUD for %d database objects", len(options.Schema.Tables))
		if err := (APIGenerator{}).GenerateCRUD(projectPath, *options.Schema); err != nil {
			return fmt.Errorf("generate database CRUD: %w", err)
		}
	}
	g.logStep("tidying Go module dependencies")
	if err := runner.Run(ctx, projectPath, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("run go mod tidy: %w", err)
	}
	g.logStep("project generation completed")
	return nil
}
