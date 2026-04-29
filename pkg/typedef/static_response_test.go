package typedef

import (
	"encoding/json"
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/generator"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestStaticResponseIntegration(t *testing.T) {
	t.Run("end-to-end static response flow", func(t *testing.T) {
		spec := []byte(`
openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
paths:
  /users:
    get:
      operationId: getUsers
      responses:
        '200':
          description: Success
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
              x-static-response: '[{"id":"1","name":"Alice"},{"id":"2","name":"Bob"}]'
`)

		// Create parse context
		cfg := codegen.NewDefaultConfiguration()
		parseCtx, errs := codegen.CreateParseContext(spec, cfg)
		assert.Empty(t, errs)

		// Create registry with spec bytes to extract static responses
		registry := NewTypeDefinitionRegistry(parseCtx, 0, spec)
		assert.NotNil(t, registry)

		// Find the operation
		op := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op)

		// Get the success response
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Verify static content is set
		assert.Equal(t, `[{"id":"1","name":"Alice"},{"id":"2","name":"Bob"}]`, success.Content.StaticContent)

		// Now test that the generator uses the static content
		gen, err := generator.NewGenerator(nil, nil)
		assert.NoError(t, err)

		// Create a response schema from the operation
		respSchema := &schema.ResponseSchema{
			Body:        success.Content,
			ContentType: success.ContentType,
		}

		// Generate response
		respData := gen.Response(respSchema, nil)

		// Verify the response is the static content
		assert.False(t, respData.IsError)
		assert.NotNil(t, respData.Body)

		// Parse the response body
		var users []map[string]string
		err = json.Unmarshal(respData.Body, &users)
		assert.NoError(t, err)

		// Verify the content matches the static response
		assert.Len(t, users, 2)
		assert.Equal(t, "1", users[0]["id"])
		assert.Equal(t, "Alice", users[0]["name"])
		assert.Equal(t, "2", users[1]["id"])
		assert.Equal(t, "Bob", users[1]["name"])
	})

	t.Run("static response with different status codes", func(t *testing.T) {
		spec := []byte(`
openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
paths:
  /products/{id}:
    get:
      operationId: getProduct
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                type: object
              x-static-response: '{"id":"123","name":"Widget"}'
        '404':
          description: Not found
          content:
            application/json:
              schema:
                type: object
              x-static-response: '{"error":"Not found"}'
`)

		cfg := codegen.NewDefaultConfiguration()
		parseCtx, errs := codegen.CreateParseContext(spec, cfg)
		assert.Empty(t, errs)

		registry := NewTypeDefinitionRegistry(parseCtx, 0, spec)
		assert.NotNil(t, registry)

		op := registry.FindOperation("/products/{id}", "GET")
		assert.NotNil(t, op)

		// Check 200 response
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.Equal(t, `{"id":"123","name":"Widget"}`, success.Content.StaticContent)

		// Check 404 response
		notFound := op.Response.GetResponse(404)
		assert.NotNil(t, notFound)
		assert.Equal(t, `{"error":"Not found"}`, notFound.Content.StaticContent)
	})
}

func TestExtractStaticResponses(t *testing.T) {
	t.Run("returns error for invalid spec", func(t *testing.T) {
		_, err := ExtractStaticResponses([]byte(`invalid: [`))
		assert.Error(t, err)
	})

	t.Run("returns empty map for spec without paths", func(t *testing.T) {
		spec := []byte(`openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
`)
		result, err := ExtractStaticResponses(spec)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("skips responses without content", func(t *testing.T) {
		spec := []byte(`openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /test:
    get:
      responses:
        '204':
          description: No content
`)
		result, err := ExtractStaticResponses(spec)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("skips non-numeric status codes", func(t *testing.T) {
		spec := []byte(`openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /test:
    get:
      responses:
        default:
          description: Default
          content:
            application/json:
              schema:
                type: object
              x-static-response: '{"error":"default"}'
`)
		result, err := ExtractStaticResponses(spec)
		assert.NoError(t, err)
		assert.Empty(t, result)
	})
}
