package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotcli/blueprints"
)

const uploadConfigMarker = "# got:upload-config"

type UploadGenerator struct{}

func (UploadGenerator) Generate(projectPath string) error {
	module, err := readModuleName(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return err
	}
	rootPath := filepath.Join(projectPath, "routes", "root.go")
	if err := validateRouteRegistry(rootPath, "Upload"); err != nil {
		return err
	}
	files := map[string]string{
		"pkg/upload/storage.go":       blueprints.UploadStorage,
		"services/uploads/uploads.go": blueprints.UploadService,
		"handlers/uploads/uploads.go": blueprints.UploadHandler,
		"routes/uploads.go":           blueprints.UploadRoute,
	}
	for path := range files {
		if _, err := os.Stat(filepath.Join(projectPath, path)); err == nil {
			return fmt.Errorf("upload component already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	configPath := filepath.Join(projectPath, "config.yml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config.yml: %w", err)
	}
	if strings.Contains(string(config), uploadConfigMarker) {
		return fmt.Errorf("upload configuration already exists in %s", configPath)
	}
	for path, source := range files {
		content, err := render(path, source, templateData{ModuleName: module})
		if err != nil {
			return err
		}
		if err := writeNewFile(filepath.Join(projectPath, path), content); err != nil {
			return err
		}
	}
	block := "\n" + uploadConfigMarker + "\nUPLOAD_DIR: \"uploads\"\nUPLOAD_MAX_SIZE: 10485760\nUPLOAD_ALLOWED_EXTENSIONS: \".jpg,.jpeg,.png,.pdf\"\n"
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open config.yml: %w", err)
	}
	if _, err := file.WriteString(block); err != nil {
		_ = file.Close()
		return fmt.Errorf("write upload config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config.yml: %w", err)
	}
	return registerRoute(rootPath, "Upload")
}
