# Raw Specs (Docker Compose)

Same layout as `../docker/raw-specs/`, wrapped in a compose file so you
can pull a healthchecked mock server up alongside other services.

## Quick start

```bash
docker compose up
```

## Test

```bash
# Spec-driven endpoints
curl http://localhost:2200/petstore/pets
curl http://localhost:2200/petstore/pets/42

# Spec with mount override → mounted at /stripe/v1
curl http://localhost:2200/stripe/v1/customers

# Static-only service
curl http://localhost:2200/myapi/users
curl http://localhost:2200/myapi/users/1

# Merge: spec drives /orders, static overrides /orders/{orderId}
curl http://localhost:2200/orders
curl http://localhost:2200/orders/ord_X               # always returns ord_known_001
curl http://localhost:2200/orders/openapi.yml         # spec as literal asset
```

## Layout

```
raw-specs/
├── docker-compose.yml
├── app.yml                                 ← optional global settings
└── services/
    ├── petstore/                           ← spec-only service
    │   ├── openapi.yml
    │   ├── config.yml
    │   └── context.yml
    ├── stripe/                             ← spec with mount override
    │   ├── openapi.yml
    │   └── config.yml                      ← mount: stripe/v1
    ├── myapi/                              ← static-only service
    │   ├── users/get/index.json
    │   └── users/{id}/get/index.json
    └── orders/                             ← merge: spec + static
        ├── openapi.yml
        └── {orderId}/get/index.json
```

See `../docker/raw-specs/README.md` for the full reference on
discovery, per-service config, and content-type mapping.

## Integration with your app

Uncomment the `app` service in `docker-compose.yml` to run your
application alongside the mock server:

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
