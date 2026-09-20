package generator

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var methodPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

type MethodOptions struct {
	Folder     string
	Name       string
	HTTPMethod string
	Path       string
}

type MethodGenerator struct{}

func (MethodGenerator) Generate(projectPath string, options MethodOptions) error {
	if err := validateName("folder", options.Folder); err != nil {
		return err
	}
	if !methodPattern.MatchString(options.Name) {
		return fmt.Errorf("invalid method name %q: use an exported Go name such as Approve", options.Name)
	}
	options.HTTPMethod = strings.ToUpper(options.HTTPMethod)
	if options.HTTPMethod == "" {
		options.HTTPMethod = "POST"
	}
	if !contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, options.HTTPMethod) {
		return fmt.Errorf("unsupported HTTP method %q", options.HTTPMethod)
	}
	if options.Path == "" {
		options.Path = "/" + kebabCase(options.Name)
	}
	if !strings.HasPrefix(options.Path, "/") {
		options.Path = "/" + options.Path
	}
	module, err := readModuleName(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(projectPath, "handlers", options.Folder)); os.IsNotExist(err) {
		return scaffoldMethodFeature(projectPath, module, options)
	} else if err != nil {
		return err
	}
	return extendMethodFeature(projectPath, module, options)
}

func scaffoldMethodFeature(projectPath, module string, options MethodOptions) error {
	entity := singularTypeName(options.Folder)
	data := struct{ Module, Folder, Entity, Method, Verb, Path string }{module, options.Folder, entity, options.Name, fiberMethodName(options.HTTPMethod), options.Path}
	files := map[string]string{
		filepath.Join("repositories", options.Folder, "interface.go"):       methodRepoInterface,
		filepath.Join("repositories", options.Folder, options.Folder+".go"): methodRepo,
		filepath.Join("services", options.Folder, "interface.go"):           methodServiceInterface,
		filepath.Join("services", options.Folder, options.Folder+".go"):     methodService,
		filepath.Join("handlers", options.Folder, "interface.go"):           methodHandlerInterface,
		filepath.Join("handlers", options.Folder, options.Folder+".go"):     methodHandler,
		filepath.Join("routes", options.Folder+".go"):                       methodRoute,
	}
	for path, source := range files {
		content, err := renderAny(path, source, data)
		if err != nil {
			return err
		}
		if err := writeNewFile(filepath.Join(projectPath, path), content); err != nil {
			return err
		}
	}
	return registerRoute(filepath.Join(projectPath, "routes", "root.go"), entity)
}

func extendMethodFeature(projectPath, module string, options MethodOptions) error {
	layers := []struct{ dir, suffix string }{{"repositories", "Repository"}, {"services", "Service"}, {"handlers", "Handler"}}
	for _, layer := range layers {
		interfacePath := filepath.Join(projectPath, layer.dir, options.Folder, "interface.go")
		implPath := filepath.Join(projectPath, layer.dir, options.Folder, options.Folder+".go")
		if err := addInterfaceMethod(interfacePath, layer.suffix, options.Name); err != nil {
			return err
		}
		if err := addImplementationMethod(implPath, layer.dir, module, options.Name); err != nil {
			return err
		}
	}
	return addRouteMethod(filepath.Join(projectPath, "routes", options.Folder+".go"), options)
}

func parseGoFile(path string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return fset, file, nil
}

func writeGoFile(path string, fset *token.FileSet, file *ast.File) error {
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := format.Node(output, fset, file); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func addInterfaceMethod(path, suffix, method string) error {
	fset, file, err := parseGoFile(path)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(typeSpec.Name.Name, suffix) {
				continue
			}
			iface, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			for _, field := range iface.Methods.List {
				if len(field.Names) > 0 && field.Names[0].Name == method {
					return fmt.Errorf("method %s already exists in %s", method, path)
				}
			}
			var methodType *ast.FuncType
			if suffix == "Handler" {
				methodType = &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{ast.NewIdent("c")}, Type: &ast.StarExpr{X: &ast.SelectorExpr{X: ast.NewIdent("fiber"), Sel: ast.NewIdent("Ctx")}}}}}, Results: errorResults()}
			} else {
				methodType = &ast.FuncType{Params: &ast.FieldList{}, Results: errorResults()}
			}
			iface.Methods.List = append(iface.Methods.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(method)}, Type: methodType})
			return writeGoFile(path, fset, file)
		}
	}
	return fmt.Errorf("%s interface not found in %s", suffix, path)
}

func errorResults() *ast.FieldList {
	return &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("error")}}}
}

func addImplementationMethod(path, layer, module, method string) error {
	fset, file, err := parseGoFile(path)
	if err != nil {
		return err
	}
	receiver, field := "", ""
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
			receiver = receiverType(fn)
			break
		}
	}
	if receiver == "" {
		return fmt.Errorf("implementation receiver not found in %s", path)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != receiver {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if ok && len(st.Fields.List) > 0 && len(st.Fields.List[0].Names) > 0 {
				field = st.Fields.List[0].Names[0].Name
			}
		}
	}
	if field == "" {
		field = receiverFieldFromFile(filepath.Join(filepath.Dir(path), "interface.go"), receiver)
	}
	receiverVar := receiver[:1]
	if field == "" {
		switch layer {
		case "services":
			field = "repo"
		case "handlers":
			field = "srv"
		}
	}
	var body string
	switch layer {
	case "repositories":
		body = fmt.Sprintf("func (%s *%s) %s() error { panic(\"TODO: implement %s\") }", receiverVar, receiver, method, method)
	case "services":
		body = fmt.Sprintf("func (%s *%s) %s() error { return %s.%s.%s() }", receiverVar, receiver, method, receiverVar, field, method)
	default:
		ensureImport(file, module+"/pkg/response")
		body = fmt.Sprintf("func (%s *%s) %s(c *fiber.Ctx) error { if err := %s.%s.%s(); err != nil { return err }; return response.Send(c, fiber.StatusOK, nil) }", receiverVar, receiver, method, receiverVar, field, method)
	}
	parsed, err := parser.ParseFile(fset, "snippet.go", "package "+file.Name.Name+"\n"+body, 0)
	if err != nil {
		return err
	}
	file.Decls = append(file.Decls, parsed.Decls[0])
	return writeGoFile(path, fset, file)
}

func receiverFieldFromFile(path, receiver string) string {
	_, file, err := parseGoFile(path)
	if err != nil {
		return ""
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != receiver {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if ok && len(structure.Fields.List) > 0 && len(structure.Fields.List[0].Names) > 0 {
				return structure.Fields.List[0].Names[0].Name
			}
		}
	}
	return ""
}

func ensureImport(file *ast.File, importPath string) {
	for _, value := range file.Imports {
		if strings.Trim(value.Path.Value, "\"") == importPath {
			return
		}
	}
	spec := &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", importPath)}}
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			gen.Specs = append(gen.Specs, spec)
			file.Imports = append(file.Imports, spec)
			return
		}
	}
	decl := &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}
	file.Decls = append([]ast.Decl{decl}, file.Decls...)
	file.Imports = append(file.Imports, spec)
}

func receiverType(fn *ast.FuncDecl) string {
	if len(fn.Recv.List) == 0 {
		return ""
	}
	switch value := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := value.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return value.Name
	}
	return ""
}

func addRouteMethod(path string, options MethodOptions) error {
	fset, file, err := parseGoFile(path)
	if err != nil {
		return err
	}
	verb := fiberMethodName(options.HTTPMethod)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "Register") {
			continue
		}
		groupName, handlerName := "routeGroup", "hdr"
		for _, statement := range fn.Body.List {
			assignment, ok := statement.(*ast.AssignStmt)
			if !ok || len(assignment.Lhs) == 0 || len(assignment.Rhs) == 0 {
				continue
			}
			left, ok := assignment.Lhs[0].(*ast.Ident)
			if !ok {
				continue
			}
			call, ok := assignment.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if selector.Sel.Name == "Group" {
				groupName = left.Name
			}
			if strings.HasPrefix(selector.Sel.Name, "New") && strings.HasSuffix(selector.Sel.Name, "Handler") {
				handlerName = left.Name
			}
		}
		statement := &ast.ExprStmt{X: &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent(groupName), Sel: ast.NewIdent(verb)},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", options.Path)}, &ast.SelectorExpr{X: ast.NewIdent(handlerName), Sel: ast.NewIdent(options.Name)}},
		}}
		fn.Body.List = append(fn.Body.List, statement)
		return writeGoFile(path, fset, file)
	}
	return fmt.Errorf("route registration function not found in %s", path)
}

func fiberMethodName(method string) string {
	method = strings.ToLower(method)
	if method == "" {
		return ""
	}
	return strings.ToUpper(method[:1]) + method[1:]
}

func kebabCase(value string) string { return strings.ReplaceAll(toSnakeCase(value), "_", "-") }

const methodRepoInterface = `package {{.Folder}}
import "gorm.io/gorm"
type {{.Entity}}Repository interface { {{.Method}}() error }
type repository struct { db *gorm.DB }
func New{{.Entity}}Repository(db *gorm.DB) {{.Entity}}Repository { return &repository{db:db} }
`
const methodRepo = `package {{.Folder}}
func (r *repository) {{.Method}}() error { panic("TODO: implement {{.Method}}") }
`
const methodServiceInterface = `package {{.Folder}}
import repository "{{.Module}}/repositories/{{.Folder}}"
type {{.Entity}}Service interface { {{.Method}}() error }
type service struct { repository repository.{{.Entity}}Repository }
func New{{.Entity}}Service(repository repository.{{.Entity}}Repository) {{.Entity}}Service { return &service{repository:repository} }
`
const methodService = `package {{.Folder}}
func (s *service) {{.Method}}() error { return s.repository.{{.Method}}() }
`
const methodHandlerInterface = `package {{.Folder}}
import (
 "github.com/gofiber/fiber/v2"
 service "{{.Module}}/services/{{.Folder}}"
)
type {{.Entity}}Handler interface { {{.Method}}(c *fiber.Ctx) error }
type handler struct { service service.{{.Entity}}Service }
func New{{.Entity}}Handler(service service.{{.Entity}}Service) {{.Entity}}Handler { return &handler{service:service} }
`
const methodHandler = `package {{.Folder}}
import (
 "github.com/gofiber/fiber/v2"
 "{{.Module}}/pkg/response"
)
func (h *handler) {{.Method}}(c *fiber.Ctx) error { if err:=h.service.{{.Method}}(); err!=nil{return err}; return response.Send(c,fiber.StatusOK,nil) }
`
const methodRoute = `package routes
import (
 handler "{{.Module}}/handlers/{{.Folder}}"
 repository "{{.Module}}/repositories/{{.Folder}}"
 service "{{.Module}}/services/{{.Folder}}"
 "github.com/gofiber/fiber/v2"
 "gorm.io/gorm"
)
func Register{{.Entity}}Routes(db *gorm.DB, route fiber.Router) {
 repo:=repository.New{{.Entity}}Repository(db); svc:=service.New{{.Entity}}Service(repo); h:=handler.New{{.Entity}}Handler(svc)
 group:=route.Group("/{{.Folder}}"); group.{{.Verb}}("{{.Path}}",h.{{.Method}})
}
`
