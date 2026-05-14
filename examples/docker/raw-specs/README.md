# Raw Specs (Docker)

Mount a per-service tree, get a mock server.

## Quick start

```bash
docker run -p 2200:2200 -v $(pwd)/services:/data/services mockzilla/mockzilla:latest
```

## Test

```bash
# Spec-driven endpoints (petstore)
curl http://localhost:2200/petstore/pets
curl http://localhost:2200/petstore/pets/42

# Spec with mount override → mounted at /stripe/v1 (not /stripe)
curl http://localhost:2200/stripe/v1/customers

# Static-only service (myapi)
curl http://localhost:2200/myapi/users
curl http://localhost:2200/myapi/users/1

# Merge: spec drives /orders, static overrides /orders/{orderId}
curl http://localhost:2200/orders                    # generated (spec)
curl http://localhost:2200/orders/ord_X              # always returns ord_known_001
curl http://localhost:2200/orders/openapi.yml        # spec served as literal asset
```

## Layout

```
services/
  petstore/                              ← spec-only service
    openapi.yml
    config.yml                           ← latency, caching
    context.yml                          ← flat replacement values

  stripe/                                ← spec with mount override
    openapi.yml
    config.yml                           ← mount: stripe/v1

  myapi/                                 ← static-only service
    users/get/index.json                 → GET /myapi/users
    users/{id}/get/index.json            → GET /myapi/users/{id}

  orders/                                ← merge: spec + static override
    openapi.yml                          ← drives /orders and /orders/{orderId}
    {orderId}/get/index.json             ← overrides GET /orders/{orderId}
```

## How discovery works inside each service folder

The folder name (`petstore`, `stripe`, etc.) is the service identity.
The runtime mounts it at `/<folder-name>` unless `config.yml` says
otherwise via `mount:`.

| Folder contains | Mode | Result |
|---|---|---|
| A `*.{yml,yaml,json}` spec, no `<method>/index.<ext>` files | **spec mode** | Endpoints come from the spec, generated. |
| Only `<…>/<method>/index.<ext>` files, no spec | **static mode** | Endpoints come from the static files. Spec is synthesized. |
| Both | **merge mode** | Spec drives all endpoints; each static file overrides matching `(path, method)` or adds a new one. Spec file is also served at `GET /<svc>/<filename>` as a literal asset for docs. |

## Per-service `config.yml`

```yaml
latency: 100ms              # constant latency
# OR percentiles
latencies:
  p50: 50ms
  p95: 200ms
errors:                     # percentile error injection
  p5: 500                   # 5% of requests → 500
mount: stripe/v1            # override URL prefix
upstream:                   # forward to a real backend
  url: https://api.stripe.com/v1
  timeout: 10s
cache:
  requests: true
```

## Per-service `context.yml`

Flat replacement values. No service-name wrapper:

```yaml
name: ["Fluffy", "Spot", "Rover"]
tag: ["cat", "dog", "bird"]
```

## Static endpoint files

Drop response bodies under `<service>/<path>/<method>/index.<ext>`.
Parent dir of `index.<ext>` must be a lowercase HTTP method
(`get`, `post`, `put`, `patch`, `delete`, `head`, `options`, `trace`).
Everything before the method dir is the URL path.

Extension drives content-type: `.json` (JSON), `.html` (HTML),
`.xml` (XML), `.yaml`/`.yml` (YAML), `.txt` (plain text).

## Override the data root

By default the entrypoint serves `/data` if it exists. To use a
different in-container path:

```bash
docker run -p 2200:2200 \
  -e MOCKZILLA_DATA=/srv/mocks \
  -v $(pwd)/services:/srv/mocks/services \
  mockzilla/mockzilla:latest
```

## Hot reload

Changes to spec files, `config.yml`, `context.yml`, and static files
are detected automatically. The affected service rebuilds and its
handler swaps in place.
