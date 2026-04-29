# Service Generator

Generates a complete mock service from an OpenAPI spec or static files.

See the [full documentation](https://mockzilla.github.io/mockzilla/commands/service/).

## Quick Start

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest \
  -name petstore \
  https://petstore3.swagger.io/api/v3/openapi.json
```

## Generated Structure

```
services/petstore/
├── setup/
│   ├── openapi.yml     # OpenAPI spec
│   ├── config.yml      # Service config
│   ├── codegen.yml     # Code generation config
│   └── context.yml     # Data generation context
├── types/              # Generated Go types
├── handler/            # Generated request handlers
├── register.go         # Service registration
└── middleware.go       # Custom middleware (editable)
```
