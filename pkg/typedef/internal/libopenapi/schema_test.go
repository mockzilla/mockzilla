package libopenapi

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
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

func TestConvertSchema_NullableAllOfTypeConflictEmitsNull(t *testing.T) {
	// Outer says type:object, allOf branch dictates integer. Both are
	// enforced; no scalar fits, so nullable falls back to null even
	// though the inner enum offers concrete values.
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
                properties:
                  status:
                    allOf:
                      - $ref: "#/components/schemas/StatusEnum"
                    nullable: true
                    type: object
components:
  schemas:
    StatusEnum:
      type: integer
      enum: [0, 1, 2]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	prop, ok := s.Properties["status"]
	require.True(t, ok)
	assert.True(t, prop.IsNull, "allOf type conflict + nullable must fall back to null")
}

func TestConvertSchema_NullableOneOfTypeConflictKeepsEnumBranch(t *testing.T) {
	// Outer says type:integer but oneOf branches dictate string enums.
	// oneOf is exclusive: a string from the enum branch satisfies one
	// oneOf path. IsNull must stay false so the generator picks an
	// enum value.
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
                properties:
                  status:
                    type: integer
                    nullable: true
                    oneOf:
                      - $ref: "#/components/schemas/StatusEnum"
                      - $ref: "#/components/schemas/NullEnum"
components:
  schemas:
    StatusEnum:
      type: string
      enum: [pending, active]
    NullEnum:
      type: object
      enum: [null]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	prop, ok := s.Properties["status"]

	require.True(t, ok)
	assert.False(t, prop.IsNull, "oneOf with enum must let the generator pick a branch value")
	assert.NotEmpty(t, prop.Enum, "composed enum from picked oneOf branch must survive")
}

func TestConvertSchema_NullableOneOfTypeConflictNoEnumEmitsNull(t *testing.T) {
	// Outer is array, oneOf picks an object branch with no enum.
	// Generator has nothing concrete to pick from the union; nullable
	// falls back to null.
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
                properties:
                  files:
                    type: array
                    nullable: true
                    oneOf:
                      - type: object
                        properties:
                          id: {type: string}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	prop, ok := s.Properties["files"]
	require.True(t, ok)
	assert.True(t, prop.IsNull, "oneOf type conflict + nullable + no enum must fall back to null")
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
	doc, err := reg.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestConvertSchema_NilReturnsNil(t *testing.T) {
	assert.Nil(t, convertSchema(nil, newConvertCtx()))
}

func TestConvertProxy_NilProxyReturnsNil(t *testing.T) {
	assert.Nil(t, convertProxy(nil, newConvertCtx()))
}

func TestConvertProperties_NilProperties(t *testing.T) {
	props, nullOnly := convertProperties(nil, newConvertCtx())
	assert.Empty(t, props)
	assert.Empty(t, nullOnly)
}

func TestConvertProxy_PopulatesNameFromRef(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /things:
    get:
      responses:
        "200":
          description: ok
          content:
            application/xml:
              schema:
                $ref: "#/components/schemas/timetable"
components:
  schemas:
    timetable:
      type: object
      properties:
        s:
          type: array
          items:
            $ref: "#/components/schemas/stop"
        inline:
          type: object
          properties:
            x: {type: string}
    stop:
      type: object
      properties:
        eva: {type: integer}
`
	s := loadResponseSchema(t, spec, "/things", "GET")
	require.NotNil(t, s)
	assert.Equal(t, "timetable", s.Name)
	require.NotNil(t, s.Properties["s"])
	require.NotNil(t, s.Properties["s"].Items)
	assert.Equal(t, "stop", s.Properties["s"].Items.Name)
	require.NotNil(t, s.Properties["inline"])
	assert.Equal(t, "", s.Properties["inline"].Name, "inline schemas have no name")
}

func TestConvertProxy_CycleEmitsRecursiveMarker(t *testing.T) {
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
        next: {$ref: "#/components/schemas/Node"}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s)
	require.NotNil(t, s.Properties)
	require.Contains(t, s.Properties, "next")
	assert.True(t, s.Properties["next"].Recursive, "self-ref should be marked Recursive")
}

func TestInferType(t *testing.T) {
	assert.Equal(t, types.TypeArray, inferType(&schema.Schema{}, nil, nil, nil))
	assert.Equal(t, types.TypeObject, inferType(nil, map[string]*schema.Schema{"k": nil}, nil, nil))
	assert.Equal(t, types.TypeObject, inferType(nil, nil, &schema.Schema{}, nil))
	assert.Equal(t, types.TypeString, inferType(nil, nil, nil, []any{"x"}))
	assert.Equal(t, types.TypeString, inferType(nil, nil, nil, nil), "empty schema defaults to string so the generator has a concrete type")
}

func TestChooseType(t *testing.T) {
	typ, isNull := chooseType([]string{"null"})
	assert.Equal(t, "", typ)
	assert.True(t, isNull)

	typ, isNull = chooseType([]string{"string", "null"})
	assert.Equal(t, "string", typ)
	assert.False(t, isNull)

	typ, isNull = chooseType(nil)
	assert.Equal(t, "", typ)
	assert.False(t, isNull)
}

func TestFirstNonNullType(t *testing.T) {
	assert.Equal(t, "string", firstNonNullType([]string{"null", "string"}))
	assert.Equal(t, "", firstNonNullType([]string{"null"}))
	assert.Equal(t, "", firstNonNullType(nil))
}

func TestIsAllNullType(t *testing.T) {
	assert.False(t, isAllNullType(nil))
	assert.True(t, isAllNullType([]string{"null"}))
	assert.True(t, isAllNullType([]string{"NULL", "Null"}))
	assert.False(t, isAllNullType([]string{"null", "string"}))
}

func TestMergeStringLists(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, mergeStringLists([]string{"a"}, []string{"b"}))
	assert.Equal(t, []string{"a"}, mergeStringLists(nil, []string{"a"}))
	assert.Equal(t, []string{"a"}, mergeStringLists([]string{"a"}, nil))
	assert.Equal(t, []string{"a", "b"}, mergeStringLists([]string{"a"}, []string{"a", "b"}))
}

func TestAppendUnique(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, appendUnique([]string{"a"}, "b"))
	assert.Equal(t, []string{"a"}, appendUnique([]string{"a"}, "a"))
}

func TestShortName(t *testing.T) {
	assert.Equal(t, "Foo", shortName("#/components/schemas/Foo"))
	assert.Equal(t, "bare", shortName("bare"))
	assert.Equal(t, "", shortName(""))
}

// specWithRefSiblings declares one property as a bare $ref and one as a $ref
// carrying sibling keys, which OpenAPI 3.1 permits as annotations.
const specWithRefSiblings = `openapi: 3.1.0
info: {title: t, version: 1}
paths:
  /thing:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Wrapper'
components:
  schemas:
    Amount:
      type: object
      properties:
        currency: {type: string}
        value: {type: integer}
      required: [currency, value]
    Total:
      type: object
      properties:
        currency: {type: string}
        value: {type: integer}
      required: [currency, value]
    Wrapper:
      type: object
      properties:
        bare:
          $ref: '#/components/schemas/Amount'
        annotated:
          $ref: '#/components/schemas/Total'
          description: carries a sibling key
`

// A $ref with sibling keys resolves to the schema it names, not to the siblings.
// libopenapi hands back only the sibling node, which would otherwise convert to
// a typeless schema and render as a bare string.
func TestConvertProxy_RefWithSiblingsResolvesTarget(t *testing.T) {
	reg, err := NewRegistry([]byte(specWithRefSiblings), Options{})
	require.NoError(t, err)

	op := reg.FindOperation("/thing", "GET")
	require.NotNil(t, op)
	content := op.Response.GetSuccess().Content
	require.NotNil(t, content)

	// Each property names a different target: a bare ref converted first would
	// otherwise populate the cache under that ref and hide the annotated one.
	for name, want := range map[string]string{"bare": "Amount", "annotated": "Total"} {
		t.Run(name, func(t *testing.T) {
			prop, ok := content.Properties[name]
			require.True(t, ok)
			assert.Equal(t, "object", prop.Type)
			assert.Len(t, prop.Properties, 2)
			assert.Equal(t, want, prop.Name)
			assert.ElementsMatch(t, []string{"currency", "value"}, prop.Required)
		})
	}
}

func TestResolveTarget(t *testing.T) {
	reg, err := NewRegistry([]byte(specWithRefSiblings), Options{})
	require.NoError(t, err)

	components := reg.componentSchemas()
	require.NotNil(t, components)

	wrapper, ok := components.Get("Wrapper")
	require.True(t, ok)
	annotated, ok := wrapper.Schema().Properties.Get("annotated")
	require.True(t, ok)

	t.Run("resolves a component reference", func(t *testing.T) {
		ctx := newConvertCtx()
		ctx.components = components

		target := ctx.resolveTarget(annotated)
		require.NotNil(t, target)
		assert.Equal(t, 2, target.Properties.Len())
	})

	t.Run("nil without components to resolve against", func(t *testing.T) {
		assert.Nil(t, newConvertCtx().resolveTarget(annotated))
	})

	t.Run("nil for an inline schema", func(t *testing.T) {
		ctx := newConvertCtx()
		ctx.components = components

		amount, ok := components.Get("Amount")
		require.True(t, ok)
		inline, ok := amount.Schema().Properties.Get("currency")
		require.True(t, ok)
		assert.Nil(t, ctx.resolveTarget(inline))
	})

	t.Run("nil for a reference this document does not hold", func(t *testing.T) {
		ctx := newConvertCtx()
		ctx.components = components
		assert.Nil(t, ctx.resolveTarget(base.CreateSchemaProxyRef("#/components/schemas/Missing")))
		assert.Nil(t, ctx.resolveTarget(base.CreateSchemaProxyRef("https://example.test/other.yml#/Thing")))
	})

	t.Run("nil proxy", func(t *testing.T) {
		ctx := newConvertCtx()
		ctx.components = components
		assert.Nil(t, ctx.resolveTarget(nil))
	})
}
