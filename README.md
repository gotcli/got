# GOT CLI

Go Templatify

General-purpose Go/Fiber project generator.

## What is GOT?

GOT is a command-line generator for starting and extending Go services built
with Fiber and GORM. It creates conventional project layers while leaving
application-specific business logic in your hands.

## Why GOT?

GOT provides repeatable project structure and removes routine setup work while
keeping the generated Go source readable and under your control. It is intended
for real services rather than throwaway demos: generated projects can be built,
tested, and extended using normal Go tooling.

## Features

- Generate a standard API, a deployable microservice, or a multi-service workspace.
- Scaffold routes, handlers, services, repositories, schemas, and entities.
- Inspect PostgreSQL or SQL Server schemas and generate CRUD layers.
- Add JWT authentication, file uploads, and OpenAPI/Swagger support.
- Generate structured logging, health checks, Docker files, and configuration.
- Check local tools and generated projects with `got doctor`.

See [User-Guide.md](User-Guide.md) for feature status, detailed workflows, and
the complete command reference.

## Quick Start

After an official version has been tagged:

```sh
go install github.com/gotcli/got@latest
got version
got init --name catalog --module example.com/catalog --db pg
cd catalog
go test ./...
go run .
```

Run `got init` without flags for an interactive setup.

## Installation

Installing GOT from source and building newly generated projects require Go
1.25 or newer. After an official version has been tagged, install it with:

```sh
go install github.com/gotcli/got@latest
```

Ensure the Go binary directory is in `PATH`, then verify with `got version`.
Release binaries may also be provided on the repository's Releases page; use
only artifacts published there and verify any supplied checksums.

## Example Commands

```sh
got init
got init service --architecture microservice
got init workspace --name platform --module example.com/platform --services order,auth
got generate crud
got api --name users
got add method --folder users --name Approve --http-method PATCH
got add auth --jwt
got add upload
got add swagger
got doctor --project
```

Use `got <command> --help` before running a generator to review its flags and
defaults. Existing files are protected from accidental overwrite.

## Architecture

Generated services follow a layered structure: routes call handlers, handlers
call services, and services call repositories. Standard mode creates a single
service; microservice mode adds lifecycle and container files; workspace mode
coordinates independently buildable child services.

## Database Support

Project generation supports PostgreSQL, MySQL, and SQL Server through GORM.
Schema inspection and CRUD generation support PostgreSQL and SQL Server. See
the [status table](User-Guide.md#17-สถานะและ-roadmap) for stability details.

## Documentation

Detailed installation, configuration, workflows, and troubleshooting are in
the [GOT CLI User Guide](User-Guide.md). CLI help is the authoritative reference
for the installed version:

```sh
got --help
got init --help
```

## Community

GOT Community is the complete free and open-source foundation of GOT. Bug
reports, focused feature proposals, documentation improvements, and code
contributions are welcome through the repository's GitHub issues and pull
requests.

## Support GOT

GOT Community is free and open source. If GOT saves you time or helps your
projects, you can support it by:

- Starring the repository.
- Reporting reproducible bugs.
- Suggesting focused improvements.
- Contributing code or documentation.
- Sponsoring the project when an official sponsorship channel is available.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before
opening a pull request. Participation is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md). Please report vulnerabilities according
to [SECURITY.md](SECURITY.md).

## Security

Do not publish vulnerability details or credentials in public issues. Follow
the private reporting guidance in [SECURITY.md](SECURITY.md).

## License

Licensed under the [Apache License 2.0](LICENSE).
