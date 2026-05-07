# Docker Compose

Run Mockzilla as a container alongside your app. No Go or binary install needed — just mount your spec files and start the stack.

## Quick Start

Create a `docker-compose.yml`:

```yaml
services:
  my-service:
    image: mockzilla/mockzilla:latest
    ports:
      - "2200:2200"
    volumes:
      - ./openapi:/app/resources/data/openapi:ro
```

Place your OpenAPI specs in the `openapi/` directory and run `docker compose up`.

## Spec Files and Static Responses

The container reads from two mount points:

- `/app/resources/data/openapi/` — OpenAPI spec files (YAML or JSON).
- `/app/resources/data/static/` — static response files organized by path and method.

A flat file like `openapi/petstore.yml` creates a service at `/petstore/...`. A nested directory like `openapi/stripe/openapi.yml` creates a service at `/stripe/...`. Both patterns work side by side.

Static files follow a directory convention — the path and HTTP method become the URL:

```text
static/myapi/users/get/index.json  →  GET /myapi/users
```

Supported file types: `.json`, `.xml`, `.html`, `.txt`, `.yaml`, `.yml`. See [Services](../services.md) for more on how service names are resolved.

## Configuration

The Docker image runs in portable mode. Mount an `app.yml` to `/app/resources/data/app.yml` to configure app-level settings and per-service behavior in a single file:

```yaml
app:
  title: My API Mock
  history:
    enabled: false

services:
  stripe:
    latency: 50ms
    cache:
      requests: true
    errors:
      p5: 500
      p2: 429
```

```yaml
volumes:
  - ./app.yml:/app/resources/data/app.yml:ro
```

Per-service context replacements can be mounted the same way at `/app/resources/data/context.yml`.

Environment variables also work and override file values:

```yaml
environment:
  - ROUTER_HISTORY_ENABLED=false
  - APP_DISABLE_CONFIG_UI=true
```

See [Service Config](../config/service.md) for all per-service options.

## Health Check and App Integration

Use the built-in readiness endpoint to make your app wait for Mockzilla:

```yaml
services:
  my-service:
    image: mockzilla/mockzilla:latest
    ports:
      - "2200:2200"
    volumes:
      - ./openapi:/app/resources/data/openapi:ro
      - ./static:/app/resources/data/static:ro
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:2200/healthz"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 30s

  app:
    build: ./your-app
    environment:
      - API_BASE_URL=http://my-service:2200
    depends_on:
      my-service:
        condition: service_healthy
```

Other containers in the same Compose network reach Mockzilla at `http://my-service:2200`.

## Full Example

```text
project/
├── docker-compose.yml
├── app.yml
├── openapi/
│   ├── petstore.yml
│   └── stripe/
│       └── openapi.yml
└── static/
    └── myapi/
        └── users/
            └── get/
                └── index.json
```

```bash
curl http://localhost:2200/petstore/pets
curl http://localhost:2200/stripe/customers
curl http://localhost:2200/myapi/users
```

A working version is in [`examples/docker-compose/raw-specs`](https://github.com/mockzilla/mockzilla/tree/main/examples/docker-compose/raw-specs).
