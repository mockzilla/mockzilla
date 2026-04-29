# Code Generation Tools

This directory contains code generation commands for Mockzilla.

## Commands

| Command | Description | Documentation |
|---------|-------------|---------------|
| `service` | Generate a service from OpenAPI spec or static files | [docs](../../docs/commands/service.md) |
| `simplify` | Simplify large OpenAPI specs | [docs](../../docs/commands/simplify.md) |
| `discover` | Discover services and generate imports | [usage](#discover) |
| `fakes` | Generate fake function list for docs (internal) | - |

## Quick Start

```bash
# Generate a service from an OpenAPI spec
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest \
  -name petstore \
  https://petstore3.swagger.io/api/v3/openapi.json
```

See the [documentation](https://mockzilla.github.io/mockzilla/) for detailed usage.

## Discover

Scans a directory for `register.go` files and generates an imports file for service auto-registration.

```bash
# Scan default directory
go run github.com/mockzilla/mockzilla/v2/cmd/gen/discover@latest

# Scan custom directory
go run github.com/mockzilla/mockzilla/v2/cmd/gen/discover@latest pkg/services
```

This generates `cmd/server/services_gen.go` with imports for all discovered services.

Nested directories are supported:
```
pkg/
  adyen/
    v70/register.go  → import "module/pkg/adyen/v70"
    v71/register.go  → import "module/pkg/adyen/v71"
  stripe/
    register.go      → import "module/pkg/stripe"
```
