package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotcli/blueprints"
)

const jwtConfigMarker = "# got:jwt-config"

type AuthOptions struct{ JWT bool }

type AuthGenerator struct{ Runner CommandRunner }

func (g AuthGenerator) Generate(ctx context.Context, projectPath string, options AuthOptions) error {
	if !options.JWT {
		return errorsJWTFlag()
	}
	module, err := readModuleName(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return err
	}
	files := map[string]string{
		"pkg/auth/jwt.go":    blueprints.JWTManager,
		"middlewares/jwt.go": blueprints.JWTMiddleware,
	}
	for path := range files {
		if _, err := os.Stat(filepath.Join(projectPath, path)); err == nil {
			return fmt.Errorf("authentication already exists: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	configPath := filepath.Join(projectPath, "config.yml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config.yml: %w", err)
	}
	if strings.Contains(string(config), jwtConfigMarker) {
		return fmt.Errorf("JWT configuration already exists in %s", configPath)
	}
	runner := g.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	// Install first so a network failure does not leave generated files that
	// prevent the user from retrying the command.
	if err := runner.Run(ctx, projectPath, "go", "get", "github.com/golang-jwt/jwt/v5@v5.3.1"); err != nil {
		return fmt.Errorf("install JWT dependency: %w", err)
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
	block := "\n" + jwtConfigMarker + "\nJWT_ISSUER: \"" + module + "\"\nJWT_AUDIENCE: \"" + module + "\"\nJWT_ACCESS_SECRET: \"\"\nJWT_REFRESH_SECRET: \"\"\nJWT_ACCESS_TTL: \"15m\"\nJWT_REFRESH_TTL: \"168h\"\n"
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open config.yml: %w", err)
	}
	if _, err := file.WriteString(block); err != nil {
		_ = file.Close()
		return fmt.Errorf("write JWT config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config.yml: %w", err)
	}
	if err := runner.Run(ctx, projectPath, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("tidy JWT dependency: %w", err)
	}
	return nil
}

func errorsJWTFlag() error { return fmt.Errorf("select an authentication type; use --jwt") }
