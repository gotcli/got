package generator

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/gotcli/got/internal/dbschema"
)

type hexagonalCRUDData struct {
	crudData
	Lower            string // unexported Go prefix, e.g. orderItem
	ModelImports     string
	MigrationVersion int
	MigrationName    string
	Statements       []string // Go string literals
}

type hexagonalEdit struct {
	path, marker, line string
}

// GenerateHexagonalCRUD writes one entity per table through every hexagonal
// layer and registers it in the port registries and the HTTP routes.
func GenerateHexagonalCRUD(projectPath string, database dbschema.Database) error {
	files, edits, err := planHexagonalCRUD(projectPath, database)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := writeNewFile(filepath.Join(projectPath, path), files[path]); err != nil {
			return err
		}
	}
	return applyHexagonalEdits(projectPath, edits, true)
}

// PlanHexagonalCRUD returns the files GenerateHexagonalCRUD would create and the
// registries it would edit, after running every check.
func PlanHexagonalCRUD(projectPath string, database dbschema.Database) ([]string, error) {
	files, edits, err := planHexagonalCRUD(projectPath, database)
	if err != nil {
		return nil, err
	}
	planned := make([]string, 0, len(files)+len(edits))
	for path := range files {
		planned = append(planned, path)
	}
	sort.Strings(planned)
	seen := map[string]bool{}
	for _, edit := range edits {
		if !seen[edit.path] {
			seen[edit.path] = true
			planned = append(planned, edit.path+" (register)")
		}
	}
	return planned, nil
}

func planHexagonalCRUD(projectPath string, database dbschema.Database) (map[string][]byte, []hexagonalEdit, error) {
	items, err := buildCRUDData(projectPath, database)
	if err != nil {
		return nil, nil, err
	}
	version, err := nextMigrationVersion(filepath.Join(projectPath, "internal", "migration", "postgres"))
	if err != nil {
		return nil, nil, err
	}
	files := map[string][]byte{}
	var edits []hexagonalEdit
	tables := make(map[string]dbschema.Table, len(database.Tables))
	for _, table := range database.Tables {
		tables[table.Name] = table
	}
	for _, item := range items {
		table := tables[item.Table]
		statements, err := postgresCreateTable(database.Schema, table)
		if err != nil {
			return nil, nil, err
		}
		data := hexagonalCRUDData{
			crudData: item, Lower: lowerFirst(item.Entity), ModelImports: modelImports(table, item.RequestFields),
			MigrationVersion: version, MigrationName: "create_" + item.Package,
		}
		for _, statement := range statements {
			data.Statements = append(data.Statements, goRawString(statement))
		}
		version++
		for path, source := range hexagonalCRUDFiles(data) {
			if _, exists := files[path]; exists {
				return nil, nil, fmt.Errorf("two tables generate the same file %s", path)
			}
			if _, err := os.Stat(filepath.Join(projectPath, path)); err == nil {
				return nil, nil, fmt.Errorf("refusing to overwrite existing file: %s", filepath.Join(projectPath, path))
			} else if !os.IsNotExist(err) {
				return nil, nil, err
			}
			content, err := renderAny(path, source, data)
			if err != nil {
				return nil, nil, err
			}
			files[path] = content
		}
		edits = append(edits,
			hexagonalEdit{"internal/port/inbound/registry_service.go", hexagonalInboundMarker, item.Entity + "Services"},
			hexagonalEdit{"internal/port/outbound/registry_repository.go", hexagonalOutboundMarker, item.Entity + "Repositories"},
			hexagonalEdit{"internal/adapter/inbound/fiber/routes.go", hexagonalRoutesMarker, fmt.Sprintf("register%sRoutes(api, services.%s())", item.Entity, item.Entity)},
		)
	}
	if err := applyHexagonalEdits(projectPath, edits, false); err != nil {
		return nil, nil, err
	}
	return files, edits, nil
}

// applyHexagonalEdits inserts each line above its marker. With write false it
// only checks that every marker exists and the line is not registered yet.
func applyHexagonalEdits(projectPath string, edits []hexagonalEdit, write bool) error {
	byPath := map[string][]hexagonalEdit{}
	var order []string
	for _, edit := range edits {
		if _, ok := byPath[edit.path]; !ok {
			order = append(order, edit.path)
		}
		byPath[edit.path] = append(byPath[edit.path], edit)
	}
	for _, path := range order {
		fullPath := filepath.Join(projectPath, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		source := string(content)
		for _, edit := range byPath[path] {
			if strings.Count(source, edit.marker) != 1 {
				return fmt.Errorf("%s must contain the marker %q exactly once", path, edit.marker)
			}
			if strings.Contains(source, edit.line) {
				return fmt.Errorf("%s already registers %s", path, edit.line)
			}
			source = strings.Replace(source, edit.marker, edit.line+"\n\t"+edit.marker, 1)
		}
		if !write {
			continue
		}
		formatted, err := format.Source([]byte(source))
		if err != nil {
			return fmt.Errorf("format %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, formatted, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

var migrationFilePattern = regexp.MustCompile(`^(\d+)_.+\.go$`)

func nextMigrationVersion(directory string) (int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, fmt.Errorf("read migrations: %w", err)
	}
	highest := 0
	for _, entry := range entries {
		if match := migrationFilePattern.FindStringSubmatch(entry.Name()); match != nil {
			if version, err := strconv.Atoi(match[1]); err == nil && version > highest {
				highest = version
			}
		}
	}
	return highest + 1, nil
}

func lowerFirst(value string) string {
	runes := []rune(value)
	for i := 0; i < len(runes) && unicode.IsUpper(runes[i]); i++ {
		// Lower a leading initialism as a whole: "APIKey" -> "apiKey".
		if i > 0 && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			break
		}
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

func modelImports(table dbschema.Table, fields []crudField) string {
	imports := map[string]bool{}
	for _, column := range table.Columns {
		if _, required := columnGoType(column); required != "" {
			imports[required] = true
		}
	}
	for _, field := range fields {
		if field.Required && field.String {
			imports["strings"] = true
		}
	}
	keys := make([]string, 0, len(imports))
	for value := range imports {
		keys = append(keys, strconv.Quote(value))
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return "import (\n\t" + strings.Join(keys, "\n\t") + "\n)\n"
}

func goRawString(value string) string {
	if !strings.Contains(value, "`") {
		return "`" + value + "`"
	}
	return strconv.Quote(value)
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// postgresCreateTable reconstructs idempotent DDL for an introspected table:
// enum types, columns with types, defaults, nullability, and the primary key.
// Unique constraints, foreign keys, checks, and indexes are not introspected.
func postgresCreateTable(schema string, table dbschema.Table) ([]string, error) {
	var statements []string
	qualified := quoteIdentifier(schema) + "." + quoteIdentifier(table.Name)
	var columns []string
	for _, column := range table.Columns {
		if column.DataType == "USER-DEFINED" && len(column.EnumValues) > 0 {
			values := make([]string, len(column.EnumValues))
			for i, value := range column.EnumValues {
				values[i] = quoteLiteral(value)
			}
			statements = append(statements, fmt.Sprintf(
				"DO $$ BEGIN\n\tCREATE TYPE %s.%s AS ENUM (%s);\nEXCEPTION WHEN duplicate_object THEN NULL;\nEND $$",
				quoteIdentifier(schema), quoteIdentifier(column.UDTName), strings.Join(values, ", ")))
		}
		definition, err := postgresColumnDefinition(schema, column)
		if err != nil {
			return nil, fmt.Errorf("table %s: %w", table.Name, err)
		}
		columns = append(columns, "\t"+quoteIdentifier(column.Name)+" "+definition)
	}
	if len(table.PrimaryKey) > 0 {
		keys := make([]string, len(table.PrimaryKey))
		for i, key := range table.PrimaryKey {
			keys[i] = quoteIdentifier(key)
		}
		columns = append(columns, "\tPRIMARY KEY ("+strings.Join(keys, ", ")+")")
	}
	statements = append(statements, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n)", qualified, strings.Join(columns, ",\n")))
	return statements, nil
}

func postgresColumnDefinition(schema string, column dbschema.Column) (string, error) {
	sequence := strings.HasPrefix(column.DefaultExpr, "nextval(")
	var columnType string
	switch {
	case sequence:
		serial := map[string]string{"smallint": "smallserial", "integer": "serial", "bigint": "bigserial"}[column.DataType]
		if serial == "" {
			return "", fmt.Errorf("column %s uses a sequence default on unsupported type %s", column.Name, column.DataType)
		}
		columnType = serial
	case column.DataType == "USER-DEFINED":
		columnType = quoteIdentifier(schema) + "." + quoteIdentifier(column.UDTName)
	case column.DataType == "ARRAY":
		columnType = strings.TrimPrefix(column.UDTName, "_") + "[]"
	case column.DataType == "character varying" && column.CharLength > 0:
		columnType = fmt.Sprintf("varchar(%d)", column.CharLength)
	case column.DataType == "character" && column.CharLength > 0:
		columnType = fmt.Sprintf("char(%d)", column.CharLength)
	case column.DataType == "numeric" && column.NumericPrecision > 0:
		columnType = fmt.Sprintf("numeric(%d,%d)", column.NumericPrecision, column.NumericScale)
	default:
		columnType = column.DataType
	}
	parts := []string{columnType}
	if column.Identity {
		parts = append(parts, "GENERATED BY DEFAULT AS IDENTITY")
	} else if column.DefaultExpr != "" && !sequence {
		parts = append(parts, "DEFAULT "+column.DefaultExpr)
	}
	if !column.Nullable {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " "), nil
}

func hexagonalCRUDFiles(data hexagonalCRUDData) map[string]string {
	p := data.Package
	return map[string]string{
		"internal/model/" + p + ".go":                                                 hexModel,
		"internal/port/inbound/" + p + ".go":                                          hexInboundPort,
		"internal/port/inbound/registry_" + p + ".go":                                 hexInboundRegistry,
		"internal/port/outbound/" + p + ".go":                                         hexOutboundPort,
		"internal/port/outbound/registry_" + p + ".go":                                hexOutboundRegistry,
		"internal/domain/" + p + "/domain.go":                                         hexDomain,
		"internal/domain/" + p + "/domain_test.go":                                    hexDomainTest,
		"internal/domain/registry_" + p + ".go":                                       hexDomainRegistry,
		"internal/adapter/outbound/postgres/" + p + ".go":                             hexRepository,
		"internal/adapter/inbound/fiber/" + p + ".go":                                 hexHandler,
		fmt.Sprintf("internal/migration/postgres/%d_%s.go", data.MigrationVersion, p): hexMigration,
	}
}

const hexModel = `package model

{{ .ModelImports }}
type {{ .Entity }} struct {
{{ .Fields }}}

func ({{ .Entity }}) TableName() string { return "{{ .Schema }}.{{ .Table }}" }

// Create{{ .Entity }}Request lists the columns a client may set. Generated keys
// and created_at/updated_at/deleted_at are managed by the server.
type Create{{ .Entity }}Request struct {
{{- range .RequestFields }}
	{{ .Name }} *{{ .Type }} ` + "`json:\"{{ .JSON }}\"`" + `
{{- end }}
}

func (r Create{{ .Entity }}Request) Validate() map[string]string {
	problems := map[string]string{}
{{- range .RequestFields }}
{{- if .Required }}
	if r.{{ .Name }} == nil {
		problems["{{ .JSON }}"] = "is required"
	}
{{- if .String }}
	if r.{{ .Name }} != nil && strings.TrimSpace(*r.{{ .Name }}) == "" {
		problems["{{ .JSON }}"] = "must not be blank"
	}
{{- end }}
{{- end }}
{{- if .EnumList }}
	if r.{{ .Name }} != nil && !oneOf(*r.{{ .Name }}, {{ .EnumList }}) {
		problems["{{ .JSON }}"] = {{ .EnumMessage }}
	}
{{- end }}
{{- end }}
	return problems
}

func (r Create{{ .Entity }}Request) ToEntity() {{ .Entity }} {
	var item {{ .Entity }}
{{- range .RequestFields }}
{{- if .EntityPointer }}
	item.{{ .Name }} = r.{{ .Name }}
{{- else }}
	if r.{{ .Name }} != nil {
		item.{{ .Name }} = *r.{{ .Name }}
	}
{{- end }}
{{- end }}
	return item
}

// Update{{ .Entity }}Request is a partial update: omitted fields keep their
// stored value.
type Update{{ .Entity }}Request struct {
{{- range .RequestFields }}
	{{ .Name }} *{{ .Type }} ` + "`json:\"{{ .JSON }}\"`" + `
{{- end }}
}

func (r Update{{ .Entity }}Request) Validate() map[string]string {
	problems := map[string]string{}
{{- range .RequestFields }}
{{- if and .String .Required }}
	if r.{{ .Name }} != nil && strings.TrimSpace(*r.{{ .Name }}) == "" {
		problems["{{ .JSON }}"] = "must not be blank"
	}
{{- end }}
{{- if .EnumList }}
	if r.{{ .Name }} != nil && !oneOf(*r.{{ .Name }}, {{ .EnumList }}) {
		problems["{{ .JSON }}"] = {{ .EnumMessage }}
	}
{{- end }}
{{- end }}
	return problems
}

func (r Update{{ .Entity }}Request) Apply(item *{{ .Entity }}) {
{{- range .RequestFields }}
	if r.{{ .Name }} != nil {
{{- if .EntityPointer }}
		item.{{ .Name }} = r.{{ .Name }}
{{- else }}
		item.{{ .Name }} = *r.{{ .Name }}
{{- end }}
	}
{{- end }}
}
`

const hexInboundPort = `package inbound

import (
	"context"

	"{{ .Module }}/internal/model"
)

type {{ .Entity }}Service interface {
	List(ctx context.Context, query model.ListQuery) (model.Page[model.{{ .Entity }}], error)
	Get(ctx context.Context, id {{ .PrimaryType }}) (model.{{ .Entity }}, error)
	Create(ctx context.Context, request model.Create{{ .Entity }}Request) (model.{{ .Entity }}, error)
	Update(ctx context.Context, id {{ .PrimaryType }}, request model.Update{{ .Entity }}Request) (model.{{ .Entity }}, error)
	Delete(ctx context.Context, id {{ .PrimaryType }}) error
}
`

const hexInboundRegistry = `package inbound

type {{ .Entity }}Services interface {
	{{ .Entity }}() {{ .Entity }}Service
}
`

const hexOutboundPort = `package outbound

import (
	"context"

	"{{ .Module }}/internal/model"
)

// {{ .Entity }}Repository returns model.ErrNotFound for a missing record.
type {{ .Entity }}Repository interface {
	List(ctx context.Context, limit, offset int) ([]model.{{ .Entity }}, int64, error)
	Get(ctx context.Context, id {{ .PrimaryType }}) (model.{{ .Entity }}, error)
	Create(ctx context.Context, item *model.{{ .Entity }}) error
	Update(ctx context.Context, item *model.{{ .Entity }}) error
	Delete(ctx context.Context, id {{ .PrimaryType }}) error
}
`

const hexOutboundRegistry = `package outbound

type {{ .Entity }}Repositories interface {
	{{ .Entity }}() {{ .Entity }}Repository
}
`

const hexDomain = `package {{ .Package }}

import (
	"context"
	"time"

	"{{ .Module }}/internal/model"
	"{{ .Module }}/internal/port/inbound"
	"{{ .Module }}/internal/port/outbound"
	"{{ .Module }}/utils/logger"
)

// Service implements inbound.{{ .Entity }}Service. Business rules for
// {{ .Table }} belong here; the domain never imports adapters.
type Service struct {
	repository outbound.{{ .Entity }}Repository
}

var _ inbound.{{ .Entity }}Service = (*Service)(nil)

func NewService(repository outbound.{{ .Entity }}Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, query model.ListQuery) (page model.Page[model.{{ .Entity }}], err error) {
	started := time.Now()
	defer func() { logger.ServiceResult(ctx, "{{ .Package }}", "list", started, page, err) }()
	items, total, err := s.repository.List(ctx, query.Limit, query.Offset)
	if err != nil {
		return page, err
	}
	return model.Page[model.{{ .Entity }}]{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (s *Service) Get(ctx context.Context, id {{ .PrimaryType }}) (item model.{{ .Entity }}, err error) {
	started := time.Now()
	defer func() { logger.ServiceResult(ctx, "{{ .Package }}", "get", started, item, err) }()
	return s.repository.Get(ctx, id)
}

func (s *Service) Create(ctx context.Context, request model.Create{{ .Entity }}Request) (item model.{{ .Entity }}, err error) {
	started := time.Now()
	defer func() { logger.ServiceResult(ctx, "{{ .Package }}", "create", started, item, err) }()
	item = request.ToEntity()
	err = s.repository.Create(ctx, &item)
	return item, err
}

func (s *Service) Update(ctx context.Context, id {{ .PrimaryType }}, request model.Update{{ .Entity }}Request) (item model.{{ .Entity }}, err error) {
	started := time.Now()
	defer func() { logger.ServiceResult(ctx, "{{ .Package }}", "update", started, item, err) }()
	item, err = s.repository.Get(ctx, id)
	if err != nil {
		return item, err
	}
	request.Apply(&item)
	err = s.repository.Update(ctx, &item)
	return item, err
}

func (s *Service) Delete(ctx context.Context, id {{ .PrimaryType }}) (err error) {
	started := time.Now()
	defer func() { logger.ServiceResult(ctx, "{{ .Package }}", "delete", started, nil, err) }()
	return s.repository.Delete(ctx, id)
}
`

const hexDomainTest = `package {{ .Package }}

import (
	"context"
	"errors"
	"slices"
	"testing"

	"{{ .Module }}/internal/model"
)

// fakeRepository is an in-memory outbound.{{ .Entity }}Repository, so domain
// rules are tested without a database.
type fakeRepository struct {
	items  map[{{ .PrimaryType }}]model.{{ .Entity }}
	nextID {{ .PrimaryType }}
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{items: map[{{ .PrimaryType }}]model.{{ .Entity }}{}}
}

func (r *fakeRepository) List(_ context.Context, limit, offset int) ([]model.{{ .Entity }}, int64, error) {
	ids := make([]{{ .PrimaryType }}, 0, len(r.items))
	for id := range r.items {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	page := []model.{{ .Entity }}{}
	for index, id := range ids {
		if index >= offset && len(page) < limit {
			page = append(page, r.items[id])
		}
	}
	return page, int64(len(ids)), nil
}

func (r *fakeRepository) Get(_ context.Context, id {{ .PrimaryType }}) (model.{{ .Entity }}, error) {
	item, ok := r.items[id]
	if !ok {
		return model.{{ .Entity }}{}, model.ErrNotFound
	}
	return item, nil
}

func (r *fakeRepository) Create(_ context.Context, item *model.{{ .Entity }}) error {
	r.nextID++
	item.{{ .PrimaryKey }} = r.nextID
	r.items[item.{{ .PrimaryKey }}] = *item
	return nil
}

func (r *fakeRepository) Update(_ context.Context, item *model.{{ .Entity }}) error {
	if _, ok := r.items[item.{{ .PrimaryKey }}]; !ok {
		return model.ErrNotFound
	}
	r.items[item.{{ .PrimaryKey }}] = *item
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, id {{ .PrimaryType }}) error {
	if _, ok := r.items[id]; !ok {
		return model.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func TestCreateThenGet(t *testing.T) {
	service := NewService(newFakeRepository())
	created, err := service.Create(context.Background(), model.Create{{ .Entity }}Request{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(context.Background(), created.{{ .PrimaryKey }})
	if err != nil {
		t.Fatal(err)
	}
	if got.{{ .PrimaryKey }} != created.{{ .PrimaryKey }} {
		t.Fatalf("got id %v, want %v", got.{{ .PrimaryKey }}, created.{{ .PrimaryKey }})
	}
}

func TestListReturnsRequestedPage(t *testing.T) {
	service := NewService(newFakeRepository())
	var ids []{{ .PrimaryType }}
	for range 3 {
		created, err := service.Create(context.Background(), model.Create{{ .Entity }}Request{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.{{ .PrimaryKey }})
	}
	page, err := service.List(context.Background(), model.ListQuery{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].{{ .PrimaryKey }} != ids[1] {
		t.Fatalf("unexpected page: total=%d items=%d", page.Total, len(page.Items))
	}
}

func TestUpdateUnknownReturnsNotFound(t *testing.T) {
	service := NewService(newFakeRepository())
	_, err := service.Update(context.Background(), 404, model.Update{{ .Entity }}Request{})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected model.ErrNotFound, got %v", err)
	}
}

func TestDeleteRemovesRecord(t *testing.T) {
	service := NewService(newFakeRepository())
	created, err := service.Create(context.Background(), model.Create{{ .Entity }}Request{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), created.{{ .PrimaryKey }}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), created.{{ .PrimaryKey }}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected model.ErrNotFound after delete, got %v", err)
	}
}
`

const hexDomainRegistry = `package domain

import (
	"{{ .Module }}/internal/domain/{{ .Package }}"
	"{{ .Module }}/internal/port/inbound"
)

func (r *Registry) {{ .Entity }}() inbound.{{ .Entity }}Service {
	return {{ .Package }}.NewService(r.repositories.{{ .Entity }}())
}
`

const hexRepository = `package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"{{ .Module }}/internal/model"
	"{{ .Module }}/internal/port/outbound"
)

type {{ .Lower }}Repository struct {
	db *gorm.DB
}

func (r *Registry) {{ .Entity }}() outbound.{{ .Entity }}Repository {
	return &{{ .Lower }}Repository{db: r.db}
}

func (r *{{ .Lower }}Repository) List(ctx context.Context, limit, offset int) ([]model.{{ .Entity }}, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.{{ .Entity }}{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := []model.{{ .Entity }}{}
	err := r.db.WithContext(ctx).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "{{ .PrimaryColumn }}"}}).
		Limit(limit).Offset(offset).Find(&items).Error
	return items, total, err
}

func (r *{{ .Lower }}Repository) Get(ctx context.Context, id {{ .PrimaryType }}) (model.{{ .Entity }}, error) {
	var item model.{{ .Entity }}
	err := r.db.WithContext(ctx).First(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return item, model.ErrNotFound
	}
	return item, err
}

func (r *{{ .Lower }}Repository) Create(ctx context.Context, item *model.{{ .Entity }}) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *{{ .Lower }}Repository) Update(ctx context.Context, item *model.{{ .Entity }}) error {
	return r.db.WithContext(ctx).Save(item).Error
}

func (r *{{ .Lower }}Repository) Delete(ctx context.Context, id {{ .PrimaryType }}) error {
	result := r.db.WithContext(ctx).Delete(&model.{{ .Entity }}{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return model.ErrNotFound
	}
	return nil
}
`

const hexHandler = `package fiber

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"{{ .Module }}/internal/model"
	"{{ .Module }}/internal/port/inbound"
)

type {{ .Lower }}Handler struct {
	service inbound.{{ .Entity }}Service
}

func register{{ .Entity }}Routes(api fiber.Router, service inbound.{{ .Entity }}Service) {
	h := &{{ .Lower }}Handler{service: service}
	group := api.Group("/{{ .Table }}")
	group.Get("/", h.List)
	group.Get("/:id", h.Get)
	group.Post("/", h.Create)
	group.Put("/:id", h.Update)
	group.Patch("/:id", h.Update)
	group.Delete("/:id", h.Delete)
}

// List returns one page: GET /?limit=20&offset=0.
func (h *{{ .Lower }}Handler) List(c *fiber.Ctx) error {
	query, problems := model.ParseListQuery(c.Query("limit"), c.Query("offset"))
	if len(problems) > 0 {
		return SendMessage(c, fiber.StatusBadRequest, "invalid query parameters", problems)
	}
	page, err := h.service.List(c.UserContext(), query)
	if err != nil {
		return err
	}
	return Send(c, fiber.StatusOK, page)
}

func (h *{{ .Lower }}Handler) Get(c *fiber.Ctx) error {
	{{ .ParseID }}
	item, err := h.service.Get(c.UserContext(), id)
	if errors.Is(err, model.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return err
	}
	return Send(c, fiber.StatusOK, item)
}

func (h *{{ .Lower }}Handler) Create(c *fiber.Ctx) error {
	var request model.Create{{ .Entity }}Request
	if err := decodeBody(c, &request); err != nil {
		return err
	}
	if problems := request.Validate(); len(problems) > 0 {
		return SendMessage(c, fiber.StatusUnprocessableEntity, "validation failed", problems)
	}
	item, err := h.service.Create(c.UserContext(), request)
	if err != nil {
		return err
	}
	return Send(c, fiber.StatusCreated, item)
}

// Update applies a partial update; it serves both PUT and PATCH.
func (h *{{ .Lower }}Handler) Update(c *fiber.Ctx) error {
	{{ .ParseID }}
	var request model.Update{{ .Entity }}Request
	if err := decodeBody(c, &request); err != nil {
		return err
	}
	if problems := request.Validate(); len(problems) > 0 {
		return SendMessage(c, fiber.StatusUnprocessableEntity, "validation failed", problems)
	}
	item, err := h.service.Update(c.UserContext(), id, request)
	if errors.Is(err, model.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return err
	}
	return Send(c, fiber.StatusOK, item)
}

func (h *{{ .Lower }}Handler) Delete(c *fiber.Ctx) error {
	{{ .ParseID }}
	err = h.service.Delete(c.UserContext(), id)
	if errors.Is(err, model.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
`

const hexMigration = `package postgres

// Reconstructed from the {{ .Table }} table when CRUD was generated. It is
// idempotent, so it is a no-op on the database it was read from. Unique
// constraints, foreign keys, checks, and indexes are not included; add them in
// a new migration.
func init() {
	register(Migration{Version: {{ .MigrationVersion }}, Name: "{{ .MigrationName }}", Up: []string{
{{- range .Statements }}
		{{ . }},
{{- end }}
	}})
}
`
