# Simplify Command

Simplifies large OpenAPI specs by removing union types (anyOf/oneOf) and limiting optional properties.

See the [full documentation](https://mockzilla.github.io/mockzilla/commands/simplify/).

## Quick Start

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest \
  -output simplified.yml \
  https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json
```
