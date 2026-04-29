package typedef

import (
	"embed"
	"path/filepath"
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/**
var registryTestDataFS embed.FS

func loadSpecForRegistry(t *testing.T, fileName string) *codegen.ParseContext {
	t.Helper()

	cfg := codegen.NewDefaultConfiguration()

	specContents, err := registryTestDataFS.ReadFile(filepath.Join("testdata", fileName))
	if err != nil {
		t.Fatalf("Error reading file: %v", err)
	}

	parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
	if len(errs) > 0 {
		t.Fatalf("Error parsing OpenAPI spec: %v", errs[0])
	}

	return parseCtx
}

func TestTypeDefinitionRegistry_FindOperation(t *testing.T) {
	t.Run("Finds operation by path and method", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users/{id}", "GET")
		assert.NotNil(t, op)
		assert.Equal(t, "GET", op.Method)
		assert.Equal(t, "/users/{id}", op.Path)
	})

	t.Run("Returns nil for non-existent operation", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/nonexistent", "GET")
		assert.Nil(t, op)
	})

	t.Run("Operations returns all operations", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		ops := registry.Operations()
		assert.NotNil(t, ops)
		assert.Greater(t, len(ops), 0)
	})

	t.Run("Returns nil for wrong method", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users/{id}", "POST")
		assert.Nil(t, op)
	})
}

func TestTypeDefinitionRegistry_GetResponseSchema(t *testing.T) {
	t.Run("Returns response schema for valid operation", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		respSchema := registry.GetResponseSchema("/users", "POST")
		assert.NotNil(t, respSchema)
		assert.Equal(t, "application/json", respSchema.ContentType)
		assert.NotNil(t, respSchema.Body)
	})

	t.Run("Returns nil for non-existent operation", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		respSchema := registry.GetResponseSchema("/nonexistent", "GET")
		assert.Nil(t, respSchema)
	})

	t.Run("Returns schema for 204 No Content response", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		specContents := []byte(`
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /test:
    delete:
      operationId: deleteTest
      responses:
        '204':
          description: No Content
`)
		parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
		assert.Empty(t, errs)

		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)
		respSchema := registry.GetResponseSchema("/test", "DELETE")
		assert.NotNil(t, respSchema)
		// 204 is still a success response, body may be empty or placeholder
	})
}

func TestTypeDefinitionRegistry_GetTypeDefinitionLookup(t *testing.T) {
	t.Run("Returns type definition lookup map", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		lookup := registry.GetTypeDefinitionLookup()
		assert.NotNil(t, lookup)
		assert.Contains(t, lookup, "User")
	})
}

func TestResolveCodegenSchema(t *testing.T) {
	t.Run("Integration test with real spec", func(t *testing.T) {
		// Use actual OpenAPI spec parsing instead of manually constructing schemas
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Verify that schema resolution worked correctly
		op := registry.FindOperation("/users", "POST")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Body)

		// The body should have resolved UserInput schema
		assert.Contains(t, op.Body.Properties, "name")
	})

	t.Run("Handles nested references", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// User has nested Address reference
		op := registry.FindOperation("/users", "POST")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.Contains(t, success.Content.Properties, "address")

		// Address should be resolved
		address := success.Content.Properties["address"]
		assert.Equal(t, "object", address.Type)
		assert.Contains(t, address.Properties, "street")
	})

	t.Run("Handles array of references", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "GET")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)

		// users is an array of User objects
		users := success.Content.Properties["users"]
		assert.Equal(t, "array", users.Type)
		assert.NotNil(t, users.Items)
		assert.Contains(t, users.Items.Properties, "id")
		assert.Contains(t, users.Items.Properties, "name")
	})
}

func TestNewTypeDefinitionRegistry_WithRefs(t *testing.T) {
	t.Run("Handles multiple operations", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		assert.NotNil(t, registry)

		// Check GET operation
		getOp := registry.FindOperation("/users", "GET")
		assert.NotNil(t, getOp)
		assert.Equal(t, "GET", getOp.Method)
		assert.Equal(t, "/users", getOp.Path)
		assert.NotNil(t, getOp.Query)
		assert.NotNil(t, getOp.Response)

		// Check POST operation
		postOp := registry.FindOperation("/users", "POST")
		assert.NotNil(t, postOp)
		assert.Equal(t, "POST", postOp.Method)
		assert.Equal(t, "/users", postOp.Path)
		assert.NotNil(t, postOp.Body)
		assert.NotNil(t, postOp.Response)
	})

	t.Run("Resolves schema references", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "POST")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Body)

		// Body should have UserInput schema properties
		assert.Equal(t, "object", op.Body.Type)
		assert.Contains(t, op.Body.Properties, "name")
		assert.Contains(t, op.Body.Properties, "email")
		assert.Contains(t, op.Body.Properties, "age")
	})

	t.Run("Handles nested schema references", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "POST")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// User schema should have address property
		assert.Contains(t, success.Content.Properties, "address")
		addressSchema := success.Content.Properties["address"]
		assert.Equal(t, "object", addressSchema.Type)
		assert.Contains(t, addressSchema.Properties, "street")
		assert.Contains(t, addressSchema.Properties, "city")
	})

	t.Run("Handles error responses", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "GET")
		errorResp := op.Response.GetResponse(400)
		assert.NotNil(t, errorResp)
		assert.Equal(t, 400, errorResp.StatusCode)
		assert.NotNil(t, errorResp.Content)
		assert.Equal(t, "object", errorResp.Content.Type)
		assert.Contains(t, errorResp.Content.Properties, "code")
		assert.Contains(t, errorResp.Content.Properties, "message")
	})

	t.Run("Handles query parameters", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op.Query)
		assert.Len(t, op.Query, 2, "Should have 2 query parameters")

		limitParam, ok := op.Query["limit"]
		assert.True(t, ok, "Should have 'limit' parameter")
		assert.NotNil(t, limitParam)
		assert.NotNil(t, limitParam.Schema)
		assert.Equal(t, "integer", limitParam.Schema.Type)
		assert.NotNil(t, limitParam.Schema.Minimum)
		assert.Equal(t, float64(1), *limitParam.Schema.Minimum)
		assert.NotNil(t, limitParam.Schema.Maximum)
		assert.Equal(t, float64(100), *limitParam.Schema.Maximum)

		offsetParam, ok := op.Query["offset"]
		assert.True(t, ok, "Should have 'offset' parameter")
		assert.NotNil(t, offsetParam)
		assert.NotNil(t, offsetParam.Schema)
		assert.Equal(t, "integer", offsetParam.Schema.Type)
	})

	t.Run("Handles array responses", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "GET")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		assert.Contains(t, success.Content.Properties, "users")
		usersSchema := success.Content.Properties["users"]
		assert.Equal(t, "array", usersSchema.Type)
		assert.NotNil(t, usersSchema.Items)
		assert.Equal(t, "object", usersSchema.Items.Type)
		assert.Contains(t, usersSchema.Items.Properties, "id")
		assert.Contains(t, usersSchema.Items.Properties, "name")
	})
}

func TestOperation(t *testing.T) {
	t.Run("Operation has all required fields", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "POST")
		assert.NotNil(t, op)
		assert.Equal(t, "POST", op.Method)
		assert.Equal(t, "/users", op.Path)
		assert.Equal(t, "application/json", op.ContentType)
		assert.NotNil(t, op.Body)
		assert.NotNil(t, op.Response)
	})
}

func TestResponseItem(t *testing.T) {
	t.Run("ResponseItem has correct structure", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "POST")
		success := op.Response.GetSuccess()

		assert.NotNil(t, success)
		assert.Equal(t, 201, success.StatusCode)
		assert.Equal(t, "application/json", success.ContentType)
		assert.NotNil(t, success.Content)
	})
}

func TestNewTypeDefinitionRegistry_WithUnions(t *testing.T) {
	t.Run("Handles union types in responses", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-unions.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/pets", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have array type
		assert.Equal(t, "array", success.Content.Type)
		assert.NotNil(t, success.Content.Items)

		// Items should be resolved to the first union element (Dog)
		// Dog has: name, breed, barkVolume
		// Cat has: name, color, meowPitch
		assert.Equal(t, "object", success.Content.Items.Type)
		assert.Equal(t, 3, len(success.Content.Items.Properties), "Should have exactly 3 properties from Dog")

		// Verify it's Dog properties, not Cat
		assert.NotNil(t, success.Content.Items.Properties["name"], "Should have name from Dog")
		assert.NotNil(t, success.Content.Items.Properties["breed"], "Should have breed from Dog")
		assert.NotNil(t, success.Content.Items.Properties["barkVolume"], "Should have barkVolume from Dog")

		// Should NOT have Cat-specific properties
		assert.Nil(t, success.Content.Items.Properties["color"], "Should NOT have color from Cat")
		assert.Nil(t, success.Content.Items.Properties["meowPitch"], "Should NOT have meowPitch from Cat")
	})

	t.Run("Handles union types in request body", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-unions.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/payment", "POST")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Body)

		// Union types should be resolved to the first element
		assert.Equal(t, "object", op.Body.Type)

		// Should have properties from ONLY the first union element (CreditCard)
		// CreditCard has: cardNumber, expiryDate, cvv
		// BankAccount has: accountNumber, routingNumber, bankName
		assert.Equal(t, 3, len(op.Body.Properties), "Should have exactly 3 properties from CreditCard")

		// Verify it's CreditCard properties, not BankAccount
		assert.NotNil(t, op.Body.Properties["cardNumber"], "Should have cardNumber from CreditCard")
		assert.NotNil(t, op.Body.Properties["expiryDate"], "Should have expiryDate from CreditCard")
		assert.NotNil(t, op.Body.Properties["cvv"], "Should have cvv from CreditCard")

		// Should NOT have BankAccount properties
		assert.Nil(t, op.Body.Properties["accountNumber"], "Should NOT have accountNumber from BankAccount")
		assert.Nil(t, op.Body.Properties["routingNumber"], "Should NOT have routingNumber from BankAccount")
		assert.Nil(t, op.Body.Properties["bankName"], "Should NOT have bankName from BankAccount")
	})

	t.Run("Handles inline oneOf in array items", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-unions.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/authors", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have authors array
		authors := success.Content.Properties["authors"]
		assert.NotNil(t, authors, "Should have authors property")
		assert.Equal(t, "array", authors.Type, "authors should be an array")
		assert.NotNil(t, authors.Items, "authors.items should not be nil")

		// Items should be resolved to the first union element (string)
		// The oneOf has: string, Author object
		// We should pick the first one (string)
		assert.Equal(t, "string", authors.Items.Type, "items should be resolved to string (first oneOf element)")
	})

	t.Run("Picks first oneOf variant", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-unions.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/schedule", "POST")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Body)

		// Union types should be resolved to the first element
		assert.Equal(t, "object", op.Body.Type)

		// Should have properties from SweepSchedule (the first variant)
		// SweepSchedule has: type (optional)
		assert.Equal(t, 1, len(op.Body.Properties), "Should have exactly 1 property from SweepSchedule")

		// Verify it's SweepSchedule properties
		assert.NotNil(t, op.Body.Properties["type"], "Should have type from SweepSchedule")
	})

	t.Run("Preserves constraints (enum, min, max, etc.) when unwrapping primitive union types", func(t *testing.T) {
		spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /test:
    get:
      operationId: getTest
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                oneOf:
                  - type: string
                    enum: [smooth, rough]
                    minLength: 3
                    maxLength: 10
                  - type: integer
                    minimum: 1
                    maximum: 100
`

		cfg := codegen.NewDefaultConfiguration()
		parseCtx, errs := codegen.CreateParseContext([]byte(spec), cfg)
		assert.Empty(t, errs)

		registry := NewTypeDefinitionRegistry(parseCtx, 1, nil)
		ops := registry.Operations()

		assert.Len(t, ops, 1)

		op := ops[0]
		successResp := op.Response.GetSuccess()
		assert.NotNil(t, successResp)

		schema := successResp.Content
		assert.NotNil(t, schema)

		// Should pick the first union element (string)
		assert.Equal(t, "string", schema.Type)

		// Properties should be empty for primitive types
		assert.Empty(t, schema.Properties)

		// Should preserve all constraints from the OneOf schema
		assert.NotNil(t, schema.Enum)
		assert.Len(t, schema.Enum, 2)
		assert.Equal(t, "smooth", schema.Enum[0])
		assert.Equal(t, "rough", schema.Enum[1])

		assert.NotNil(t, schema.MinLength)
		assert.Equal(t, int64(3), *schema.MinLength)

		assert.NotNil(t, schema.MaxLength)
		assert.Equal(t, int64(10), *schema.MaxLength)
	})
}

func TestIsMediaTypeJSON(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		expected  bool
	}{
		{"application/json", "application/json", true},
		{"application/json with charset", "application/json; charset=utf-8", true},
		{"application/merge-patch+json", "application/merge-patch+json", true},
		{"application/json-patch+json", "application/json-patch+json", true},
		{"application/vnd.api+json", "application/vnd.api+json", true},
		{"application/problem+json", "application/problem+json", true},
		{"application/xml", "application/xml", false},
		{"text/plain", "text/plain", false},
		{"application/octet-stream", "application/octet-stream", false},
		{"application/pdf", "application/pdf", false},
		{"invalid", "invalid-media-type", false},
		{"empty string", "", false},
		{"malformed with semicolon", "application/json;", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMediaTypeJSON(tt.mediaType)
			assert.Equal(t, tt.expected, result, "isMediaTypeJSON(%q) should be %v", tt.mediaType, tt.expected)
		})
	}
}

func TestNewTypeDefinitionRegistry_JSONContentTypes(t *testing.T) {
	t.Run("Normalizes application/merge-patch+json request body to application/json", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "json-content-types.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/contacts/{id}", "PATCH")
		assert.NotNil(t, op)
		assert.Equal(t, "application/json", op.ContentType, "Request ContentType should be normalized to application/json")
		assert.NotNil(t, op.Body)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.Equal(t, "application/json", success.ContentType, "Response ContentType should be application/json")
	})

	t.Run("Normalizes application/vnd.api+json response to application/json", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "json-content-types.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/resources", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.Equal(t, "application/json", success.ContentType, "Response ContentType should be normalized to application/json")
	})
}

func TestNewTypeDefinitionRegistry_NullableArrays(t *testing.T) {
	assert := assert.New(t)
	t.Parallel()

	t.Run("Nullable array in nested request body (Stripe case)", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "nullable-array.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/v1/billing_portal/sessions", "POST")
		assert.NotNil(op)
		assert.NotNil(op.Body)

		// Navigate to flow_data.subscription_update_confirm.discounts
		flowData := op.Body.Properties["flow_data"]
		assert.NotNil(flowData, "flow_data property should exist")
		assert.Equal("object", flowData.Type)

		subscriptionUpdateConfirm := flowData.Properties["subscription_update_confirm"]
		assert.NotNil(subscriptionUpdateConfirm, "subscription_update_confirm property should exist")
		assert.Equal("object", subscriptionUpdateConfirm.Type)

		// Check that discounts is an array, not a string
		discountsSchema := subscriptionUpdateConfirm.Properties["discounts"]
		assert.NotNil(discountsSchema, "discounts property should exist")
		assert.Equal("array", discountsSchema.Type, "discounts should be type array, not string")
		assert.True(discountsSchema.Nullable, "discounts should be nullable")
		assert.NotNil(discountsSchema.Items, "discounts should have items schema")
		assert.Equal("object", discountsSchema.Items.Type, "discounts items should be objects")

		// Also check the items array
		itemsSchema := subscriptionUpdateConfirm.Properties["items"]
		assert.NotNil(itemsSchema, "items property should exist")
		assert.Equal("array", itemsSchema.Type, "items should be type array")
		assert.NotNil(itemsSchema.Items, "items should have items schema")

		// Debug: print the schema to see what we have
		t.Logf("discounts schema: Type=%s, Nullable=%v, Items=%v",
			discountsSchema.Type, discountsSchema.Nullable, discountsSchema.Items != nil)
		if discountsSchema.Items != nil {
			t.Logf("discounts.items schema: Type=%s", discountsSchema.Items.Type)
		}
	})
}

func TestNewTypeDefinitionRegistry_EdgeCases(t *testing.T) {
	t.Run("Handles empty spec", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		specContents := []byte(`
openapi: 3.0.0
info:
  title: Empty API
  version: 1.0.0
paths: {}
`)
		parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
		assert.Empty(t, errs)

		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)
		assert.NotNil(t, registry)

		op := registry.FindOperation("/nonexistent", "GET")
		assert.Nil(t, op)
	})

	t.Run("Handles spec with no components", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		specContents := []byte(`
openapi: 3.0.0
info:
  title: Simple API
  version: 1.0.0
paths:
  /ping:
    get:
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                type: string
`)
		parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
		assert.Empty(t, errs)

		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)
		assert.NotNil(t, registry)

		op := registry.FindOperation("/ping", "GET")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Response)
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.Equal(t, "string", success.Content.Type)
	})

	t.Run("Object with empty additionalProperties schema", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "empty-additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/config", "PATCH")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Body)

		// The body should be an object type (map[string]any in Go)
		assert.Equal(t, "object", op.Body.Type)

		// Should have no defined properties
		assert.Empty(t, op.Body.Properties)

		// Should have additionalProperties set (oapi-codegen generates map[string]any for empty additionalProperties)
		assert.NotNil(t, op.Body.AdditionalProperties, "AdditionalProperties should not be nil for object with additionalProperties: {}")

		// AdditionalProperties should have a type (any for empty schema)
		assert.NotEmpty(t, op.Body.AdditionalProperties.Type, "AdditionalProperties.Type should not be empty")
	})
}

func TestLazyTypeDefinitionRegistry(t *testing.T) {
	specBytes := []byte(`
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /users:
    get:
      operationId: getUsers
      responses:
        "200":
          description: Success
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/User'
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: Success
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
components:
  schemas:
    User:
      type: object
      properties:
        id:
          type: string
        name:
          type: string
`)

	cfg := codegen.NewDefaultConfiguration()

	t.Run("Creates lazy registry with route info", func(t *testing.T) {
		registry, err := NewLazyTypeDefinitionRegistry(specBytes, cfg, nil)
		assert.NoError(t, err)
		assert.NotNil(t, registry)

		routes := registry.GetRouteInfo()
		assert.Len(t, routes, 2)

		// Check route info is populated
		routeMap := make(map[string]RouteInfo)
		for _, r := range routes {
			routeMap[r.Method+":"+r.Path] = r
		}

		assert.Contains(t, routeMap, "GET:/users")
		assert.Contains(t, routeMap, "GET:/users/{id}")
		assert.Equal(t, "getUsers", routeMap["GET:/users"].ID)
		assert.Equal(t, "getUser", routeMap["GET:/users/{id}"].ID)
	})

	t.Run("FindOperation parses on-demand and caches", func(t *testing.T) {
		registry, err := NewLazyTypeDefinitionRegistry(specBytes, cfg, nil)
		assert.NoError(t, err)

		// Initially no operations are cached
		assert.Empty(t, registry.Operations())

		// Find an operation - should parse on-demand
		op := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op)
		assert.Equal(t, "GET", op.Method)
		assert.Equal(t, "/users", op.Path)

		// Now one operation should be cached
		assert.Len(t, registry.Operations(), 1)

		// Find same operation again - should use cache
		op2 := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op2)
		assert.Equal(t, op, op2) // Same pointer

		// Still only one operation cached
		assert.Len(t, registry.Operations(), 1)

		// Find different operation
		op3 := registry.FindOperation("/users/{id}", "GET")
		assert.NotNil(t, op3)
		assert.Equal(t, "/users/{id}", op3.Path)

		// Now two operations cached
		assert.Len(t, registry.Operations(), 2)
	})

	t.Run("FindOperation returns nil for non-existent operation", func(t *testing.T) {
		registry, err := NewLazyTypeDefinitionRegistry(specBytes, cfg, nil)
		assert.NoError(t, err)

		op := registry.FindOperation("/nonexistent", "GET")
		assert.Nil(t, op)
	})

	t.Run("Implements OperationRegistry interface", func(t *testing.T) {
		registry, err := NewLazyTypeDefinitionRegistry(specBytes, cfg, nil)
		assert.NoError(t, err)

		// Should satisfy the interface
		var _ OperationRegistry = registry
	})

	t.Run("TypeDefinitionRegistry also implements OperationRegistry", func(t *testing.T) {
		parseCtx, errs := codegen.CreateParseContext(specBytes, cfg)
		assert.Empty(t, errs)

		registry := NewTypeDefinitionRegistry(parseCtx, 0, specBytes)

		// Should satisfy the interface
		var _ OperationRegistry = registry

		// GetRouteInfo should work
		routes := registry.GetRouteInfo()
		assert.Len(t, routes, 2)
	})
}

func TestResolveCodegenSchema_CycleDetection(t *testing.T) {
	t.Run("handles circular references", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "circular-array.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Should not panic or infinite loop
		assert.NotNil(t, registry)
	})
}

func TestResolveCodegenSchema_NestedArrays(t *testing.T) {
	t.Run("handles nested array types", func(t *testing.T) {
		// Create a schema with nested array type [][]string
		schema := &codegen.GoSchema{
			GoType: "[][]string",
		}
		tdLookup := make(map[string]*codegen.TypeDefinition)

		result := resolveCodegenSchema(schema, tdLookup, nil)
		assert.NotNil(t, result)
		assert.NotNil(t, result.ArrayType)
		assert.Equal(t, "[]string", result.ArrayType.GoType)
	})
}

func TestResolveCodegenSchema_UnionElements(t *testing.T) {
	t.Run("handles union with primitive type", func(t *testing.T) {
		parseCtx := loadSpecForRegistry(t, "with-unions.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Should handle unions correctly
		assert.NotNil(t, registry)
	})
}

func TestExtractRouteInfo(t *testing.T) {
	t.Run("extracts routes from valid spec", func(t *testing.T) {
		specBytes := []byte(`
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /users:
    get:
      operationId: getUsers
      responses:
        '200':
          description: OK
    post:
      operationId: createUser
      responses:
        '201':
          description: Created
  /users/{id}:
    get:
      responses:
        '200':
          description: OK
`)
		cfg := codegen.Configuration{}
		routes, err := extractRouteInfo(specBytes, cfg)
		assert.NoError(t, err)
		assert.Len(t, routes, 3)

		// Check that routes have correct info
		var foundGetUsers, foundCreateUser, foundGetUserById bool
		for _, r := range routes {
			if r.ID == "getUsers" && r.Method == "GET" && r.Path == "/users" {
				foundGetUsers = true
			}
			if r.ID == "createUser" && r.Method == "POST" && r.Path == "/users" {
				foundCreateUser = true
			}
			// Generated operation ID for missing operationId
			if r.Method == "GET" && r.Path == "/users/{id}" {
				foundGetUserById = true
				assert.Equal(t, "get_/users/{id}", r.ID)
			}
		}
		assert.True(t, foundGetUsers)
		assert.True(t, foundCreateUser)
		assert.True(t, foundGetUserById)
	})

	t.Run("returns error for invalid spec", func(t *testing.T) {
		specBytes := []byte(`invalid yaml: [`)
		cfg := codegen.Configuration{}
		routes, err := extractRouteInfo(specBytes, cfg)
		assert.Error(t, err)
		assert.Nil(t, routes)
	})

	t.Run("returns nil for spec without paths", func(t *testing.T) {
		specBytes := []byte(`
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
`)
		cfg := codegen.Configuration{}
		routes, err := extractRouteInfo(specBytes, cfg)
		assert.NoError(t, err)
		assert.Nil(t, routes)
	})
}

func TestNewLazyTypeDefinitionRegistry_Errors(t *testing.T) {
	t.Run("returns error for invalid spec", func(t *testing.T) {
		specBytes := []byte(`invalid yaml: [`)
		cfg := codegen.Configuration{}
		registry, err := NewLazyTypeDefinitionRegistry(specBytes, cfg, nil)
		assert.Error(t, err)
		assert.Nil(t, registry)
	})
}

func TestNewRegistryFromSpec(t *testing.T) {
	specBytes := []byte(`
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

	t.Run("Creates eager registry by default", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		registry, err := NewRegistryFromSpec(specBytes, cfg, nil)
		assert.NoError(t, err)
		assert.NotNil(t, registry)

		// Should be TypeDefinitionRegistry (eager)
		_, ok := registry.(*TypeDefinitionRegistry)
		assert.True(t, ok, "Should return TypeDefinitionRegistry when LazyLoad is false")

		// Should have operations
		ops := registry.Operations()
		assert.NotEmpty(t, ops)
	})

	t.Run("Creates lazy registry when LazyLoad is true", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		specOptions := &config.SpecOptions{LazyLoad: true}
		registry, err := NewRegistryFromSpec(specBytes, cfg, specOptions)
		assert.NoError(t, err)
		assert.NotNil(t, registry)

		// Should be LazyTypeDefinitionRegistry
		_, ok := registry.(*LazyTypeDefinitionRegistry)
		assert.True(t, ok, "Should return LazyTypeDefinitionRegistry when LazyLoad is true")

		// Operations should be empty initially (lazy)
		ops := registry.Operations()
		assert.Empty(t, ops)

		// But FindOperation should work
		op := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op)
	})

	t.Run("GetResponseSchema works on both registry types", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()

		// Eager registry
		eagerRegistry, err := NewRegistryFromSpec(specBytes, cfg, nil)
		assert.NoError(t, err)
		respSchema := eagerRegistry.GetResponseSchema("/users", "GET")
		assert.NotNil(t, respSchema)
		assert.Equal(t, "application/json", respSchema.ContentType)

		// Lazy registry
		lazyRegistry, err := NewRegistryFromSpec(specBytes, cfg, &config.SpecOptions{LazyLoad: true})
		assert.NoError(t, err)
		respSchema = lazyRegistry.GetResponseSchema("/users", "GET")
		assert.NotNil(t, respSchema)
		assert.Equal(t, "application/json", respSchema.ContentType)

		// Non-existent operation returns nil on lazy registry
		respSchema = lazyRegistry.GetResponseSchema("/nonexistent", "GET")
		assert.Nil(t, respSchema)
	})

	t.Run("Returns error on invalid spec", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		invalidSpec := []byte(`invalid yaml: [`)

		registry, err := NewRegistryFromSpec(invalidSpec, cfg, nil)
		assert.Error(t, err)
		assert.Nil(t, registry)
	})

	t.Run("Returns error on invalid spec with LazyLoad", func(t *testing.T) {
		cfg := codegen.NewDefaultConfiguration()
		invalidSpec := []byte(`invalid yaml: [`)

		registry, err := NewRegistryFromSpec(invalidSpec, cfg, &config.SpecOptions{LazyLoad: true})
		assert.Error(t, err)
		assert.Nil(t, registry)
	})
}
