# Codegen Mode

Codegen mode generates typed Go code from your OpenAPI specs, giving you compiled services with custom middleware,
typed request/response handlers, and a single binary you can deploy anywhere.

Use codegen mode when you need more than what [Portable Mode](portable.md) offers -
custom business logic, authentication middleware, request validation, or a self-contained binary.

## Quick Start

Generate a service from the Petstore spec:

```bash
mkdir myapp && cd myapp
go mod init myapp

go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest \
  -name petstore \
  https://petstore3.swagger.io/api/v3/openapi.json
```

Add the dependency and generate the code:

```bash
go get github.com/mockzilla/mockzilla/v2@latest
go mod tidy
go generate ./...
```

Create `main.go`:

```go
package main

import (
    "log"
    "net/http"

    "github.com/mockzilla/mockzilla/v2/pkg/api"
    "github.com/mockzilla/mockzilla/v2/pkg/loader"

    _ "myapp/petstore"
)

func main() {
    router := api.NewRouter()
    loader.LoadAll(router)
    log.Println("Starting server on :2200")
    log.Fatal(http.ListenAndServe(":2200", router))
}
```

Run it:

```bash
go run main.go
```

## How It Works

1. **Generate** - the `gen/service` command reads your OpenAPI spec and produces Go code
2. **Customize** - edit `service.go` and `middleware.go` (never overwritten on regeneration)
3. **Build** - compile into a single binary with `go build`
4. **Deploy** - ship the binary anywhere, no runtime dependencies

## Generated Structure

```text
petstore/
├── generate.go          # go generate directive for regeneration
├── gen.go               # Generated types, handlers, registration (do not edit)
├── service.go           # Your business logic (edit this)
├── middleware.go         # Custom middleware (edit this, enable in codegen.yml)
└── setup/
    ├── codegen.yml      # Code generation settings
    ├── config.yml       # Service runtime config
    ├── context.yml      # Context values for response generation
    └── openapi.json     # OpenAPI spec
```

`service.go` and `middleware.go` are generated once and never overwritten. `gen.go` regenerates with `go generate`.

## Enabling Middleware

To get `middleware.go` generated, uncomment the middleware line in `setup/codegen.yml`:

```yaml
generate:
  handler:
    service: {}
    middleware: {}   # uncomment this line
```

Then regenerate:

```bash
go generate ./...
```

## Customizing Handlers

Each operation gets a method in `service.go`. Return `nil, nil` to use the auto-generated mock response, or return your own:

```go
func (s *service) GetPetByID(ctx context.Context, opts *GetPetByIDServiceRequestOptions) (*GetPetByIDResponseData, error) {
    // Use the generator as a starting point
    resp, err := opts.GenerateResponse()
    if err != nil {
        return nil, err
    }

    // Override specific fields
    resp.Body.ID = opts.PathParams.PetId
    resp.Body.Name = "Custom Pet"

    return resp, nil
}
```

## Custom Middleware

Edit `middleware.go` to add authentication, logging, or request modification. See [Custom Middleware](../middleware.md) for details and examples.

## Multiple Services

Generate each service, then import them all in a single `main.go`:

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest -name petstore ./petstore.yml
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest -name payments ./payments.yml
go generate ./...
```

```go
package main

import (
    "log"
    "net/http"

    "github.com/mockzilla/mockzilla/v2/pkg/api"
    "github.com/mockzilla/mockzilla/v2/pkg/loader"

    _ "myapp/petstore"
    _ "myapp/payments"
)

func main() {
    router := api.NewRouter()
    loader.LoadAll(router)
    log.Println("Starting server on :2200")
    log.Fatal(http.ListenAndServe(":2200", router))
}
```

Each service registers itself via `init()` when imported.

## Regeneration

After changing the OpenAPI spec or codegen settings, regenerate:

```bash
go generate ./...
```

This regenerates `gen.go`. Your `service.go` and `middleware.go` are preserved.

## Service Configuration

Each service has a `setup/config.yml` for runtime behavior. See [Service Config](../config/service.md) for all options:

```yaml
latency: 100ms
errors:
  p5: 500
upstream:
  url: https://api.example.com
cache:
  requests: true
```

## Code Generation Settings

Control what gets generated via `setup/codegen.yml`. See [Codegen Config](../config/codegen.md) for details.

## Learn More

- [Service Config](../config/service.md) - latency, errors, upstream proxy, caching, replay
- [Codegen Config](../config/codegen.md) - code generation settings
- [Custom Middleware](../middleware.md) - authentication, logging, request modification
- [Contexts](../contexts.md) - control generated values with fake data, patterns, aliases
- [Replay](../replay.md) - record and replay API responses
- [Factory](../factory.md) - use the generation engine programmatically from Go
- [Service Command](../commands/service.md) - full reference for `gen/service`

## Template

Start from a GitHub template with CI/CD, API Explorer UI, and example services ready to go:

- [connexions-codegen-template](https://github.com/mockzilla/connexions-codegen-template)

See [Templates](../templates.md) for all available templates.

## When to Use Codegen Mode

| Need | Use |
|------|-----|
| Quick prototyping, testing | [Portable Mode](portable.md) |
| Custom auth, business logic | Codegen Mode |
| Single deployable binary | Codegen Mode |
| Typed request/response handling | Codegen Mode |
| CI/CD mock server | Either |
