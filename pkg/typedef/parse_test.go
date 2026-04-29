package typedef

import (
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestCreateParseContext_BasicSpec(t *testing.T) {
	spec := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /users:
    get:
      operationId: getUsers
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: string
                    name:
                      type: string
`)

	cfg := codegen.Configuration{}
	ctx, errs := CreateParseContext(spec, cfg, nil)

	assert.Empty(t, errs)
	assert.NotNil(t, ctx)
	assert.NotEmpty(t, ctx.Operations)
}

func TestCreateParseContext_WithSimplify(t *testing.T) {
	spec := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths: {}
components:
  schemas:
    User:
      type: object
      required:
        - id
        - name
      properties:
        id:
          type: string
        name:
          type: string
        optionalUnion:
          anyOf:
            - type: string
            - type: integer
        requiredUnion:
          anyOf:
            - type: string
            - type: number
`)

	cfg := codegen.Configuration{}
	specOptions := &config.SpecOptions{
		Simplify: true,
		OptionalProperties: &config.OptionalProperties{
			Min: 1,
			Max: 3,
		},
	}

	ctx, errs := CreateParseContext(spec, cfg, specOptions)

	assert.Empty(t, errs)
	assert.NotNil(t, ctx)

	// Verify that simplification happened by checking type definitions
	// The User schema should have been processed
	assert.NotNil(t, ctx.TypeDefinitions)
}

func TestCreateParseContext_WithoutSimplify(t *testing.T) {
	spec := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths: {}
components:
  schemas:
    User:
      type: object
      properties:
        name:
          type: string
`)

	cfg := codegen.Configuration{}
	specOptions := &config.SpecOptions{
		Simplify: false,
	}

	ctx, errs := CreateParseContext(spec, cfg, specOptions)

	assert.Empty(t, errs)
	assert.NotNil(t, ctx)
	assert.NotNil(t, ctx.TypeDefinitions)
}

func TestCreateParseContext_InvalidSpec(t *testing.T) {
	spec := []byte(`invalid yaml content`)

	cfg := codegen.Configuration{}
	ctx, errs := CreateParseContext(spec, cfg, nil)

	assert.NotEmpty(t, errs)
	assert.Nil(t, ctx)
}

func TestCreateParseContext_NilSpecOptions(t *testing.T) {
	spec := []byte(`
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths: {}
`)

	cfg := codegen.Configuration{}
	ctx, errs := CreateParseContext(spec, cfg, nil)

	assert.Empty(t, errs)
	assert.NotNil(t, ctx)
	// Should use default spec options (no simplification)
}
