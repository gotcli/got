package generator

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
var projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$`)

func validateProjectName(value string) error {
	if !projectNamePattern.MatchString(value) {
		return fmt.Errorf("invalid project name %q: use lowercase letters, numbers, hyphens, or underscores; start with a letter", value)
	}
	return nil
}

type templateData struct {
	ModuleName   string
	ApiName      string
	TypeName     string
	DBName       string
	DBUrl        string
	DBHost       string
	DBPort       uint16
	DatabaseName string
	DBUser       string
}

func validateName(kind, value string) error {
	if !namePattern.MatchString(value) {
		return fmt.Errorf("invalid %s %q: use lowercase letters, numbers, and underscores; start with a letter", kind, value)
	}
	return nil
}

func typeName(name string) string {
	parts := strings.Split(name, "_")
	for i := range parts {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func render(name, source string, data templateData) ([]byte, error) {
	return renderAny(name, source, data)
}

func renderAny(name, source string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	content := output.Bytes()
	if filepath.Ext(name) == ".go" {
		content, err = format.Source(content)
		if err != nil {
			return nil, fmt.Errorf("format generated %s: %w", name, err)
		}
	}
	return content, nil
}

func writeNewFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
