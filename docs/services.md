# Services

Services are the core building blocks of Connexions. A service represents a collection of API endpoints that can be served as a mock server.

## Multiple APIs on One Server

Each service gets its own **URL prefix** based on its name. This allows you to run multiple APIs simultaneously:

```
openapi/
├── petstore.yml      → /petstore/pets, /petstore/pets/{id}
├── stripe/
│   └── openapi.yml   → /stripe/customers, /stripe/charges
└── github.yml        → /github/repos, /github/users
```

All services share the same port (default: 2200), so you can mock your entire microservices architecture with a single Connexions instance.

The service name is determined by:

1. The `name` property in `config.yml` (if provided)
2. The directory name (for nested specs like `openapi/stripe/openapi.yml`)
3. The filename (for flat specs like `openapi/petstore.yml`)

See [Service Configuration](config/service.md) for details on the `name` property.

## Service Types

Connexions supports three types of services, each suited for different use cases:

| Type | Best For | Hot Reload | Customization |
|------|----------|------------|---------------|
| **OpenAPI Spec** | Quick prototyping, testing | ✅ Yes | Basic (config.yml) |
| **Static Files** | Fixed responses, edge cases | ✅ Yes | Full control |
| **Compiled Go** | Production, full control | ❌ No | Complete (middleware, handlers) |

### OpenAPI Spec Services

The simplest way to create a mock server. Just provide an OpenAPI specification and Connexions generates responses automatically.

```bash
connexions my-spec.yml
```

Responses are generated based on:

- Schema definitions (types, formats, constraints)
- Example values in the spec
- Context files for realistic data

### Static File Services

For endpoints that need fixed, predictable responses. Useful for:

- Testing specific edge cases
- Returning exact response bodies
- Mocking endpoints not in your OpenAPI spec

Static files override OpenAPI-generated responses when both exist for the same endpoint.

### Compiled Go Services

For maximum control and performance. Generated Go code that you can customize:

- Add custom middleware (authentication, logging)
- Modify request/response handling
- Add business logic
- Compile into a single binary

```bash
# Generate a service from spec
go run github.com/mockzilla/connexions/v2/cmd/gen/service@latest \
  -name petstore \
  https://petstore3.swagger.io/api/v3/openapi.json

# Build and run
cd services/petstore && go generate && go build && ./petstore
```

See [service command](commands/service.md) for details.

#### Customizing Generated Responses

When implementing a service method, use `opts.GenerateResponse()` to get a
pre-populated response based on the OpenAPI spec, then modify specific fields:

```go
func (s *service) GetUser(ctx context.Context, opts *GetUserServiceRequestOptions) (*GetUserResponseData, error) {
    // Get a spec-compliant response with generated values
    resp, err := opts.GenerateResponse()
    if err != nil {
        return nil, err
    }

    // Override specific fields
    resp.Body.ID = opts.PathParams.UserID
    resp.Body.Email = "custom@example.com"

    return resp, nil
}
```

This is useful when you need realistic data but want to control specific values
(e.g., IDs matching the request, business-specific logic).


## File Structure

Services are organized in the `resources/data/` directory:

```
resources/data/
├── openapi/           # OpenAPI spec files
│   ├── petstore.yml   # → serves at /*
│   └── payments/
│       └── v1.yml     # → serves at /payments/*
├── static/            # Static response files
│   └── petstore/
│       └── pets/
│           └── get/
│               └── index.json  # → GET /petstore/pets
└── services/          # Compiled Go services (generated)
    └── petstore/
        ├── setup/     # Configuration files
        ├── types/     # Generated types
        └── handler/   # Generated handlers
```

### OpenAPI Directory

Place OpenAPI specs in `openapi/`. The file/folder name becomes the service name:

| File | Serves |
|------|--------|
| `petstore.yml` | `/*` (all paths in spec) |
| `payments/v1.yml` | `/payments/*` |

### Static Directory

Static files provide fixed responses. Structure: `static/{service}/{path}/{method}/index.json`

| File | Endpoint |
|------|----------|
| `petstore/pets/get/index.json` | `GET /petstore/pets` |
| `petstore/pets/post/index.json` | `POST /petstore/pets` |
| `petstore/pets/{id}/get/index.json` | `GET /petstore/pets/{id}` |

Use `{param}` in directory names for path parameters.

### Services Directory

Generated Go services live in `services/`. Each service has:

- `setup/` - Configuration files (`config.yml`, `codegen.yml`, `openapi.yml`)
- `types/` - Generated Go types from OpenAPI schemas
- `handler/` - Generated HTTP handlers
- `register.go` - Service registration
- `middleware.go` - Customizable middleware (not overwritten)
