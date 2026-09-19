package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uploadTestProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "config.yml"), []byte("APP_PORT: 3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "routes"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := "package routes\n\nfunc register() {\n\t" + routeMarker + "\n}\n"
	if err := os.WriteFile(filepath.Join(project, "routes", "root.go"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	return project
}

func TestUploadGeneratorCreatesSecureUploadLayers(t *testing.T) {
	project := uploadTestProject(t)
	if err := (UploadGenerator{}).Generate(project); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "pkg", "upload", "storage.go"), "crypto/rand")
	assertFileContains(t, filepath.Join(project, "pkg", "upload", "storage.go"), "http.DetectContentType")
	assertFileContains(t, filepath.Join(project, "services", "uploads", "uploads.go"), "ErrFileTooLarge")
	assertFileContains(t, filepath.Join(project, "routes", "uploads.go"), `route.Post("/uploads", uploadHandler.Upload)`)
	assertFileContains(t, filepath.Join(project, "routes", "root.go"), "RegisterUploadRoutes(db, routeGroup)")
	assertFileContains(t, filepath.Join(project, "config.yml"), "UPLOAD_MAX_SIZE: 10485760")
	if err := (UploadGenerator{}).Generate(project); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
