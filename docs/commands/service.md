# Service Command

The service command generates a complete service from an OpenAPI spec or static files.
It handles both setup (creating configuration files) and code generation in a single step.

## Usage

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest [-name <name>] [-type openapi|static] [-output <dir>] [spec-file-or-url]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `<source>` | Optional path to OpenAPI spec file or URL |

## Flags

| Flag | Description |
|------|-------------|
| `-name` | Service name. If not provided, inferred from directory name |
| `-type` | Service type: `openapi` or `static`. Inferred from source if not provided |
| `-output` | Output directory for the service. Defaults to current directory |
| `-codegen-config` | Optional path to custom `codegen.yml` to merge with template |
| `-service-config` | Optional path to custom `config.yml` to merge with template |
| `-quiet` | Suppress non-error output |

## How It Works

The command performs two steps automatically:

1. **Ensure setup directory exists** - If `<output>/setup/` doesn't exist, creates it with configuration files
2. **Generate service code** - Generates types, handlers, register.go, middleware.go, and optionally server/

## Source Handling

The command automatically detects whether the source is a URL or a local path:

- **URL provided**: The URL is embedded in `generate.go`. The spec is fetched at generation time.
- **Local path provided**: The file is copied into the setup directory as `openapi.yml`.

## Output Structure

The command creates a service directory with this structure:

```
<output>/
├── generate.go          # Go generate file for regeneration
├── setup/
│   ├── codegen.yml      # Code generation settings
│   ├── config.yml       # Service runtime configuration
│   ├── context.yml      # Context variables for response generation
│   └── openapi.yml      # OpenAPI spec (only if local path was provided)
├── types/               # Generated Go types
├── handler/             # Generated request handlers
├── register.go          # Service registration
├── middleware.go        # Middleware configuration (only generated once)
└── server/              # Optional server main.go
```

## Examples

### From URL (in current directory)

```bash
cd myservice
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest https://petstore3.swagger.io/api/v3/openapi.json
```

### From local file

```bash
cd myservice
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest ./specs/openapi.yml
```

### With custom output directory

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest -output /path/to/service ./specs/openapi.yml
```

### With custom name

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest -name petstore https://petstore3.swagger.io/api/v3/openapi.json
```

### With custom configs

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest \
  -codegen-config ./my-codegen.yml \
  -service-config ./my-config.yml \
  ./specs/openapi.yml
```

## Regeneration

After initial generation, you can regenerate the service by running:

```bash
cd <service-dir> && go generate
```

Or simply run the service command again from the service directory.

## Configuration

### Enabling Server Generation

By default, `server/main.go` is not generated. To enable it, add to your `setup/config.yml`:

```yaml
generate:
  server: {}
```

