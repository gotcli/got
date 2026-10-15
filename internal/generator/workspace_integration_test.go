package generator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotcli/got/internal/dbschema"
)

// TestWorkspaceGeneratorOutputBuildsWithRealGo runs the real Go toolchain over a
// generated workspace. Unit tests use recordedRunner and cannot catch go.mod
// wiring mistakes, such as a required shared-module version that cannot resolve.
func TestWorkspaceGeneratorOutputBuildsWithRealGo(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs the Go toolchain and downloads modules")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not found in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	parent := t.TempDir()
	generator := WorkspaceGenerator{Runner: ExecRunner{}}
	if err := generator.Generate(ctx, WorkspaceOptions{Name: "platform", Module: "example.com/platform", Database: "pg", ParentPath: parent, Services: []string{"order"}}); err != nil {
		t.Fatalf("generate workspace: %v", err)
	}
	root := filepath.Join(parent, "platform")
	if err := generator.AddService(ctx, root, "payment"); err != nil {
		t.Fatalf("add service: %v", err)
	}

	runGo(t, ctx, root, "", "build", "./libs/...")
	for _, service := range []string{"order-service", "payment-service"} {
		dir := filepath.Join(root, workspaceServicesDir, service)
		// Workspace mode, as used by the generated Makefile and Dockerfile.
		runGo(t, ctx, dir, "", "build", "./...")
		runGo(t, ctx, dir, "", "vet", "./...")
		// go mod tidy and GOT commands such as "got add auth" ignore go.work.
		runGo(t, ctx, dir, "off", "mod", "tidy")
		runGo(t, ctx, dir, "off", "build", "./...")
	}
}

func runGo(t *testing.T, ctx context.Context, dir, gowork string, args ...string) {
	t.Helper()
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK="+gowork)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %v in %s (GOWORK=%q) failed: %v\n%s", args, dir, gowork, err, output)
	}
}

// TestHexagonalOutputBuildsAndTestsWithRealGo compiles a hexagonal project with
// CRUD, Swagger, JWT, and a migration, and runs its generated domain tests.
func TestHexagonalOutputBuildsAndTestsWithRealGo(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test runs the Go toolchain and downloads modules")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not found in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	project := generateHexagonalProject(t, ExecRunner{})
	if err := GenerateHexagonalCRUD(project, dbschema.Database{Schema: "public", Tables: []dbschema.Table{productsTable()}}); err != nil {
		t.Fatal(err)
	}
	if err := (SwaggerGenerator{}).Generate(project); err != nil {
		t.Fatal(err)
	}
	if err := (AuthGenerator{Runner: ExecRunner{}}).Generate(ctx, project, AuthOptions{JWT: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := (MigrationGenerator{}).Generate(project, "add_index"); err != nil {
		t.Fatal(err)
	}
	runGo(t, ctx, project, "off", "mod", "tidy")
	runGo(t, ctx, project, "off", "vet", "./...")
	runGo(t, ctx, project, "off", "test", "./...")
}
