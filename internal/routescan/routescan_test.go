package routescan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanFollowsGroupsAcrossPackages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/shop\n")
	writeFile(t, root, "main.go", `package main

import (
	"example.com/shop/routes"
	"github.com/gofiber/fiber/v2"
)

func main() {
	conn := connect()
	app := fiber.New(fiber.Config{})
	routes.RegisterRootRoutes(conn, app)
	routes.RegisterSwaggerRoutes(app)
}
`)
	writeFile(t, root, "routes/root.go", `package routes

import (
	handler "example.com/shop/handlers/orders"
	"github.com/gofiber/fiber/v2"
)

func RegisterRootRoutes(db any, route *fiber.App) {
	api := route.Group("api")
	api.Get("", func(c *fiber.Ctx) error { return nil })
	RegisterOrderRoutes(db, api)
	RegisterUploadRoutes(db, api)
}

func RegisterOrderRoutes(db any, route fiber.Router) {
	h := handler.NewOrderHandler(db)
	group := route.Group("/orders")
	group.Get("/", h.List)
	group.Patch("/:id", h.Update)
	group.Get("/", h.List)
}

func RegisterUploadRoutes(_ any, route fiber.Router) {
	route.Post("/uploads", upload)
}

func RegisterSwaggerRoutes(app *fiber.App) {
	app.Get("/swagger/index.html", func(c *fiber.Ctx) error { return nil })
}
`)
	// A nested module (a workspace service) is not part of this project.
	writeFile(t, root, "services/other/go.mod", "module example.com/other\n")
	writeFile(t, root, "services/other/main.go", "package main\n\nfunc main() {}\n")

	routes, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make([][3]string, len(routes))
	for i, route := range routes {
		got[i] = [3]string{route.Method, route.Path, route.Handler}
	}
	want := [][3]string{
		{"GET", "/api", "inline func"},
		{"GET", "/api/orders", "OrderHandler.List"},
		{"GET", "/api/orders", "OrderHandler.List"},
		{"PATCH", "/api/orders/:id", "OrderHandler.Update"},
		{"POST", "/api/uploads", "upload"},
		{"GET", "/swagger/index.html", "inline func"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes:\n got %v\nwant %v", got, want)
	}
	if routes[1].Source != "routes/root.go:18" {
		t.Errorf("source = %q", routes[1].Source)
	}
	if duplicates := Duplicates(routes); !reflect.DeepEqual(duplicates, []string{"GET /api/orders"}) {
		t.Errorf("duplicates = %v", duplicates)
	}
}

func TestJoinPathMatchesFiber(t *testing.T) {
	tests := []struct{ prefix, path, want string }{
		{"", "", "/"}, {"", "api", "/api"}, {"/api", "", "/api"}, {"/api", "/", "/api"},
		{"/api", "v1", "/api/v1"}, {"/api/", "/orders", "/api/orders"},
	}
	for _, test := range tests {
		if got := joinPath(test.prefix, test.path); got != test.want {
			t.Errorf("joinPath(%q, %q) = %q, want %q", test.prefix, test.path, got, test.want)
		}
	}
}
