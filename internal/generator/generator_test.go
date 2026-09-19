package generator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotcli/got-community/internal/dbschema"
)

type recordedRunner struct{ calls [][]string }

func (r *recordedRunner) Run(_ context.Context, _ string, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

func TestProjectGeneratorCreatesProject(t *testing.T) {
	runner := &recordedRunner{}
	var output bytes.Buffer
	parent := t.TempDir()
	err := (ProjectGenerator{Runner: runner, Output: &output}).Generate(context.Background(), ProjectOptions{
		Name: "orders", Module: "example.com/orders", Database: "pg", ParentPath: parent,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"main.go", "config.yml", "configs/viper.go", "databases/pg.go", "routes/root.go", "pkg/response/response.go", "pkg/logger/logger.go"} {
		if _, err := os.Stat(filepath.Join(parent, "orders", path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	if len(runner.calls) != 4 {
		t.Fatalf("expected 4 Go commands, got %d", len(runner.calls))
	}
	for _, step := range []string{"validating project configuration", "creating project files", "installing project dependencies", "project generation completed"} {
		if !strings.Contains(output.String(), step) {
			t.Errorf("step log does not contain %q:\n%s", step, output.String())
		}
	}
	assertFileContains(t, filepath.Join(parent, "orders", "pkg", "logger", "logger.go"), `filepath.Join(directory, "application.log")`)
	assertFileContains(t, filepath.Join(parent, "orders", "pkg", "logger", "logger.go"), `"event", "database"`)
	assertFileContains(t, filepath.Join(parent, "orders", "middlewares", "request_logger.go"), `"event", "http.inbound"`)
	assertFileContains(t, filepath.Join(parent, "orders", "databases", "pg.go"), `logger.NewGORMLogger(viper.GetDuration("DB_SLOW_QUERY_THRESHOLD"))`)
}

func TestProjectGeneratorRejectsExistingDestination(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "orders"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := (ProjectGenerator{Runner: &recordedRunner{}}).Generate(context.Background(), ProjectOptions{
		Name: "orders", Module: "example.com/orders", Database: "pg", ParentPath: parent,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected destination error, got %v", err)
	}
}

func TestProjectGeneratorCreatesMicroserviceAssets(t *testing.T) {
	parent := t.TempDir()
	err := (ProjectGenerator{Runner: &recordedRunner{}}).Generate(context.Background(), ProjectOptions{
		Name: "catalog", Module: "example.com/catalog", Architecture: "microservice", Database: "pg", ParentPath: parent,
	})
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(parent, "catalog")
	for _, path := range []string{"Dockerfile", ".dockerignore", "routes/health.go"} {
		if _, err := os.Stat(filepath.Join(project, path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	assertFileContains(t, filepath.Join(project, "main.go"), "signal.NotifyContext")
	assertFileContains(t, filepath.Join(project, "routes", "health.go"), `app.Get("/health/ready"`)
}

func TestProjectGeneratorRejectsUnknownArchitecture(t *testing.T) {
	err := (ProjectGenerator{Runner: &recordedRunner{}}).Generate(context.Background(), ProjectOptions{
		Name: "orders", Architecture: "monolith", Database: "pg", ParentPath: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported architecture") {
		t.Fatalf("expected architecture error, got %v", err)
	}
}

func TestAPIGeneratorCreatesSliceAndRegistersRoute(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := "package routes\n\nfunc register() {\n\t" + routeMarker + "\n}\n"
	if err := os.WriteFile(filepath.Join(project, "routes", "root.go"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (APIGenerator{}).Generate(project, "order_item"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(project, "routes", "root.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "RegisterOrderItemRoutes(db, routeGroup)") {
		t.Fatalf("route was not registered:\n%s", content)
	}
	if _, err := os.Stat(filepath.Join(project, "services", "order_item", "order_item.go")); err != nil {
		t.Fatal(err)
	}
}

func TestAPIGeneratorValidatesName(t *testing.T) {
	err := (APIGenerator{}).Generate(t.TempDir(), "Order-Item")
	if err == nil || !strings.Contains(err.Error(), "invalid API name") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestAPIGeneratorDoesNotWriteWhenRouteMarkerIsMissing(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "routes", "root.go"), []byte("package routes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (APIGenerator{}).Generate(project, "users")
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("expected marker error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "entities")); !os.IsNotExist(err) {
		t.Fatalf("generator wrote files before validation: %v", err)
	}
}

func TestAPIGeneratorGeneratesWorkingCRUDLayers(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := "package routes\n\nfunc register() {\n\t" + routeMarker + "\n}\n"
	if err := os.WriteFile(filepath.Join(project, "routes", "root.go"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	database := dbschema.Database{Schema: "public", Tables: []dbschema.Table{{
		Name: "order_items", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{
			{Name: "id", DataType: "integer", UDTName: "int4", HasDefault: true},
			{Name: "order_id", DataType: "integer", UDTName: "int4"},
			{Name: "price", DataType: "numeric", UDTName: "numeric"},
			{Name: "note", DataType: "text", UDTName: "text", Nullable: true},
		},
	}}}
	if err := (APIGenerator{}).GenerateCRUD(project, database); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "repositories", "order_items", "order_items.go"), "func (r *repository) Create")
	assertFileContains(t, filepath.Join(project, "services", "order_items", "order_items.go"), "return s.repository.Update")
	assertFileContains(t, filepath.Join(project, "handlers", "order_items", "order_items.go"), "func (h *handler) Delete")
	assertFileContains(t, filepath.Join(project, "entities", "order_items", "order_items.go"), "Price   decimal.Decimal")
	assertFileContains(t, filepath.Join(project, "entities", "order_items", "order_items.go"), "ID      int")
	content, err := os.ReadFile(filepath.Join(project, "entities", "order_items", "order_items.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), `gorm:"column:id;primaryKey;default"`) {
		t.Fatal("generated entity contains an invalid GORM default tag")
	}
	assertFileContains(t, filepath.Join(project, "routes", "order_items.go"), "group.Get(\"/:id\", h.Get)")
	assertFileContains(t, filepath.Join(project, "handlers", "order_items", "order_items.go"), "response.Send(c, fiber.StatusOK, items)")
	assertFileContains(t, filepath.Join(project, "services", "order_items", "order_items.go"), `logger.ServiceResult(ctx, "order_items", "list", started, items, err)`)
}

func TestCRUDPreflightPreventsPartialGeneration(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := "package routes\n\nfunc register() {\n\t" + routeMarker + "\n}\n"
	if err := os.WriteFile(filepath.Join(project, "routes", "root.go"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "routes", "betas.go"), []byte("package routes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	database := dbschema.Database{Schema: "public", Tables: []dbschema.Table{
		{Name: "alphas", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{{Name: "id", DataType: "integer"}}},
		{Name: "betas", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{{Name: "id", DataType: "integer"}}},
	}}
	err := (APIGenerator{}).GenerateCRUD(project, database)
	if err == nil || !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("expected preflight collision, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "entities", "alphas")); !os.IsNotExist(err) {
		t.Fatalf("generator wrote the first table before detecting a later collision: %v", err)
	}
}

func assertFileContains(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s does not contain %q:\n%s", path, expected, content)
	}
}

func TestSQLServerNamesAndTypes(t *testing.T) {
	if got := toSnakeCase("OrderItems"); got != "order_items" {
		t.Fatalf("toSnakeCase(OrderItems) = %q", got)
	}
	if got := goFieldName("UserID"); got != "UserID" {
		t.Fatalf("goFieldName(UserID) = %q", got)
	}
	tests := []struct {
		dataType string
		want     string
	}{
		{"int", "int"}, {"bigint", "int64"}, {"bit", "bool"},
		{"decimal", "decimal.Decimal"}, {"datetime2", "time.Time"}, {"uniqueidentifier", "string"},
	}
	for _, test := range tests {
		got, _ := columnGoType(dbschema.Column{DataType: test.dataType})
		if got != test.want {
			t.Errorf("columnGoType(%s) = %s, want %s", test.dataType, got, test.want)
		}
	}
}
