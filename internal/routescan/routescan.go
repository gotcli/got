// Package routescan statically discovers Fiber routes in a generated project.
//
// It starts at func main, tracks variables holding a *fiber.App or a group
// (fiber.New, x.Group("prefix")), follows calls that pass those variables into
// functions of the same module, and records every x.Get/Post/... call with the
// fully joined path. Nothing is compiled or executed.
package routescan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Route struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler"`
	Source  string `json:"source"`
}

var httpMethods = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Patch": "PATCH", "Delete": "DELETE",
	"Head": "HEAD", "Options": "OPTIONS", "All": "ALL",
}

type function struct {
	decl *ast.FuncDecl
	file *parsedFile
}

type parsedFile struct {
	path    string
	imports map[string]string // local package name -> project-relative directory
}

type scanner struct {
	root      string
	fset      *token.FileSet
	functions map[string]function // functionKey(dir, name) -> declaration
	routes    []Route
	active    map[string]bool
}

// Scan returns the routes reachable from func main in projectPath, sorted by
// path and method.
func Scan(projectPath string) ([]Route, error) {
	module, err := readModule(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return nil, err
	}
	s := &scanner{root: projectPath, fset: token.NewFileSet(), functions: map[string]function{}, active: map[string]bool{}}
	if err := s.parse(module); err != nil {
		return nil, err
	}
	entry, ok := s.functions[functionKey(".", "main")]
	if !ok {
		return nil, fmt.Errorf("func main not found in %s", projectPath)
	}
	s.walk(entry, map[string]string{})
	sort.SliceStable(s.routes, func(i, j int) bool {
		if s.routes[i].Path != s.routes[j].Path {
			return s.routes[i].Path < s.routes[j].Path
		}
		return s.routes[i].Method < s.routes[j].Method
	})
	return s.routes, nil
}

// Duplicates returns "METHOD path" entries registered more than once. Fiber
// silently serves only the first registration.
func Duplicates(routes []Route) []string {
	counts := map[string]int{}
	for _, route := range routes {
		counts[route.Method+" "+route.Path]++
	}
	var duplicates []string
	for key, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, key)
		}
	}
	sort.Strings(duplicates)
	return duplicates
}

func (s *scanner) parse(module string) error {
	return filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != s.root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "logs" || name == "uploads") {
				return filepath.SkipDir
			}
			// Nested modules, such as workspace services, are scanned separately.
			if path != s.root {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(s.fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		relative, _ := filepath.Rel(s.root, path)
		parsed := &parsedFile{path: filepath.ToSlash(relative), imports: map[string]string{}}
		for _, spec := range file.Imports {
			importPath, _ := strconv.Unquote(spec.Path.Value)
			if importPath != module && !strings.HasPrefix(importPath, module+"/") {
				continue
			}
			dir := strings.TrimPrefix(strings.TrimPrefix(importPath, module), "/")
			name := filepath.Base(dir)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			parsed.imports[name] = dir
		}
		dir := filepath.ToSlash(filepath.Dir(relative))
		for _, declaration := range file.Decls {
			if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
				s.functions[functionKey(dir, fn.Name.Name)] = function{decl: fn, file: parsed}
			}
		}
		return nil
	})
}

// walk visits a function body in source order. env maps router variables to
// their path prefix.
func (s *scanner) walk(fn function, env map[string]string) {
	key := fn.file.path + "." + fn.decl.Name.Name
	if s.active[key] {
		return
	}
	s.active[key] = true
	defer delete(s.active, key)

	constructors := map[string]string{} // variable -> constructor name, for handler labels
	dir := filepath.ToSlash(filepath.Dir(fn.file.path))
	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if len(node.Lhs) != 1 || len(node.Rhs) != 1 {
				return true
			}
			name, ok := node.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			call, ok := node.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			if prefix, ok := s.routerExpr(call, env); ok {
				env[name.Name] = prefix
			} else if constructor := calleeName(call); strings.HasPrefix(constructor, "New") {
				constructors[name.Name] = strings.TrimPrefix(constructor, "New")
			}
		case *ast.CallExpr:
			s.call(node, env, constructors, fn.file, dir)
		}
		return true
	})
}

// routerExpr reports whether call produces a router: fiber.New() or
// <router>.Group("prefix").
func (s *scanner) routerExpr(call *ast.CallExpr, env map[string]string) (string, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "fiber" && selector.Sel.Name == "New" {
		return "", true
	}
	if selector.Sel.Name != "Group" || len(call.Args) == 0 {
		return "", false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	prefix, ok := env[receiver.Name]
	if !ok {
		return "", false
	}
	return joinPath(prefix, literal(call.Args[0])), true
}

func (s *scanner) call(call *ast.CallExpr, env, constructors map[string]string, file *parsedFile, dir string) {
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		if receiver, ok := selector.X.(*ast.Ident); ok {
			if prefix, isRouter := env[receiver.Name]; isRouter {
				if method, ok := httpMethods[selector.Sel.Name]; ok && len(call.Args) > 0 {
					position := s.fset.Position(call.Pos())
					s.routes = append(s.routes, Route{
						Method:  method,
						Path:    joinPath(prefix, literal(call.Args[0])),
						Handler: handlerLabel(call.Args[len(call.Args)-1], constructors),
						Source:  fmt.Sprintf("%s:%d", file.path, position.Line),
					})
				}
				return
			}
		}
	}
	callee, ok := s.resolve(call.Fun, file, dir)
	if !ok {
		return
	}
	params := parameterNames(callee.decl)
	calleeEnv := map[string]string{}
	for index, argument := range call.Args {
		ident, ok := argument.(*ast.Ident)
		if !ok || index >= len(params) {
			continue
		}
		if prefix, ok := env[ident.Name]; ok {
			calleeEnv[params[index]] = prefix
		}
	}
	if len(calleeEnv) > 0 {
		s.walk(callee, calleeEnv)
	}
}

// resolve finds a project function called as Func(...) or pkg.Func(...).
func (s *scanner) resolve(expression ast.Expr, file *parsedFile, dir string) (function, bool) {
	switch expression := expression.(type) {
	case *ast.Ident:
		fn, ok := s.functions[functionKey(dir, expression.Name)]
		return fn, ok
	case *ast.SelectorExpr:
		pkg, ok := expression.X.(*ast.Ident)
		if !ok {
			return function{}, false
		}
		target, ok := file.imports[pkg.Name]
		if !ok {
			return function{}, false
		}
		fn, ok := s.functions[functionKey(target, expression.Sel.Name)]
		return fn, ok
	}
	return function{}, false
}

// functionKey identifies a package-level function by its project-relative
// directory ("." for the root package) and name.
func functionKey(dir, name string) string {
	return dir + ":" + name
}

func parameterNames(declaration *ast.FuncDecl) []string {
	var names []string
	for _, field := range declaration.Type.Params.List {
		if len(field.Names) == 0 {
			names = append(names, "_")
			continue
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

// joinPath mirrors Fiber's group path joining.
func joinPath(prefix, path string) string {
	if path == "" || path == "/" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(prefix, "/") + path
}

func literal(expression ast.Expr) string {
	if basic, ok := expression.(*ast.BasicLit); ok && basic.Kind == token.STRING {
		if value, err := strconv.Unquote(basic.Value); err == nil {
			return value
		}
	}
	return "{dynamic}"
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

func handlerLabel(expression ast.Expr, constructors map[string]string) string {
	switch expression := expression.(type) {
	case *ast.FuncLit:
		return "inline func"
	case *ast.SelectorExpr:
		if receiver, ok := expression.X.(*ast.Ident); ok {
			if constructed, ok := constructors[receiver.Name]; ok {
				return constructed + "." + expression.Sel.Name
			}
			return receiver.Name + "." + expression.Sel.Name
		}
		return expression.Sel.Name
	case *ast.Ident:
		return expression.Name
	case *ast.CallExpr:
		return calleeName(expression) + "(...)"
	}
	return "?"
}

func readModule(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("module declaration not found in %s", path)
}
