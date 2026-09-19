package generator

import (
	"bufio"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gotcli/blueprints"
)

const routeMarker = "// got:api-routes"

type APIGenerator struct{}

func (APIGenerator) Generate(projectPath, apiName string) error {
	if err := validateName("API name", apiName); err != nil {
		return err
	}
	moduleName, err := readModuleName(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return err
	}
	data := templateData{ModuleName: moduleName, ApiName: apiName, TypeName: typeName(apiName)}
	rootRoutePath := filepath.Join(projectPath, "routes", "root.go")
	if err := validateRouteRegistry(rootRoutePath, data.TypeName); err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join("entities", apiName, apiName+".go"):      blueprints.EntityFile,
		filepath.Join("repositories", apiName, "interface.go"): blueprints.RepositoryInterface,
		filepath.Join("repositories", apiName, apiName+".go"):  blueprints.RepositoryMethod,
		filepath.Join("schemas", apiName, apiName+".go"):       blueprints.SchemaFile,
		filepath.Join("services", apiName, "interface.go"):     blueprints.ServiceInterface,
		filepath.Join("services", apiName, apiName+".go"):      blueprints.ServiceMethod,
		filepath.Join("handlers", apiName, "interface.go"):     blueprints.HandlerInterface,
		filepath.Join("handlers", apiName, apiName+".go"):      blueprints.HandlerMethod,
		filepath.Join("routes", apiName+".go"):                 blueprints.RouteDynamic,
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		absolutePath := filepath.Join(projectPath, path)
		if _, err := os.Stat(absolutePath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", absolutePath)
		} else if !os.IsNotExist(err) {
			return err
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		content, err := render(path, files[path], data)
		if err != nil {
			return err
		}
		if err := writeNewFile(filepath.Join(projectPath, path), content); err != nil {
			return err
		}
	}
	return registerRoute(rootRoutePath, data.TypeName)
}

func readModuleName(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("current directory is not a Go module (go.mod not found)")
		}
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("module declaration not found in go.mod")
}

func registerRoute(path, apiTypeName string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read route registry: %w", err)
	}
	registration := fmt.Sprintf("Register%sRoutes(db, routeGroup)", apiTypeName)
	if strings.Contains(string(content), registration) {
		return nil
	}
	if !strings.Contains(string(content), routeMarker) {
		return fmt.Errorf("route registry marker %q not found in %s", routeMarker, path)
	}
	updated := strings.Replace(string(content), routeMarker, registration+"\n\t"+routeMarker, 1)
	formatted, err := formatSource(path, []byte(updated))
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("update route registry: %w", err)
	}
	return nil
}

func validateRouteRegistry(path, apiTypeName string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read route registry: %w", err)
	}
	registration := fmt.Sprintf("Register%sRoutes(db, routeGroup)", apiTypeName)
	if strings.Contains(string(content), registration) {
		return fmt.Errorf("API route %s is already registered", apiTypeName)
	}
	if !strings.Contains(string(content), routeMarker) {
		return fmt.Errorf("route registry marker %q not found in %s", routeMarker, path)
	}
	return nil
}

func formatSource(name string, source []byte) ([]byte, error) {
	formatted, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("format %s: %w", name, err)
	}
	return formatted, nil
}
