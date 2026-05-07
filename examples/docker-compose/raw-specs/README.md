# Raw Specs with Docker Compose

Mount OpenAPI specs and static files to get a mock server.

## Quick Start

```bash
docker compose up
```

## Test

```bash
curl http://localhost:2200/petstore/pets
curl http://localhost:2200/myapi/users
curl http://localhost:2200/stripe/customers
```

## Directory Structure

```
raw-specs/
├── docker-compose.yml
├── app.yml                       # App + per-service config
├── openapi/
│   ├── petstore.yml              # Flat: service name from filename
│   └── stripe/
│       └── openapi.yml           # Nested: service name from directory
└── static/
    └── myapi/
        └── users/
            └── get/
                └── index.json
```

## Configuration

Mount `app.yml` to configure app settings and per-service behavior:

```yaml
app:
  title: Mockzilla
  history:
    enabled: false

services:
  stripe:
    latency: 50ms
    cache:
      requests: true
```

See `app.yml` in this directory for a full example.

## Integration with Your App

Uncomment the `app` service in `docker-compose.yml` to run your application alongside the mock server:

```yaml
services:
  mockzilla:
    # ...

  app:
    build: ./your-app
    environment:
      - API_BASE_URL=http://mockzilla:2200
    depends_on:
      mockzilla:
        condition: service_healthy
```
