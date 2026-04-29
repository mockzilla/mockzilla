# Pre-generated Services with Docker

This example demonstrates deploying pre-generated mockzilla services as a standalone Docker container.

## Quick Start

```bash
# 1. Generate the services (from this directory)
go generate ./services/...

# 2. Build the Docker image
docker build -t my-mock-server .

# 3. Run the container
docker run -p 2200:2200 my-mock-server

# 4. Test the APIs
curl http://localhost:2200/petstore/pet/1
curl http://localhost:2200/spoonacular/food/jokes/random
```

## What's Included

- **go.mod** - User's module with mockzilla as a dependency
- **cmd/server/main.go** - Simple server that imports and runs services
- **services/** - Pre-generated services with custom middleware
- **Dockerfile** - Multi-stage build for minimal production image

## How It Works

1. User generates services locally with `go generate ./services/...`
2. User can customize `middleware.go` in each service (add auth, logging, etc.)
3. The `cmd/server/main.go` imports all services and starts the server
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
docker build -t my-mock-server .
docker run -p 2200:2200 my-mock-server
```

## Directory Structure

```
myproject/
├── go.mod
├── go.sum
├── Dockerfile
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
