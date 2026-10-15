package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ProjectManifestName records how a service was generated so later GOT
// versions, such as "got upgrade", can recognize and evolve it.
const ProjectManifestName = "got-project.json"

type ProjectManifest struct {
	GOTVersion   string `json:"gotVersion"`
	Module       string `json:"module"`
	Architecture string `json:"architecture"`
	Database     string `json:"database"`
}

func ReadProjectManifest(projectPath string) (ProjectManifest, error) {
	content, err := os.ReadFile(filepath.Join(projectPath, ProjectManifestName))
	if err != nil {
		return ProjectManifest{}, err
	}
	var manifest ProjectManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return ProjectManifest{}, fmt.Errorf("invalid %s: %w", ProjectManifestName, err)
	}
	return manifest, nil
}

func EncodeProjectManifest(manifest ProjectManifest) ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// IsHexagonal reports whether got-project.json declares the hexagonal layout.
func IsHexagonal(projectPath string) bool {
	manifest, err := ReadProjectManifest(projectPath)
	return err == nil && manifest.Architecture == hexagonalArchitecture
}
