package typedef

import (
	"embed"
	"path/filepath"
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/**
var testDataFS embed.FS

func loadTestSpec(t *testing.T, fileName string) *codegen.ParseContext {
	t.Helper()

	cfg := codegen.NewDefaultConfiguration()

	specContents, err := testDataFS.ReadFile(filepath.Join("testdata", fileName))
	if err != nil {
		t.Fatalf("Error reading file: %v", err)
	}

	parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
	if len(errs) > 0 {
		t.Fatalf("Error parsing OpenAPI spec: %v", errs[0])
	}

	return parseCtx
}

func TestInferType(t *testing.T) {
	t.Run("Default to object when nil OpenAPI schema", func(t *testing.T) {
		result := inferType(&codegen.GoSchema{})
		assert.Equal(t, "object", result)
	})

	t.Run("Returns first non-null type from OpenAPI schema", func(t *testing.T) {
		result := inferType(&codegen.GoSchema{
			OpenAPISchema: &base.Schema{
				Type: []string{"string"},
			},
		})
		assert.Equal(t, "string", result)
	})

	t.Run("Skips null type and returns first non-null type", func(t *testing.T) {
		result := inferType(&codegen.GoSchema{
			OpenAPISchema: &base.Schema{
				Type: []string{"null", "string"},
			},
		})
		assert.Equal(t, "string", result)
	})

	t.Run("Returns object when all types are null", func(t *testing.T) {
		result := inferType(&codegen.GoSchema{
			OpenAPISchema: &base.Schema{
				Type: []string{"null", "null"},
			},
		})
		assert.Equal(t, "object", result)
	})

	t.Run("Integration test - infers types from real spec", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Verify type inference worked for different field types
		op := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Query)

		// Query parameters should have correct types
		limitParam := op.Query["limit"]
		assert.NotNil(t, limitParam)
		assert.NotNil(t, limitParam.Schema)
		assert.Equal(t, "integer", limitParam.Schema.Type)

		offsetParam := op.Query["offset"]
		assert.NotNil(t, offsetParam)
		assert.NotNil(t, offsetParam.Schema)
		assert.Equal(t, "integer", offsetParam.Schema.Type)
	})
}

func TestDeref(t *testing.T) {
	t.Run("Non-nil pointer", func(t *testing.T) {
		val := 42
		result := deref(&val)
		assert.Equal(t, 42, result)
	})

	t.Run("Nil pointer returns zero value", func(t *testing.T) {
		var ptr *int
		result := deref(ptr)
		assert.Equal(t, 0, result)
	})

	t.Run("String pointer", func(t *testing.T) {
		val := "hello"
		result := deref(&val)
		assert.Equal(t, "hello", result)
	})

	t.Run("Bool pointer", func(t *testing.T) {
		val := true
		result := deref(&val)
		assert.Equal(t, true, result)
	})
}

func TestPromoteProperties(t *testing.T) {
	t.Run("Promote properties from schema", func(t *testing.T) {
		source := &schema.Schema{
			Properties: map[string]*schema.Schema{
				"name": {Type: "string"},
				"age":  {Type: "integer"},
			},
		}
		target := make(map[string]*schema.Schema)

		promoteProperties(source, target)

		assert.Len(t, target, 2)
		assert.Equal(t, "string", target["name"].Type)
		assert.Equal(t, "integer", target["age"].Type)
	})

	t.Run("Nil schema does nothing", func(t *testing.T) {
		target := make(map[string]*schema.Schema)
		promoteProperties(nil, target)
		assert.Len(t, target, 0)
	})
}

func TestNewTypeDefinitionRegistry(t *testing.T) {
	t.Run("Creates registry from simple spec", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "simple.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		assert.NotNil(t, registry)

		// Check that operation was registered
		op := registry.FindOperation("/users/{id}", "GET")
		assert.NotNil(t, op)
		assert.Equal(t, "GET", op.Method)
		assert.Equal(t, "/users/{id}", op.Path)

		// Check response
		assert.NotNil(t, op.Response)
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Check schema properties
		assert.Equal(t, "object", success.Content.Type)
		assert.Contains(t, success.Content.Properties, "id")
		assert.Contains(t, success.Content.Properties, "name")
		assert.Contains(t, success.Content.Properties, "email")
	})
}

func TestCollectTypeDefinitions(t *testing.T) {
	t.Run("Collects nested type definitions", func(t *testing.T) {
		tds := []codegen.TypeDefinition{
			{
				Name: "User",
				Schema: codegen.GoSchema{
					AdditionalTypes: []codegen.TypeDefinition{
						{Name: "Address"},
					},
				},
			},
			{
				Name: "Product",
			},
		}

		result := collectTypeDefinitions(tds)

		assert.Len(t, result, 3)
		assert.Equal(t, "User", result[0].Name)
		assert.Equal(t, "Address", result[1].Name)
		assert.Equal(t, "Product", result[2].Name)
	})
}

func TestNewSchemaFromGoSchema_EdgeCases(t *testing.T) {
	t.Run("Integration test - handles schema with constraints", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Test schema with min/max constraints
		op := registry.FindOperation("/users", "GET")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Query)

		// limit parameter has min/max constraints
		limitParam := op.Query["limit"]
		assert.NotNil(t, limitParam)
		assert.NotNil(t, limitParam.Schema)
		assert.Equal(t, "integer", limitParam.Schema.Type)
		assert.NotNil(t, limitParam.Schema.Minimum)
		assert.Equal(t, float64(1), *limitParam.Schema.Minimum)
		assert.NotNil(t, limitParam.Schema.Maximum)
		assert.Equal(t, float64(100), *limitParam.Schema.Maximum)
	})

	t.Run("Integration test - handles schema with format", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Test email format
		op := registry.FindOperation("/users", "POST")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)

		email := success.Content.Properties["email"]
		assert.Equal(t, "string", email.Type)
		assert.Equal(t, "email", email.Format)
	})

	t.Run("Integration test - property with minimum zero has constraints", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Test age property which has minimum: 0, maximum: 150
		op := registry.FindOperation("/users", "POST")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)

		age := success.Content.Properties["age"]
		assert.NotNil(t, age, "age property should exist")
		assert.Equal(t, "integer", age.Type)
		assert.NotNil(t, age.Minimum, "age should have Minimum constraint")
		assert.Equal(t, float64(0), *age.Minimum, "age Minimum should be 0")
		assert.NotNil(t, age.Maximum, "age should have Maximum constraint")
		assert.Equal(t, float64(150), *age.Maximum, "age Maximum should be 150")
	})

	t.Run("Integration test - nested ref property preserves constraints", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Test that when User is referenced from another schema, the age constraints are preserved
		op := registry.FindOperation("/users", "GET")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)

		// The response has a "users" array of User objects
		users := success.Content.Properties["users"]
		assert.NotNil(t, users, "users property should exist")
		assert.Equal(t, "array", users.Type)
		assert.NotNil(t, users.Items, "users.Items should exist")

		// Check that the User schema in the array has the age constraints
		age := users.Items.Properties["age"]
		assert.NotNil(t, age, "age property should exist in array items")
		assert.Equal(t, "integer", age.Type)
		assert.NotNil(t, age.Minimum, "age should have Minimum constraint in array items")
		assert.Equal(t, float64(0), *age.Minimum, "age Minimum should be 0 in array items")
		assert.NotNil(t, age.Maximum, "age should have Maximum constraint in array items")
		assert.Equal(t, float64(150), *age.Maximum, "age Maximum should be 150 in array items")
	})

	t.Run("Integration test - handles required fields", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/users", "POST")
		success := op.Response.GetSuccess()
		assert.NotNil(t, success)

		// User schema has required fields
		assert.Contains(t, success.Content.Required, "id")
		assert.Contains(t, success.Content.Required, "name")
	})

	t.Run("Handles schema with nil OpenAPISchema", func(t *testing.T) {
		goSchema := &codegen.GoSchema{
			GoType:        "CustomType",
			OpenAPISchema: nil,
		}

		result := newSchemaFromGoSchema(goSchema, nil, 0)
		assert.NotNil(t, result)
		// When OpenAPISchema is nil, type defaults to empty or inferred from GoType
		// The actual behavior depends on the implementation
		assert.NotEmpty(t, result)
	})

	t.Run("Adds discriminator property when missing from schema", func(t *testing.T) {
		// This tests the case where a oneOf has a discriminator but the discriminator
		// property doesn't exist in the union element schemas (e.g., Linode's x-linode-ref-name)
		firstElementSchema := codegen.GoSchema{
			GoType: "StatsDataAvailable",
			Properties: []codegen.Property{
				{GoName: "CPU", JsonFieldName: "cpu", Schema: codegen.GoSchema{GoType: "[]StatsData"}},
			},
		}

		goSchema := &codegen.GoSchema{
			GoType: "GetManagedStats_Response_Data",
			UnionElements: []codegen.UnionElement{
				{TypeName: "StatsDataAvailable", Schema: firstElementSchema},
				{TypeName: "StatsDataUnavailable", Schema: codegen.GoSchema{GoType: "[]string"}},
			},
			Discriminator: &codegen.Discriminator{
				Property: "x-linode-ref-name",
				Mapping: map[string]string{
					"StatsDataAvailable":   "StatsDataAvailable",
					"StatsDataUnavailable": "StatsDataUnavailable",
				},
			},
		}

		tdLookUp := map[string]*codegen.TypeDefinition{
			"StatsDataAvailable": {
				Name:   "StatsDataAvailable",
				Schema: firstElementSchema,
			},
		}

		result := newSchemaFromGoSchema(goSchema, tdLookUp, 3)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Properties)

		// The discriminator property should be added with the correct value
		discProp, ok := result.Properties["x-linode-ref-name"]
		assert.True(t, ok, "discriminator property should be added")
		assert.NotNil(t, discProp)
		assert.Equal(t, "string", discProp.Type)
		assert.Equal(t, []any{"StatsDataAvailable"}, discProp.Enum)
	})
}

func TestConvertEnumValue(t *testing.T) {
	tests := []struct {
		value      string
		schemaType string
		expected   any
	}{
		{"42", "integer", int64(42)},
		{"-100", "integer", int64(-100)},
		{"not-a-number", "integer", "not-a-number"},
		{"3.14", "number", float64(3.14)},
		{"42", "number", float64(42)},
		{"not-a-number", "number", "not-a-number"},
		{"true", "boolean", true},
		{"false", "boolean", false},
		{"not-a-bool", "boolean", "not-a-bool"},
		{"hello", "string", "hello"},
		{"value", "unknown", "value"},
		// String enum values that look like numbers should stay as strings
		{"0", "string", "0"},
		{"1", "string", "1"},
		{"2", "string", "2"},
		{"3", "string", "3"},
		// Test extractLeadingNumber branch for integer
		{"101 (EastAsia)", "integer", int64(101)},
		{"0 (User)", "integer", int64(0)},
		// Test extractLeadingNumber branch for number
		{"3.14 (Pi)", "number", float64(3.14)},
		{"42.5 (Value)", "number", float64(42.5)},
	}

	for _, tt := range tests {
		result := convertEnumValue(tt.value, tt.schemaType)
		assert.Equal(t, tt.expected, result)
	}
}

// TestStringEnumWithNumericValues tests that string enums with numeric-looking values
// (like "0", "1", "2") are preserved as strings throughout the entire chain from
// OpenAPI spec parsing to schema generation.
func TestStringEnumWithNumericValues(t *testing.T) {
	// OpenAPI spec with a string enum that has numeric values
	specYAML := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    post:
      operationId: TestOp
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                cb_avalgo:
                  type: string
                  enum:
                    - '0'
                    - '1'
                    - '2'
                    - '3'
                    - 'A'
      responses:
        '200':
          description: OK
`

	cfg := codegen.NewDefaultConfiguration()
	parseCtx, errs := codegen.CreateParseContext([]byte(specYAML), cfg)
	if len(errs) > 0 {
		t.Fatalf("Failed to parse spec: %v", errs[0])
	}

	// Create type definition registry
	registry := NewTypeDefinitionRegistry(parseCtx, 10, []byte(specYAML))

	// Get the operation
	ops := registry.Operations()
	assert.Len(t, ops, 1, "Should have one operation")

	op := ops[0]
	assert.NotNil(t, op.Body, "Operation should have a body")

	// Get the cb_avalgo property
	cbAvalgo, ok := op.Body.Properties["cb_avalgo"]
	assert.True(t, ok, "Should have cb_avalgo property")
	assert.Equal(t, "string", cbAvalgo.Type, "cb_avalgo should be string type")

	// Verify enum values are strings, not integers
	assert.NotNil(t, cbAvalgo.Enum, "cb_avalgo should have enum values")
	assert.Len(t, cbAvalgo.Enum, 5, "Should have 5 enum values")

	// Check each enum value is a string
	for i, enumVal := range cbAvalgo.Enum {
		_, ok := enumVal.(string)
		assert.True(t, ok, "Enum value at index %d should be a string, got %T: %v", i, enumVal, enumVal)
	}

	// Specifically check the numeric-looking values
	expectedValues := []string{"0", "1", "2", "3", "A"}
	for i, expected := range expectedValues {
		actual, ok := cbAvalgo.Enum[i].(string)
		assert.True(t, ok, "Enum value should be string")
		assert.Equal(t, expected, actual, "Enum value at index %d should be %q", i, expected)
	}
}

func TestAdditionalProperties(t *testing.T) {
	t.Run("Map with any values (additionalProperties: true)", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-any", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties set
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "string", success.Content.AdditionalProperties.Type)
	})

	t.Run("Map with string values", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-string", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties with string type
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "string", success.Content.AdditionalProperties.Type)
	})

	t.Run("Map with number values", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-number", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties with number type
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "number", success.Content.AdditionalProperties.Type)
	})

	t.Run("Map with int32 format preserves format", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-int32", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties with integer type and int32 format
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "integer", success.Content.AdditionalProperties.Type)
		assert.Equal(t, "int32", success.Content.AdditionalProperties.Format)
	})

	t.Run("Map with object values", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-object", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties with object type
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "object", success.Content.AdditionalProperties.Type)
		assert.Contains(t, success.Content.AdditionalProperties.Properties, "name")
		assert.Contains(t, success.Content.AdditionalProperties.Properties, "value")
	})

	t.Run("Object with properties and additionalProperties", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/object-with-props-and-additional", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have both properties and additionalProperties
		assert.Contains(t, success.Content.Properties, "id")
		assert.Contains(t, success.Content.Properties, "name")
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "string", success.Content.AdditionalProperties.Type)
	})

	t.Run("Map with object values (no explicit type)", func(t *testing.T) {
		// This tests the case where additionalProperties has properties but no explicit type: object
		// The type should be inferred as object from the presence of properties
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-object-no-type", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties with object type (inferred)
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "object", success.Content.AdditionalProperties.Type,
			"additionalProperties with properties but no type should be inferred as object")
		assert.Contains(t, success.Content.AdditionalProperties.Properties, "name")
		assert.Contains(t, success.Content.AdditionalProperties.Properties, "path")
	})

	t.Run("Map with oneOf in additionalProperties", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/map-oneof", "GET")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Should have additionalProperties with the first oneOf element (StringValue)
		assert.NotNil(t, success.Content.AdditionalProperties)
		assert.Equal(t, "object", success.Content.AdditionalProperties.Type)
		assert.Contains(t, success.Content.AdditionalProperties.Properties, "value")

		// The value property should be a string
		valueProp := success.Content.AdditionalProperties.Properties["value"]
		assert.NotNil(t, valueProp)
		assert.Equal(t, "string", valueProp.Type)
	})
}

func TestNestedOneOfWrapper(t *testing.T) {
	t.Run("Resolves oneOf wrapped in intermediate type definitions", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "adyen-schedule.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/test", "POST")
		assert.NotNil(t, op)
		assert.NotNil(t, op.Body)

		// The schedule property should be resolved to CronSweepSchedule (first oneOf element)
		scheduleProp := op.Body.Properties["schedule"]
		assert.NotNil(t, scheduleProp)
		assert.Equal(t, "object", scheduleProp.Type)

		// Should have cronExpression property from CronSweepSchedule
		assert.Contains(t, scheduleProp.Properties, "cronExpression")
		assert.Equal(t, "string", scheduleProp.Properties["cronExpression"].Type)

		// Should have type property
		assert.Contains(t, scheduleProp.Properties, "type")
		assert.Equal(t, "string", scheduleProp.Properties["type"].Type)

		// Should have enum values from CronSweepSchedule
		typeEnum := scheduleProp.Properties["type"].Enum
		assert.NotNil(t, typeEnum)
		assert.Contains(t, typeEnum, "daily")
		assert.Contains(t, typeEnum, "weekly")
		assert.Contains(t, typeEnum, "monthly")
	})

	t.Run("Handles additionalProperties with array type", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties-array.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		op := registry.FindOperation("/test", "POST")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// Check terminalsWithErrors property
		terminalsWithErrors := success.Content.Properties["terminalsWithErrors"]
		assert.NotNil(t, terminalsWithErrors)
		assert.Equal(t, "object", terminalsWithErrors.Type)

		// Should have additionalProperties with array type
		assert.NotNil(t, terminalsWithErrors.AdditionalProperties)
		assert.Equal(t, "array", terminalsWithErrors.AdditionalProperties.Type)

		// The array items should be strings
		assert.NotNil(t, terminalsWithErrors.AdditionalProperties.Items)
		assert.Equal(t, "string", terminalsWithErrors.AdditionalProperties.Items.Type)
	})
}

func TestSchemaReferenceExpansion(t *testing.T) {
	t.Run("Schema references are properly expanded and cached", func(t *testing.T) {
		// This test verifies the fix for the issue where schema references
		// were not being properly expanded, causing properties to have empty
		// Type fields instead of the referenced schema's type and properties.
		//
		// The bug was that when a schema had a $ref (e.g., status: $ref: '#/components/schemas/Account'),
		// the cache placeholder was created but never updated with the expanded schema,
		// so subsequent accesses would get an empty schema instead of the full Account schema.
		parseCtx := loadTestSpec(t, "with-refs.yml")
		registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

		// Get an operation that has a response with a referenced schema
		op := registry.FindOperation("/users", "POST")
		assert.NotNil(t, op)

		success := op.Response.GetSuccess()
		assert.NotNil(t, success)
		assert.NotNil(t, success.Content)

		// The response should have an "address" property that references the Address schema
		address := success.Content.Properties["address"]
		assert.NotNil(t, address, "address property should exist")

		// The address property should have been expanded to include the full Address schema
		assert.Equal(t, "object", address.Type, "address should have type 'object'")
		assert.NotEmpty(t, address.Properties, "address should have properties from the referenced Address schema")

		// Verify that the Address schema properties are present
		assert.Contains(t, address.Properties, "street")
		assert.Contains(t, address.Properties, "city")
		assert.Equal(t, "string", address.Properties["street"].Type)
		assert.Equal(t, "string", address.Properties["city"].Type)
	})
}

func TestArrayWithRefItems(t *testing.T) {
	assert := assert.New(t)

	t.Run("simple case", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "array-with-ref-items.yaml")
		reg := NewTypeDefinitionRegistry(parseCtx, 10, nil)

		// Find the POST /test operation
		op := reg.FindOperation("/test", "POST")
		assert.NotNil(op, "Operation should exist")
		assert.NotNil(op.Body, "Operation should have a body")

		// Navigate to spending_controls
		spendingControls, ok := op.Body.Properties["spending_controls"]
		assert.True(ok, "spending_controls property should exist")
		assert.Equal("object", spendingControls.Type, "spending_controls should be an object")

		// Navigate to spending_limits - THIS IS THE KEY TEST
		spendingLimits, ok := spendingControls.Properties["spending_limits"]
		assert.True(ok, "spending_limits property should exist")

		// The bug: spending_limits has type "string" instead of "array"
		assert.Equal("array", spendingLimits.Type, "spending_limits should have type 'array'")
		assert.NotNil(spendingLimits.Items, "spending_limits should have items schema")

		if spendingLimits.Items != nil {
			assert.Equal("object", spendingLimits.Items.Type, "spending_limits items should be objects")
			assert.Contains(spendingLimits.Items.Properties, "amount", "items should have amount property")
			assert.Contains(spendingLimits.Items.Properties, "interval", "items should have interval property")
		}
	})

}

func TestOneOfArrayVsAdditionalProperties(t *testing.T) {
	// This test reproduces the issue from artifacthub.io spec where we have:
	// oneOf:
	//   - array of objects
	//   - object with additionalProperties: true
	parseCtx := loadTestSpec(t, "falco-rules-oneof.yml")
	reg := NewTypeDefinitionRegistry(parseCtx, 10, nil)

	// Find the GET /test operation
	op := reg.FindOperation("/test", "GET")
	assert.NotNil(t, op)

	// Get the 200 response schema
	response := op.Response.GetResponse(200)
	assert.NotNil(t, response)
	assert.NotNil(t, response.Content)

	// Navigate to data.rules which has the oneOf
	dataSchema := response.Content.Properties["data"]
	assert.NotNil(t, dataSchema)
	assert.Equal(t, "object", dataSchema.Type)

	rulesSchema := dataSchema.Properties["rules"]
	assert.NotNil(t, rulesSchema)

	// The oneOf should be resolved to one of the options
	// Expected: either array type OR object type with additionalProperties
	// For oneOf with array vs object, we should pick one consistently
	// Preferably the array since it's more specific
	switch rulesSchema.Type {
	case "array":
		assert.NotNil(t, rulesSchema.Items, "Array type should have items")
	case "object":
		assert.NotNil(t, rulesSchema.AdditionalProperties, "Object type should have additionalProperties")
	default:
		t.Fatalf("Unexpected type for oneOf: %s", rulesSchema.Type)
	}

	t.Run("additionalProperties with oneOf primitives resolves to first union element", func(t *testing.T) {
		parseCtx := loadTestSpec(t, "additional-properties-oneof-primitives.yml")
		reg := NewTypeDefinitionRegistry(parseCtx, 10, nil)

		// Find the GET /test operation
		op := reg.FindOperation("/test", "GET")
		assert.NotNil(t, op, "Operation should exist")

		// Get the 200 response schema
		response := op.Response.GetResponse(200)
		assert.NotNil(t, response)
		assert.NotNil(t, response.Content)

		// Check customData property
		customDataSchema, ok := response.Content.Properties["customData"]
		assert.True(t, ok, "customData property should exist")
		assert.Equal(t, "object", customDataSchema.Type)

		// When additionalProperties has oneOf with primitives (string, number, boolean),
		// oapi-codegen now generates proper marshal/unmarshal methods for the union type.
		// We resolve to the first union element (string in this case).
		assert.NotNil(t, customDataSchema.AdditionalProperties, "customData should have additionalProperties")

		// The first union element is string
		assert.Equal(t, "string", customDataSchema.AdditionalProperties.Type,
			"additionalProperties with oneOf primitives resolves to first union element (string)")
	})

	t.Run("type array [string, object, null] generates object not string", func(t *testing.T) {
		// OpenAPI 3.1 type arrays like type: [string, object, null] are simplified
		// by oapi-codegen to map[string]any. We should generate an object, not pick
		// the first union element (string).
		parseCtx := loadTestSpec(t, "type-array-string-object.yml")
		reg := NewTypeDefinitionRegistry(parseCtx, 10, nil)

		op := reg.FindOperation("/test", "GET")
		assert.NotNil(t, op, "Operation should exist")

		response := op.Response.GetResponse(200)
		assert.NotNil(t, response)
		assert.NotNil(t, response.Content)

		metadataSchema, ok := response.Content.Properties["metadata"]
		assert.True(t, ok, "metadata property should exist")

		// Should be object type (for map[string]any), not string
		assert.Equal(t, "object", metadataSchema.Type,
			"type array [string, object, null] should resolve to object for map[string]any")
	})

	t.Run("handles []struct{} from empty item schemas", func(t *testing.T) {
		// Test that []struct{} (generated by oapi-codegen for items: {})
		// is converted to array with items of type "any"
		goSchema := &codegen.GoSchema{
			GoType: "[]struct{}",
		}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 10,
		}
		result := newSchemaFromGoSchemaWithContext(goSchema, nil, ctx)

		assert.Equal(t, "array", result.Type, "Should be array type")
		assert.NotNil(t, result.Items, "Should have items schema")
		assert.Equal(t, "any", result.Items.Type, "Items should be type 'any' for struct{}")
	})

	t.Run("handles struct{} from empty schemas", func(t *testing.T) {
		// Test that struct{} (generated by oapi-codegen for empty schemas {})
		// is converted to type "any" so the generator creates empty objects {}
		goSchema := &codegen.GoSchema{
			GoType: "struct{}",
		}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 10,
		}
		result := newSchemaFromGoSchemaWithContext(goSchema, nil, ctx)

		assert.Equal(t, "any", result.Type, "struct{} should be converted to type 'any'")
	})

	t.Run("handles nested arrays ([][]T)", func(t *testing.T) {
		// Test that nested arrays like [][]struct{...} are correctly converted
		// to array with items of type array
		goSchema := &codegen.GoSchema{
			GoType: "[][]struct { Name *string }",
		}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 10,
		}
		result := newSchemaFromGoSchemaWithContext(goSchema, nil, ctx)

		assert.Equal(t, "array", result.Type, "Should be array type")
		assert.NotNil(t, result.Items, "Should have items schema")
		assert.Equal(t, "array", result.Items.Type, "Items should be array type for nested array")
		assert.NotNil(t, result.Items.Items, "Nested array should have items schema")
	})

	t.Run("handles triple nested arrays ([][][]T)", func(t *testing.T) {
		// Test that triple nested arrays are correctly converted
		goSchema := &codegen.GoSchema{
			GoType: "[][][]string",
		}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 10,
		}
		result := newSchemaFromGoSchemaWithContext(goSchema, nil, ctx)

		assert.Equal(t, "array", result.Type, "Level 1 should be array")
		assert.NotNil(t, result.Items, "Level 1 should have items")
		assert.Equal(t, "array", result.Items.Type, "Level 2 should be array")
		assert.NotNil(t, result.Items.Items, "Level 2 should have items")
		assert.Equal(t, "array", result.Items.Items.Type, "Level 3 should be array")
		assert.NotNil(t, result.Items.Items.Items, "Level 3 should have items")
		assert.Equal(t, "string", result.Items.Items.Items.Type, "Level 4 should be string")
	})

	t.Run("handles nested arrays with union wrapper items (tisane.ai case)", func(t *testing.T) {
		// This test reproduces the tisane.ai issue where:
		// - Response is [][]ListHypernyms_Response_Item
		// - ListHypernyms_Response_Item is a wrapper struct with embedded ListHypernyms_Response_AnyOf
		// - ListHypernyms_Response_AnyOf is a union of float32 and string
		// The bug was that the inner array was being unwrapped to the union type,
		// resulting in items=number instead of items=array with items=number

		// Create the union type
		unionSchema := &codegen.GoSchema{
			GoType: "struct { runtime.Either[float32, string] }",
			UnionElements: []codegen.UnionElement{
				{TypeName: "float32", Schema: codegen.GoSchema{GoType: "float32"}},
				{TypeName: "string", Schema: codegen.GoSchema{GoType: "string"}},
			},
		}

		// Create the wrapper struct (like ListHypernyms_Response_Item)
		wrapperSchema := &codegen.GoSchema{
			GoType: "struct { ListHypernyms_Response_AnyOf *ListHypernyms_Response_AnyOf }",
			Properties: []codegen.Property{
				{
					GoName:        "ListHypernyms_Response_AnyOf",
					JsonFieldName: "", // Empty = embedded field
					Schema: codegen.GoSchema{
						RefType: "ListHypernyms_Response_AnyOf",
					},
				},
			},
		}

		// Create the inner array ([]ListHypernyms_Response_Item)
		innerArraySchema := &codegen.GoSchema{
			GoType:    "[]ListHypernyms_Response_Item",
			ArrayType: wrapperSchema,
		}

		// Create the outer array ([][]ListHypernyms_Response_Item)
		outerArraySchema := &codegen.GoSchema{
			GoType:    "[][]ListHypernyms_Response_Item",
			ArrayType: innerArraySchema,
		}

		// Create type definitions lookup
		tdLookUp := map[string]*codegen.TypeDefinition{
			"ListHypernyms_Response_AnyOf": {
				Name:   "ListHypernyms_Response_AnyOf",
				Schema: *unionSchema,
			},
			"ListHypernyms_Response_Item": {
				Name:   "ListHypernyms_Response_Item",
				Schema: *wrapperSchema,
			},
		}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 10,
		}
		result := newSchemaFromGoSchemaWithContext(outerArraySchema, tdLookUp, ctx)

		// Verify the structure: array -> array -> (number or string, depending on union order)
		assert.Equal(t, "array", result.Type, "Outer should be array")
		assert.NotNil(t, result.Items, "Outer should have items")
		assert.Equal(t, "array", result.Items.Type, "Inner should be array (not number!)")
		assert.NotNil(t, result.Items.Items, "Inner should have items")
		// The innermost type should be one of the union elements (number or string)
		assert.Contains(t, []string{"number", "string"}, result.Items.Items.Type, "Innermost should be a union element type")
	})

	t.Run("handles primitive union elements not in tdLookUp", func(t *testing.T) {
		// This test reproduces the issue where:
		// - A property has type: [integer, boolean] (OpenAPI 3.1 style type array)
		// - oapi-codegen generates UnionElements with TypeName="int64" and TypeName="bool"
		// - These primitives are NOT in tdLookUp
		// - The bug was that the schema type defaulted to "string" instead of "integer"

		// Create a union schema like PublicationStats_ActiveSubscriptions
		unionSchema := &codegen.GoSchema{
			GoType: "struct { runtime.Either[int64, bool] }",
			UnionElements: []codegen.UnionElement{
				{TypeName: "int64", Schema: codegen.GoSchema{GoType: "int64", DefineViaAlias: true}},
				{TypeName: "bool", Schema: codegen.GoSchema{GoType: "bool", DefineViaAlias: true}},
			},
		}

		// Empty tdLookUp - primitives are not registered as type definitions
		tdLookUp := map[string]*codegen.TypeDefinition{}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 10,
			schemaToTypeName:  make(map[uintptr]string),
		}
		result := newSchemaFromGoSchemaWithContext(unionSchema, tdLookUp, ctx)

		// The result should be "integer" (from the first union element int64), not "string"
		assert.Equal(t, "integer", result.Type, "Should infer integer type from GoType=int64")
	})

	t.Run("marks recursive properties with Recursive flag", func(t *testing.T) {
		// Test the Telegram-like case: Chat -> pinned_message -> Message -> chat -> Chat
		// When maxRecursionDepth=0, the second occurrence of Chat should be marked as Recursive
		chatSchema := &codegen.GoSchema{
			GoType: "Chat",
		}
		messageSchema := &codegen.GoSchema{
			GoType: "Message",
		}

		// Set up the circular reference
		chatSchema.Properties = []codegen.Property{
			{
				JsonFieldName: "id",
				Schema:        codegen.GoSchema{GoType: "int64"},
			},
			{
				JsonFieldName: "pinned_message",
				Schema:        codegen.GoSchema{RefType: "Message"},
			},
		}
		messageSchema.Properties = []codegen.Property{
			{
				JsonFieldName: "message_id",
				Schema:        codegen.GoSchema{GoType: "int64"},
			},
			{
				JsonFieldName: "chat",
				Schema:        codegen.GoSchema{RefType: "Chat"},
			},
		}

		tdLookUp := map[string]*codegen.TypeDefinition{
			"Chat":    {Name: "Chat", Schema: *chatSchema},
			"Message": {Name: "Message", Schema: *messageSchema},
		}

		ctx := &schemaContext{
			cache:             make(map[string]*schema.Schema),
			depthTrack:        make(map[string]int),
			maxRecursionDepth: 0, // No recursion allowed
		}
		result := newSchemaFromGoSchemaWithContext(chatSchema, tdLookUp, ctx)

		// Chat should have pinned_message property
		assert.NotNil(t, result.Properties["pinned_message"], "Chat should have pinned_message property")

		// pinned_message -> Message should have chat property marked as Recursive
		messageProps := result.Properties["pinned_message"]
		assert.NotNil(t, messageProps.Properties["chat"], "Message should have chat property")
		if messageProps.Properties["chat"] != nil {
			chatProp := messageProps.Properties["chat"]
			assert.True(t, chatProp.Recursive, "Message.chat should be marked as Recursive")
		}
	})
}

func TestTelegramRecursiveChat(t *testing.T) {
	// Test the actual Telegram spec to verify recursive Chat handling
	// The structure is: Chat -> pinned_message (Message) -> chat (Chat)
	// With maxRecursionDepth=0, the nested Chat should be marked as Recursive

	parseCtx := loadTestSpec(t, "telegram-recursive-chat.yml")

	// Use maxRecursionDepth=0 like the integration test
	registry := NewTypeDefinitionRegistry(parseCtx, 0, nil)

	op := registry.FindOperation("/getChat", "POST")
	assert.NotNil(t, op, "Operation should not be nil")

	resp := op.Response.GetSuccess()
	assert.NotNil(t, resp, "Response should not be nil")

	content := resp.Content
	assert.NotNil(t, content, "Content should not be nil")

	// Check result property
	result := content.Properties["result"]
	assert.NotNil(t, result, "result property should not be nil")

	// Check pinned_message property
	pinnedMessage := result.Properties["pinned_message"]
	assert.NotNil(t, pinnedMessage, "pinned_message property should not be nil")

	// Check chat property inside pinned_message
	chat := pinnedMessage.Properties["chat"]
	assert.NotNil(t, chat, "chat property should not be nil")

	// The nested chat should be marked as Recursive
	assert.True(t, chat.Recursive, "Nested chat should be marked as Recursive")
}

func TestCircularArrayRecursion(t *testing.T) {
	// Test circular array: Node -> children (array of Node)
	// With maxRecursionDepth=0, the first recursion is blocked:
	// - First Node (response schema) is allowed
	// - Second Node (children.Items) is blocked (first recursion, depth 1 > maxRecursionDepth 0)
	// So children.Items should have Recursive=true
	//
	// Note: The content generator handles this by generating an empty array for required
	// array properties with recursive items.

	parseCtx := loadTestSpec(t, "circular-array.yml")

	reg := NewTypeDefinitionRegistry(parseCtx, 0, nil)

	op := reg.FindOperation("/nodes/{id}", "GET")
	assert.NotNil(t, op)

	response := op.Response.GetSuccess()
	assert.NotNil(t, response)

	nodeSchema := response.Content
	children := nodeSchema.Properties["children"]
	assert.NotNil(t, children, "children property should exist")

	// With maxRecursionDepth=0, the first nested Node (children.Items) is blocked
	// because it's the first recursion (depth 1 > maxRecursionDepth 0)
	assert.True(t, children.Items.Recursive, "First nested Node should be marked as Recursive")
}

func TestEnumCollision(t *testing.T) {
	// This test reproduces the issue where two schemas have the same name but different enum values:
	// - product_name schema has 6 enum values
	// - product.name property has 25 enum values (inline)
	// oapi-codegen generates a single ProductName type, but we need to ensure
	// the correct enum values are used for each context.
	parseCtx := loadTestSpec(t, "enum-collision.yml")
	reg := NewTypeDefinitionRegistry(parseCtx, 10, nil)

	// Check the GetMerchants operation which uses the product schema
	merchantsOp := reg.FindOperation("/merchants", "GET")
	assert.NotNil(t, merchantsOp)

	response := merchantsOp.Response.GetResponse(200)
	assert.NotNil(t, response)
	assert.NotNil(t, response.Content)

	productsSchema := response.Content.Properties["products"]
	assert.NotNil(t, productsSchema)
	assert.Equal(t, "array", productsSchema.Type)
	assert.NotNil(t, productsSchema.Items)

	// The items should be the product object
	productSchema := productsSchema.Items
	assert.Equal(t, "object", productSchema.Type)

	// The name property should have the FULL 25 enum values, not just 6
	nameSchema := productSchema.Properties["name"]
	assert.NotNil(t, nameSchema, "product.name should exist")
	assert.Equal(t, "string", nameSchema.Type)

	// The inline enum should have 25 values, not 6
	assert.GreaterOrEqual(t, len(nameSchema.Enum), 25,
		"product.name should have all 25 enum values, not just the 6 from product_name schema")

	// Verify some of the values that are in product.name but NOT in product_name
	enumValues := make(map[any]bool)
	for _, v := range nameSchema.Enum {
		enumValues[v] = true
	}
	assert.True(t, enumValues["EMAIL_PAYMENTS"], "Should have EMAIL_PAYMENTS")
	assert.True(t, enumValues["PAYFLOW_PRO"], "Should have PAYFLOW_PRO")
	assert.True(t, enumValues["VIRTUAL_TERMINAL"], "Should have VIRTUAL_TERMINAL")
	assert.True(t, enumValues["PPCP_STANDARD"], "Should have PPCP_STANDARD")
}

func TestEnumCollisionPayPal(t *testing.T) {
	// Test with the actual PayPal spec to reproduce the integration test failure
	specContents, err := testDataFS.ReadFile("testdata/paypal-customer-partner-referrals.json")
	if err != nil {
		t.Skip("PayPal spec not available in testdata")
	}

	cfg := codegen.NewDefaultConfiguration()
	parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
	if len(errs) > 0 {
		t.Fatalf("Error parsing OpenAPI spec: %v", errs[0])
	}

	reg := NewTypeDefinitionRegistry(parseCtx, 10, nil)

	// Find the GET /v1/customer/partners/{partner_id}/merchant-integrations/{merchant_id} operation
	// which returns merchant_integration that has products array with product objects
	op := reg.FindOperation("/v1/customer/partners/{partner_id}/merchant-integrations/{merchant_id}", "GET")
	assert.NotNil(t, op)

	response := op.Response.GetResponse(200)
	assert.NotNil(t, response)
	assert.NotNil(t, response.Content)

	// Navigate to products array
	productsSchema := response.Content.Properties["products"]
	if productsSchema == nil {
		t.Skip("products property not found")
	}

	assert.Equal(t, "array", productsSchema.Type)
	assert.NotNil(t, productsSchema.Items)

	// The items should be the product object
	productSchema := productsSchema.Items

	// The name property should have the FULL 25 enum values, not just 6
	nameSchema := productSchema.Properties["name"]
	if nameSchema == nil {
		t.Skip("name property not found in product")
	}

	// The inline enum should have 25 values, not 6
	assert.GreaterOrEqual(t, len(nameSchema.Enum), 25,
		"product.name should have all 25 enum values, not just the 6 from product_name schema")
}

func TestNullableEnumValues(t *testing.T) {
	// Test that nullable enums with null as a valid value are parsed correctly.
	// The "null" string value from YAML is preserved in the schema - filtering
	// happens at generation time in the replacer package.
	specYAML := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      operationId: TestOp
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/TestObject'
components:
  schemas:
    TestObject:
      type: object
      properties:
        status:
          $ref: '#/components/schemas/Status'
    Status:
      type: string
      nullable: true
      enum:
        - ACTIVE
        - INACTIVE
        - null
`

	cfg := codegen.NewDefaultConfiguration()
	parseCtx, errs := codegen.CreateParseContext([]byte(specYAML), cfg)
	if len(errs) > 0 {
		t.Fatalf("Failed to parse spec: %v", errs[0])
	}

	registry := NewTypeDefinitionRegistry(parseCtx, 10, []byte(specYAML))
	ops := registry.Operations()
	assert.Len(t, ops, 1)

	op := ops[0]
	response := op.Response.GetResponse(200)
	assert.NotNil(t, response)

	statusSchema := response.Content.Properties["status"]
	assert.NotNil(t, statusSchema)

	// The schema should be marked as nullable
	assert.True(t, statusSchema.Nullable)

	// The enum values include "null" as a string (YAML parses it this way)
	// The replacer package filters this out at generation time
	assert.Len(t, statusSchema.Enum, 3)
	assert.Contains(t, statusSchema.Enum, "ACTIVE")
	assert.Contains(t, statusSchema.Enum, "INACTIVE")
	assert.Contains(t, statusSchema.Enum, "null") // string "null", not nil
}

func TestMergeRequired(t *testing.T) {
	t.Run("empty additional returns base", func(t *testing.T) {
		base := []string{"a", "b"}
		result := mergeRequired(base, nil)
		assert.Equal(t, base, result)
	})

	t.Run("empty base returns additional", func(t *testing.T) {
		additional := []string{"c", "d"}
		result := mergeRequired(nil, additional)
		assert.Equal(t, additional, result)
	})

	t.Run("merges without duplicates", func(t *testing.T) {
		base := []string{"a", "b"}
		additional := []string{"b", "c"}
		result := mergeRequired(base, additional)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("handles duplicates in base", func(t *testing.T) {
		base := []string{"a", "a", "b"}
		additional := []string{"c"}
		result := mergeRequired(base, additional)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("handles duplicates in additional", func(t *testing.T) {
		base := []string{"a"}
		additional := []string{"b", "b", "c"}
		result := mergeRequired(base, additional)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("both empty returns empty", func(t *testing.T) {
		result := mergeRequired(nil, nil)
		assert.Nil(t, result)
	})
}

func TestFindUnionSchema(t *testing.T) {
	t.Run("returns nil for empty refType", func(t *testing.T) {
		result := findUnionSchema("", nil)
		assert.Nil(t, result)
	})

	t.Run("returns nil for non-existent type", func(t *testing.T) {
		tdLookUp := make(map[string]*codegen.TypeDefinition)
		result := findUnionSchema("NonExistent", tdLookUp)
		assert.Nil(t, result)
	})

	t.Run("returns schema with union elements", func(t *testing.T) {
		tdLookUp := map[string]*codegen.TypeDefinition{
			"UnionType": {
				Schema: codegen.GoSchema{
					UnionElements: []codegen.UnionElement{
						{TypeName: "string", Schema: codegen.GoSchema{GoType: "string"}},
						{TypeName: "int", Schema: codegen.GoSchema{GoType: "int"}},
					},
				},
			},
		}
		result := findUnionSchema("UnionType", tdLookUp)
		assert.NotNil(t, result)
		assert.Len(t, result.UnionElements, 2)
	})

	t.Run("returns schema with IsUnionWrapper", func(t *testing.T) {
		tdLookUp := map[string]*codegen.TypeDefinition{
			"WrapperType": {
				Schema: codegen.GoSchema{
					IsUnionWrapper: true,
				},
			},
		}
		result := findUnionSchema("WrapperType", tdLookUp)
		assert.NotNil(t, result)
		assert.True(t, result.IsUnionWrapper)
	})

	t.Run("follows reference chain to find union", func(t *testing.T) {
		tdLookUp := map[string]*codegen.TypeDefinition{
			"WrapperType": {
				Schema: codegen.GoSchema{
					Properties: []codegen.Property{
						{
							JsonFieldName: "", // embedded property
							Schema: codegen.GoSchema{
								RefType: "ActualUnion",
							},
						},
					},
				},
			},
			"ActualUnion": {
				Schema: codegen.GoSchema{
					UnionElements: []codegen.UnionElement{
						{TypeName: "string", Schema: codegen.GoSchema{GoType: "string"}},
					},
				},
			},
		}
		result := findUnionSchema("WrapperType", tdLookUp)
		assert.NotNil(t, result)
		assert.Len(t, result.UnionElements, 1)
	})

	t.Run("handles circular references", func(t *testing.T) {
		tdLookUp := map[string]*codegen.TypeDefinition{
			"TypeA": {
				Schema: codegen.GoSchema{
					Properties: []codegen.Property{
						{
							JsonFieldName: "",
							Schema: codegen.GoSchema{
								RefType: "TypeB",
							},
						},
					},
				},
			},
			"TypeB": {
				Schema: codegen.GoSchema{
					Properties: []codegen.Property{
						{
							JsonFieldName: "",
							Schema: codegen.GoSchema{
								RefType: "TypeA", // circular reference
							},
						},
					},
				},
			},
		}
		result := findUnionSchema("TypeA", tdLookUp)
		assert.Nil(t, result) // Should not infinite loop
	})

	t.Run("returns nil when no union found", func(t *testing.T) {
		tdLookUp := map[string]*codegen.TypeDefinition{
			"SimpleType": {
				Schema: codegen.GoSchema{
					GoType: "string",
				},
			},
		}
		result := findUnionSchema("SimpleType", tdLookUp)
		assert.Nil(t, result)
	})
}

func TestFindDiscriminatorValue(t *testing.T) {
	t.Run("returns empty for nil discriminator", func(t *testing.T) {
		result := findDiscriminatorValue(nil, "SomeType")
		assert.Equal(t, "", result)
	})

	t.Run("returns empty for nil mapping", func(t *testing.T) {
		discriminator := &codegen.Discriminator{
			Property: "type",
			Mapping:  nil,
		}
		result := findDiscriminatorValue(discriminator, "SomeType")
		assert.Equal(t, "", result)
	})

	t.Run("returns value when type found in mapping", func(t *testing.T) {
		discriminator := &codegen.Discriminator{
			Property: "type",
			Mapping: map[string]string{
				"dog": "Dog",
				"cat": "Cat",
			},
		}
		result := findDiscriminatorValue(discriminator, "Dog")
		assert.Equal(t, "dog", result)
	})

	t.Run("returns empty when type not found in mapping", func(t *testing.T) {
		discriminator := &codegen.Discriminator{
			Property: "type",
			Mapping: map[string]string{
				"dog": "Dog",
				"cat": "Cat",
			},
		}
		result := findDiscriminatorValue(discriminator, "Bird")
		assert.Equal(t, "", result)
	})
}

func TestExtractLeadingNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"101 (EastAsia)", "101"},
		{"0 (User)", "0"},
		{"-42 (Negative)", "-42"},
		{"3.14 (Pi)", "3.14"},
		{"-3.14 (NegPi)", "-3.14"},
		{"abc", ""},
		{"", ""},
		{"123", "123"},
		{"123abc456", "123"},
	}

	for _, tt := range tests {
		result := extractLeadingNumber(tt.input)
		assert.Equal(t, tt.expected, result, "input: %s", tt.input)
	}
}
