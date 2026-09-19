package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func authTestProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "config.yml"), []byte("APP_PORT: 3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestAuthGeneratorAddsJWT(t *testing.T) {
	project := authTestProject(t)
	runner := &recordedRunner{}
	if err := (AuthGenerator{Runner: runner}).Generate(context.Background(), project, AuthOptions{JWT: true}); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "pkg", "auth", "jwt.go"), "func (m *Manager) IssuePair")
	assertFileContains(t, filepath.Join(project, "middlewares", "jwt.go"), "func RequireJWT")
	assertFileContains(t, filepath.Join(project, "config.yml"), "JWT_ACCESS_SECRET")
	if len(runner.calls) != 2 {
		t.Fatalf("expected dependency and tidy commands, got %d", len(runner.calls))
	}
	if got := strings.Join(runner.calls[0], " "); got != "go get github.com/golang-jwt/jwt/v5@v5.3.1" {
		t.Fatalf("unexpected dependency command: %s", got)
	}
}

func TestAuthGeneratorRequiresJWTFlag(t *testing.T) {
	err := (AuthGenerator{Runner: &recordedRunner{}}).Generate(context.Background(), authTestProject(t), AuthOptions{})
	if err == nil || !strings.Contains(err.Error(), "--jwt") {
		t.Fatalf("expected JWT flag error, got %v", err)
	}
}

func TestAuthGeneratorRefusesDuplicate(t *testing.T) {
	project := authTestProject(t)
	generator := AuthGenerator{Runner: &recordedRunner{}}
	if err := generator.Generate(context.Background(), project, AuthOptions{JWT: true}); err != nil {
		t.Fatal(err)
	}
	if err := generator.Generate(context.Background(), project, AuthOptions{JWT: true}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
