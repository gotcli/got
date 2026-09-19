# Contributing to GOT CLI

Thank you for helping improve GOT Community.

## Development requirements

- Go 1.22 or newer
- Git
- Docker is optional and is only needed to exercise generated container files
- PostgreSQL or SQL Server is optional and is only needed for live schema inspection

## Workflow

1. Fork the repository and clone your fork.
2. Create a focused branch from the current development branch.
3. Make your changes without changing existing commands, flags, or defaults unless the change is explicitly discussed.
4. Format and validate the repository:

   ```sh
   go fmt ./...
   go vet ./...
   go test ./...
   go build ./...
   ```

5. Update relevant documentation and tests.
6. Submit a pull request describing the problem, the change, and how it was tested.

Do not include credentials, customer data, private URLs, generated binaries, or
local configuration in a contribution. Keep pull requests focused and avoid
unrelated refactors.
