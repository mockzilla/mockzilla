FROM golang:1.25.3 AS builder
ENV CGO_ENABLED=1
RUN apt-get install -y git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Build tools (will be included in final image for user projects)
RUN go build -o /app/.build/gen-discover ./cmd/gen/discover
RUN go build -o /app/.build/gen-service ./cmd/gen/service

# Generate service imports for mockzilla itself
RUN /app/.build/gen-discover

# Build server
RUN go build -o /app/.build/mockzilla ./cmd/mockzilla

# Get version
RUN git describe --tags --abbrev=0 > version.txt || echo "dev" > version.txt

FROM golang:1.25.3
ENV CGO_ENABLED=1

WORKDIR /app

COPY --from=builder /app/.build/mockzilla /usr/local/bin/api
COPY --from=builder /app/.build/gen-discover /usr/local/bin/gen-discover
COPY --from=builder /app/.build/gen-service /usr/local/bin/gen-service
COPY --from=builder /app/go.mod /app/go.mod
COPY --from=builder /app/go.sum /app/go.sum
COPY --from=builder /go/pkg/mod /go/pkg/mod
COPY --from=builder /app/cmd /app/cmd
COPY --from=builder /app/pkg /app/pkg
COPY --from=builder /app/internal /app/internal
COPY --from=builder /app/resources /app/resources
COPY --from=builder /app/version.txt /app/version.txt

RUN export APP_VERSION=$(cat /app/version.txt) && \
    echo "APP_VERSION=$APP_VERSION" >> /app/.env

# Data root. Mount your service tree here in any of the recognised
# portable-mode shapes:
#
#  Multi-service:        /data/services/<name>/...
#  Flat root of specs:   /data/<spec1>.yml, /data/<spec2>.yml, ...
#  Single service:       /data/<spec>.yml + /data/config.yml + ...
#
# Inside a service folder:
#  openapi.yml                          spec (any *.{yml,yaml,json} name)
#  config.yml                           optional: latency, errors, mount, ...
#  context.yml                          optional: flat replacement values
#  <path>/index.<ext>                   static endpoint (GET by default)
#  <path>/<method>/index.<ext>          static endpoint with explicit method
#
# Optional /data/app.yml carries global settings. Override the mount
# point with MOCKZILLA_DATA=<path>.
RUN mkdir -p /data

COPY resources/ui /app/resources/ui
COPY entrypoint.sh /app/entrypoint.sh

RUN chmod +x /usr/local/bin/api
RUN chmod +x /usr/local/bin/gen-discover
RUN chmod +x /usr/local/bin/gen-service
RUN chmod +x /app/entrypoint.sh

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["api"]
