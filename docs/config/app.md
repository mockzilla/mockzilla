# App Configuration

App-level configuration for Mockzilla server.

## File Location

`resources/data/app.yml`

For portable mode, use the `--config` flag with the `app` section. See [Portable Mode](../usage/portable.md).

For Docker, mount your custom config:
```yaml
volumes:
  - ./app.yml:/app/resources/data/app.yml:ro
```

## Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `title` | string | `Mockzilla` | App title displayed in UI |
| `port` | int | `2200` | Server port |
| `baseURL` | string | - | Public base URL (e.g., `https://api.example.com`) |
| `internalURL` | string | - | Internal URL for service-to-service calls |
| `homeURL` | string | `/` | URL for the UI home page (see [HomeURL and root-mounted services](#homeurl-and-root-mounted-services) below) |
| `serviceURL` | string | `/.services` | URL for service endpoints in UI |
| `contextAreaPrefix` | string | `in-` | Prefix for context area replacements |
| `disableUI` | bool | `false` | Disable the web UI (`/`, `/.ui/*`, docs, file assets) entirely |
| `editor.theme` | string | `chrome` | Code editor theme in UI |
| `editor.fontSize` | int | `16` | Code editor font size |
| `extra` | map | `{}` | User-defined key-value config (accessible in services) |

## HomeURL and root-mounted services

`homeURL` defaults to `/`. The UI landing page, docs, and static
assets all live under that prefix.

When a service mounts at the root (empty service name, or an explicit
`mount: /` in its `config.yml`), `/` is needed for the service's
content. Mockzilla detects this at startup and **automatically
relocates the UI from `/` to `/.ui`** so the two don't collide:

```text
GET /              → root-mounted service (e.g. your index.json)
GET /.ui/          → UI landing page
GET /.services/    → internal API (unchanged)
GET /<unmatched>   → falls through to the root service
```

A log line at startup makes the move visible:

```
INFO Service requested root mount; relocating UI from / to /.ui
```

The `--ready-stamp` JSON output also reflects the new UI URL.

If you set `homeURL` to something other than `/` yourself, no
relocation happens.

If `disableUI: true` is set, the UI isn't registered at all, and the
root mount is available without any relocation step.

## History Configuration

Configure request/response history recording and the History Explorer UI.

```yaml
history:
  enabled: true
  url: /.history
  duration: 60m
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `history.enabled` | bool | `true` | Enable history API and UI tab |
| `history.url` | string | `/.history` | URL prefix for history API endpoints |
| `history.duration` | duration | `60m` | How long to keep request history in memory |

Setting `enabled: false` hides the History tab from the UI and disables the history API endpoints. History recording per service is controlled separately via [service configuration](service.md).

The `ROUTER_HISTORY_DURATION` environment variable overrides `history.duration`.

## Storage Configuration

Configure shared storage for features like history persistence across restarts.

```yaml
storage:
  type: redis
  redis:
    address: localhost:6379
    password: ""
    db: 0
```

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `storage.type` | string | `memory` | Storage type: `memory` or `redis` |
| `storage.redis.address` | string | - | Redis address (host:port) |
| `storage.redis.password` | string | - | Redis password |
| `storage.redis.db` | int | `0` | Redis database number |

## Environment Variables

Environment variables override file values:

| Variable | Overrides |
|----------|-----------|
| `APP_BASE_URL` | `baseURL` |
| `APP_INTERNAL_URL` | `internalURL` |
| `ROUTER_HISTORY_DURATION` | `history.duration` |

## Example

```yaml
title: My API Mock Server
port: 8080
baseURL: https://api.example.com
internalURL: http://localhost:2200
disableUI: false

history:
  duration: 60m

editor:
  theme: monokai
  fontSize: 14

storage:
  type: redis
  redis:
    address: redis:6379

extra:
  apiKey: "secret123"
  maxRetries: 3
```

## Accessing Config in Services

When using scaffolded services, access config via `ServiceParams`:

```go
func newService(params *api.ServiceParams) *service {
    baseURL := params.AppConfig.BaseURL
    apiKey := params.AppConfig.Extra["apiKey"].(string)
    return &service{params: params}
}
```

