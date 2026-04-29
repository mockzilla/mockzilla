

## Go Environment
- Go version is specified in `go.mod`
- If you encounter Go version mismatch errors, run: `unset GOROOT && /opt/homebrew/bin/go clean -cache`
- Then prefix commands with `PATH="/opt/homebrew/bin:$PATH"` to use the correct Go version

## Building
- Use Makefile settings or commands for building
- Always use build dir set in `Makefile`
- All commands located in `cmd/`
- For code-generation commands located in `cmd/gen/`

## Common errors in integration tests
- Some errors could be related to the wrongly generated data types or missing data.
- Some errors might be related to the code-generation from OpenAPI spec.
- For OpenAPI spec we use `github.com/doordash-oss/oapi-codegen-dd/v3` library.
  It generates types inside `types/` package.
- When server builds, we transform oapi-codegen lib data type to our simplified internal types. 
  The transformation is done in `pkg/typedef` package.
  There we could check and debug if all expected properties are set correctly.
- Locate the reason for the failure and check if unit tests are covering the same scenario.
  If not - add unit test.
- If data validation fails, try to replicate this with unit test.

## Running integration tests
- **NEVER run the complete integration test suite** - always specify a single spec
- Run a single spec: `make test-integration testdata/specs/3.0/misc/spoonacular.com.yml`
- Find available specs: `find testdata/specs -name "*.yml" -o -name "*.yaml"`

## Running single operation tests
- Locate Operation ID in Failure Results, for example: `PostIssuingCardsCard`
- Create custom codegen.yml somewhere in the sandbox with filter to include only that operation:
```yaml
filter:
  include:
    operation-ids:
      - PostIssuingCardsCard
```
- Run `CODEGEN_CONFIG=<path-to-codegen.yml> make test-integration <path-to-spec.yml>`

## Investigating validation errors in integration tests
- Get the request and response payloads the mockzilla generate bypassing validation:
  Create custom codegen config somewhere in the sandbox with validation disabled:
```yaml
generate:
  handler:
    validation:
      request: false
      response: false
```
- Log the generated request in integration test after `http.Post to generateURL`
- Log the generated response received from the server after `respBody, _ := io.ReadAll(endpointResp.Body)`
- Locate needed resource and all components in the OpenAPI schema located in `setup/openapi.yml`
- Run `CODEGEN_CONFIG=<path-to-codegen.yml> SERVICE_CONFIG=<path-to-service-config.yml> make test-integration <path-to-spec.yml>`
- This should be enough to understand the reason for the failure.
- To test the issue resolved, do not set `CODEGEN_CONFIG` and `SERVICE_CONFIG` to use defaults.

## Writing unit tests
- Follow the structure of the file, do not place tests in random order, tests should appear in the same order the 
  functions we are testing appear in the source code.
- For long yaml spec, prefer testdata, for short snippets: inline yaml is ok.
- Prefer table tests for multiple inputs.
