# Lint Command

The lint command finds schemas in an OpenAPI spec that no value can satisfy.

Specs in the wild ship with these defects. Mockzilla cannot generate a valid response for such a
schema, and response validation fails on every request that uses it. Run lint first to know a
spec is broken before you serve or deploy it.

## Usage

```bash
mockzilla lint [flags] <path-to-spec>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `<path-to-spec>` | Path or URL to the OpenAPI spec (required). Use `-` to read from stdin. |

## Flags

| Flag | Description |
|------|-------------|
| `--format` | Output format: `text` (default) or `json`. JSON is `{"defects": [{"rule", "path", "detail"}]}`. |
| `-h, --help` | Show help and exit. |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | No defects found. |
| `1` | Defects found, or the spec could not be read or parsed. |

Only OpenAPI 3.x specs are supported. A Swagger 2.0 spec exits with an error.

## Rules

Each defect names the rule that found it. Rule names are stable, so you can filter on them.

| Rule | What it finds |
|------|---------------|
| `array-enum-scalars` | `type: array` with an enum of scalar values. An array never equals a scalar. |
| `additional-props-false-with-oneof` | `additionalProperties: false` next to `oneOf` branches that declare properties. Every branch property counts as additional and is rejected. |
| `allof-non-overlapping-enums` | `allOf` branches that limit the same property to enum sets with no value in common. |
| `allof-additional-props-conflicts-sibling` | One `allOf` branch types `additionalProperties`, another declares a property of a different type. |
| `pattern-unicode-circumflex` | A `pattern` that contains `ˆ` (U+02C6), which looks like `^` but is a literal character. |
| `param-missing-schema` | A parameter with neither `schema` nor `content`, usually Swagger 2.0 style with `type` on the parameter. |

A schema under `components.schemas` is reported once, at its component path, no matter how many
operations reference it.

## Examples

```bash
# Text output
mockzilla lint openapi.yml

# JSON output, from a URL
mockzilla lint --format json https://petstore3.swagger.io/api/v3/openapi.json

# Read spec from stdin
cat openapi.yml | mockzilla lint -

# Gate a deploy in CI
mockzilla lint openapi.yml && ./deploy.sh
```

Text output lists one defect per line, as `<path>: <detail> [<rule>]`:

```
components.schemas.Tags: type: array with scalar enum: arrays can never equal a scalar, schema is unsatisfiable [array-enum-scalars]
Error: 1 defect(s) found
```
