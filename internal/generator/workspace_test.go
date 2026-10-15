package generator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, string, ...string) error {
	return errors.New("runner failed")
}

func TestWorkspaceGeneratorCreatesIndependentServices(t *testing.T) {
	parent := t.TempDir()
	runner := &recordedRunner{}
	err := (WorkspaceGenerator{Runner: runner}).Generate(context.Background(), WorkspaceOptions{Name: "restaurant-platform", Module: "example.com/restaurant", Database: "pg", ParentPath: parent, Services: []string{"order", "pos", "auth"}})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "restaurant-platform")
	for _, path := range []string{"go.work", "deploy/compose.yml", "Makefile", "README.md", workspaceManifestName, "services/order-service/go.mod", "services/pos-service/Dockerfile", "services/auth-service/routes/health.go", "libs/go.mod", "libs/logger/logger.go", "libs/middleware/middleware.go", "libs/observability", ".github/workflows"} {
		if strings.HasSuffix(path, "go.mod") {
			continue
		} // go.mod is produced by the injected command runner in integration tests.
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	assertFileContains(t, filepath.Join(root, "go.work"), "./services/order-service")
	assertFileContains(t, filepath.Join(root, "go.work"), "./libs")
	assertFileContains(t, filepath.Join(root, "go.work"), "go 1.25.0")
	assertFileContains(t, filepath.Join(root, "go.work"), "toolchain go1.25.0")
	assertFileContains(t, filepath.Join(root, "deploy", "compose.yml"), "dockerfile: services/order-service/Dockerfile")
	assertFileContains(t, filepath.Join(root, "deploy", "compose.yml"), `"3001:3000"`)
	assertFileContains(t, filepath.Join(root, "deploy", "compose.yml"), `"3003:3000"`)
	assertFileContains(t, filepath.Join(root, "services", "auth-service", "main.go"), `example.com/restaurant/libs/middleware`)
	assertFileContains(t, filepath.Join(root, "services", "auth-service", "main.go"), "middlewares.Register(app, loggers)")
	if _, err := os.Stat(filepath.Join(root, "services", "auth-service", "middlewares", "request_logger.go")); !os.IsNotExist(err) {
		t.Fatalf("service-local request logger should be removed: %v", err)
	}
	if len(runner.calls) != 19 {
		t.Fatalf("expected shared tidy and six Go commands for each service, got %d", len(runner.calls))
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "-dropreplace") {
			t.Fatalf("in-repository libs must keep its replace directive, got %q", call)
		}
	}
	assertFileContains(t, filepath.Join(root, "services", "auth-service", "Dockerfile"), "FROM golang:1.25.0-alpine")
}

func TestWorkspaceGeneratorUsesExternalSharedModuleWithoutPersistentReplace(t *testing.T) {
	parent := t.TempDir()
	runner := &recordedRunner{}
	err := (WorkspaceGenerator{Runner: runner}).Generate(context.Background(), WorkspaceOptions{
		Name: "platform", Module: "gitlab.example.com/team/platform", Database: "pg", ParentPath: parent,
		SharedModule: "gitlab.example.com/team/platform-shared", SharedVersion: "v1.2.0", Services: []string{"auth"},
	})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "platform")
	assertFileContains(t, filepath.Join(root, "libs", "go.mod"), "module gitlab.example.com/team/platform-shared")
	assertFileContains(t, filepath.Join(root, "services", "auth-service", "main.go"), `gitlab.example.com/team/platform-shared/middleware`)
	assertFileContains(t, filepath.Join(root, workspaceManifestName), `"sharedVersion": "v1.2.0"`)
	last := runner.calls[len(runner.calls)-1]
	if !strings.Contains(strings.Join(last, " "), "-dropreplace=gitlab.example.com/team/platform-shared") {
		t.Fatalf("expected local replace cleanup, last command was %q", last)
	}
}

func TestWorkspaceGeneratorAddsServiceAndKeepsExistingPorts(t *testing.T) {
	parent := t.TempDir()
	runner := &recordedRunner{}
	generator := WorkspaceGenerator{Runner: runner}
	if err := generator.Generate(context.Background(), WorkspaceOptions{Name: "platform", Module: "example.com/platform", Database: "pg", ParentPath: parent, Services: []string{"order", "pos"}}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "platform")
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readmePath, append(readme, []byte("\nCustom workspace notes.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generator.AddService(context.Background(), root, "payment"); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(root, "go.work"), "./services/payment-service")
	compose, err := os.ReadFile(filepath.Join(root, "deploy", "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), `order-service:`+"\n") || !strings.Contains(string(compose), `"3001:3000"`) {
		t.Fatalf("existing service port changed:\n%s", compose)
	}
	if !strings.Contains(string(compose), `"3003:3000"`) {
		t.Fatalf("new service port missing:\n%s", compose)
	}
	assertFileContains(t, readmePath, "Custom workspace notes.")
	assertFileContains(t, readmePath, "`payment-service`")
	if err := generator.AddService(context.Background(), root, "payment"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestNormalizeServices(t *testing.T) {
	services, err := normalizeServices([]string{"order", "pos_service", "auth-service"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"order-service", "pos-service", "auth-service"}
	for index := range want {
		if services[index] != want[index] {
			t.Fatalf("services=%v", services)
		}
	}
}

func TestWorkspaceGeneratorCleansNewWorkspaceAfterFailure(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "platform")
	err := (WorkspaceGenerator{Runner: failingRunner{}}).Generate(context.Background(), WorkspaceOptions{Name: "platform", Module: "example.com/platform", Database: "pg", ParentPath: parent, Services: []string{"order"}})
	if err == nil {
		t.Fatal("expected generation failure")
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("partial workspace was not cleaned: %v", statErr)
	}
}

func TestWorkspaceGeneratorCleansNewServiceAfterFailure(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "platform")
	good := WorkspaceGenerator{Runner: &recordedRunner{}}
	if err := good.Generate(context.Background(), WorkspaceOptions{Name: "platform", Module: "example.com/platform", Database: "pg", ParentPath: parent, Services: []string{"order"}}); err != nil {
		t.Fatal(err)
	}
	err := (WorkspaceGenerator{Runner: failingRunner{}}).AddService(context.Background(), root, "payment")
	if err == nil {
		t.Fatal("expected add service failure")
	}
	if _, statErr := os.Stat(filepath.Join(root, "services", "payment-service")); !os.IsNotExist(statErr) {
		t.Fatalf("partial service was not cleaned: %v", statErr)
	}
}
