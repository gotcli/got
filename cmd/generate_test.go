package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotcli/got/internal/dbschema"
)

type passwordPromptStub struct {
	password string
	calls    int
}

func (p *passwordPromptStub) AskForPassword(_ string, _ bool) (string, error) {
	p.calls++
	return p.password, nil
}

type commandRecorder struct{ dirs, commands []string }

func (r *commandRecorder) Run(_ context.Context, dir, name string, args ...string) error {
	r.dirs = append(r.dirs, dir)
	r.commands = append(r.commands, strings.Join(append([]string{name}, args...), " "))
	return nil
}

func generateTestProject(t *testing.T, driver string) string {
	t.Helper()
	project := t.TempDir()
	writeDoctorFile(t, filepath.Join(project, "go.mod"), "module example.com/ordering-api\n")
	writeDoctorFile(t, filepath.Join(project, "config.yml"), "DB_DRIVER: \""+driver+"\"\nDB_HOST: \"db.internal\"\nDB_PORT: 5432\nDB_NAME: \"ordering\"\nDB_USERNAME: \"app_user\"\n")
	writeDoctorFile(t, filepath.Join(project, "routes", "root.go"), "package routes\n\nfunc root() {\n\t"+"// got:api-routes"+"\n}\n")
	return project
}

func TestLoadProjectCRUDConnection(t *testing.T) {
	project := generateTestProject(t, "postgres")
	t.Setenv("ORDERING_API_DB_HOST", "env-db.internal")
	connection, err := loadProjectCRUDConnection(project)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Driver != "pg" || connection.Host != "env-db.internal" || connection.Port != 5432 || connection.Schema != "public" {
		t.Fatalf("unexpected connection: %+v", connection)
	}
}

func TestGenerateCRUDCommandUsesProjectConfigurationAndMaskedPrompt(t *testing.T) {
	project := generateTestProject(t, "postgres")
	prompt := &passwordPromptStub{password: "not-printed-secret"}
	var inspected crudConnection
	runner := &commandRecorder{}
	dependencies := generateDependencies{
		prompt: prompt,
		runner: runner,
		inspector: func(_ context.Context, input crudConnection) (dbschema.Database, error) {
			inspected = input
			return dbschema.Database{Schema: input.Schema, Tables: []dbschema.Table{{
				Name: "orders", PrimaryKey: []string{"id"},
				Columns: []dbschema.Column{{Name: "id", DataType: "integer"}},
			}}}, nil
		},
	}
	command := newGenerateCommandWithDependencies(&config{cwd: project}, dependencies)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"crud", "--db-host", "override.internal"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if prompt.calls != 1 || inspected.Host != "override.internal" || inspected.Password != "not-printed-secret" {
		t.Fatalf("prompt calls=%d inspected=%+v", prompt.calls, inspected)
	}
	if strings.Contains(output.String(), "not-printed-secret") {
		t.Fatal("command output exposed the database password")
	}
	if len(runner.commands) != 1 || runner.commands[0] != "go mod tidy" || runner.dirs[0] != project {
		t.Fatalf("expected go mod tidy in the project, got %v in %v", runner.commands, runner.dirs)
	}
	for _, path := range []string{"entities/orders/orders.go", "repositories/orders/orders.go", "services/orders/orders.go", "handlers/orders/orders.go", "routes/orders.go"} {
		content, err := os.ReadFile(filepath.Join(project, path))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "package ") {
			t.Fatalf("%s is not a Go source file", path)
		}
	}
}

func TestGenerateCRUDRejectsUnsupportedProjectBeforePasswordPrompt(t *testing.T) {
	project := generateTestProject(t, "mysql")
	prompt := &passwordPromptStub{password: "unused"}
	command := newGenerateCommandWithDependencies(&config{cwd: project}, generateDependencies{
		prompt: prompt,
		inspector: func(context.Context, crudConnection) (dbschema.Database, error) {
			t.Fatal("inspector must not be called")
			return dbschema.Database{}, nil
		},
	})
	command.SetArgs([]string{"crud"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("expected unsupported driver error, got %v", err)
	}
	if prompt.calls != 0 {
		t.Fatalf("password prompt called %d time(s)", prompt.calls)
	}
}

func twoTableInspector(_ context.Context, input crudConnection) (dbschema.Database, error) {
	return dbschema.Database{Schema: input.Schema, Tables: []dbschema.Table{
		{Name: "orders", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{{Name: "id", DataType: "integer"}}},
		{Name: "Invoices", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{{Name: "id", DataType: "integer"}}},
	}}, nil
}

func TestGenerateCRUDDryRunWritesNothing(t *testing.T) {
	project := generateTestProject(t, "postgres")
	runner := &commandRecorder{}
	command := newGenerateCommandWithDependencies(&config{cwd: project}, generateDependencies{
		prompt: &passwordPromptStub{password: "secret"}, inspector: twoTableInspector, runner: runner,
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"crud", "--dry-run", "--tables", "invoices"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"would generate CRUD for 1 tables", "schemas/invoices/invoices.go", "routes/root.go (register routes)"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("dry-run output missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "orders") {
		t.Errorf("dry-run included an unselected table:\n%s", output.String())
	}
	if _, err := os.Stat(filepath.Join(project, "entities")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote files: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("dry run ran commands: %v", runner.commands)
	}
}

func TestGenerateCRUDTablesSelectsAndRejectsUnknown(t *testing.T) {
	project := generateTestProject(t, "postgres")
	newCommand := func(args ...string) error {
		command := newGenerateCommandWithDependencies(&config{cwd: project}, generateDependencies{
			prompt: &passwordPromptStub{password: "secret"}, inspector: twoTableInspector,
		})
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		command.SetArgs(append([]string{"crud"}, args...))
		return command.Execute()
	}
	err := newCommand("--tables", "orders,payments")
	if err == nil || !strings.Contains(err.Error(), "tables not found in schema \"public\": payments (available: Invoices, orders)") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := newCommand("--tables", "orders"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "entities", "invoices")); !os.IsNotExist(err) {
		t.Fatalf("unselected table was generated: %v", err)
	}
	// A second full run collides with orders and points the developer to --tables.
	err = newCommand()
	if err == nil || !strings.Contains(err.Error(), "use --tables") {
		t.Fatalf("expected --tables hint, got %v", err)
	}
	if err := newCommand("--tables", "invoices"); err != nil {
		t.Fatalf("adding a new table after an earlier run failed: %v", err)
	}
}
