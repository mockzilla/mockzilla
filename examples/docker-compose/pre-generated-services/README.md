# Pre-generated Services with Docker Compose

Deploy pre-generated Mockzilla services as a standalone container.

## Quick Start

```bash
# 1. Generate the services
go generate ./services/...

# 2. Build and run
docker compose up --build
```

## Test

```bash
curl http://localhost:2200/petstore/pet/1
curl http://localhost:2200/spoonacular/food/jokes/random
```

## Directory Structure

```
pre-generated-services/
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
├── cmd/
│   └── server/
│       └── main.go
└── services/
    └── myapi/
        ├── generate.go
        ├── setup/
        │   ├── codegen.yml
        │   ├── config.yml
        │   ├── context.yml
        │   └── openapi.yml
        ├── types/
        ├── handler/
        ├── register.go
        └── middleware.go  # Customize this!
```

## How It Works

1. Generate services locally with `go generate ./services/...`
2. Customize `middleware.go` in each service (add auth, logging, etc.)
3. `cmd/server/main.go` imports all services and starts the server
4. Docker builds a standalone binary with all dependencies

## Your Own Project

1. Create a `go.mod`:

```go
module myproject

go 1.23

require github.com/mockzilla/mockzilla/v2 v2.x.x
```

2. Create `cmd/server/main.go`:

```go
package main

import (
    "net/http"

    "github.com/mockzilla/mockzilla/v2/pkg/api"
    "github.com/mockzilla/mockzilla/v2/pkg/loader"

    _ "myproject/services/myapi"  // Import to register
)

func main() {
    router := api.NewRouter()
    loader.LoadAll(router)
    http.ListenAndServe(":2200", router)
}
```

3. Generate services:

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service \
    -name myapi https://example.com/openapi.json
```

4. Build and run:

```bash
docker compose up --build
```

## Integration with Your App

Uncomment the `app` service in `docker-compose.yml`:

```yaml
services:
  mock-server:
    # ...

  app:
    build: ./your-app
    environment:
      - API_BASE_URL=http://mock-server:2200
    depends_on:
      mock-server:
        condition: service_healthy
```

