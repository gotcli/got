package generator

import (
	"fmt"
	"strings"

	"github.com/gotcli/blueprints"
	"github.com/gotcli/got/internal/toolchain"
)

// Hexagonal (ports and adapters) layout:
//
//	cmd/main.go                          entry point
//	internal/app.go                      wiring
//	internal/domain/                     business logic, implements inbound ports
//	internal/port/{inbound,outbound}/    interfaces
//	internal/adapter/inbound/            fiber (HTTP), command (CLI), rabbitmq
//	internal/adapter/outbound/           postgres, http, rabbitmq, redis
//	internal/model/                      entities, requests, responses
//	internal/migration/postgres/         numbered migrations
//	utils/                               config and logging helpers
const hexagonalArchitecture = "hexagonal"

// Markers where "got generate crud" registers each new entity.
const (
	hexagonalRoutesMarker   = "// got:hexagonal-routes"
	hexagonalInboundMarker  = "// got:inbound-registry"
	hexagonalOutboundMarker = "// got:outbound-registry"
)

// rebase adapts a shared blueprint to the hexagonal layout. Each replacement
// must match exactly once, so a changed blueprint fails generation loudly.
func rebase(name, source string, replacements ...string) (string, error) {
	for i := 0; i+1 < len(replacements); i += 2 {
		if count := strings.Count(source, replacements[i]); count != 1 {
			return "", fmt.Errorf("blueprint %s: expected %q once, found %d", name, replacements[i], count)
		}
		source = strings.Replace(source, replacements[i], replacements[i+1], 1)
	}
	return source, nil
}

func hexagonalFiles() (map[string]string, error) {
	config, err := rebase("ConfigViper", blueprints.ConfigViper, "package configs", "package config")
	if err != nil {
		return nil, err
	}
	connect, err := rebase("ORMInterface", blueprints.ORMInterface,
		"package databases", "package postgres",
		"{{ .ModuleName }}/pkg/logger", "{{ .ModuleName }}/utils/logger")
	if err != nil {
		return nil, err
	}
	response, err := rebase("HTTPResponse", blueprints.HTTPResponse, "package response", "package fiber")
	if err != nil {
		return nil, err
	}
	requestLogger, err := rebase("RequestLoggerMiddleware", blueprints.RequestLoggerMiddleware,
		"package middlewares", "package fiber",
		"{{ .ModuleName }}/pkg/logger", "{{ .ModuleName }}/utils/logger")
	if err != nil {
		return nil, err
	}
	return map[string]string{
		".gitignore":                  blueprints.GitIgnore,
		".dockerignore":               blueprints.MicroserviceDockerIgnore,
		"config.yml":                  blueprints.ConfigYml,
		"Dockerfile":                  strings.ReplaceAll(hexagonalDockerfile, "{{GO_VERSION}}", toolchain.DockerImage),
		"cmd/main.go":                 hexagonalMain,
		"internal/app.go":             hexagonalApp,
		"internal/domain/registry.go": hexagonalDomainRegistry,
		"internal/port/inbound/registry_service.go":        hexagonalInboundRegistry,
		"internal/port/outbound/registry_repository.go":    hexagonalOutboundRegistry,
		"internal/adapter/inbound/fiber/server.go":         hexagonalServer,
		"internal/adapter/inbound/fiber/routes.go":         hexagonalRoutes,
		"internal/adapter/inbound/fiber/health.go":         hexagonalHealth,
		"internal/adapter/inbound/fiber/request.go":        hexagonalDecode,
		"internal/adapter/inbound/fiber/response.go":       response,
		"internal/adapter/inbound/fiber/request_logger.go": requestLogger,
		"internal/adapter/inbound/command/command.go":      hexagonalCommand,
		"internal/adapter/inbound/rabbitmq/doc.go":         placeholder("rabbitmq", "consumes messages and calls inbound ports, as the fiber adapter does for HTTP requests."),
		"internal/adapter/outbound/postgres/connect.go":    connect,
		"internal/adapter/outbound/postgres/registry.go":   hexagonalPostgresRegistry,
		"internal/adapter/outbound/http/doc.go":            placeholder("http", "implements outbound ports by calling other HTTP services."),
		"internal/adapter/outbound/rabbitmq/doc.go":        placeholder("rabbitmq", "implements outbound ports by publishing messages."),
		"internal/adapter/outbound/redis/doc.go":           placeholder("redis", "implements outbound ports backed by Redis, such as caches."),
		"internal/model/entity.go":                         hexagonalModelEntity,
		"internal/model/request.go":                        hexagonalModelRequest,
		"internal/model/response.go":                       hexagonalModelResponse,
		"internal/migration/postgres/migrate.go":           hexagonalMigrate,
		"utils/config/config.go":                           config,
		"utils/logger/logger.go":                           blueprints.StructuredLogger,
	}, nil
}

func placeholder(pkg, purpose string) string {
	return fmt.Sprintf("// Package %s %s\n//\n// It is generated empty; add an adapter here when the service needs one.\npackage %s\n", pkg, purpose, pkg)
}

const hexagonalMain = `package main

import (
	"log"
	"os"

	"{{ .ModuleName }}/internal"
)

// Usage: <binary> [serve|migrate]. Without arguments the HTTP server starts.
func main() {
	if err := internal.Run(os.Args[1:]); err != nil {
		log.Fatalf("%v", err)
	}
}
`

const hexagonalApp = `// Package internal wires the application. Outbound adapters implement the
// outbound ports, the domain implements the inbound ports on top of them, and
// inbound adapters (HTTP, CLI) reach the domain only through inbound ports.
package internal

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/viper"

	"{{ .ModuleName }}/internal/adapter/inbound/command"
	httpadapter "{{ .ModuleName }}/internal/adapter/inbound/fiber"
	"{{ .ModuleName }}/internal/adapter/outbound/postgres"
	"{{ .ModuleName }}/internal/domain"
	migration "{{ .ModuleName }}/internal/migration/postgres"
	"{{ .ModuleName }}/utils/config"
	"{{ .ModuleName }}/utils/logger"
)

func Run(args []string) error {
	if err := config.InitViper(); err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	loggers, err := logger.Setup(viper.GetString("LOG_DIR"))
	if err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}
	defer loggers.Close()

	db, err := postgres.ConnectDB()
	if err != nil {
		return fmt.Errorf("failed to start: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to open database pool: %w", err)
	}
	defer sqlDB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handled, err := command.Run(ctx, args, command.Handlers{
		Migrate: func(ctx context.Context) error { return migration.Up(ctx, db) },
	})
	if handled {
		return err
	}

	services := domain.NewRegistry(postgres.NewRegistry(db))
	app := httpadapter.NewServer(services, loggers, sqlDB.PingContext)
	return httpadapter.Serve(ctx, app)
}
`

const hexagonalDomainRegistry = `package domain

import (
	"{{ .ModuleName }}/internal/port/inbound"
	"{{ .ModuleName }}/internal/port/outbound"
)

// Registry implements inbound.ServiceRegistry on top of the outbound ports.
// Each entity adds its accessor in registry_<entity>.go.
type Registry struct {
	repositories outbound.RepositoryRegistry
}

var _ inbound.ServiceRegistry = (*Registry)(nil)

func NewRegistry(repositories outbound.RepositoryRegistry) *Registry {
	return &Registry{repositories: repositories}
}
`

const hexagonalInboundRegistry = `package inbound

// ServiceRegistry exposes every inbound port. The domain implements it and
// inbound adapters depend only on it. Each entity embeds its registry_<entity>.go
// interface here.
type ServiceRegistry interface {
	` + hexagonalInboundMarker + `
}
`

const hexagonalOutboundRegistry = `package outbound

// RepositoryRegistry exposes every outbound port. Outbound adapters implement
// it. Each entity embeds its registry_<entity>.go interface here.
type RepositoryRegistry interface {
	` + hexagonalOutboundMarker + `
}
`

const hexagonalServer = `package fiber

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/spf13/viper"

	"{{ .ModuleName }}/internal/port/inbound"
	"{{ .ModuleName }}/utils/logger"
)

// ReadinessCheck reports whether dependencies such as the database are ready.
type ReadinessCheck func(ctx context.Context) error

func NewServer(services inbound.ServiceRegistry, loggers *logger.Loggers, ready ReadinessCheck) *fiber.App {
	app := fiber.New(fiber.Config{
		ReadBufferSize: 8192,
		BodyLimit:      viper.GetInt("HTTP_BODY_LIMIT"),
		ErrorHandler:   ErrorHandler,
		ReadTimeout:    viper.GetDuration("HTTP_READ_TIMEOUT"),
		WriteTimeout:   viper.GetDuration("HTTP_WRITE_TIMEOUT"),
		IdleTimeout:    viper.GetDuration("HTTP_IDLE_TIMEOUT"),
	})
	app.Use(recover.New())
	app.Use(RequestLogger(loggers))
	registerHealthRoutes(app, ready)
	api := app.Group("/api")
	registerRoutes(api, services)
	return app
}

// Serve listens until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, app *fiber.App) error {
	serverError := make(chan error, 1)
	go func() { serverError <- app.Listen(fmt.Sprintf(":%d", viper.GetInt("APP_PORT"))) }()
	select {
	case err := <-serverError:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), viper.GetDuration("SHUTDOWN_TIMEOUT"))
	defer cancel()
	return app.ShutdownWithContext(shutdown)
}
`

const hexagonalRoutes = `package fiber

import (
	"github.com/gofiber/fiber/v2"

	"{{ .ModuleName }}/internal/port/inbound"
)

// registerRoutes mounts every resource under /api. "got generate crud" adds one
// line per entity above the marker.
func registerRoutes(api fiber.Router, services inbound.ServiceRegistry) {
	api.Get("", func(c *fiber.Ctx) error { return Send(c, fiber.StatusOK, "version 1.0.0") })
	` + hexagonalRoutesMarker + `
}
`

const hexagonalHealth = `package fiber

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
)

func registerHealthRoutes(app *fiber.App, ready ReadinessCheck) {
	app.Get("/health/live", func(c *fiber.Ctx) error {
		return Send(c, fiber.StatusOK, map[string]string{"state": "alive"})
	})
	app.Get("/health/ready", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "database unavailable")
		}
		return Send(c, fiber.StatusOK, map[string]string{"state": "ready"})
	})
}
`

const hexagonalDecode = `package fiber

import (
	"bytes"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// decodeBody rejects unknown fields so typos and server-managed columns such as
// the primary key fail loudly instead of being silently ignored.
func decodeBody(c *fiber.Ctx, target any) error {
	if len(bytes.TrimSpace(c.Body())) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body: "+err.Error())
	}
	return nil
}
`

const hexagonalCommand = `// Package command is the CLI inbound adapter. One-off tasks such as
// migrations share the wiring of the HTTP server.
package command

import (
	"context"
	"fmt"
)

type Handlers struct {
	Migrate func(ctx context.Context) error
}

// Run executes the subcommand named by args. It reports false when args name
// no subcommand, so the caller starts the HTTP server instead.
func Run(ctx context.Context, args []string, handlers Handlers) (bool, error) {
	if len(args) == 0 || args[0] == "serve" {
		return false, nil
	}
	switch args[0] {
	case "migrate":
		return true, handlers.Migrate(ctx)
	default:
		return true, fmt.Errorf("unknown command %q (available: serve, migrate)", args[0])
	}
}
`

const hexagonalPostgresRegistry = `package postgres

import (
	"gorm.io/gorm"

	"{{ .ModuleName }}/internal/port/outbound"
)

// Registry implements outbound.RepositoryRegistry with GORM. Each entity adds
// its accessor next to its repository.
type Registry struct {
	db *gorm.DB
}

var _ outbound.RepositoryRegistry = (*Registry)(nil)

func NewRegistry(db *gorm.DB) *Registry {
	return &Registry{db: db}
}
`

const hexagonalModelEntity = `// Package model holds the data structures shared by every layer: entities
// (one <entity>.go file each), request models, and response models.
package model
`

const hexagonalModelRequest = `package model

import (
	"strconv"
	"strings"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

type ListQuery struct {
	Limit  int
	Offset int
}

// ParseListQuery reads ?limit= and ?offset=. Limit defaults to DefaultLimit and
// is capped at MaxLimit so a list request can never load a whole table.
func ParseListQuery(limit, offset string) (ListQuery, map[string]string) {
	query := ListQuery{Limit: DefaultLimit}
	problems := map[string]string{}
	if value := strings.TrimSpace(limit); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			problems["limit"] = "must be a positive integer"
		} else {
			query.Limit = min(parsed, MaxLimit)
		}
	}
	if value := strings.TrimSpace(offset); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			problems["offset"] = "must be a non-negative integer"
		} else {
			query.Offset = parsed
		}
	}
	return query, problems
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
`

const hexagonalModelResponse = `package model

import "errors"

// ErrNotFound is returned by outbound adapters when a record does not exist,
// so the domain and inbound adapters never depend on a database library.
var ErrNotFound = errors.New("record not found")

type Page[T any] struct {
	Items  []T   ` + "`json:\"items\"`" + `
	Total  int64 ` + "`json:\"total\"`" + `
	Limit  int   ` + "`json:\"limit\"`" + `
	Offset int   ` + "`json:\"offset\"`" + `
}
`

const hexagonalMigrate = `// Package postgres runs the numbered migrations in this directory. Each
// <version>_<name>.go file registers one migration from init.
package postgres

import (
	"context"
	"fmt"
	"log"
	"sort"

	"gorm.io/gorm"
)

type Migration struct {
	Version int
	Name    string
	Up      []string
}

var migrations []Migration

func register(migration Migration) {
	migrations = append(migrations, migration)
}

// Up applies pending migrations in version order. Each migration runs in its
// own transaction and is recorded in schema_migrations.
func Up(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	if err := db.Exec(` + "`" + `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY,
		name text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now()
	)` + "`" + `).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	var applied []int
	if err := db.Raw("SELECT version FROM schema_migrations").Scan(&applied).Error; err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	done := make(map[int]bool, len(applied))
	for _, version := range applied {
		done[version] = true
	}
	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	for i, migration := range sorted {
		if i > 0 && sorted[i-1].Version == migration.Version {
			return fmt.Errorf("duplicate migration version %d", migration.Version)
		}
		if done[migration.Version] {
			continue
		}
		// Recording an empty migration would skip statements added to it later.
		if len(migration.Up) == 0 {
			return fmt.Errorf("migration %d_%s has no statements; add SQL to Up before migrating", migration.Version, migration.Name)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			for _, statement := range migration.Up {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return tx.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)", migration.Version, migration.Name).Error
		})
		if err != nil {
			return fmt.Errorf("migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		log.Printf("applied migration %d_%s", migration.Version, migration.Name)
	}
	return nil
}
`

const hexagonalDockerfile = `FROM golang:{{GO_VERSION}}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /service ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /service /app/service
COPY config.yml /app/config.yml
USER nonroot:nonroot
EXPOSE 3000
ENTRYPOINT ["/app/service"]
`
