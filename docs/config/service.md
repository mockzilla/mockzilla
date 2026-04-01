# Service Configuration

Each service can have its own `config.yml` file that controls runtime behavior like latency simulation, error injection, validation, and caching.

## Location

For compiled Go services, place `config.yml` in the `setup/` directory:

```
services/petstore/
└── setup/
    ├── config.yml      # Service configuration
    ├── codegen.yml     # Code generation settings
    └── openapi.yml     # OpenAPI specification
```

For the full server mode with mounted specs, use a directory structure with `config.yml` alongside your spec:

```
openapi/
└── petstore/
    ├── openapi.yml     # OpenAPI spec (must be named openapi.yml/yaml/json)
    └── config.yml      # Service config
```

Note: Flat specs like `openapi/petstore.yml` don't support config files - use the directory structure above.

For portable mode, use the `--config` flag instead. See [Portable Mode](../usage/portable.md).

## Configuration Options

### Service Name

```yaml
name: petstore
```

The `name` property defines the **URL prefix** for all routes in this service.

For example, if your OpenAPI spec has `/pets` and `/pets/{id}`, they become:
- `GET /petstore/pets`
- `GET /petstore/pets/{id}`

This allows multiple APIs to coexist on the same server without route conflicts.

**How the name is determined:**

1. If `name` is set in `config.yml` → uses that name
2. Otherwise → inferred from directory name (e.g., `openapi/stripe/` → `stripe`)
3. For flat specs → inferred from filename (e.g., `openapi/petstore.yml` → `petstore`)

### History

```yaml
history:
  enabled: true           # Record request/response history (default: true)
  mask-headers:            # Header names to mask in history entries
    - Authorization
    - Cookie
    - Set-Cookie
    - X-Api-Key
```

When enabled (default), incoming requests and their responses are recorded in the service's history table. This data is available via the DB Explorer UI or the history API.

**Header masking:** Headers matching `mask-headers` patterns have their values replaced with asterisks, keeping only the last 4 characters visible. For example, `Bearer sk-proj-abc123` becomes `***********c123`.

Patterns support two forms:
- **Exact match** - `Authorization` matches only that header
- **Prefix match** - `X-Internal-*` matches any header starting with `X-Internal-`

Matching is case-insensitive. By default, `Authorization`, `Cookie`, `Set-Cookie`, and `X-Api-Key` are masked.

**Shorthand:** to disable history entirely:

```yaml
history:
  enabled: false
```

The boolean shorthand `history: false` is also supported for backward compatibility.

### Latency

```yaml
# Request/response history
history:
  enabled: true
  mask-headers:
    - Authorization
    - Cookie
    - Set-Cookie
    - X-Api-Key

# Simulated latency for responses
latency: 100ms

# Latency distribution by percentile
latencies:
  p50: 50ms
  p90: 100ms
  p99: 500ms

# Error injection by percentile
errors:
  p5: 500   # 5% of requests return 500
  p10: 400  # 5% return 400 (p10 - p5)

# Caching behavior
cache:
  requests: true  # Cache GET request responses
  replay:
    ttl: 24h
    auto-replay: false
    upstream-only: false
    endpoints:
      /path/{id}:
        POST:
          match:
            body:
              - data.name

# OpenAPI spec simplification
spec:
  simplify: false
  # optional-properties not set = keep all optional properties
  # optional-properties:
  #   min: 5
  #   max: 5
```

## Latency Simulation

Simulate real-world network conditions:

```yaml
# Fixed latency
latency: 100ms

# Or percentile-based distribution
latencies:
  p25: 10ms   # 25% of requests: 10ms
  p50: 50ms   # 25% of requests: 50ms  
  p90: 100ms  # 40% of requests: 100ms
  p99: 500ms  # 9% of requests: 500ms
  p100: 1s    # 1% of requests: 1s
```

## Error Injection

Test error handling by injecting HTTP errors:

```yaml
errors:
  p5: 500   # 5% return 500 Internal Server Error
  p10: 400  # 5% return 400 Bad Request
  p15: 429  # 5% return 429 Too Many Requests
```

Percentiles are cumulative - `p10: 400` means requests between p5 and p10 (5%) return 400.

## Caching

Cache responses for GET requests:

```yaml
cache:
  requests: true  # Enable response caching
```

Cached responses are returned for identical GET requests, improving performance.

## Replay

Record and replay API responses based on request fields. See [Replay](../replay.md) for full documentation.

```yaml
cache:
  replay:
    ttl: 24h
    auto-replay: false
    endpoints:
      /search:
        POST:
          match:
            body:
              - query
```

## Spec Simplification

Reduce complexity of large OpenAPI specs:

```yaml
spec:
  simplify: true   # Enable/disable simplification
  optional-properties:
    min: 5         # Keep exactly 5 optional properties (when min == max)
    max: 5
    # OR
    # min: 2       # Keep random number between 2-8 optional properties
    # max: 8
```

When `optional-properties` is not set, all optional properties are kept.
This helps with specs that have schemas with many optional fields.

## Upstream Proxy

Forward requests to a real backend:

```yaml
upstream:
  url: https://api.example.com
  timeout: 5s               # Request timeout (default: 5s)
  headers:
    X-Custom-Header: value
  fail-on:                   # Return these statuses directly (default: 400-499 except 401, 403)
    - range: "400-499"
      except: [401, 403]
```

When configured, requests are proxied to the upstream server.
If the upstream fails (timeout, network error, or error status), Connexions falls back to generating mock responses.

### Fail-On

Control which upstream error status codes are returned directly to the client instead of falling back to the generator:

```yaml
upstream:
  url: https://api.example.com
  fail-on:                     # Return these statuses directly (no generator fallback)
    - range: "400-499"
      except: [401, 403]
    - exact: 502
```

| Configuration | Behavior |
|---|---|
| Not set (omitted) | Default: `400-499` except `401` and `403` are returned directly |
| `fail-on: []` | Disabled - all errors fall back to the generator |
| `fail-on: [{range: "400-499"}]` | All 4xx returned directly (no exceptions) |

## Response Headers

Connexions adds the following headers to responses:

| Header | Values | Description |
|--------|--------|-------------|
| `X-Cxs-Source` | `upstream`, `cache`, `generated`, `replay` | Indicates where the response came from |
| `X-Cxs-Duration` | e.g. `1.234ms` | Request processing time |

## Contexts

See [Contexts](../contexts.md) for details on context files.

