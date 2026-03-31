# Portable Mode

Run a mock server directly from OpenAPI spec files - no Docker, no code generation, no setup.

## Install

With Go installed, run directly:

```bash
go run github.com/mockzilla/connexions/v2/cmd/server@latest petstore.yml
```

Or build a binary:

```bash
go install github.com/mockzilla/connexions/v2/cmd/server@latest
```

This installs the `server` binary to your `$GOPATH/bin`. You can rename it to `connexions` if you prefer.

## Quick Start

Try it now - no files needed:

```bash
go run github.com/mockzilla/connexions/v2/cmd/server@latest \
  https://petstore3.swagger.io/api/v3/openapi.json
```

The server starts on port 2200 with the Petstore API mounted at `/openapi/...`.

Or from a local file:

```bash
connexions petstore.yml
```

## Multiple Specs

Pass any mix of files, directories, and URLs:

```bash
# Multiple files
connexions petstore.yml stripe.yml spoonacular.yml

# Directory containing specs
connexions ./my-specs/

# URLs
connexions https://petstore3.swagger.io/api/v3/openapi.json https://example.com/api.yml

# Mix of files and URLs
connexions petstore.yml https://example.com/stripe.yml ./more-specs/
```

Each spec becomes a separate service. The service name is derived from the filename or URL path (e.g., `petstore.yml` becomes `/petstore/`).

## Flags

| Flag | Description |
|------|-------------|
| `--port` | Server port (default: from config or 2200) |
| `--config` | Unified config YAML (app settings + per-service config) |
| `--context` | Per-service context YAML for value replacements |

```bash
connexions --port 3000 --config config.yml --context contexts.yml petstore.yml stripe.yml
```

## Config File

The `--config` flag accepts a unified YAML file with two optional sections: 
`app` for application settings and `services` for per-service configuration.

```yaml
app:
  port: 3000
  title: "My Mock Server"

services:
  petstore:
    latency: 100ms
    errors:
      p10: 400
  stripe:
    latency: 200ms
    errors:
      p5: 500
      p10: 429
```

### App Section

Controls application-level settings. All fields from [App Config](../config/app.md) are supported. If omitted, defaults are used.

```yaml
app:
  port: 3000
  title: "My Mocks"
```

The `--port` flag overrides the port from the config file.

### Services Section

Per-service configuration for latency, errors, upstream proxy, caching, and more. 
Keys are service names (derived from spec filenames). 
All fields from [Service Config](../config/service.md) are supported.

```yaml
services:
  petstore:
    latency: 100ms
    errors:
      p10: 400
    upstream:
      url: https://petstore.example.com
  stripe:
    latency: 50ms
    cache:
      requests: true
```

Services not listed in the config get default settings.

## Context File

The `--context` flag accepts a YAML file with per-service context values for controlling generated data. 
Keys are service names, values follow the [Contexts](../contexts.md) format.

```yaml
petstore:
  name: "doggie"
  status:
    - available
    - pending
    - sold

spoonacular:
  cuisineType:
    - Italian
    - Chinese
    - Mexican
```

Services not listed get default contexts only.

## Static Files

If a directory contains a `static/` subdirectory, its contents are automatically converted to OpenAPI specs. Organize static responses by service name, path, and HTTP method:

```text
my-mocks/
├── petstore.yml              # regular OpenAPI spec
└── static/
    └── myapi/
        └── users/
            ├── get/
            │   └── index.json    # GET /myapi/users
            └── {id}/
                └── get/
                    └── index.json    # GET /myapi/users/{id}
```

```bash
connexions ./my-mocks/
```

This registers both `petstore` (from the spec) and `myapi` (from static files). Static files are converted to OpenAPI specs internally - the service behaves identically to a spec-based one.

Supported file types: `.json`, `.xml`, `.html`, `.txt`, `.yaml`, `.yml`.

## Hot Reload

Spec files are watched for changes. When you edit a spec file, the service handler is hot-swapped without restarting the server. New spec files added to watched directories are automatically registered.

## Examples

From a URL:

```bash
connexions https://petstore3.swagger.io/api/v3/openapi.json
```

With config and contexts:

```bash
connexions \
  --config config.yml \
  --context contexts.yml \
  --port 8080 \
  petstore.yml stripe.yml
```

Mix of local files and URLs:

```bash
connexions petstore.yml https://example.com/stripe.yml ./more-specs/
```

Config-only (no custom contexts):

```bash
connexions --config config.yml ./specs/
```

## Template

Start from a GitHub template to get a ready-to-use project with CI/CD that builds a single binary with your specs embedded:

- [connexions-portable-template](https://github.com/mockzilla/connexions-portable-template)

See [Templates](../templates.md) for all available templates.
