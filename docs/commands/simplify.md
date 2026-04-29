# Simplify Command

The simplify command reduces the complexity of large OpenAPI specs by removing union types
(anyOf/oneOf) and limiting the number of optional properties per schema.

This is particularly useful for enormous specs like Stripe's API that would otherwise
generate unwieldy code with deeply nested union types.

## Usage

```bash
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest [options] <path-to-spec>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `<path-to-spec>` | Path to the OpenAPI spec file (required) |

## Flags

| Flag | Description |
|------|-------------|
| `-output` | Output file path. If not specified, outputs to stdout |
| `-keep-optional` | Keep exactly this many optional properties per schema (default: 5) |
| `-min-optional-properties` | Minimum number of optional properties to keep (overrides `-keep-optional`) |
| `-max-optional-properties` | Maximum number of optional properties to keep (overrides `-keep-optional`) |
| `-help` | Show help and exit |

## What It Does

The simplify command performs the following transformations:

1. **Removes optional union properties** - Properties with `anyOf`/`oneOf` that are not required are removed entirely
2. **Simplifies required union properties** - Required properties with unions have the `anyOf`/`oneOf` removed (property is kept)
3. **Limits optional properties** - Keeps only a specified number of optional properties per schema (alphabetically first)

## Examples

### Basic Usage

```bash
# Simplify and output to stdout
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest openapi.yml

# Simplify and save to file
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest -output simplified.yml openapi.yml
```

### Controlling Optional Properties

```bash
# Keep exactly 3 optional properties per schema
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest -keep-optional 3 openapi.yml

# Keep between 1-3 optional properties per schema
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest \
  -min-optional-properties 1 \
  -max-optional-properties 3 \
  openapi.yml

# Keep all optional properties (only simplify unions)
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest -keep-optional 0 openapi.yml
```

### With Service Generation

A common workflow is to simplify a spec before generating a service:

```bash
# 1. Simplify the spec
go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify@latest \
  -output simplified.yml \
  https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json

# 2. Generate service from simplified spec
go run github.com/mockzilla/mockzilla/v2/cmd/gen/service@latest -name stripe simplified.yml
```

## Use Cases

### Large API Specs

APIs like Stripe have specs with:
- Hundreds of endpoints
- Deeply nested schemas with many union types
- Thousands of optional properties

Without simplification, code generation can:
- Take a very long time
- Produce enormous generated files
- Create complex types that are hard to work with

### Testing and Development

When developing against a large API, you often only need a subset of the functionality.
Simplifying the spec makes it faster to iterate.

## Example

See the [simplify example](https://github.com/mockzilla/mockzilla/tree/master/examples/commands/simplify) for a runnable example.

```bash
cd examples/commands/simplify
go generate
```

