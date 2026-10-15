package generator

import (
	"fmt"
	"path/filepath"
)

// MigrationGenerator adds an empty, numbered migration to a hexagonal project.
type MigrationGenerator struct{}

// Generate writes internal/migration/postgres/<next>_<name>.go and returns its
// project-relative path.
func (MigrationGenerator) Generate(projectPath, name string) (string, error) {
	if !IsHexagonal(projectPath) {
		return "", fmt.Errorf("migrations are generated only for hexagonal projects (internal/migration/postgres)")
	}
	snake := toSnakeCase(name)
	if snake == "" {
		return "", fmt.Errorf("migration name %q must contain letters or digits", name)
	}
	directory := filepath.Join("internal", "migration", "postgres")
	version, err := nextMigrationVersion(filepath.Join(projectPath, directory))
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, fmt.Sprintf("%d_%s.go", version, snake))
	content, err := renderAny(path, migrationTemplate, struct {
		Version int
		Name    string
	}{version, snake})
	if err != nil {
		return "", err
	}
	if err := writeNewFile(filepath.Join(projectPath, path), content); err != nil {
		return "", err
	}
	return path, nil
}

const migrationTemplate = `package postgres

// Add SQL statements to Up, then run "go run ./cmd migrate". A migration runs
// once in a transaction and is recorded in schema_migrations, so change the
// schema again with a new migration instead of editing an applied one.
func init() {
	register(Migration{Version: {{ .Version }}, Name: "{{ .Name }}", Up: []string{
		// ` + "`" + `CREATE INDEX IF NOT EXISTS products_name_idx ON products (name)` + "`" + `,
	}})
}
`
