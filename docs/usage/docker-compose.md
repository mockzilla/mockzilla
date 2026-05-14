# Docker Compose

Run Mockzilla as a container alongside your app. No Go install
needed. Mount your per-service tree at `/data/services` and start the
stack.

## Quick Start

```yaml
services:
  my-service:
    image: mockzilla/mockzilla:latest
    ports:
      - "2200:2200"
    volumes:
      - ./services:/data/services:ro
```

Drop each service into `services/<name>/` and run `docker compose up`.

## Layout

The container expects the portable per-service shape under
`/data` (configurable via `MOCKZILLA_DATA`):

```text
project/
├── docker-compose.yml
├── app.yml                                ← optional global settings
└── services/
    ├── petstore/                          ← spec-only
    │   ├── openapi.yml
    │   ├── config.yml
    │   └── context.yml
    ├── stripe/                            ← spec with mount override
    │   ├── openapi.yml
    │   └── config.yml                     ← mount: stripe/v1
    ├── myapi/                             ← static-only
    │   ├── users/get/index.json
    │   └── users/{id}/get/index.json
    └── orders/                            ← merge: spec + static overrides
        ├── openapi.yml
        └── orders/{orderId}/get/index.json
```

```bash
curl http://localhost:2200/petstore/pets
curl http://localhost:2200/stripe/v1/customers
curl http://localhost:2200/myapi/users
curl http://localhost:2200/orders/orders         # spec
curl http://localhost:2200/orders/orders/abc     # static override
curl http://localhost:2200/orders/openapi.yml    # spec as literal asset
```

See [Services](../services.md) for full discovery rules per folder.

## Global app config

Mount an optional `app.yml` next to `services/`:

```yaml
volumes:
  - ./services:/data/services:ro
  - ./app.yml:/data/app.yml:ro
```

`app.yml` holds only global settings (port, history, storage, etc.).
Per-service knobs live in each service's `config.yml`.

```yaml
port: 2200
history:
  enabled: true
  duration: 1h
```

Environment variables also work and override file values:

```yaml
environment:
  - ROUTER_HISTORY_ENABLED=false
  - APP_DISABLE_CONFIG_UI=true
```

See [Service Config](../config/service.md) for per-service options.

## Custom data root

Override with `MOCKZILLA_DATA`:

```yaml
services:
  mockzilla:
    image: mockzilla/mockzilla:latest
    environment:
      - MOCKZILLA_DATA=/srv/mocks
    volumes:
      - ./services:/srv/mocks/services:ro
```

## Health check and app integration

```yaml
services:
  my-service:
    image: mockzilla/mockzilla:latest
    ports:
      - "2200:2200"
    volumes:
      - ./services:/data/services:ro
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

Other containers in the same Compose network reach Mockzilla at
`http://my-service:2200`.

A working version is in
[`examples/docker-compose/raw-specs`](https://github.com/mockzilla/mockzilla/tree/main/examples/docker-compose/raw-specs).
