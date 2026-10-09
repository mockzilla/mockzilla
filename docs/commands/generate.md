# Generate Command

The generate command writes Go code from an OpenAPI spec: models, and a server, a client or MCP
tools when asked. It is the generate command of
[mockzilla-codegen](https://github.com/mockzilla/mockzilla-codegen), built into mockzilla. Flags,
config and output are the same, so `mockzilla-codegen generate ...` and `mockzilla generate ...`
do the same thing.

It is not [codegen mode](../usage/codegen.md), which builds mockzilla services. Generate writes
plain Go code into your own project.

## Usage

```bash
mockzilla generate [-c codegen.yaml] [-dry-run | -check] [-strict] [-v] [flags] [spec]
```

Without a config file the defaults hold: models only, in `./gen.go`.

## Flags

| Flag | Description |
|------|-------------|
| `-c` | Config file. Without it, `codegen.yaml` in the current folder when it exists, else the defaults. |
| `-dry-run` | Print every file, its package, its parts and whether it would be written. Write nothing. |
| `-check` | Write nothing. Exit 1 and list the generated files that are missing or differ from a new run. |
| `-strict` | Exit 1 on a warning too, not only on an error. |
| `-v` | Also print info diagnostics, and the table of files written. |
| `-server <framework>` | Generate a server for the framework. Replaces `server.framework` of the config. |
| `-client`, `-mcp` | Generate a client, or MCP tools, which need a client. |
| `-no-server`, `-no-client`, `-no-mcp` | Leave that part out, whatever the config says. |
| `-o <file>` | Replaces `output.file`, relative to the current folder. |
| `-package <name>` | Replaces `package`. |
| `spec` | A spec file or URL that replaces `spec.path`, relative to the current folder. |

The config keys are in the
[mockzilla-codegen configuration reference](https://github.com/mockzilla/mockzilla-codegen/blob/main/docs/config.md).

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success. |
| `1` | A failure, an error printed (a warning too with `-strict`), or stale files with `-check`. |
| `2` | Wrong usage. |

## Examples

```bash
# Models in ./gen.go
mockzilla generate openapi.yml

# A chi server and a client
mockzilla generate openapi.yml -server chi -client -o ./api/gen.go

# What the config lists
mockzilla generate -c codegen.yaml

# In CI: fail when the generated files are out of date
mockzilla generate -check
```
