# GOT Architecture Audit and Evolution Roadmap

Date: 20 September 2026

Scope: `github.com/gotcli/got` at `d882c91` and the pinned
`github.com/gotcli/blueprints` v0.1.0. This is a source audit; database
introspection was reviewed from code and tests, not exercised against live
PostgreSQL or SQL Server instances.

## 1. Current architecture

GOT is currently a CLI and source generator, not a runtime framework library.
The generated application owns its source and uses ordinary Go dependencies
(Fiber, Viper, GORM, and database drivers). This is an important architectural
boundary: applications do not need a GOT runtime dependency.

```text
got (Cobra CLI)
  -> command validation and interactive input
  -> generators and database introspection
  -> versioned blueprints module
  -> generated, developer-owned Go source
  -> Fiber + Viper + GORM application
```

### CLI

`main.go` resolves version metadata and delegates to Cobra commands in `cmd`.
`got init` creates a standard service, microservice, or multi-module workspace.
`got api`, `got generate crud`, and `got add` extend generated projects.
`got doctor` inspects the host and optionally a generated service or workspace.

### Generation

`internal/generator` renders pinned blueprint strings, writes new files without
overwriting, and uses small marker-based or Go-AST edits where an existing file
must change. `got generate crud` obtains a neutral schema model from PostgreSQL
or SQL Server introspection and renders working GORM CRUD layers. MySQL project
generation exists, but MySQL introspection does not.

`github.com/gotcli/blueprints` is a separate, tagged module compiled into the
CLI. No template download occurs at runtime. It contains application bootstrap,
configuration, routing, response, logging, database, JWT, upload, Swagger, and
layered feature templates.

### Generated application

A service uses route -> handler -> service -> repository layering. Wiring is
explicit in route registration functions. Configuration comes from
`config.yml`, with module-prefixed environment overrides. Standard projects use
a basic Fiber startup. Microservices add HTTP timeouts, signals, graceful
shutdown, health endpoints, and a multi-stage container image.

There is no GOT module registry or application lifecycle API. A "module" today
means generated Go packages and explicit route wiring, not a runtime plugin.
That simplicity is consistent with GOT's current philosophy.

## 2. Existing strengths

- Generated source is readable, conventional Go and remains owned by the user.
- The CLI protects existing files and validates names and destinations.
- Templates are versioned separately but pinned by the CLI for reproducibility.
- Standard, microservice, and workspace outputs share one generation path.
- PostgreSQL and SQL Server schema readers return a database-neutral model.
- CRUD repositories propagate context into GORM operations.
- Microservices include bounded HTTP operation and graceful shutdown.
- Request IDs flow from middleware through service and database logs.
- Logging avoids SQL text and known secret fields and bounds logged payloads.
- JWT issuance validates secrets, issuer, audience, algorithm, token type, and
  expiry; refresh and access secrets are distinct.
- Upload storage uses random server-side names, exclusive creation, and an
  interface that permits replacement by object storage.
- `got doctor` already supports local/project/connect modes and JSON output.
- CLI generators have useful unit and integration-style filesystem tests.
- CI checks tidy state, formatting, vet, Staticcheck, race tests, coverage,
  vulnerabilities, and compilation.

## 3. Gap matrix

Status describes the framework experience delivered by generated projects, not
whether a similarly named package exists in this repository.

| Capability | Status | Existing implementation | Gap | Recommendation | Priority |
| --- | --- | --- | --- | --- | --- |
| Configuration | PARTIAL | Viper, YAML, env prefix | No typed config validation; required production values are checked late | Generate a small typed config/load boundary without hiding Viper | P1 |
| Environment handling | PARTIAL | `APP_ENV`, env overrides | No documented environment profiles or production validation | Keep one config model; add explicit validation, not profile magic | P1 |
| Dependency management | COMPLETE | Go modules with pinned generated dependencies | Versions are duplicated in generator strings | Centralize generated dependency/toolchain policy | P1 |
| Module lifecycle | MISSING | Main owns startup/shutdown | No lifecycle contract | Keep explicit composition; add only small start/close helpers if repeated needs appear | P2 |
| Module registration | PARTIAL | Explicit route registration and marker | No discoverable module metadata; marker edits are fragile | Define optional generated module descriptors, avoiding runtime reflection | P2 |
| Routing | COMPLETE | Fiber groups and explicit registrars | Route inventory/conflict checks are absent | Add read-only route inspection before adding a new runtime abstraction | P2 |
| Middleware pipeline | PARTIAL | Request logger and optional JWT | No conventional composition point or recovery/security defaults | Generate an explicit middleware setup function | P1 |
| Validation | MISSING | Manual parsing only | Request structs have no validation convention | Community validation module or generated idiomatic validator usage | P1 |
| Error handling | PARTIAL | Fiber error handler and envelope | Unexpected errors lack stable public codes; internal errors can reach logs inconsistently | Add typed application errors and safe mapping | P1 |
| API response conventions | COMPLETE | Shared envelope and status text | Pagination/error metadata model absent | Extend only when pagination/errors are introduced | P1 |
| Graceful shutdown | PARTIAL | Microservice template | Standard architecture lacks it; startup errors are only logged | Make safe shutdown the default application bootstrap | P1 |
| Context propagation | COMPLETE | Fiber user context through CRUD/service/GORM | Scaffolded `got api` methods do not model context | Align scaffold signatures with CRUD conventions | P1 |
| Connection lifecycle | PARTIAL | GORM open, microservice close, readiness ping | Standard service never closes; pool settings are absent; connect panics | Return errors and a close function; expose pool settings | P0/P1 |
| Repository conventions | PARTIAL | Interfaces and CRUD GORM repositories | `got api` and CRUD generate incompatible method shapes | Establish one context-aware convention while preserving old projects | P1 |
| Transactions | MISSING | GORM is directly available | No service/repository transaction convention | Document `db.Transaction`; add helper only after repeated use | P1 |
| Migrations | MISSING | Explicitly not generated | No migration tool or status check | Optional community integration with a proven migration tool | P1 |
| Pagination | MISSING | List returns all rows | No bounded list contract | Add cursor/limit primitives to CRUD generation | P1 |
| Soft delete | PARTIAL | Possible through GORM entity types | Introspection does not infer policy | Application opt-in; never silently apply it | P2 |
| Optimistic locking | MISSING | None | Lost-update protection absent | Optional generated version-field convention | P2 |
| Multi-database support | PARTIAL | Three generated drivers | One connection per service; introspection only PostgreSQL/SQL Server | Keep advanced multiple-connection wiring application-owned | P3 |
| Authentication | PARTIAL | JWT manager and bearer middleware | Login, identity lookup, revocation are intentionally absent | Keep identity business flow outside core | P1 |
| JWT | COMPLETE | Access/refresh issue and parse | Refresh rotation/revocation require persistence | Optional auth module, not core | P1 |
| Refresh tokens | PARTIAL | Stateless refresh token issuance | No rotation, reuse detection, or revocation | Optional persistent token module | P1 |
| Password hashing | MISSING | None | No safe default helper | Community auth module using maintained password hashing | P1 |
| Authorization | PARTIAL | Roles in claims | Roles are not enforced | Provide small middleware helpers as optional module | P1 |
| RBAC | MISSING | Claims can carry roles | No permission model/store | Community module with application-owned policy data | P2 |
| Policy authorization | MISSING | None | No policy contract | Community module; avoid embedding a policy engine in core | P3 |
| API keys | MISSING | None | No hashing/scope/rotation convention | Optional community module | P2 |
| Tenant-aware authorization | MISSING | None | No trusted tenant context | Community tenancy module; domain membership remains application logic | P2 |
| Security middleware | MISSING | JWT and body limit only | Recovery, headers, CORS, rate limits, trusted proxies not configured | Generate explicit secure defaults with opt-in network policy | P0/P1 |
| Event system | MISSING | None | No in-process event convention | Optional community package only when concrete use cases exist | P3 |
| Background jobs | MISSING | None | No durable execution | Community adapter around established backends | P3 |
| Queue abstraction | MISSING | None | No delivery/ack semantics | SHOULD NOT BE CORE; adapters should expose backend semantics | P3 |
| Scheduler | MISSING | None | No scheduler | Optional community module | P3 |
| File/storage abstraction | PARTIAL | Upload `Storage` interface and local implementation | Save-only contract; no retrieval/delete/metadata | Grow in community module based on use cases | P2 |
| Email/notifications | MISSING | None | No provider-neutral message model | Optional community module | P3 |
| Cache | MISSING | None | No caching convention | SHOULD NOT BE CORE; optional adapters | P3 |
| Webhooks | MISSING | None | No signing/delivery/retry tooling | Optional community module | P3 |
| Feature flags | MISSING | None | No evaluation abstraction | SHOULD NOT BE CORE; provider adapters or application code | P3 |
| Structured logging | COMPLETE | `slog`, JSON, redaction, bounded results | Package-level mutable logger complicates tests/multiple apps | Move toward explicit logger injection in new output | P1 |
| Request ID | COMPLETE | Generated/accepted and returned | Incoming IDs are trusted without size/character validation | Validate or replace malformed IDs | P0 |
| Correlation ID | PARTIAL | Request ID serves local correlation | No separate cross-service propagation convention | Document header propagation before adding another ID | P2 |
| Health checks | COMPLETE | Liveness and DB readiness | Only microservice output has endpoints | Make health availability an explicit architecture choice/default | P1 |
| Readiness/liveness | COMPLETE | Separate endpoints with bounded DB ping | No registry for module checks | Optional small health check registry | P2 |
| Metrics | MISSING | None | No request/runtime metrics | Optional community OpenTelemetry/Prometheus module | P2 |
| Distributed tracing | MISSING | Context is propagated | No spans or propagation middleware | Optional OpenTelemetry module | P2 |
| CLI project creation | COMPLETE | `got init` service/workspace | Creation can leave a partial destination on command failure | Roll back newly created project on failure | P0 |
| CLI extension | PARTIAL | `api`, `add`, `generate` | Multi-file mutations are not uniformly transactional | Shared preflight/rollback discipline | P0 |
| CLI removal | MISSING | None | Safe ownership/state tracking does not exist | Defer `got remove` until manifests can prove ownership | P3 |
| Route/module/config inspection | MISSING | Doctor inspects files/config | No `routes`, `modules`, or `config` views | Add only read-only commands backed by a stable project manifest | P2 |
| Doctor extensibility | PARTIAL | Rich built-in checks and JSON | Checks are one closed function chain; modules cannot contribute | Introduce an internal check registry first; public contract later | P2 |
| Testing utilities | PARTIAL | CLI filesystem tests and generated build checks | No generated handler/service test kit; DB readers have 0% coverage | Add SQL mock/integration tests and blueprint contract tests | P0/P1 |

## 4. Architecture boundary

### GOT Core

Core is the CLI, generator contracts, project/workspace manifest, safe file
mutation, configuration bootstrap, explicit composition, routing conventions,
error/response conventions, context propagation, graceful shutdown, health,
structured logging, and generator diagnostics. These are required by nearly
all generated services.

Core should remain source-generating and should not require applications to
import a large GOT runtime.

### GOT Community modules

Optional, vendor-neutral modules should cover validation, migrations, secure
auth building blocks, RBAC helpers, API keys, tenancy context, object storage,
metrics/tracing, background jobs, schedules, webhooks, notifications, and
specific cache/queue adapters. Each module must be useful independently and
must expose backend semantics rather than pretending all providers behave the
same way.

### GOT Enterprise modules

Enterprise scope includes LDAP/Active Directory, SAML/OIDC integrations tied to
organizational providers, advanced IAM provisioning, compliance evidence,
enterprise audit retention/export, secrets platforms, and organization-specific
connectors. These must build on public extension points without weakening the
community edition.

### Applications

Identity records, users, tenants, organizations, role catalogs, workflows,
subscriptions, billing rules, HR, payroll, POS, clinic, ERP, and CRM behavior
remain application code. A management platform may use community infrastructure
for auth, jobs, storage, and observability, but its domain model must not enter
GOT Core.

## 5. Technical debt

- Project generation does not remove a newly created destination when `go mod`
  or dependency installation fails. Workspace generation already rolls back,
  so behavior is inconsistent and retries can be blocked.
- `api`, `method`, `auth`, and `upload` can partially mutate projects after a
  late filesystem or formatting failure. Swagger is the only add-on with an
  explicit rollback attempt.
- CLI and blueprint releases are separate, but compatibility is enforced mostly
  by pinned versions and CLI tests rather than a declared compatibility policy.
- Dependency versions and the Go output policy are embedded in multiple places.
  The CLI and generated projects now target Go 1.25, but that shared policy
  should be named centrally and covered by contract tests.
- `got api` scaffolds non-context methods while introspected CRUD uses context,
  producing two repository/service conventions.
- Standard and microservice main templates differ in lifecycle guarantees.
- Database connection functions panic and hide connection ownership.
- The logger uses package-global mutable state.
- Marker/string mutation is sensitive to user edits; method mutation performs
  sequential writes without a full preflight or rollback.
- Database introspection and prompt packages have no automated test coverage.
- Blueprint source formatting is compact and difficult to review, and the
  blueprints repository has no tests that compile representative generated
  projects independently of the CLI.
- The blueprints README still links to the old `got-community` repository name.
- Error envelopes have no stable machine-readable code or validation details.
- Generated CRUD accepts entity-shaped request bodies, coupling persistence and
  transport models and enabling accidental field updates.
- `doctor` is useful but monolithic; its minimum Go policy is not explicitly
  distinguished from the CLI build requirement.

Potential breaking changes include changing generated package names, method
signatures, response envelopes, environment prefixes, route paths, config keys,
or workspace manifest shape. New output can evolve, but AST edits and doctor
must continue to recognize existing generated projects.

## 6. Recommended roadmap

### P0 — Foundation and correctness

1. Make new-project generation atomic. A failed dependency command must not
   leave an unusable destination that blocks a retry.
2. Add preflight and rollback to every multi-file mutating generator. Generator
   failure should mean no application change.
3. Declare and test separate CLI-build and generated-project Go versions.
4. Add generated-project contract tests using the pinned blueprints, including
   standard, microservice, auth, upload, Swagger, and representative CRUD.
5. Test database introspection query mapping and failure paths.
6. Validate inbound request IDs and establish baseline recovery/security-header
   middleware so untrusted input cannot become arbitrary log correlation data.

### P1 — Production readiness

1. Return database startup errors, configure pools, and close connections in
   every architecture. Startup and ownership must be deterministic.
2. Add typed configuration validation for required secrets, timeouts, ports,
   and production settings.
3. Align context-aware repository/service conventions across scaffold and CRUD.
4. Add safe typed application errors and machine-readable response codes.
5. Add request DTO validation and bounded pagination.
6. Offer a migration integration and transaction guidance without inventing an
   ORM-independent transaction abstraction.
7. Add optional secure authentication building blocks: password hashing,
   refresh rotation/revocation, and authorization middleware.

### P2 — Developer experience

1. Introduce a backward-compatible project manifest/version that enables safe
   discovery and future upgrades.
2. Add read-only route, module, and effective-config inspection where the
   manifest/source model can answer accurately.
3. Refactor doctor into an internal registry of checks, then expose a stable
   contribution mechanism only when community modules require it.
4. Add optional health-check registration, OpenTelemetry, storage, RBAC, tenant
   context, API keys, and optimistic locking.

### P3 — Ecosystem

1. Publish narrowly scoped community modules for jobs, scheduling, queues,
   notifications, webhooks, caching, and feature flag providers.
2. Document compatibility and release coordination between CLI, blueprints,
   and community modules.
3. Consider `got remove` only after generated ownership metadata makes removal
   safe and reviewable.

### P4 — Enterprise

Build LDAP/AD, enterprise SSO, provisioning, audit/compliance, and organization
connectors as separate products on public extension points. No enterprise need
should force tenant, identity, billing, or workflow business models into Core.

## 7. First implementation decision

The first change is P0.1: atomic creation of a new single service. It is the
smallest high-value correctness fix, matches the rollback behavior already used
by workspace generation, changes no successful output, adds no abstraction,
and is backward compatible. On any error after the destination is created, GOT
will remove only that newly created destination. Existing destinations remain
protected and untouched.
