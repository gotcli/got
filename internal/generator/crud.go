package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/gotcli/got/internal/dbschema"
)

type crudData struct {
	Module        string
	Package       string
	Table         string
	Schema        string
	Entity        string
	PrimaryKey    string
	PrimaryColumn string
	PrimaryType   string
	Fields        string
	Imports       string
	ParseID       string
}

func (APIGenerator) GenerateCRUD(projectPath string, database dbschema.Database) error {
	module, err := readModuleName(filepath.Join(projectPath, "go.mod"))
	if err != nil {
		return err
	}
	packages := make(map[string]string)
	entities := make(map[string]string)
	items := make([]crudData, 0, len(database.Tables))
	for _, table := range database.Tables {
		if len(table.PrimaryKey) != 1 {
			return fmt.Errorf("table %s must have exactly one primary key; found %d", table.Name, len(table.PrimaryKey))
		}
		packageName := toSnakeCase(table.Name)
		if err := validateName("generated package name", packageName); err != nil {
			return err
		}
		if previous, exists := packages[packageName]; exists {
			return fmt.Errorf("tables %q and %q map to the same Go package %q", previous, table.Name, packageName)
		}
		packages[packageName] = table.Name
		primaryColumn, ok := findColumn(table, table.PrimaryKey[0])
		if !ok {
			return fmt.Errorf("primary key column %s.%s not found", table.Name, table.PrimaryKey[0])
		}
		entityName := singularTypeName(packageName)
		if previous, exists := entities[entityName]; exists {
			return fmt.Errorf("tables %q and %q map to the same Go entity %q", previous, table.Name, entityName)
		}
		entities[entityName] = table.Name
		primaryType := baseGoType(primaryColumn)
		if primaryType != "int" && primaryType != "int64" {
			return fmt.Errorf("table %s primary key must be an integer; found %s", table.Name, primaryColumn.DataType)
		}
		fields, imports := entityFields(table)
		data := crudData{
			Module: module, Package: packageName, Table: table.Name, Schema: database.Schema,
			Entity: entityName, PrimaryKey: goFieldName(primaryColumn.Name), PrimaryColumn: primaryColumn.Name,
			PrimaryType: primaryType, Fields: fields, Imports: imports,
			ParseID: parseID(primaryType),
		}
		items = append(items, data)
	}
	for _, data := range items {
		if err := preflightCRUDTable(projectPath, data); err != nil {
			return err
		}
	}
	for _, data := range items {
		if err := generateCRUDTable(projectPath, data); err != nil {
			return err
		}
	}
	return nil
}

func preflightCRUDTable(projectPath string, data crudData) error {
	if err := validateRouteRegistry(filepath.Join(projectPath, "routes", "root.go"), data.Entity); err != nil {
		return err
	}
	for path := range crudFiles(data) {
		fullPath := filepath.Join(projectPath, path)
		if _, err := os.Stat(fullPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", fullPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", fullPath, err)
		}
	}
	return nil
}

func generateCRUDTable(projectPath string, data crudData) error {
	rootRoutePath := filepath.Join(projectPath, "routes", "root.go")
	if err := validateRouteRegistry(rootRoutePath, data.Entity); err != nil {
		return err
	}
	files := crudFiles(data)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		content, err := renderCRUD(path, files[path], data)
		if err != nil {
			return err
		}
		if err := writeNewFile(filepath.Join(projectPath, path), content); err != nil {
			return err
		}
	}
	return registerRoute(rootRoutePath, data.Entity)
}

func crudFiles(data crudData) map[string]string {
	return map[string]string{
		filepath.Join("entities", data.Package, data.Package+".go"):     crudEntity,
		filepath.Join("repositories", data.Package, "interface.go"):     crudRepositoryInterface,
		filepath.Join("repositories", data.Package, data.Package+".go"): crudRepository,
		filepath.Join("services", data.Package, "interface.go"):         crudServiceInterface,
		filepath.Join("services", data.Package, data.Package+".go"):     crudService,
		filepath.Join("handlers", data.Package, "interface.go"):         crudHandlerInterface,
		filepath.Join("handlers", data.Package, data.Package+".go"):     crudHandler,
		filepath.Join("routes", data.Package+".go"):                     crudRoute,
	}
}

func findColumn(table dbschema.Table, name string) (dbschema.Column, bool) {
	for _, column := range table.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return dbschema.Column{}, false
}

func entityFields(table dbschema.Table) (string, string) {
	imports := make(map[string]bool)
	var fields strings.Builder
	for _, column := range table.Columns {
		goType, requiredImport := columnGoType(column)
		if requiredImport != "" {
			imports[requiredImport] = true
		}
		if column.Nullable {
			goType = "*" + goType
		}
		gormTags := []string{"column:" + column.Name}
		if contains(table.PrimaryKey, column.Name) {
			gormTags = append(gormTags, "primaryKey")
		}
		fmt.Fprintf(&fields, "\t%s %s `json:\"%s\" gorm:\"%s\"`\n", goFieldName(column.Name), goType, column.Name, strings.Join(gormTags, ";"))
	}
	keys := make([]string, 0, len(imports))
	for value := range imports {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	var importBlock string
	if len(keys) > 0 {
		quoted := make([]string, len(keys))
		for i, value := range keys {
			quoted[i] = fmt.Sprintf("\t%q", value)
		}
		importBlock = "import (\n" + strings.Join(quoted, "\n") + "\n)\n"
	}
	return fields.String(), importBlock
}

func columnGoType(column dbschema.Column) (string, string) {
	switch column.DataType {
	case "smallint", "integer", "int", "tinyint":
		return "int", ""
	case "bigint":
		return "int64", ""
	case "numeric", "decimal", "money", "smallmoney":
		return "decimal.Decimal", "github.com/shopspring/decimal"
	case "real", "double precision", "float":
		return "float64", ""
	case "boolean", "bit":
		return "bool", ""
	case "timestamp without time zone", "timestamp with time zone", "date", "datetime", "datetime2", "smalldatetime", "datetimeoffset", "time":
		return "time.Time", "time"
	case "json", "jsonb":
		return "json.RawMessage", "encoding/json"
	case "ARRAY":
		return "[]string", ""
	case "binary", "varbinary", "image", "rowversion", "timestamp":
		return "[]byte", ""
	default:
		return "string", ""
	}
}

func baseGoType(column dbschema.Column) string {
	goType, _ := columnGoType(column)
	return goType
}

func parseID(goType string) string {
	switch goType {
	case "int":
		return "parsed, err := strconv.Atoi(c.Params(\"id\"))\n\tif err != nil {\n\t\treturn fiber.NewError(fiber.StatusBadRequest, \"invalid id\")\n\t}\n\tid := parsed"
	case "int64":
		return "id, err := strconv.ParseInt(c.Params(\"id\"), 10, 64)\n\tif err != nil {\n\t\treturn fiber.NewError(fiber.StatusBadRequest, \"invalid id\")\n\t}"
	default:
		return "id := c.Params(\"id\")"
	}
}

func goFieldName(value string) string {
	initialisms := map[string]string{
		"api": "API", "db": "DB", "http": "HTTP", "https": "HTTPS", "id": "ID",
		"ip": "IP", "json": "JSON", "qr": "QR", "sql": "SQL", "url": "URL", "uuid": "UUID",
	}
	parts := strings.Split(toSnakeCase(value), "_")
	for i, part := range parts {
		if initialism, ok := initialisms[strings.ToLower(part)]; ok {
			parts[i] = initialism
			continue
		}
		parts[i] = typeName(part)
	}
	return strings.Join(parts, "")
}

func toSnakeCase(value string) string {
	var output []rune
	var previous rune
	for index, current := range []rune(strings.TrimSpace(value)) {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			if unicode.IsUpper(current) && index > 0 && previous != '_' && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
				output = append(output, '_')
			}
			output = append(output, unicode.ToLower(current))
			previous = current
			continue
		}
		if len(output) > 0 && output[len(output)-1] != '_' {
			output = append(output, '_')
		}
		previous = '_'
	}
	return strings.Trim(string(output), "_")
}

func singularTypeName(table string) string {
	parts := strings.Split(table, "_")
	last := parts[len(parts)-1]
	switch {
	case strings.HasSuffix(last, "ies") && len(last) > 3:
		last = strings.TrimSuffix(last, "ies") + "y"
	case strings.HasSuffix(last, "ses") && len(last) > 3:
		last = strings.TrimSuffix(last, "es")
	case strings.HasSuffix(last, "s") && !strings.HasSuffix(last, "ss"):
		last = strings.TrimSuffix(last, "s")
	}
	parts[len(parts)-1] = last
	return typeName(strings.Join(parts, "_"))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func renderCRUD(name, source string, data crudData) ([]byte, error) {
	return renderAny(name, source, data)
}

const crudEntity = `package {{ .Package }}

{{ .Imports }}
type {{ .Entity }} struct {
{{ .Fields }}}

func ({{ .Entity }}) TableName() string { return "{{ .Schema }}.{{ .Table }}" }
`

const crudRepositoryInterface = `package {{ .Package }}

import (
	"context"
	entity "{{ .Module }}/entities/{{ .Package }}"
)

type {{ .Entity }}Repository interface {
	List(ctx context.Context) ([]entity.{{ .Entity }}, error)
	Get(ctx context.Context, id {{ .PrimaryType }}) (entity.{{ .Entity }}, error)
	Create(ctx context.Context, item *entity.{{ .Entity }}) error
	Update(ctx context.Context, item *entity.{{ .Entity }}) error
	Delete(ctx context.Context, id {{ .PrimaryType }}) error
}
`

const crudRepository = `package {{ .Package }}

import (
	"context"
	entity "{{ .Module }}/entities/{{ .Package }}"
	"gorm.io/gorm"
)

type repository struct { db *gorm.DB }

func New{{ .Entity }}Repository(db *gorm.DB) {{ .Entity }}Repository { return &repository{db: db} }

func (r *repository) List(ctx context.Context) ([]entity.{{ .Entity }}, error) {
	var items []entity.{{ .Entity }}
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

func (r *repository) Get(ctx context.Context, id {{ .PrimaryType }}) (entity.{{ .Entity }}, error) {
	var item entity.{{ .Entity }}
	err := r.db.WithContext(ctx).First(&item, "{{ .PrimaryColumn }} = ?", id).Error
	return item, err
}

func (r *repository) Create(ctx context.Context, item *entity.{{ .Entity }}) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *repository) Update(ctx context.Context, item *entity.{{ .Entity }}) error {
	return r.db.WithContext(ctx).Save(item).Error
}

func (r *repository) Delete(ctx context.Context, id {{ .PrimaryType }}) error {
	return r.db.WithContext(ctx).Delete(&entity.{{ .Entity }}{}, "{{ .PrimaryColumn }} = ?", id).Error
}
`

const crudServiceInterface = `package {{ .Package }}

import (
	"context"
	entity "{{ .Module }}/entities/{{ .Package }}"
)

type {{ .Entity }}Service interface {
	List(ctx context.Context) ([]entity.{{ .Entity }}, error)
	Get(ctx context.Context, id {{ .PrimaryType }}) (entity.{{ .Entity }}, error)
	Create(ctx context.Context, item *entity.{{ .Entity }}) error
	Update(ctx context.Context, item *entity.{{ .Entity }}) error
	Delete(ctx context.Context, id {{ .PrimaryType }}) error
}
`

const crudService = `package {{ .Package }}

import (
	"context"
	"errors"
	"time"
	entity "{{ .Module }}/entities/{{ .Package }}"
	"{{ .Module }}/pkg/logger"
	repository "{{ .Module }}/repositories/{{ .Package }}"
)

type service struct { repository repository.{{ .Entity }}Repository }

func New{{ .Entity }}Service(repository repository.{{ .Entity }}Repository) {{ .Entity }}Service {
	return &service{repository: repository}
}
func (s *service) List(ctx context.Context) (items []entity.{{ .Entity }}, err error) {
	started:=time.Now(); defer func(){ logger.ServiceResult(ctx,"{{ .Package }}","list",started,items,err) }()
	return s.repository.List(ctx)
}
func (s *service) Get(ctx context.Context, id {{ .PrimaryType }}) (item entity.{{ .Entity }}, err error) {
	started:=time.Now(); defer func(){ logger.ServiceResult(ctx,"{{ .Package }}","get",started,item,err) }()
	return s.repository.Get(ctx,id)
}
func (s *service) Create(ctx context.Context, item *entity.{{ .Entity }}) (err error) {
	started:=time.Now(); defer func(){ logger.ServiceResult(ctx,"{{ .Package }}","create",started,item,err) }()
	if item == nil { return errors.New("item is required") }; return s.repository.Create(ctx,item)
}
func (s *service) Update(ctx context.Context, item *entity.{{ .Entity }}) (err error) {
	started:=time.Now(); defer func(){ logger.ServiceResult(ctx,"{{ .Package }}","update",started,item,err) }()
	if item == nil { return errors.New("item is required") }; return s.repository.Update(ctx,item)
}
func (s *service) Delete(ctx context.Context, id {{ .PrimaryType }}) (err error) {
	started:=time.Now(); defer func(){ logger.ServiceResult(ctx,"{{ .Package }}","delete",started,nil,err) }()
	return s.repository.Delete(ctx,id)
}
`

const crudHandlerInterface = `package {{ .Package }}

import "github.com/gofiber/fiber/v2"

type {{ .Entity }}Handler interface {
	List(c *fiber.Ctx) error
	Get(c *fiber.Ctx) error
	Create(c *fiber.Ctx) error
	Update(c *fiber.Ctx) error
	Delete(c *fiber.Ctx) error
}
`

const crudHandler = `package {{ .Package }}

import (
	"errors"
	"strconv"
	entity "{{ .Module }}/entities/{{ .Package }}"
	"{{ .Module }}/pkg/response"
	service "{{ .Module }}/services/{{ .Package }}"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type handler struct { service service.{{ .Entity }}Service }
func New{{ .Entity }}Handler(service service.{{ .Entity }}Service) {{ .Entity }}Handler { return &handler{service: service} }

func (h *handler) List(c *fiber.Ctx) error {
	items, err := h.service.List(c.UserContext())
	if err != nil { return err }
	return response.Send(c, fiber.StatusOK, items)
}
func (h *handler) Get(c *fiber.Ctx) error {
	{{ .ParseID }}
	item, err := h.service.Get(c.UserContext(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) { return fiber.ErrNotFound }
	if err != nil { return err }
	return response.Send(c, fiber.StatusOK, item)
}
func (h *handler) Create(c *fiber.Ctx) error {
	var item entity.{{ .Entity }}
	if err := c.BodyParser(&item); err != nil { return fiber.NewError(fiber.StatusBadRequest, "invalid request body") }
	if err := h.service.Create(c.UserContext(), &item); err != nil { return err }
	return response.Send(c, fiber.StatusCreated, item)
}
func (h *handler) Update(c *fiber.Ctx) error {
	{{ .ParseID }}
	var item entity.{{ .Entity }}
	if err := c.BodyParser(&item); err != nil { return fiber.NewError(fiber.StatusBadRequest, "invalid request body") }
	item.{{ .PrimaryKey }} = id
	if err := h.service.Update(c.UserContext(), &item); err != nil { return err }
	return response.Send(c, fiber.StatusOK, item)
}
func (h *handler) Delete(c *fiber.Ctx) error {
	{{ .ParseID }}
	if err := h.service.Delete(c.UserContext(), id); err != nil { return err }
	return response.Send(c, fiber.StatusOK, nil)
}
`

const crudRoute = `package routes

import (
	handler "{{ .Module }}/handlers/{{ .Package }}"
	repository "{{ .Module }}/repositories/{{ .Package }}"
	service "{{ .Module }}/services/{{ .Package }}"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func Register{{ .Entity }}Routes(db *gorm.DB, route fiber.Router) {
	repo := repository.New{{ .Entity }}Repository(db)
	svc := service.New{{ .Entity }}Service(repo)
	h := handler.New{{ .Entity }}Handler(svc)
	group := route.Group("/{{ .Table }}")
	group.Get("/", h.List)
	group.Get("/:id", h.Get)
	group.Post("/", h.Create)
	group.Put("/:id", h.Update)
	group.Delete("/:id", h.Delete)
}
`
