# Example: User's project with pre-generated services
#
# This shows how users can deploy their pre-generated mockzilla services.
# Users have full control over their code, dependencies, and custom middleware.
#
# Build: docker build -t my-mock-server -f examples/docker-compose/pre-generated-services/Dockerfile .
# Run:   docker run -p 2200:2200 my-mock-server
#
# TODO: Once mockzilla v2 is published, remove the replace directive hack.

# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Copy mockzilla source (for local development with replace directive)
COPY go.mod go.sum ./mockzilla/
COPY cmd/ ./mockzilla/cmd/
COPY pkg/ ./mockzilla/pkg/
COPY internal/ ./mockzilla/internal/
COPY resources/ ./mockzilla/resources/

# Copy example project
COPY examples/docker-compose/pre-generated-services/go.mod examples/docker-compose/pre-generated-services/go.sum ./
COPY examples/docker-compose/pre-generated-services/ ./

# Update go.sum and build the server
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/server .

# Copy service setup files (needed at runtime for OpenAPI specs)
COPY --from=builder /app/services/ ./services/

EXPOSE 2200
CMD ["./server"]
