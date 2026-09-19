package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotcli/got/internal/dbschema"
)

func methodTestProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := "package routes\n\nfunc root() {\n\t" + routeMarker + "\n}\n"
	if err := os.WriteFile(filepath.Join(project, "routes", "root.go"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestMethodGeneratorScaffoldsMissingFeature(t *testing.T) {
	project := methodTestProject(t)
	err := (MethodGenerator{}).Generate(project, MethodOptions{Folder: "payments", Name: "Capture", HTTPMethod: "POST", Path: "/:id/capture"})
	if err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "handlers", "payments", "payments.go"), "func (h *handler) Capture")
	assertFileContains(t, filepath.Join(project, "services", "payments", "payments.go"), "s.repository.Capture()")
	assertFileContains(t, filepath.Join(project, "repositories", "payments", "payments.go"), "TODO: implement Capture")
	assertFileContains(t, filepath.Join(project, "handlers", "payments", "payments.go"), "response.Send(c, fiber.StatusOK, nil)")
	assertFileContains(t, filepath.Join(project, "routes", "payments.go"), `group.Post("/:id/capture", h.Capture)`)
}

func TestMethodGeneratorExtendsCRUDFeature(t *testing.T) {
	project := methodTestProject(t)
	database := dbschema.Database{Schema: "public", Tables: []dbschema.Table{{Name: "orders", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{{Name: "id", DataType: "integer"}}}}}
	if err := (APIGenerator{}).GenerateCRUD(project, database); err != nil {
		t.Fatal(err)
	}
	options := MethodOptions{Folder: "orders", Name: "Approve", HTTPMethod: "PATCH"}
	if err := (MethodGenerator{}).Generate(project, options); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "handlers", "orders", "interface.go"), "Approve(c *fiber.Ctx) error")
	assertFileContains(t, filepath.Join(project, "services", "orders", "orders.go"), "s.repository.Approve()")
	assertFileContains(t, filepath.Join(project, "routes", "orders.go"), `group.Patch("/approve", h.Approve)`)
	if err := (MethodGenerator{}).Generate(project, options); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestMethodGeneratorExtendsAPIScaffold(t *testing.T) {
	project := methodTestProject(t)
	if err := (APIGenerator{}).Generate(project, "orders"); err != nil {
		t.Fatal(err)
	}
	options := MethodOptions{Folder: "orders", Name: "Approve", HTTPMethod: "PATCH", Path: "/:id/approve"}
	if err := (MethodGenerator{}).Generate(project, options); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "services", "orders", "orders.go"), "repo.")
	assertFileContains(t, filepath.Join(project, "handlers", "orders", "orders.go"), "srv.")
	assertFileContains(t, filepath.Join(project, "routes", "orders.go"), `routeGroup.Patch("/:id/approve", hdr.Approve)`)
}
