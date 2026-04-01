# Storage

Connexions uses a layered storage architecture that provides service isolation while sharing a single backend connection.

## Design Principles

### Shared Backend, Isolated Views

All services share a single storage backend (memory or Redis), but each service gets an isolated "view" into that storage through key prefixing. 
This means:

- **Single connection** - One Redis client or memory store for the entire application
- **No cross-service access** - Service A cannot read or modify Service B's data
- **Automatic namespacing** - Keys are prefixed with the service name transparently

### Lazy Resource Creation

Tables and history stores are created on first access, not upfront. 
This keeps memory usage low when services don't use all features.

### TTL Support

Records can have individual expiration times:

- **Per-record TTL** - Each `Set()` call can specify its own TTL
- **Zero means forever** - A TTL of `0` means the record never expires
- **Lazy expiration (memory)** - Expired records are deleted on access, not via background cleanup
- **Native TTL (Redis)** - Uses Redis's built-in key expiration

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                      Application                         │
├─────────────────────────────────────────────────────────┤
│  Service A           Service B           Service C      │
│  ┌─────────┐         ┌─────────┐         ┌─────────┐   │
│  │   DB    │         │   DB    │         │   DB    │   │
│  ├─────────┤         ├─────────┤         ├─────────┤   │
│  │ History │         │ History │         │ History │   │
│  │ Table() │         │ Table() │         │ Table() │   │
│  └────┬────┘         └────┬────┘         └────┬────┘   │
│       │                   │                   │         │
├───────┴───────────────────┴───────────────────┴─────────┤
│                      Storage                             │
│              (Memory or Redis backend)                   │
│                                                          │
│  Keys: "serviceA:history:GET:/users"                    │
│        "serviceB:cache:token123"                        │
│        "serviceC:history:POST:/orders"                  │
└─────────────────────────────────────────────────────────┘
```

## Components

### Storage

The shared backend that manages the actual data. Supports:

- **Memory** - In-process storage, data lost on restart
- **Redis** - Distributed storage, persists across restarts

### DB

A service-scoped wrapper that provides:

- **History()** - Typed access to request/response history
- **Table(name)** - Generic key-value storage with TTL support

### Table

Generic key-value store with per-record TTL:

- `Get(ctx, key)` - Retrieve a value (returns false if expired or missing)
- `Set(ctx, key, value, ttl)` - Store with optional expiration
- `Delete(ctx, key)` - Remove a key
- `Data(ctx)` - Get all non-expired entries
- `Clear(ctx)` - Remove all entries

### HistoryTable

Typed wrapper for request/response tracking:

- Stores request body, response data, status codes
- Used by caching and upstream middleware
- Cleared periodically based on `history.duration` config

## Choosing a Backend

| Feature | Memory | Redis |
|---------|--------|-------|
| Setup | None | Requires Redis server |
| Persistence | No (lost on restart) | Yes |
| Multi-instance | No (each instance isolated) | Yes (shared state) |
| Performance | Fastest | Network overhead |

**Use Memory when:**
- Running a single instance
- Data loss on restart is acceptable
- Simplicity is preferred

**Use Redis when:**
- Running multiple instances behind a load balancer
- Request history should persist across restarts

## Configuration

See [App Configuration](config/app.md#storage-configuration) for setup options.

```yaml
# Memory (default)
storage:
  type: memory

# Redis
storage:
  type: redis
  redis:
    address: localhost:6379
    password: ""
    db: 0
```

