package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func swaggerTestProject(t *testing.T, compact bool) string {
	t.Helper()
	project := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		fullPath := filepath.Join(project, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/customer-api\n")
	write("config.yml", "APP_ENV: development\nAPP_PORT: 3000\n")
	registration := "routes.RegisterRootRoutes(conn, app)"
	if compact {
		registration = "routes.RegisterRootRoutes(conn,app)"
	}
	write("main.go", "package main\n\nfunc main() {\n\t"+registration+"\n}\n")
	return project
}

func TestSwaggerGeneratorCreatesEmbeddedOpenAPI(t *testing.T) {
	project := swaggerTestProject(t, false)
	if err := (SwaggerGenerator{}).Generate(project); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "main.go"), "routes.RegisterSwaggerRoutes(app)")
	assertFileContains(t, filepath.Join(project, "routes", "swagger.go"), `app.Get("/swagger/index.html"`)
	assertFileContains(t, filepath.Join(project, "routes", "swagger.go"), `APP_ENV`)
	assertFileContains(t, filepath.Join(project, "docs", "openapi.go"), "//go:embed openapi.json")
	assertFileContains(t, filepath.Join(project, "config.yml"), "SWAGGER_ENABLED: true")

	spec, err := os.ReadFile(filepath.Join(project, "docs", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(spec, &document); err != nil {
		t.Fatalf("generated OpenAPI is invalid JSON: %v", err)
	}
	if document["openapi"] != "3.0.3" {
		t.Fatalf("unexpected OpenAPI version: %v", document["openapi"])
	}

	err = (SwaggerGenerator{}).Generate(project)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestSwaggerGeneratorSupportsCompactMicroserviceMain(t *testing.T) {
	project := swaggerTestProject(t, true)
	if err := (SwaggerGenerator{}).Generate(project); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "main.go"), "routes.RegisterSwaggerRoutes(app)")
}

func TestSwaggerGeneratorRejectsUnknownMainLayoutWithoutPartialFiles(t *testing.T) {
	project := swaggerTestProject(t, false)
	if err := os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (SwaggerGenerator{}).Generate(project)
	if err == nil || !strings.Contains(err.Error(), "cannot find root route") {
		t.Fatalf("expected registration error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "docs")); !os.IsNotExist(err) {
		t.Fatalf("generator left partial docs directory: %v", err)
	}
}
