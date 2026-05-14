# Flat Specs (Docker)

The simplest layout: a folder full of OpenAPI specs. Each spec
becomes its own service named after its filename. Optional
`context.yml` and `app.yml` at the root apply to every service.

## Quick start

```bash
docker run -p 2200:2200 -v $(pwd):/data mockzilla/mockzilla:latest
```

## Test

```bash
curl http://localhost:2200/petstore/pets        # GET → from petstore.yml
curl http://localhost:2200/petstore/pets/42
curl http://localhost:2200/stripe/customers     # GET → from stripe.yml
```

## Layout

```
flat-specs/
├── petstore.yml      → service "petstore"     → /petstore/*
├── stripe.yml        → service "stripe"       → /stripe/*
├── context.yml       ← shared replacement values (applied to every service)
└── app.yml           ← optional global settings (omit for defaults)
```

## When to use this shape vs others

| Need | Layout | See |
|---|---|---|
| Just mock several specs, no fuss | **flat root** (this one) | this dir |
| Per-service config / context / static endpoints | **single-service folders** under `services/` | `../raw-specs/` |
| Compiled Go server with custom middleware | **codegen** | `../pre-generated-services/` |

In flat root mode the same `context.yml` is applied to every service.
If you need per-service contexts, configs, or static endpoints,
graduate one of the specs to a folder under `services/`:

```diff
- stripe.yml
+ services/stripe/openapi.yml
+ services/stripe/config.yml
+ services/stripe/context.yml
```

If a `services/` subdir is present it takes precedence over flat root
scanning, so pick one shape per project rather than mixing.
