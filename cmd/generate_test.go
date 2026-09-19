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
	dependencies := generateDependencies{
		prompt: prompt,
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
