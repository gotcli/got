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
	for path := range files {
		if _, err := os.Stat(filepath.Join(projectPath, path)); err == nil {
			return fmt.Errorf("Swagger component already exists: %s", path)
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
		return fmt.Errorf("Swagger configuration already exists in %s", configPath)
	}
	mainPath := filepath.Join(projectPath, "main.go")
	mainSource, err := os.ReadFile(mainPath)
	if err != nil {
		return fmt.Errorf("read main.go: %w", err)
	}
	updatedMain, err := addSwaggerRegistration(mainSource)
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
		return fmt.Errorf("update main.go: %w", err)
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
		return nil, fmt.Errorf("Swagger route registration already exists")
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
