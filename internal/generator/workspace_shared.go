package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotcli/blueprints"
	"github.com/gotcli/got/internal/toolchain"
)

func configureWorkspaceService(ctx context.Context, runner CommandRunner, servicePath string, manifest workspaceManifest) error {
	if err := useSharedInfrastructure(servicePath, manifest); err != nil {
		return err
	}
	dockerfile := strings.NewReplacer("{{SERVICE}}", filepath.Base(servicePath), "{{GO_VERSION}}", toolchain.DockerImage).Replace(workspaceServiceDockerfile)
	if err := writeOrReplaceFile(filepath.Join(servicePath, "Dockerfile"), []byte(dockerfile)); err != nil {
		return err
	}
	libsModule := sharedModule(manifest)
	libsVersion := manifest.SharedVersion
	if libsVersion == "" {
		libsVersion = defaultSharedVersion
	}
	if err := runner.Run(ctx, servicePath, "go", "mod", "edit", "-require="+libsModule+"@"+libsVersion, "-replace="+libsModule+"=../../libs"); err != nil {
		return fmt.Errorf("link shared module: %w", err)
	}
	if err := runner.Run(ctx, servicePath, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("tidy service module: %w", err)
	}
	// The in-repository libs module is never published, so its version cannot be
	// resolved without the replace; even go.work mode fetches the required version.
	// Only an external shared module, published separately, drops the replace.
	if !hasExternalSharedModule(manifest) {
		return nil
	}
	if err := runner.Run(ctx, servicePath, "go", "mod", "edit", "-dropreplace="+libsModule); err != nil {
		return fmt.Errorf("remove local shared-module replacement: %w", err)
	}
	return nil
}

func loggerModuleForProject(projectPath, serviceModule string) string {
	manifest, err := readWorkspaceManifest(filepath.Clean(filepath.Join(projectPath, "..", "..")))
	if err == nil && manifest.Module != "" {
		return sharedModule(manifest) + "/logger"
	}
	return serviceModule + "/pkg/logger"
}

func writeSharedModule(root string, manifest workspaceManifest) error {
	module := sharedModule(manifest)
	logger, err := render("logger.go", blueprints.StructuredLogger, templateData{ModuleName: module})
	if err != nil {
		return err
	}
	middlewareSource := strings.ReplaceAll(blueprints.RequestLoggerMiddleware, "{{ .ModuleName }}/pkg/logger", "{{ .ModuleName }}/logger")
	middlewareSource = strings.Replace(middlewareSource, `"github.com/gofiber/fiber/v2"`, `"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"`, 1)
	middleware, err := render("middleware.go", middlewareSource+sharedMiddlewareChain, templateData{ModuleName: module})
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"go.mod":                             []byte(fmt.Sprintf("module %s\n\ngo %s\n\ntoolchain %s\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 %s\n\tgorm.io/gorm %s\n)\n", module, toolchain.GoVersion, toolchain.GoToolchain, fiberVersion, gormVersion)),
		filepath.Join("logger", "logger.go"): logger,
		filepath.Join("middleware", "middleware.go"): middleware,
	}
	for name, content := range files {
		if err := writeOrReplaceFile(filepath.Join(root, "libs", name), content); err != nil {
			return err
		}
	}
	return nil
}

func useSharedInfrastructure(servicePath string, manifest workspaceManifest) error {
	replacements := map[string]string{
		manifest.Module + "/" + filepath.Base(servicePath) + "/pkg/logger":  sharedModule(manifest) + "/logger",
		manifest.Module + "/" + filepath.Base(servicePath) + "/middlewares": sharedModule(manifest) + "/middleware",
		"app.Use(middlewares.RequestLogger(loggers))":                       "middlewares.Register(app, loggers)",
	}
	required := map[string][]string{
		"main.go": {manifest.Module + "/" + filepath.Base(servicePath) + "/middlewares", "app.Use(middlewares.RequestLogger(loggers))"},
	}
	for _, relative := range []string{"main.go", filepath.Join("databases", manifest.Database+".go")} {
		path := filepath.Join(servicePath, relative)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := string(content)
		// Fail loudly when the blueprint changes shape instead of silently
		// leaving imports to packages that are deleted below.
		for _, needle := range required[relative] {
			if !strings.Contains(updated, needle) {
				return fmt.Errorf("%s: expected %q from the service blueprint", relative, needle)
			}
		}
		for old, replacement := range replacements {
			updated = strings.ReplaceAll(updated, old, replacement)
		}
		if err := writeOrReplaceFile(path, []byte(updated)); err != nil {
			return err
		}
	}
	for _, path := range []string{filepath.Join(servicePath, "pkg", "logger", "logger.go"), filepath.Join(servicePath, "middlewares", "request_logger.go")} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(filepath.Dir(path))
	}
	return nil
}

func sharedModule(manifest workspaceManifest) string {
	if manifest.SharedModule != "" {
		return manifest.SharedModule
	}
	return defaultSharedModule(manifest.Module)
}

func defaultSharedModule(module string) string {
	return module + "/libs"
}

// hasExternalSharedModule reports whether the shared module lives outside the
// workspace module path and is expected to be published at SharedVersion.
func hasExternalSharedModule(manifest workspaceManifest) bool {
	return sharedModule(manifest) != defaultSharedModule(manifest.Module)
}

const defaultSharedVersion = "v0.1.0"

const sharedMiddlewareChain = `

// Register installs the workspace-wide HTTP middleware chain.
func Register(app *fiber.App, loggers *logger.Loggers) {
	app.Use(recover.New())
	app.Use(RequestLogger(loggers))
}
`

const workspaceServiceDockerfile = `FROM golang:{{GO_VERSION}}-alpine AS build
WORKDIR /src
COPY libs ./libs
COPY services/{{SERVICE}} ./services/{{SERVICE}}
RUN go work init ./libs ./services/{{SERVICE}}
WORKDIR /src/services/{{SERVICE}}
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /service .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /service /app/service
COPY services/{{SERVICE}}/config.yml /app/config.yml
USER nonroot:nonroot
EXPOSE 3000
ENTRYPOINT ["/app/service"]
`
