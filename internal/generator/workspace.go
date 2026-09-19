package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const workspaceManifestName = "got-workspace.json"
const workspaceServicesStart = "<!-- got:services:start -->"
const workspaceServicesEnd = "<!-- got:services:end -->"

type WorkspaceOptions struct {
	Name, Module, Database, ParentPath string
	Services                           []string
}

type WorkspaceGenerator struct {
	Runner CommandRunner
	Output io.Writer
}

type workspaceManifest struct {
	Name     string   `json:"name"`
	Module   string   `json:"module"`
	Database string   `json:"database"`
	Services []string `json:"services"`
}

func (g WorkspaceGenerator) Generate(ctx context.Context, options WorkspaceOptions) (returnErr error) {
	if err := validateProjectName(options.Name); err != nil {
		return err
	}
	if options.Module == "" {
		options.Module = options.Name
	}
	if _, ok := databases[options.Database]; !ok {
		return fmt.Errorf("unsupported database %q (supported: mysql, pg, mssql)", options.Database)
	}
	services, err := normalizeServices(options.Services)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("at least one service is required")
	}
	root := filepath.Join(options.ParentPath, options.Name)
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("destination already exists: %s", root)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.RemoveAll(root)
		}
	}()
	manifest := workspaceManifest{Name: options.Name, Module: strings.TrimSuffix(options.Module, "/"), Database: options.Database, Services: services}
	if err := writeWorkspaceFiles(root, manifest); err != nil {
		return err
	}
	for _, service := range services {
		if g.Output != nil {
			fmt.Fprintf(g.Output, "[step] generating workspace service %s\n", service)
		}
		if err := (ProjectGenerator{Runner: g.Runner, Output: g.Output}).Generate(ctx, ProjectOptions{Name: service, Module: manifest.Module + "/" + service, Architecture: "microservice", Database: manifest.Database, ParentPath: root}); err != nil {
			return fmt.Errorf("generate service %s: %w", service, err)
		}
	}
	return nil
}

func (g WorkspaceGenerator) AddService(ctx context.Context, root, name string) error {
	manifest, err := readWorkspaceManifest(root)
	if err != nil {
		return err
	}
	services, err := normalizeServices([]string{name})
	if err != nil {
		return err
	}
	service := services[0]
	for _, existing := range manifest.Services {
		if existing == service {
			return fmt.Errorf("service %q already exists", service)
		}
	}
	servicePath := filepath.Join(root, service)
	if _, err := os.Stat(servicePath); err == nil {
		return fmt.Errorf("service destination already exists: %s", servicePath)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := (ProjectGenerator{Runner: g.Runner, Output: g.Output}).Generate(ctx, ProjectOptions{Name: service, Module: manifest.Module + "/" + service, Architecture: "microservice", Database: manifest.Database, ParentPath: root}); err != nil {
		_ = os.RemoveAll(servicePath)
		return err
	}
	manifest.Services = append(manifest.Services, service)
	return updateWorkspaceFiles(root, manifest)
}

func normalizeServices(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		value = strings.ReplaceAll(value, "_", "-")
		if !strings.HasSuffix(value, "-service") {
			value += "-service"
		}
		if err := validateProjectName(value); err != nil {
			return nil, fmt.Errorf("invalid service %q: %w", value, err)
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("duplicate service %q", value)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func readWorkspaceManifest(root string) (workspaceManifest, error) {
	content, err := os.ReadFile(filepath.Join(root, workspaceManifestName))
	if err != nil {
		return workspaceManifest{}, fmt.Errorf("read workspace manifest: %w", err)
	}
	var manifest workspaceManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return manifest, fmt.Errorf("parse workspace manifest: %w", err)
	}
	return manifest, nil
}

func writeWorkspaceFiles(root string, manifest workspaceManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	files := map[string][]byte{
		workspaceManifestName: encoded,
		"go.work":             []byte(renderGoWork(manifest.Services)),
		"compose.yml":         []byte(renderCompose(manifest.Services)),
		"Makefile":            []byte(workspaceMakefile),
		"README.md":           []byte(renderWorkspaceREADME(manifest)),
		".gitignore":          []byte(".DS_Store\n**/logs/\n**/uploads/\n**/config.yml\n"),
	}
	for name, content := range files {
		if err := writeOrReplaceFile(filepath.Join(root, name), content); err != nil {
			return err
		}
	}
	return nil
}

func updateWorkspaceFiles(root string, manifest workspaceManifest) error {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	for name, content := range map[string][]byte{workspaceManifestName: encoded, "go.work": []byte(renderGoWork(manifest.Services)), "compose.yml": []byte(renderCompose(manifest.Services))} {
		if err := writeOrReplaceFile(filepath.Join(root, name), content); err != nil {
			return err
		}
	}
	readmePath := filepath.Join(root, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read workspace README: %w", err)
	}
	start := strings.Index(string(content), workspaceServicesStart)
	end := strings.Index(string(content), workspaceServicesEnd)
	if start < 0 || end < start {
		return fmt.Errorf("workspace README service markers are missing")
	}
	end += len(workspaceServicesEnd)
	serviceBlock := workspaceServicesStart + "\n" + workspaceServiceList(manifest.Services) + workspaceServicesEnd
	updated := string(content[:start]) + serviceBlock + string(content[end:])
	return writeOrReplaceFile(readmePath, []byte(updated))
}

func writeOrReplaceFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func renderGoWork(services []string) string {
	var b strings.Builder
	b.WriteString("go 1.22.0\ntoolchain go1.22.1\n\nuse (\n")
	for _, service := range services {
		fmt.Fprintf(&b, "\t./%s\n", service)
	}
	b.WriteString(")\n")
	return b.String()
}
func renderCompose(services []string) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for index, service := range services {
		fmt.Fprintf(&b, "  %s:\n    build:\n      context: ./%s\n    ports:\n      - \"%d:3000\"\n    restart: unless-stopped\n", service, service, 3001+index)
	}
	return b.String()
}
func renderWorkspaceREADME(manifest workspaceManifest) string {
	return fmt.Sprintf("# %s\n\nGOT microservice workspace. Each child directory is an independently buildable and deployable Go module.\n\n## Services\n\n%s\n%s%s\n## Commands\n\n```sh\nmake test\nmake vet\ndocker compose up --build\ngot add service payment\n```\n", manifest.Name, workspaceServicesStart, workspaceServiceList(manifest.Services), workspaceServicesEnd)
}
func workspaceServiceList(services []string) string {
	var b strings.Builder
	for _, service := range services {
		fmt.Fprintf(&b, "- `%s`\n", service)
	}
	b.WriteString("\n")
	return b.String()
}

const workspaceMakefile = `.PHONY: test vet tidy

test:
	@for dir in *-service; do (cd $$dir && go test ./...) || exit 1; done

vet:
	@for dir in *-service; do (cd $$dir && go vet ./...) || exit 1; done

tidy:
	@for dir in *-service; do (cd $$dir && go mod tidy) || exit 1; done
`
