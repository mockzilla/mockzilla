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
├── openapi/
│   ├── petstore.yml              # Flat: service name from filename
│   └── stripe/                   # Nested: service name from directory
│       ├── openapi.yml           # The OpenAPI spec
│       └── config.yml            # Optional service config
└── static/
    └── myapi/
        ├── config.yml            # Optional service config
        └── users/
            └── get/
                └── index.json
```

## Service Configuration (Optional)

Add a `config.yml` alongside your spec to customize service behavior:

```yaml
cache:
  requests: false          # Disable GET request caching

latency: 50ms              # Add artificial latency

errors:                    # Simulate errors at percentiles
  p10: 500                 # 10% of requests return 500
  p5: 429                  # 5% of requests return 429
```

## Hot Reload

Changes to specs and static files are automatically detected:
- Edit a JSON file → service regenerates → server restarts
- Add/remove specs → services added/removed automatically

## Integration with Your App

Uncomment the `app` service in `docker-compose.yml` to run your application alongside the mock server:

```yaml
services:
  connexions:
    # ...

  app:
    build: ./your-app
    environment:
      - API_BASE_URL=http://connexions:2200
    depends_on:
      connexions:
        condition: service_healthy
```

