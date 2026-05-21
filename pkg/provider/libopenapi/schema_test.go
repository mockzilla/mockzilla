package libopenapi

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadResponseSchema parses an inline spec, looks up the operation
// returned by the registry, and yields the success response's content
// Schema. Used by every conversion table case below.
func loadResponseSchema(t *testing.T, specYAML string, path, method string) *schema.Schema {
	t.Helper()
	reg, err := NewRegistry([]byte(specYAML), Options{})
	require.NoError(t, err)
	op := reg.FindOperation(path, method)
	require.NotNil(t, op, "operation %s %s missing", method, path)
	require.NotNil(t, op.Response, "operation has no response")
	success := op.Response.GetSuccess()
	require.NotNil(t, success, "operation has no success response")
	return success.Content
}

func TestConvertSchema_PrimitiveString(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: string
                minLength: 3
                maxLength: 9
                pattern: "^[a-z]+$"
                format: uuid
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s)
	assert.Equal(t, types.TypeString, s.Type)
	require.NotNil(t, s.MinLength)
	assert.Equal(t, int64(3), *s.MinLength)
	require.NotNil(t, s.MaxLength)
	assert.Equal(t, int64(9), *s.MaxLength)
	assert.Equal(t, "^[a-z]+$", s.Pattern)
	assert.Equal(t, "uuid", s.Format)
}

func TestConvertSchema_ArrayWithItems(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                minItems: 1
                maxItems: 5
                items:
                  type: integer
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeArray, s.Type)
	require.NotNil(t, s.Items)
	assert.Equal(t, types.TypeInteger, s.Items.Type)
	require.NotNil(t, s.MinItems)
	assert.Equal(t, int64(1), *s.MinItems)
}

func TestConvertSchema_ObjectWithRequired(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: [name]
                properties:
                  name: {type: string}
                  age:  {type: integer}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeObject, s.Type)
	assert.Equal(t, []string{"name"}, s.Required)
	assert.Contains(t, s.Properties, "name")
	assert.Contains(t, s.Properties, "age")
}

func TestConvertSchema_AdditionalPropertiesFalse(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                additionalProperties: false
                properties:
                  k: {type: string}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.True(t, s.AdditionalPropertiesForbidden)
	assert.Nil(t, s.AdditionalProperties)
}

func TestConvertSchema_AdditionalPropertiesSchema(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                additionalProperties:
                  type: integer
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s.AdditionalProperties)
	assert.Equal(t, types.TypeInteger, s.AdditionalProperties.Type)
	assert.False(t, s.AdditionalPropertiesForbidden)
}

func TestConvertSchema_EnumWithStringTypeIntValues(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: string
                enum: [1, 2, 3]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.Len(t, s.Enum, 3)
	assert.Equal(t, int64(1), s.Enum[0])
}

func TestConvertSchema_ConstLowersToEnum(t *testing.T) {
	spec := `openapi: 3.1.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                const: alpha
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.Len(t, s.Enum, 1)
	assert.Equal(t, "alpha", s.Enum[0])
	assert.Equal(t, types.TypeString, s.Type)
}

func TestConvertSchema_NullTypeOnly(t *testing.T) {
	spec := `openapi: 3.1.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: "null"
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.True(t, s.IsNull, "type: null should set IsNull")
	assert.Equal(t, "", s.Type)
}

func TestConvertSchema_MultiTypeWithNull(t *testing.T) {
	spec := `openapi: 3.1.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: [string, "null"]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeString, s.Type)
	assert.False(t, s.IsNull, "first non-null type wins")
}

func TestConvertSchema_NullableTrue(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: string
                nullable: true
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.True(t, s.Nullable)
}

func TestConvertSchema_DefaultsAndExamples(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: integer
                default: 42
                example: 7
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.EqualValues(t, 42, s.Default)
	assert.EqualValues(t, 7, s.Example)
}

func TestConvertSchema_NullOnlyPropertyEmitsIsNull(t *testing.T) {
	spec := `openapi: 3.1.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: [trailer]
                properties:
                  trailer:
                    type: "null"
`
	s := loadResponseSchema(t, spec, "/", "GET")
	prop, ok := s.Properties["trailer"]
	require.True(t, ok, "null-only property should still be present")
	assert.True(t, prop.IsNull)
}

func TestConvertSchema_RecursiveSelfReference(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Node"
components:
  schemas:
    Node:
      type: object
      properties:
        name: {type: string}
        next: {$ref: "#/components/schemas/Node"}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeObject, s.Type)
	assert.Contains(t, s.Properties, "name")
	assert.Contains(t, s.Properties, "next")
}

func TestConvertSchema_LoadsRawDocument(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths: {}`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	provider, ok := reg.(*Registry)
	require.True(t, ok)
	doc, err := provider.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}
