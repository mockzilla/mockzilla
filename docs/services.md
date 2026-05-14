# Services

A service is one collection of API endpoints. In a multi-service
layout, each service is a folder under `services/` and the folder
name is the service identity. For other shapes, the name comes from
[inside-the-folder signals](#service-name-inference-for-single-folder-invocations)
or defaults to empty (a root-mounted service).

## Three directory shapes

Mockzilla picks one of three layouts at the root of your data dir,
in order of complexity. Pick the one that matches your needs.

### Flat root: just specs

```text
./
├── petstore.yml          → /petstore/*
├── stripe.yml            → /stripe/*
├── github.yml            → /github/*
├── app.yml               (optional)
└── context.yml           (optional shared context)
```

Each top-level spec file is its own service named after the filename
basename. Best for "I have N specs, just mock them all." Per-service
config and static endpoints are not supported in this shape; graduate
to a service folder when you need them.

### Single-service folder

```text
pets/                     → /pets/*  (when config.yml has `name: pets`)
├── openapi.yml
├── config.yml            # name: pets
├── context.yml
└── v1/get/index.json     → GET /pets/v1
```

Triggered by any of: a `config.yml` at the dir root, static
endpoints anywhere in the tree, or exactly one spec at the root.
The service name comes from
[inside-the-folder signals](#service-name-inference-for-single-folder-invocations),
not the folder basename.

### Multi-service `services/` subtree

```text
services/
├── petstore/                              → /petstore/*
│   └── openapi.yml
├── stripe/                                → /stripe/v1/*   (mount override)
│   ├── openapi.yml
│   └── config.yml                            # mount: stripe/v1
└── github/                                → /github/*
    └── openapi.yml
```

Each `services/<name>/` follows the single-service folder rules.
Best for projects where multiple services need their own configs and
maybe static overrides.

All services share the same port (default: 2200), so a single
Mockzilla instance can mock your entire microservices fabric.

The service name is the **folder name** unless `config.yml` overrides
the URL prefix via `mount:`. The folder name is preserved verbatim: no
snake-casing, no normalization.

See [Service Configuration](config/service.md) for the full set of
per-service knobs (latency, errors, mount, upstream, cache, …).

### Service-name inference for single-folder invocations

When you run `mockzilla <dir>` on a folder that isn't a `services/`
multi-service root, the service name is inferred from **inside** the
folder rather than the folder's own basename. In priority order:

1. `name:` field in `config.yml`.
2. The basename of a non-generic top-level spec file (anything except
   `openapi.{yml,yaml,json}`).
3. Otherwise the service has an empty name and **mounts at `/`**
   (root). The UI is automatically moved to `/.ui` in that case so
   the two don't collide. See [HomeURL and root-mounted
   services](config/app.md#homeurl-and-root-mounted-services) for the
   full mechanism.

This rule keeps the cwd's basename from leaking into URL prefixes
when you run `mockzilla .` from an arbitrary directory.

## Three modes (per service folder)

The runtime picks one of three modes per folder, based on what files
are present. You don't pick the mode directly; it's a function of the
layout.

| Folder contents | Mode | What happens |
|---|---|---|
| Top-level spec file, no `<…>/<method>/index.<ext>` files | **spec** | Endpoints come from the OpenAPI document, generated. |
| `<…>/<method>/index.<ext>` files, no spec | **static** | A spec is synthesized from the static files. |
| Both | **merge** | Spec drives endpoints. Each static file overrides matching `(path, method)` or adds a new endpoint. The spec file itself is also exposed at `GET /<svc>/<filename>` as a literal asset. |

### Spec mode

The simplest case. Drop an OpenAPI document in the service folder and
Mockzilla generates realistic responses against the spec's schemas,
examples, and context replacements.

```bash
mockzilla ./
```

Responses are produced from:

- Schema definitions (types, formats, constraints)
- Example values in the spec
- `context.yml` (replacement values for matched fields)

### Static mode

For endpoints that need byte-for-byte responses. Drop a file named
`index.<ext>` at the URL path you want to serve. The verb defaults
to `GET`; wrap in a `<method>/` directory for non-GET.

```text
services/petstore/
  index.json                       → GET /petstore
  pets/index.json                  → GET /petstore/pets       (implicit GET)
  pets/post/index.json             → POST /petstore/pets      (explicit method)
  pets/{id}/index.json             → GET /petstore/pets/{id}
  pets/{id}/delete/index.json      → DELETE /petstore/pets/{id}
```

Rules:

- A file `index.<ext>` whose parent dir is a lowercase HTTP method
  (`get`, `post`, `put`, `patch`, `delete`, `head`, `options`, `trace`)
  uses that method. Path is everything before the method dir.
- Otherwise the file mounts as `GET <path>`, where `<path>` is the
  dir hierarchy from the service root to the file's parent.
- A top-level `index.<ext>` at the service-folder root mounts at the
  service's root URL (i.e. just below the service mount prefix; for a
  root-mounted service that's literally `/`)
  (the service's root URL).

Extension drives content-type: `.json`, `.html`, `.xml`, `.yaml`/`.yml`,
`.txt`.

If no spec is present, Mockzilla synthesizes one from the file tree.

### Merge mode

Combine the spec generator with surgical overrides. Drop a single
`<path>/<method>/index.<ext>` next to your spec to override that one
endpoint; everything else still comes from the spec. The dir name
inside `{}` must match the spec's parameter name (`{petId}`, not
`{id}`, if the spec says `/pets/{petId}`).

```text
services/orders/
  openapi.yml                      ← spec drives /orders, /orders/{orderId}, …
  orders/{orderId}/get/index.json  ← overrides GET /orders/{orderId}
```

The spec file is also served as a literal asset at
`GET /orders/openapi.yml` so it stays fetchable for docs.

## Service folder file structure

```text
services/<name>/
├── openapi.yml          # OpenAPI spec (any *.{yml,yaml,json} name works)
├── config.yml           # optional: per-service settings
├── context.yml          # optional: replacement values (flat)
└── <path>/              # optional: static endpoints
    ├── <method>/
    │   └── index.<ext>  # GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, TRACE
    └── {param}/...      # parameterised path segments
```

### Reserved filenames at the folder root

`config.yml`, `context.yml`, and `app.yml` are reserved. They are
never treated as specs or static assets.

### Skipped directories

Dotted dirs (`.git`, `.idea`, `.vscode`), underscore-prefixed dirs
(`_build`, `_vendor`), and well-known noise (`node_modules`, `vendor`,
`target`, `dist`) are skipped during scanning.

## Compiled Go services (codegen mode)

For maximum control and performance, services can be generated as Go
code from an OpenAPI spec:

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest \
  -name petstore \
  https://petstore3.swagger.io/api/v3/openapi.json
```

The generated layout follows the same per-service shape used at
runtime, with extra files for codegen:

```text
pkg/petstore/
├── setup/
│   ├── openapi.yml
│   ├── config.yml
│   ├── context.yml
│   └── codegen.yml
├── gen.go                # generated handlers (do not edit)
├── service.go            # custom logic (overrides, kept across regen)
└── middleware.go         # custom middleware (kept across regen)
```

When implementing a service method, use `opts.GenerateResponse()` to
get a spec-compliant response with generated values, then modify
specific fields:

```go
func (s *service) GetUser(ctx context.Context, opts *GetUserServiceRequestOptions) (*GetUserResponseData, error) {
    resp, err := opts.GenerateResponse()
    if err != nil {
        return nil, err
    }
    resp.Body.ID = opts.PathParams.UserID
    resp.Body.Email = "custom@example.com"
    return resp, nil
}
```

See [service command](commands/service.md) for details.
