# Replay

Record API responses and replay them on subsequent requests that match specific request fields. Works like VCR - record once, replay on match.

```yaml
cache:
  replay:
    ttl: 24h
    auto-replay: false
    upstream-only: false
    endpoints:
      /foo/{f-id}/bar/{b-id}:
        POST:
          match:
            path:
              - f-id
            body:
              - data.name
              - data.address.zip
```

## How It Works

1. A request comes in with the `X-Mz-Replay` header (or to an `auto-replay` endpoint)
2. Specified fields are extracted from the request body and/or query string
3. A content-addressed key is built from the method, path pattern, and extracted values
4. If a recording exists for that key - return it immediately with `X-Mz-Source: replay`
5. If no recording exists - forward to downstream, capture the response, store it, and return it

## Activation

Replay activates in two ways:

**Header-based (default):** Send the `X-Mz-Replay` header to activate replay for any request.

```bash
# Empty header - uses match fields from config
curl -X POST /svc/foo/123/bar/456 \
  -H "X-Mz-Replay:" \
  -d '{"data": {"name": "Jane", "address": {"zip": "12345"}}}'

# Header with body fields - overrides config
curl -X POST /svc/foo/123/bar/456 \
  -H "X-Mz-Replay: data.name,data.address.zip" \
  -d '{"data": {"name": "Jane", "address": {"zip": "12345"}}}'

# Header with explicit body and query fields
curl -X POST /svc/pay?channel=web \
  -H "X-Mz-Replay: body:biller,reference;query:channel" \
  -d 'biller=BLR0001&reference=REF123'
```

Fields can also include path variables using the `path:` prefix:

```bash
# Header with path, body, and query fields
curl -X POST /svc/pay/credit-card/tx/123 \
  -H "X-Mz-Replay: path:paymentMethodName;body:reference;query:channel" \
  -d 'reference=REF123'
```

Unqualified fields in the header (without `path:`, `body:`, or `query:` prefix) are treated as body fields.

**Auto-replay:** Set `auto-replay: true` in config to activate for configured endpoints without requiring the header.

```yaml
cache:
  replay:
    auto-replay: true
    endpoints:
      /users:
        POST:
          match:
            body:
              - email
```

## Endpoint Configuration

The HTTP method level is optional. Three forms are supported:

```yaml
endpoints:
  # Path only - matches any HTTP method, no match fields
  /health:

  # Path + HTTP method - matches only POST, no match fields (key is HTTP method + path only)
  /notify:
    POST:

  # Path + HTTP method + match - full config
  /search:
    POST:
      match:
        body:
          - term
          - filters.category
```

When the HTTP method is omitted, the request's actual HTTP method is used for key building.

When multiple HTTP methods are configured for the same path, the request method must match exactly:

```yaml
endpoints:
  /users/{user-id}/orders:
    POST:
      match:
        body:
          - items[0].product_id
          - shipping.method
    PUT:
      match:
        body:
          - order_id
```

## Match Fields

Match fields specify which request values form the replay key. Each field is explicitly sourced from path variables, the request body, or the query string.

```yaml
match:
  path:           # extracted from URL path variables
    - paymentMethodName
  body:           # extracted from request body
    - biller
    - reference
  query:          # extracted from URL query string
    - channel
```

### Path fields

Path fields include specific path variable values in the replay key. By default, path variables are ignored - all path variable values produce the same key (e.g., `/pay/credit-card` and `/pay/bank-transfer` share recordings). Adding path variables to the match config makes them produce different keys.

```yaml
endpoints:
  /pay/{paymentMethodName}/tx/{txId}:
    POST:
      match:
        path:
          - paymentMethodName    # include in key - different payment methods get different recordings
        body:                    # txId is NOT listed - all txIds share recordings
          - reference
```

```bash
# credit-card and bank-transfer produce different recordings
curl -X POST /svc/pay/credit-card/tx/123 \
  -H "X-Mz-Replay:" \
  -d 'reference=REF123'

curl -X POST /svc/pay/bank-transfer/tx/456 \
  -H "X-Mz-Replay:" \
  -d 'reference=REF123'
```

### Body fields

Body fields are extracted based on the request's `Content-Type`:

**JSON body** - dotted paths for nested structures:

```yaml
match:
  body:
    - data.name                # simple nested path
    - data.items[0].name       # array index
    - data.items.name          # array wildcard (first match)
    - "[0].name"               # top-level array index
```

```bash
curl -X POST /svc/search \
  -H "Content-Type: application/json" \
  -H "X-Mz-Replay:" \
  -d '{"data": {"name": "Jane"}}'
```

**JSON array body** - when the root is an array, use `[index]` or wildcard:

```yaml
match:
  body:
    - "[0].name"               # first element's name
    - name                     # wildcard - searches all elements, returns first match
```

```bash
curl -X POST /svc/pets \
  -H "Content-Type: application/json" \
  -H "X-Mz-Replay:" \
  -d '[{"name": "doggie", "tag": "fundamental-window"}]'
```

**Form-encoded body** - flat keys matching form field names:

```yaml
match:
  body:
    - biller
    - reference
```

```bash
curl -X POST /svc/pay \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -H "X-Mz-Replay:" \
  -d 'amount=50&biller=BLR0001&reference=REF123'
```

### Query fields

Query fields are extracted from the URL query string:

```yaml
match:
  query:
    - amount
    - biller
```

```bash
curl /svc/pay?amount=50&biller=BLR0001 \
  -H "X-Mz-Replay:"
```

### Mixed: body + query

Body and query fields can be combined:

```yaml
match:
  body:
    - biller
    - reference
  query:
    - channel
```

```bash
curl -X POST /svc/pay?channel=web \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -H "X-Mz-Replay:" \
  -d 'amount=50&biller=BLR0001&reference=REF123'
```

Here `biller` and `reference` come from the form body, `channel` comes from the query string.

## Key Design

The replay key is a SHA-256 hash of: `METHOD:pattern_path|body:field1=value1|path:var1=value1|query:field2=value2|...`

- **Pattern path** is used (e.g., `/foo/{id}/bar`), not the actual URL - so different path parameter values share recordings unless explicitly included via `path` match fields
- **Fields are sorted** alphabetically for determinism
- Each field is prefixed with its source (`path:`, `body:`, or `query:`) in the key
- Only the matched fields matter - other body/query/path content is ignored
- If any configured match field is missing from the request (path variable not found, body field not found, query parameter absent), replay is skipped entirely - no recording or matching is attempted

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `ttl` | duration | `24h` | How long recordings are kept |
| `upstream-only` | bool | `false` | Only record responses from upstream services |
| `auto-replay` | bool | `false` | Activate for configured endpoints without the header |
| `endpoints` | map | - | Path patterns with optional methods and match fields |

## Upstream-Only Mode

When `upstream-only: true`, only responses from upstream services are recorded. If the response is not from upstream (e.g., generated or cached), the middleware returns a `502 Bad Gateway` error instead of passing the response through. This makes it explicit to the caller that the recording was skipped.

```yaml
cache:
  replay:
    upstream-only: true
    endpoints:
      /external-api/search:
        POST:
          match:
            body:
              - term
              - filters.category
```

## Example: Full Configuration

```yaml
name: my-service
upstream:
  url: https://api.example.com
cache:
  requests: true
  replay:
    ttl: 12h
    upstream-only: true
    auto-replay: false
    endpoints:
      # JSON body match
      /search:
        POST:
          match:
            body:
              - term
              - filters.category
      # Multiple methods with different match fields
      /users/{user-id}/orders:
        POST:
          match:
            body:
              - items[0].product_id
              - shipping.method
        PUT:
          match:
            body:
              - order_id
      # Path variable + body match
      /pay/{paymentMethodName}:
        POST:
          match:
            path:
              - paymentMethodName
            body:
              - biller
              - reference
            query:
              - channel
      # Query string only
      /lookup:
        GET:
          match:
            query:
              - account_id
              - type
      # Path only - match by method + path, any HTTP method
      /health:
```
