#!/bin/sh

set -e

if [ "$1" = "api" ]; then
  cd /app

  OPENAPI_DIR="/app/resources/data/openapi"
  STATIC_DIR="/app/resources/data/static"

  has_openapi=false
  has_static=false

  if [ -d "$OPENAPI_DIR" ] && [ "$(ls -A "$OPENAPI_DIR" 2>/dev/null)" ]; then
    has_openapi=true
  fi
  if [ -d "$STATIC_DIR" ] && [ "$(ls -A "$STATIC_DIR" 2>/dev/null)" ]; then
    has_static=true
  fi

  PORTABLE_DIR=$(mktemp -d)

  if [ "$has_openapi" = true ]; then
    for entry in "$OPENAPI_DIR"/*; do
      name=$(basename "$entry")
      if [ -d "$entry" ]; then
        spec=$(find "$entry" -maxdepth 1 -name "*.yml" -o -name "*.yaml" -o -name "*.json" | head -1)
        if [ -n "$spec" ]; then
          ln -s "$spec" "$PORTABLE_DIR/${name}.yml"
        fi
      else
        ln -s "$entry" "$PORTABLE_DIR/$name"
      fi
    done
  fi

  if [ "$has_static" = true ]; then
    ln -s "$STATIC_DIR" "$PORTABLE_DIR/static"
  fi

  CONFIG_FILE="/app/resources/data/app.yml"
  CONTEXT_FILE="/app/resources/data/context.yml"

  EXTRA_ARGS=""
  if [ -f "$CONFIG_FILE" ]; then
    EXTRA_ARGS="$EXTRA_ARGS --config $CONFIG_FILE"
  fi
  if [ -f "$CONTEXT_FILE" ]; then
    EXTRA_ARGS="$EXTRA_ARGS --context $CONTEXT_FILE"
  fi

  echo "Starting in portable mode..."
  exec api "$PORTABLE_DIR" $EXTRA_ARGS
else
  exec "$@"
fi;
