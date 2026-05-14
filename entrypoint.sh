#!/bin/sh

# Mockzilla docker entrypoint.
#
# Two ways to run:
#
#  1. Pass a portable arg directly:
#       docker run mockzilla/mockzilla petstore.yml
#       docker run mockzilla/mockzilla https://api.example.com/openapi.json
#       docker run mockzilla/mockzilla petstore.mockz
#
#  2. Mount your data at /data (or override with MOCKZILLA_DATA) and
#     let the default CMD pick it up. Three recognised shapes:
#
#       Multi-service tree:
#         docker run -v $(pwd)/services:/data/services mockzilla/mockzilla
#
#       Flat root of specs (each *.yml/*.yaml/*.json at the top is one service):
#         docker run -v $(pwd):/data mockzilla/mockzilla
#
#       Single service folder (spec + config.yml + optional statics):
#         docker run -v $(pwd):/data mockzilla/mockzilla
#
#  Static endpoint files (`<path>/index.<ext>` or `<path>/<method>/index.<ext>`)
#  are picked up anywhere inside a service folder; no `static/` wrapper.
#  An optional /data/app.yml carries global settings.

set -e

is_portable_arg() {
  case "$1" in
    http://*|https://*) return 0 ;;
    *.yml|*.yaml|*.json|*.mockz|*.tar.gz) return 0 ;;
  esac
  return 1
}

# Direct portable arg → pass straight through.
if is_portable_arg "$1"; then
  exec api "$@"
fi

# Default `CMD ["api"]` → look for a mounted data root and serve it.
if [ "$1" = "api" ]; then
  DATA_DIR="${MOCKZILLA_DATA:-/data}"
  if [ -d "$DATA_DIR" ]; then
    if [ -d "$DATA_DIR/services" ] || \
       ls "$DATA_DIR"/*.yml "$DATA_DIR"/*.yaml "$DATA_DIR"/*.json >/dev/null 2>&1; then
      shift
      exec api "$DATA_DIR" "$@"
    fi
    echo "Mounted $DATA_DIR has no services/ subdir and no top-level spec files; nothing to serve." >&2
  fi
  exec api "$@"
fi

exec "$@"
