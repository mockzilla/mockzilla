# Portable Mode

Run a mock server directly from OpenAPI specs and/or static response
files. No Docker, no code generation, no setup.

## Install

### Homebrew (macOS, Linux)

```bash
brew tap mockzilla/tap
brew install mockzilla
```

### Prebuilt binary

Download the latest release for your platform from the
[mockzilla releases page](https://github.com/mockzilla/mockzilla/releases/latest).
Linux and macOS (amd64 and arm64) are published per release.

### Docker

```bash
docker run -p 2200:2200 mockzilla/mockzilla:latest petstore.yml
```

See [Docker Compose](docker-compose.md) for mounting your own data
volumes.

### From Go source

```bash
go install github.com/mockzilla/mockzilla/v2/cmd/mockzilla@latest
```

This installs `server` into `$GOPATH/bin`. Rename it to `mockzilla`
if you prefer.

## Quick Start

Try it now, no files needed:

```bash
mockzilla https://petstore3.swagger.io/api/v3/openapi.json
```

The server starts on port 2200 with the Petstore API mounted at
`/petstore3.swagger.io/...` (the host is used as the service name when
the URL ends with a generic filename like `openapi.json`). Override
with `--mount`:

```bash
mockzilla --mount petstore https://petstore3.swagger.io/api/v3/openapi.json
```

Or from a local file:

```bash
mockzilla petstore.yml
```

## Inputs

`mockzilla` accepts any of:

```bash
# Single OpenAPI spec file
mockzilla petstore.yml

# Remote spec
mockzilla https://api.example.com/openapi.json

# A single-service folder (folder name → service name)
mockzilla ./pets/

# A directory with multiple specs at the top (flat root mode)
mockzilla ./

# A directory with a services/<name>/ subtree (multi-service mode)
mockzilla ./

# A .mockz / .tar.gz package, local or remote
mockzilla petstore.mockz
mockzilla https://example.com/my-api.mockz
```

See [Directory shapes](#directory-shapes) below for how the same
`mockzilla ./` invocation resolves to different shapes based on what's
in the dir.

## Directory shapes

When you point `mockzilla` at a directory, discovery picks one of
three shapes based on what's at the root:

### 1. Flat root: just toss specs in a folder

The simplest shape. Drop one or more spec files at the top, optionally
with a shared `context.yml` and `app.yml`. Each spec is a service
named after its filename.

```text
./
├── petstore.yml      → service "petstore"     (/petstore)
├── stripe.yml        → service "stripe"       (/stripe)
├── spoonacular.yml   → service "spoonacular"  (/spoonacular)
├── app.yml           # optional: global app settings
└── context.yml       # optional: FLAT context applied to every service
```

```bash
mockzilla ./
```

Flat root mode is for the "I just want to mock these few specs"
case. Per-service config and static endpoints are not supported in
this shape; switch to one of the shapes below if you need them.

### 2. Single-service folder

When you want per-service config, drop the spec and a `config.yml`
(and optionally `context.yml`, plus any static endpoints) in one
folder. The service identity is inferred from **inside** the folder
(see [Service name inference](#service-name-inference) below), not
from the folder's own basename.

```text
pets/
├── openapi.yml       # OpenAPI spec (any *.{yml,yaml,json} name)
├── config.yml        # name: pets, latency, errors, mount, upstream, cache
├── context.yml       # flat replacement values
├── v1/index.json     # optional: static endpoint → GET /pets/v1
└── v1/post/index.json # optional: explicit method → POST /pets/v1
```

```bash
mockzilla ./pets/    # service name comes from config.yml or the spec basename
```

The presence of any of `config.yml`, static endpoint files
(`<…>/index.<ext>`), or exactly one spec at root puts the dir into
single-service folder mode.

#### Service name inference

For single-folder invocations, in priority order:

1. `name:` field in `config.yml`.
2. The basename of a non-generic top-level spec file (anything except
   `openapi.{yml,yaml,json}`).
3. Otherwise the service has an empty name and mounts at `/` (root).
   The UI automatically moves from `/` to `/.ui` so the two don't
   collide. See [HomeURL and root-mounted services](../config/app.md#homeurl-and-root-mounted-services).

This is why `mockzilla .` from a directory named `~/Documents/foo`
doesn't accidentally produce a service called `foo` — the cwd's
basename is never used as the name.

### 3. Multi-service `services/` subtree

For projects with multiple configured services, put each one under
`services/<name>/`. Same per-service layout as shape #2, repeated:

```text
services/
├── petstore/
│   ├── openapi.yml
│   ├── config.yml
│   └── context.yml
├── myapi/
│   ├── users/index.json          → GET /myapi/users  (implicit GET)
│   └── users/post/index.json     → POST /myapi/users
└── orders/                       ← merge: spec + static overlay
    ├── openapi.yml               # paths: /, /{orderId}
    └── {orderId}/index.json      → overrides GET /orders/{orderId}
app.yml                           # optional: global app settings
```

```bash
mockzilla ./
```

## Discovery inside a service folder

Every service folder is matched against one of three shapes:

| Folder contents | Mode | What happens |
|---|---|---|
| Top-level spec, no `<…>/index.<ext>` files | **spec** | Endpoints come from the spec. |
| `<…>/index.<ext>` files, no spec | **static** | A spec is synthesized from the static files. |
| Both | **merge** | Spec drives endpoints. Each static file overrides matching `(path, method)` or adds a new one. Spec file is also served at `GET /<svc>/<filename>` as a literal asset. |

### Static endpoint convention

| File at service folder root | Mounts at |
|---|---|
| `index.<ext>` | `GET /` |
| `users/index.<ext>` | `GET /users` (implicit GET) |
| `users/post/index.<ext>` | `POST /users` (explicit method dir) |
| `users/{id}/index.<ext>` | `GET /users/{id}` |
| `users/{id}/delete/index.<ext>` | `DELETE /users/{id}` |
| `notes.json` (not `index.*`) | `GET /notes.json` (literal asset) |

The verb defaults to `GET`. To use a different HTTP method, place
`index.<ext>` under a lowercased method directory (`get`, `post`,
`put`, `patch`, `delete`, `head`, `options`, `trace`). Extension
drives content-type: `.json`, `.yaml`/`.yml`, `.html`, `.xml`, `.txt`.

### Reserved at the service folder root

`config.yml`, `context.yml`, `app.yml`, and any `index.<ext>` are
never treated as spec candidates. Dotted, underscore-prefixed, and
well-known noise dirs (`node_modules`, `vendor`, `target`, `dist`)
are skipped during the static scan.

## Flags

| Flag | Description |
|------|-------------|
| `--port N` | Server port (0 = OS picks; default: from `app.yml` or 2200) |
| `--ready-stamp` | Emit a single JSON line on stdout once the listener is bound |

### Single-service convenience flags

Only valid when exactly one service is registered (typically with a
single spec file or single-folder arg):

| Flag | Description |
|------|-------------|
| `--latency D` | Latency for the service (e.g. `100ms`, `1s`) |
| `--mount PATH` | URL mount path (e.g. `pets/v2`) |
| `--errors RULES` | Error injection rules: `p5=500,p10=503` |
| `--context FILE` | Path to a flat context YAML |

For multi-service runs, put these settings in each service's
`config.yml` / `context.yml` instead.

## Per-service `config.yml`

```yaml
latency: 100ms              # constant latency
# OR percentile latencies
latencies:
  p50: 50ms
  p95: 200ms
errors:                     # percentile error injection
  p5: 500                   # 5% of requests → 500
mount: pets/v2              # override URL prefix (default: <folder-name>)
upstream:                   # forward to a real backend
  url: https://petstore3.swagger.io/api/v3
  timeout: 10s
cache:
  requests: true
```

All fields from [Service Config](../config/service.md) are supported.

## Per-service `context.yml`

Flat replacement values. Keys are referenced from the spec/generator,
with no service-name wrapper. See [Contexts](../contexts.md) for the
full syntax.

```yaml
name: ["Fluffy", "Spot", "Rover"]
tag: ["cat", "dog", "bird"]
```

## Global `app.yml`

Optional, at the root of the data dir. All fields from
[App Config](../config/app.md) are supported.

```yaml
port: 3000
title: "My Mocks"
history:
  enabled: true
  duration: 1h
```

The `--port` CLI flag overrides this.

## Hot reload

Spec files, `config.yml`, `context.yml`, and static endpoint files are
watched for changes. The affected service's handler is hot-swapped
without restarting the server.

## Examples

From a URL:

```bash
mockzilla https://petstore3.swagger.io/api/v3/openapi.json
```

Quick one-spec serve with knobs (single-service convenience flags):

```bash
mockzilla --port 8080 --latency 50ms --mount pets/v2 petstore.yml
```

Multi-service from a folder tree:

```bash
mockzilla ./
```

`.mockz` package:

```bash
mockzilla petstore.mockz
mockzilla https://example.com/my-api.mockz
```

## Template

Start from a GitHub template to get a ready-to-use project with CI/CD
that builds a single binary with your services embedded:

- [mockzilla-portable-template](https://github.com/mockzilla/mockzilla-portable-template)

See [Templates](../templates.md) for all available templates.
