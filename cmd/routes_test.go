package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoutesCommandListsRoutesAndFailsOnDuplicates(t *testing.T) {
	project := t.TempDir()
	writeDoctorFile(t, filepath.Join(project, "go.mod"), "module example.com/shop\n")
	writeDoctorFile(t, filepath.Join(project, "main.go"), `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	api := app.Group("/api")
	api.Get("/orders", list)
	api.Get("/orders", list)
}
`)
	command := newRoutesCommand(&config{cwd: project})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "duplicate routes") || !strings.Contains(err.Error(), "GET /api/orders") {
		t.Fatalf("expected duplicate route error, got %v", err)
	}
	if !strings.Contains(output.String(), "main.go:8") {
		t.Fatalf("route table missing source location:\n%s", output.String())
	}
}
