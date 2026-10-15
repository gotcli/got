package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotcli/got/internal/toolchain"
)

const workspaceManifestName = "got-workspace.json"
const workspaceServicesDir = "services"
const workspaceDeployDir = "deploy"
const workspaceServicesStart = "<!-- got:services:start -->"
const workspaceServicesEnd = "<!-- got:services:end -->"

type WorkspaceOptions struct {
	Name, Module, Database, ParentPath string
	SharedModule, SharedVersion        string
	Services                           []string
}

type WorkspaceGenerator struct {
	Runner  CommandRunner
	Output  io.Writer
	Version string
}

type workspaceManifest struct {
	Name          string   `json:"name"`
	Module        string   `json:"module"`
	Database      string   `json:"database"`
	SharedModule  string   `json:"sharedModule"`
	SharedVersion string   `json:"sharedVersion"`
	Services      []string `json:"services"`
}

func (g WorkspaceGenerator) Generate(ctx context.Context, options WorkspaceOptions) (returnErr error) {
	if err := validateProjectName(options.Name); err != nil {
		return err
	}
	if options.Module == "" {
		options.Module = options.Name
	}
	options.Module = strings.TrimSuffix(options.Module, "/")
	if options.SharedModule == "" {
		options.SharedModule = defaultSharedModule(options.Module)
	}
	options.SharedModule = strings.TrimSuffix(options.SharedModule, "/")
	if options.SharedVersion == "" {
		options.SharedVersion = defaultSharedVersion
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
	manifest := workspaceManifest{Name: options.Name, Module: options.Module, Database: options.Database, SharedModule: options.SharedModule, SharedVersion: options.SharedVersion, Services: services}
	if err := writeWorkspaceFiles(root, manifest); err != nil {
		return err
	}
	if err := writeSharedModule(root, manifest); err != nil {
		return err
	}
	runner := g.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if err := runner.Run(ctx, filepath.Join(root, "libs"), "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("tidy shared module: %w", err)
	}
	for _, service := range services {
		if g.Output != nil {
			fmt.Fprintf(g.Output, "[step] generating workspace service %s\n", service)
		}
		if err := ProjectGenerator(g).Generate(ctx, ProjectOptions{Name: service, Module: manifest.Module + "/" + service, Architecture: "microservice", Database: manifest.Database, ParentPath: filepath.Join(root, workspaceServicesDir)}); err != nil {
			return fmt.Errorf("generate service %s: %w", service, err)
		}
		servicePath := filepath.Join(root, workspaceServicesDir, service)
		if err := configureWorkspaceService(ctx, runner, servicePath, manifest); err != nil {
			return fmt.Errorf("configure service %s: %w", service, err)
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
	if _, err := os.Stat(filepath.Join(root, "libs", "go.mod")); err != nil {
		if os.IsNotExist(err) {
			return errors.New("workspace has no libs/go.mod; it was created by an older GOT layout, recreate it with \"got init workspace\"")
		}
		return err
	}
	serviceRoot := filepath.Join(root, workspaceServicesDir)
	servicePath := filepath.Join(serviceRoot, service)
	if _, err := os.Stat(servicePath); err == nil {
		return fmt.Errorf("service destination already exists: %s", servicePath)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ProjectGenerator(g).Generate(ctx, ProjectOptions{Name: service, Module: manifest.Module + "/" + service, Architecture: "microservice", Database: manifest.Database, ParentPath: serviceRoot}); err != nil {
		_ = os.RemoveAll(servicePath)
		return err
	}
	runner := g.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if err := configureWorkspaceService(ctx, runner, servicePath, manifest); err != nil {
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
	for _, directory := range []string{
		filepath.Join(root, workspaceServicesDir),
		filepath.Join(root, "libs", "logger"),
		filepath.Join(root, "libs", "middleware"),
		filepath.Join(root, "libs", "observability"),
		filepath.Join(root, workspaceDeployDir),
		filepath.Join(root, ".github", "workflows"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create workspace directory %s: %w", directory, err)
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	files := map[string][]byte{
		workspaceManifestName: encoded,
		"go.work":             []byte(renderGoWork(manifest.Services)),
		filepath.Join(workspaceDeployDir, "compose.yml"): []byte(renderCompose(manifest.Services)),
		"Makefile":      []byte(workspaceMakefile),
		"README.md":     []byte(renderWorkspaceREADME(manifest)),
		".gitignore":    []byte(".DS_Store\n**/logs/\n**/uploads/\n**/config.yml\n"),
		".dockerignore": []byte(".git\n.github\n**/logs\n**/uploads\n**/*.log\n.env\n"),
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
	for name, content := range map[string][]byte{workspaceManifestName: encoded, "go.work": []byte(renderGoWork(manifest.Services)), filepath.Join(workspaceDeployDir, "compose.yml"): []byte(renderCompose(manifest.Services))} {
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
	fmt.Fprintf(&b, "go %s\ntoolchain %s\n\nuse (\n", toolchain.GoVersion, toolchain.GoToolchain)
	for _, service := range services {
		fmt.Fprintf(&b, "\t./%s/%s\n", workspaceServicesDir, service)
	}
	b.WriteString("\t./libs\n")
	b.WriteString(")\n")
	return b.String()
}
func renderCompose(services []string) string {
	var b strings.Builder
	b.WriteString("services:\n")
	for index, service := range services {
		fmt.Fprintf(&b, "  %s:\n    build:\n      context: ..\n      dockerfile: %s/%s/Dockerfile\n    ports:\n      - \"%d:3000\"\n    restart: unless-stopped\n", service, workspaceServicesDir, service, 3001+index)
	}
	return b.String()
}
func renderWorkspaceREADME(manifest workspaceManifest) string {
	return fmt.Sprintf("# %s\n\nGOT microservice workspace. Each directory under `services/` is an independently buildable and deployable Go module.\n\n## Services\n\n%s\n%s%s\n## Commands\n\n```sh\nmake test\nmake vet\ndocker compose -f deploy/compose.yml up --build\ngot add service payment\n```\n", manifest.Name, workspaceServicesStart, workspaceServiceList(manifest.Services), workspaceServicesEnd)
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
	@for dir in services/*-service; do (cd $$dir && go test ./...) || exit 1; done

vet:
	@for dir in services/*-service; do (cd $$dir && go vet ./...) || exit 1; done

tidy:
	@for dir in services/*-service; do (cd $$dir && go mod tidy) || exit 1; done
`
