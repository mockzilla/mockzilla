# Raw Specs Example

Mount OpenAPI specs and static files - get a mock server.

## Quick Start

```bash
# From this directory
docker run -p 2200:2200 \
  -v $(pwd)/openapi:/app/resources/data/openapi \
  -v $(pwd)/static:/app/resources/data/static \
  mockzilla/connexions:latest
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

Add a `config.yml` alongside your spec to customize service behavior. If not provided, defaults are used (caching enabled, request validation enabled):

```yaml
name: myapi                # Service name (optional, inferred from directory)

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
