package generator

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotcli/blueprints"
)

const swaggerConfigMarker = "# got:swagger-config"

type SwaggerGenerator struct{}

func (SwaggerGenerator) Generate(projectPath string) (returnErr error) {
	module, err := readModuleName(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return err
	}
	files := map[string]string{
		"docs/openapi.json": blueprints.SwaggerSpec,
		"docs/openapi.go":   blueprints.SwaggerDocument,
		"routes/swagger.go": blueprints.SwaggerRoute,
	}
	// The registration call goes into main.go, or into the HTTP adapter's
	// server.go for hexagonal projects.
	registrationFile, register := "main.go", addSwaggerRegistration
	if IsHexagonal(projectPath) {
		route, err := rebase("SwaggerRoute", blueprints.SwaggerRoute,
			"package routes", "package fiber",
			"{{ .ModuleName }}/docs", "{{ .ModuleName }}/internal/adapter/inbound/fiber/docs")
		if err != nil {
			return err
		}
		files = map[string]string{
			"internal/adapter/inbound/fiber/docs/openapi.json": hexagonalSwaggerSpec,
			"internal/adapter/inbound/fiber/docs/openapi.go":   blueprints.SwaggerDocument,
			"internal/adapter/inbound/fiber/swagger.go":        route,
		}
		registrationFile, register = "internal/adapter/inbound/fiber/server.go", addHexagonalSwaggerRegistration
	}
	for path := range files {
		if _, err := os.Stat(filepath.Join(projectPath, path)); err == nil {
			return fmt.Errorf("swagger component already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	configPath := filepath.Join(projectPath, "config.yml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config.yml: %w", err)
	}
	if strings.Contains(string(config), swaggerConfigMarker) {
		return fmt.Errorf("swagger configuration already exists in %s", configPath)
	}
	mainPath := filepath.Join(projectPath, registrationFile)
	mainSource, err := os.ReadFile(mainPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", registrationFile, err)
	}
	updatedMain, err := register(mainSource)
	if err != nil {
		return err
	}
	created := make([]string, 0, len(files))
	mainUpdated := false
	configUpdated := false
	defer func() {
		if returnErr == nil {
			return
		}
		for _, path := range created {
			_ = os.Remove(path)
		}
		if mainUpdated {
			_ = os.WriteFile(mainPath, mainSource, 0o644)
		}
		if configUpdated {
			_ = os.WriteFile(configPath, config, 0o644)
		}
	}()
	for path, source := range files {
		content, err := render(path, source, templateData{ModuleName: module})
		if err != nil {
			return err
		}
		fullPath := filepath.Join(projectPath, path)
		if err := writeNewFile(fullPath, content); err != nil {
			return err
		}
		created = append(created, fullPath)
	}
	if err := os.WriteFile(mainPath, updatedMain, 0o644); err != nil {
		return fmt.Errorf("update %s: %w", registrationFile, err)
	}
	mainUpdated = true
	block := "\n" + swaggerConfigMarker + "\nSWAGGER_ENABLED: true\n"
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open config.yml: %w", err)
	}
	if _, err := file.WriteString(block); err != nil {
		_ = file.Close()
		return fmt.Errorf("write Swagger config: %w", err)
	}
	configUpdated = true
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config.yml: %w", err)
	}
	return nil
}

func addSwaggerRegistration(source []byte) ([]byte, error) {
	if strings.Contains(string(source), "routes.RegisterSwaggerRoutes(") {
		return nil, fmt.Errorf("swagger route registration already exists")
	}
	patterns := []string{"routes.RegisterRootRoutes(conn, app)", "routes.RegisterRootRoutes(conn,app)"}
	updated := string(source)
	found := false
	for _, pattern := range patterns {
		if strings.Contains(updated, pattern) {
			updated = strings.Replace(updated, pattern, pattern+"\n\troutes.RegisterSwaggerRoutes(app)", 1)
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("cannot find root route registration in main.go")
	}
	formatted, err := format.Source([]byte(updated))
	if err != nil {
		return nil, fmt.Errorf("format main.go after Swagger registration: %w", err)
	}
	return formatted, nil
}

func addHexagonalSwaggerRegistration(source []byte) ([]byte, error) {
	if strings.Contains(string(source), "RegisterSwaggerRoutes(") {
		return nil, fmt.Errorf("swagger route registration already exists")
	}
	const anchor = "\tregisterHealthRoutes(app, ready)\n"
	if strings.Count(string(source), anchor) != 1 {
		return nil, fmt.Errorf("cannot find registerHealthRoutes(app, ready) in server.go")
	}
	updated := strings.Replace(string(source), anchor, anchor+"\tRegisterSwaggerRoutes(app)\n", 1)
	formatted, err := format.Source([]byte(updated))
	if err != nil {
		return nil, fmt.Errorf("format server.go after Swagger registration: %w", err)
	}
	return formatted, nil
}

// hexagonalSwaggerSpec starts without paths: the blueprint spec documents the
// layered /api/v1 route, which hexagonal projects do not have. Registered
// routes are added to the document at runtime.
const hexagonalSwaggerSpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "{{ .ModuleName }}",
    "description": "API documentation generated by GOT CLI. Update this specification as business endpoints evolve.",
    "version": "1.0.0"
  },
  "servers": [{ "url": "/", "description": "Current server" }],
  "paths": {}
}
`
