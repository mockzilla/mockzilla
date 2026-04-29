# Custom Middleware

Compiled Go services support custom middleware for advanced use cases like authentication, logging, request modification, and stateful behavior.

## Overview

Custom middleware is prepended before the built-in middleware chain:

```
Request → Resource Resolver → Config Override → [Custom Middleware] → Latency/Error → Replay Read/Write → Cache Read → Upstream → Cache Write → Handler → Response
```

## Adding Custom Middleware

Edit `middleware.go` in your service directory:

```go
package petstore

import (
    "net/http"
    "github.com/mockzilla/mockzilla/v2/pkg/middleware"
)

func getMiddleware() []func(*middleware.Params) func(http.Handler) http.Handler {
    return []func(*middleware.Params) func(http.Handler) http.Handler{
        createAuthMiddleware,
        createLoggingMiddleware,
    }
}

func createAuthMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("Authorization")
            if token == "" {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## Middleware Params

The `middleware.Params` struct provides access to service configuration and request history:

```go
type Params struct {
    ServiceConfig *config.ServiceConfig  // Service configuration
    History       *history.CurrentRequestStorage  // Request/response history
}
```

### ServiceConfig

Access service configuration values:

```go
func createConfigAwareMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    serviceName := params.ServiceConfig.Name
    
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            log.Printf("Service: %s, Path: %s", serviceName, r.URL.Path)
            next.ServeHTTP(w, r)
        })
    }
}
```

## Request History

Access the current request and previous requests/responses:

```go
func createHistoryMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Get current request record
            record, exists := params.History.Get(r)
            if exists {
                log.Printf("Request body: %s", string(record.Body))
                if record.Response != nil {
                    log.Printf("Previous response: %d", record.Response.StatusCode)
                }
            }
            
            next.ServeHTTP(w, r)
        })
    }
}
```

### RequestedResource

The history record contains:

```go
type RequestedResource struct {
    Resource       string           // OpenAPI path: /pets/{id}
    Body           []byte           // Request body
    Response       *HistoryResponse // Previous response (if any)
    Request        *http.Request    // Current HTTP request
    ServiceStorage Storage          // Per-service key-value storage
}
```

## Service Storage

Each service has a thread-safe key-value storage for maintaining state across requests:

```go
func createStatefulMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            record, _ := params.History.Get(r)
            storage := record.ServiceStorage
            
            // Increment request counter
            count, _ := storage.Get("request_count")
            if count == nil {
                count = 0
            }
            storage.Set("request_count", count.(int) + 1)
            
            // Store user session
            userID := r.Header.Get("X-User-ID")
            if userID != "" {
                storage.Set("last_user", userID)
            }
            
            next.ServeHTTP(w, r)
        })
    }
}
```

### Storage Interface

```go
type Storage interface {
    Get(key string) (any, bool)
    Set(key string, value any)
    Data() map[string]any
}
```

**Note:** Storage is cleared periodically based on `historyDuration` setting (default: 5 minutes).

## History Transform

Register a callback to modify history entries before they are saved. This runs before the `mask-headers` config is applied, so you can implement custom redaction logic.

```go
params.SetHistoryTransform(func(req *db.HistoryRequest, resp *db.HistoryResponse) {
    // Redact request body
    if len(req.Body) > 0 {
        req.Body = []byte("[redacted]")
    }

    // Remove specific headers entirely
    filtered := req.Headers[:0]
    for _, h := range req.Headers {
        if !strings.HasPrefix(h, "X-Internal-") {
            filtered = append(filtered, h)
        }
    }
    req.Headers = filtered
})
```

The callback receives pointers to the request and response structs and can modify them in place. After the callback returns, any `mask-headers` patterns from the service config are applied on top.

## Common Patterns

### Request Logging

```go
func createLoggingMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            log.Printf("→ %s %s", r.Method, r.URL.Path)
            
            next.ServeHTTP(w, r)
            
            log.Printf("← %s %s (%v)", r.Method, r.URL.Path, time.Since(start))
        })
    }
}
```

### Request Modification

```go
func createHeaderMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Add headers to response
            w.Header().Set("X-Service", params.ServiceConfig.Name)
            w.Header().Set("X-Request-ID", uuid.New().String())
            
            next.ServeHTTP(w, r)
        })
    }
}
```

### Conditional Logic

```go
func createConditionalMiddleware(params *middleware.Params) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Only apply to specific paths
            if strings.HasPrefix(r.URL.Path, "/admin") {
                if r.Header.Get("X-Admin-Token") != "secret" {
                    http.Error(w, "Forbidden", http.StatusForbidden)
                    return
                }
            }
            
            next.ServeHTTP(w, r)
        })
    }
}
```

