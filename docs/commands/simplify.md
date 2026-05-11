# Simplify Command

The simplify command reduces the complexity of large OpenAPI specs by removing union types
(anyOf/oneOf) and limiting the number of optional properties per schema.

This is particularly useful for enormous specs like Stripe's API that would otherwise
generate unwieldy code with deeply nested union types.

## Usage

```bash
# As a subcommand of an installed mockzilla binary:
mockzilla simplify [flags] <path-to-spec>

# Or via `go run` without installing (slower; builds the full server binary):
go run github.com/mockzilla/mockzilla/v2/cmd/server@latest simplify [flags] <path-to-spec>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `<path-to-spec>` | Path or URL to the OpenAPI spec (required). Use `-` to read from stdin. |

## Flags

| Flag | Description |
|------|-------------|
| `-o, --output` | Output file path. If not specified (or set to `-`), outputs to stdout. |
| `-c, --config` | Path to oapi-codegen-dd `codegen.yml`. Applies filter + overlay + prune before simplification. |
| `--optional` | Keep exactly N optional properties per schema. Omit to keep all; pass `0` to drop all. |
| `--optional-min` | Range mode: minimum optional properties per schema (requires `--optional-max`). |
| `--optional-max` | Range mode: maximum optional properties per schema (requires `--optional-min`). |
| `-h, --help` | Show help and exit. |

`--optional` is mutually exclusive with `--optional-min`/`--optional-max`. The range-mode flags
must be supplied together.

## What It Does

The simplify command performs the following transformations:

1. **Removes optional union properties** - Properties with `anyOf`/`oneOf` that are not required are removed entirely
2. **Simplifies required union properties** - Required properties with unions have the `anyOf`/`oneOf` removed (property is kept)
3. **Strips schema extensions** - `x-*` extension fields on schemas are dropped. Examples are deliberately preserved so referenced `$ref`s stay resolvable.
4. **Limits optional properties** - When `--optional`/range flags are supplied, keeps only that many optional properties per schema (alphabetically first, or random within range)

## Examples

### Basic Usage

```bash
# Simplify and output to stdout
mockzilla simplify openapi.yml

# Simplify and save to file
mockzilla simplify --output simplified.yml openapi.yml

# Read spec from stdin
curl -s https://example.com/openapi.json | mockzilla simplify -
```

### Controlling Optional Properties

```bash
# Keep exactly 3 optional properties per schema
mockzilla simplify --optional 3 openapi.yml

# Keep between 1-3 optional properties per schema
mockzilla simplify --optional-min 1 --optional-max 3 openapi.yml

# Drop all optional properties (keep only required ones)
mockzilla simplify --optional 0 openapi.yml

# Keep all optional properties (only simplify unions/extensions)
mockzilla simplify openapi.yml
```

### With Service Generation

A common workflow is to simplify a spec before generating a service:

```bash
# 1. Simplify the spec
mockzilla simplify --output simplified.yml \
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

See the [simplify example](https://github.com/mockzilla/mockzilla/tree/main/examples/commands/simplify) for a runnable example.

```bash
cd examples/commands/simplify
go generate
```
