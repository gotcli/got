package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotcli/got/internal/dbschema"
)

func productsTable() dbschema.Table {
	return dbschema.Table{Name: "products", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{
		{Name: "id", DataType: "integer", HasDefault: true, DefaultExpr: "nextval('products_id_seq'::regclass)"},
		{Name: "name", DataType: "character varying", CharLength: 120},
		{Name: "price", DataType: "numeric", NumericPrecision: 10, NumericScale: 2},
		{Name: "status", DataType: "USER-DEFINED", UDTName: "product_status", HasDefault: true, DefaultExpr: "'draft'::product_status", EnumValues: []string{"draft", "it's"}},
		{Name: "note", DataType: "text", Nullable: true},
	}}
}

func generateHexagonalProject(t *testing.T, runner CommandRunner) string {
	t.Helper()
	parent := t.TempDir()
	err := (ProjectGenerator{Runner: runner, Version: "v9.9.9"}).Generate(context.Background(), ProjectOptions{
		Name: "shop", Module: "example.com/shop", Architecture: "hexagonal", Database: "pg", ParentPath: parent,
	})
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(parent, "shop")
	// A recording runner does not run "go mod init".
	if _, err := os.Stat(filepath.Join(project, "go.mod")); os.IsNotExist(err) {
		if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/shop\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return project
}

func TestProjectGeneratorCreatesHexagonalLayout(t *testing.T) {
	project := generateHexagonalProject(t, &recordedRunner{})
	for _, path := range []string{
		"cmd/main.go", "internal/app.go", "internal/domain/registry.go",
		"internal/port/inbound/registry_service.go", "internal/port/outbound/registry_repository.go",
		"internal/adapter/inbound/fiber/server.go", "internal/adapter/inbound/command/command.go", "internal/adapter/inbound/rabbitmq/doc.go",
		"internal/adapter/outbound/postgres/connect.go", "internal/adapter/outbound/http/doc.go", "internal/adapter/outbound/redis/doc.go",
		"internal/model/entity.go", "internal/model/request.go", "internal/model/response.go",
		"internal/migration/postgres/migrate.go", "utils/config/config.go", "utils/logger/logger.go", "config.yml", "Dockerfile",
	} {
		if _, err := os.Stat(filepath.Join(project, path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	if !IsHexagonal(project) {
		t.Fatal("manifest does not declare the hexagonal architecture")
	}
	assertFileContains(t, filepath.Join(project, "internal/adapter/outbound/postgres/connect.go"), `"example.com/shop/utils/logger"`)
	assertFileContains(t, filepath.Join(project, "Dockerfile"), "go build -trimpath")

	err := (ProjectGenerator{Runner: &recordedRunner{}}).Generate(context.Background(), ProjectOptions{
		Name: "other", Module: "example.com/other", Architecture: "hexagonal", Database: "mysql", ParentPath: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "only --db pg") {
		t.Fatalf("expected PostgreSQL-only error, got %v", err)
	}
}

func TestHexagonalCRUDRegistersEntitiesAndNumbersMigrations(t *testing.T) {
	project := generateHexagonalProject(t, &recordedRunner{})
	if err := GenerateHexagonalCRUD(project, dbschema.Database{Schema: "public", Tables: []dbschema.Table{productsTable()}}); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "internal/port/inbound/registry_service.go"), "ProductServices\n\t// got:inbound-registry")
	assertFileContains(t, filepath.Join(project, "internal/port/outbound/registry_repository.go"), "ProductRepositories")
	assertFileContains(t, filepath.Join(project, "internal/adapter/inbound/fiber/routes.go"), "registerProductRoutes(api, services.Product())")
	assertFileContains(t, filepath.Join(project, "internal/model/products.go"), `Status *string`)
	assertFileContains(t, filepath.Join(project, "internal/domain/products/domain_test.go"), "func TestDeleteRemovesRecord")

	migration := filepath.Join(project, "internal/migration/postgres/1_products.go")
	for _, expected := range []string{`"id" serial NOT NULL`, `"name" varchar(120) NOT NULL`, `"price" numeric(10,2) NOT NULL`,
		`"status" "public"."product_status" DEFAULT 'draft'::product_status NOT NULL`, `"note" text,`,
		`AS ENUM ('draft', 'it''s')`, `PRIMARY KEY ("id")`} {
		assertFileContains(t, migration, expected)
	}

	customers := dbschema.Table{Name: "customers", PrimaryKey: []string{"id"}, Columns: []dbschema.Column{
		{Name: "id", DataType: "bigint", HasDefault: true, Identity: true},
		{Name: "email", DataType: "text"},
	}}
	err := GenerateHexagonalCRUD(project, dbschema.Database{Schema: "public", Tables: []dbschema.Table{productsTable()}})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected collision on regeneration, got %v", err)
	}
	if err := GenerateHexagonalCRUD(project, dbschema.Database{Schema: "public", Tables: []dbschema.Table{customers}}); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "internal/migration/postgres/2_customers.go"), `"id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL`)
	assertFileContains(t, filepath.Join(project, "internal/adapter/inbound/fiber/routes.go"), "registerProductRoutes(api, services.Product())\n\tregisterCustomerRoutes(api, services.Customer())")
}

func TestHexagonalCRUDRequiresMarkers(t *testing.T) {
	project := generateHexagonalProject(t, &recordedRunner{})
	routes := filepath.Join(project, "internal/adapter/inbound/fiber/routes.go")
	content, _ := os.ReadFile(routes)
	if err := os.WriteFile(routes, []byte(strings.Replace(string(content), hexagonalRoutesMarker, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	err := GenerateHexagonalCRUD(project, dbschema.Database{Schema: "public", Tables: []dbschema.Table{productsTable()}})
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("expected missing marker error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "internal/model/products.go")); !os.IsNotExist(err) {
		t.Fatal("files were written although preflight failed")
	}
}

func TestHexagonalSwaggerAuthAndMigration(t *testing.T) {
	runner := &recordedRunner{}
	project := generateHexagonalProject(t, runner)
	if err := (SwaggerGenerator{}).Generate(project); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "internal/adapter/inbound/fiber/server.go"), "registerHealthRoutes(app, ready)\n\tRegisterSwaggerRoutes(app)")
	assertFileContains(t, filepath.Join(project, "internal/adapter/inbound/fiber/swagger.go"), `"example.com/shop/internal/adapter/inbound/fiber/docs"`)
	if err := (SwaggerGenerator{}).Generate(project); err == nil {
		t.Fatal("second swagger generation should fail")
	}

	if err := (AuthGenerator{Runner: runner}).Generate(context.Background(), project, AuthOptions{JWT: true}); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, filepath.Join(project, "internal/adapter/inbound/fiber/jwt.go"), `"example.com/shop/utils/auth"`)
	assertFileContains(t, filepath.Join(project, "utils/auth/jwt.go"), "package auth")
	assertFileContains(t, filepath.Join(project, "config.yml"), "JWT_ACCESS_SECRET")

	path, err := (MigrationGenerator{}).Generate(project, "Add Products Name Index")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join("internal", "migration", "postgres", "1_add_products_name_index.go") {
		t.Fatalf("path = %s", path)
	}
	next, err := (MigrationGenerator{}).Generate(project, "second")
	if err != nil || !strings.HasSuffix(next, "2_second.go") {
		t.Fatalf("next migration = %s, %v", next, err)
	}

	layered := t.TempDir()
	if _, err := (MigrationGenerator{}).Generate(layered, "x"); err == nil || !strings.Contains(err.Error(), "only for hexagonal") {
		t.Fatalf("expected hexagonal-only error, got %v", err)
	}
}
