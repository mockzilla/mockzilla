package libopenapi

import (
	"os"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	libopenapilib "github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

func TestBuildModel_RemoveOptionalUnions(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths: {}
components:
  schemas:
    User:
      type: object
      required:
        - name
      properties:
        name:
          type: string
        optionalUnion:
          anyOf:
            - type: string
            - type: integer
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	userSchema := model.Components.Schemas.GetOrZero("User").Schema()
	assert.NotNil(t, userSchema)

	// optionalUnion should be removed
	assert.Nil(t, userSchema.Properties.GetOrZero("optionalUnion"))
	// name should still exist
	assert.NotNil(t, userSchema.Properties.GetOrZero("name"))
}

func TestBuildModel_KeepFirstVariantForRequired(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths: {}
components:
  schemas:
    User:
      type: object
      required:
        - name
        - id
      properties:
        name:
          type: string
        id:
          anyOf:
            - type: string
            - type: integer
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	userSchema := model.Components.Schemas.GetOrZero("User").Schema()
	assert.NotNil(t, userSchema)

	// id should still exist (required)
	idProp := userSchema.Properties.GetOrZero("id")
	assert.NotNil(t, idProp)

	idSchema := idProp.Schema()
	assert.NotNil(t, idSchema)

	// anyOf is removed and first variant (string) is merged
	assert.Len(t, idSchema.AnyOf, 0)
	assert.Contains(t, idSchema.Type, "string")
}

func TestBuildModel_OneOf(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths: {}
components:
  schemas:
    Response:
      type: object
      required:
        - data
      properties:
        data:
          oneOf:
            - type: object
              properties:
                name:
                  type: string
            - type: array
              items:
                type: string
        optionalData:
          oneOf:
            - type: string
            - type: number
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	responseSchema := model.Components.Schemas.GetOrZero("Response").Schema()
	assert.NotNil(t, responseSchema)

	// data (required) should exist with first variant merged in
	dataProp := responseSchema.Properties.GetOrZero("data")
	assert.NotNil(t, dataProp)

	dataSchema := dataProp.Schema()
	assert.NotNil(t, dataSchema)
	// oneOf is removed and first variant is merged
	assert.Len(t, dataSchema.OneOf, 0)
	assert.Contains(t, dataSchema.Type, "object")
	assert.NotNil(t, dataSchema.Properties.GetOrZero("name"))

	// optionalData should be removed (optional union)
	assert.Nil(t, responseSchema.Properties.GetOrZero("optionalData"))
}

func TestBuildModel_NestedUnions(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths: {}
components:
  schemas:
    NestedUnion:
      type: object
      required:
        - data
      properties:
        data:
          anyOf:
            - anyOf:
                - type: string
                - type: number
            - type: boolean
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	assert.NoError(t, err)

	schema := model.Components.Schemas.GetOrZero("NestedUnion").Schema()
	assert.NotNil(t, schema)

	dataProp := schema.Properties.GetOrZero("data")
	assert.NotNil(t, dataProp)

	dataSchema := dataProp.Schema()
	assert.NotNil(t, dataSchema)

	// anyOf is removed and first variant (which has nested anyOf) is merged
	// The nested anyOf is also removed and its first variant (string) is merged
	assert.Len(t, dataSchema.AnyOf, 0)
	assert.Contains(t, dataSchema.Type, "string")
}

func TestBuildModel_MultipleAnyOf(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths: {}
components:
  schemas:
    MultiUnion:
      type: object
      required:
        - field
      properties:
        field:
          anyOf:
            - type: string
            - type: integer
          oneOf:
            - type: boolean
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	assert.NoError(t, err)

	schema := model.Components.Schemas.GetOrZero("MultiUnion").Schema()
	assert.NotNil(t, schema)

	fieldProp := schema.Properties.GetOrZero("field")
	assert.NotNil(t, fieldProp)

	fieldSchema := fieldProp.Schema()
	assert.NotNil(t, fieldSchema)

	// Both anyOf and oneOf are removed, first anyOf variant (string) is merged
	assert.Empty(t, fieldSchema.AnyOf)
	assert.Empty(t, fieldSchema.OneOf)
	assert.Contains(t, fieldSchema.Type, "string")
}

func TestBuildModel_ComplexTestData(t *testing.T) {
	// Load complex test data
	data, err := os.ReadFile("../../testdata/simplify-unions-complex.yml")
	if !assert.NoError(t, err) {
		t.Skip("Test data file not found")
	}

	doc, err := libopenapilib.NewDocument(data)
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	assert.NoError(t, err)

	t.Run("UserProfile - optional union removed", func(t *testing.T) {
		schema := model.Components.Schemas.GetOrZero("UserProfile").Schema()
		assert.NotNil(t, schema)

		// Required fields should exist
		assert.NotNil(t, schema.Properties.GetOrZero("userId"))
		assert.NotNil(t, schema.Properties.GetOrZero("username"))

		// Optional union should be removed
		assert.Nil(t, schema.Properties.GetOrZero("optionalMetadata"))
	})

	t.Run("PaymentMethod - required union simplified", func(t *testing.T) {
		schema := model.Components.Schemas.GetOrZero("PaymentMethod").Schema()
		assert.NotNil(t, schema)

		typeProp := schema.Properties.GetOrZero("type")
		assert.NotNil(t, typeProp)

		typeSchema := typeProp.Schema()
		assert.NotNil(t, typeSchema)

		// oneOf is removed and first variant merged
		assert.Empty(t, typeSchema.OneOf)
		assert.Contains(t, typeSchema.Type, "string")
		assert.NotEmpty(t, typeSchema.Enum)
	})

	t.Run("NestedUnionData - nested unions simplified", func(t *testing.T) {
		schema := model.Components.Schemas.GetOrZero("NestedUnionData").Schema()
		assert.NotNil(t, schema)

		dataProp := schema.Properties.GetOrZero("data")
		assert.NotNil(t, dataProp)

		dataSchema := dataProp.Schema()
		assert.NotNil(t, dataSchema)

		// anyOf is removed and first variant (which has nested anyOf) is merged
		// The nested anyOf is also removed and its first variant (string) is merged
		assert.Empty(t, dataSchema.AnyOf)
		assert.Contains(t, dataSchema.Type, "string")
	})

	t.Run("MixedUnions - required kept, optional removed", func(t *testing.T) {
		schema := model.Components.Schemas.GetOrZero("MixedUnions").Schema()
		assert.NotNil(t, schema)

		// Required union should exist with first variant merged
		requiredProp := schema.Properties.GetOrZero("requiredUnion")
		assert.NotNil(t, requiredProp)

		requiredSchema := requiredProp.Schema()
		assert.NotNil(t, requiredSchema)
		assert.Empty(t, requiredSchema.AnyOf)
		assert.Contains(t, requiredSchema.Type, "string")

		// Optional union should be removed
		assert.Nil(t, schema.Properties.GetOrZero("optionalUnion"))
	})

	t.Run("ResponseData - refs handled correctly", func(t *testing.T) {
		schema := model.Components.Schemas.GetOrZero("ResponseData").Schema()
		assert.NotNil(t, schema)

		// Required union with refs should be simplified
		resultProp := schema.Properties.GetOrZero("result")
		assert.NotNil(t, resultProp)

		// Optional union should be removed
		assert.Nil(t, schema.Properties.GetOrZero("optionalError"))
	})

	t.Run("MultipleUnionTypes - only required field kept", func(t *testing.T) {
		schema := model.Components.Schemas.GetOrZero("MultipleUnionTypes").Schema()
		assert.NotNil(t, schema)

		// field1 is required, should be simplified
		field1 := schema.Properties.GetOrZero("field1")
		assert.NotNil(t, field1)

		// field2 and field3 are optional, should be removed
		assert.Nil(t, schema.Properties.GetOrZero("field2"))
		assert.Nil(t, schema.Properties.GetOrZero("field3"))
	})
}

func TestBuildModel_WithOptionalConfig(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ManyRequired'
  /test2:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/FewRequired'
components:
  schemas:
    ManyRequired:
      type: object
      required:
        - field1
        - field2
        - field3
        - field4
        - field5
      properties:
        field1:
          type: string
        field2:
          type: string
        field3:
          type: string
        field4:
          type: string
        field5:
          type: string
        optional1:
          type: string
        optional2:
          type: string
        optional3:
          type: string
    FewRequired:
      type: object
      required:
        - field1
      properties:
        field1:
          type: string
        optional1:
          type: string
        optional2:
          type: string
        optional3:
          type: string
        optional4:
          type: string
        optional5:
          type: string
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	opts := &config.OptionalProperties{Min: 1, Max: 2}

	model, err := BuildModel(doc, true, opts)
	assert.NoError(t, err)

	// ManyRequired has 5 required properties + 5 optional, should keep 1-2 optional
	manyReqSchema := model.Components.Schemas.GetOrZero("ManyRequired").Schema()
	assert.NotNil(t, manyReqSchema)
	totalProps := manyReqSchema.Properties.Len()
	assert.GreaterOrEqual(t, totalProps, 6, "ManyRequired should have at least 6 properties (5 required + 1 optional)")
	assert.LessOrEqual(t, totalProps, 7, "ManyRequired should have at most 7 properties (5 required + 2 optional)")
	t.Logf("ManyRequired has %d total properties", totalProps)

	// FewRequired has 1 required property + 5 optional, should keep 1-2 optional
	fewReqSchema := model.Components.Schemas.GetOrZero("FewRequired").Schema()
	assert.NotNil(t, fewReqSchema)
	totalProps = fewReqSchema.Properties.Len()
	assert.GreaterOrEqual(t, totalProps, 2, "FewRequired should have at least 2 properties (1 required + 1 optional)")
	assert.LessOrEqual(t, totalProps, 3, "FewRequired should have at most 3 properties (1 required + 2 optional)")
	t.Logf("FewRequired has %d total properties", totalProps)
}

func TestBuildModel_CircularReference(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /navigation:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/NavElement'
components:
  schemas:
    NavElement:
      type: object
      properties:
        id:
          type: string
        label:
          type: string
        children:
          type: array
          items:
            $ref: '#/components/schemas/NavElement'
`

	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	// This should not cause stack overflow
	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, model)

	// Verify the schema still exists and has the circular reference
	navSchema := model.Components.Schemas.GetOrZero("NavElement").Schema()
	assert.NotNil(t, navSchema)
	assert.NotNil(t, navSchema.Properties.GetOrZero("id"))
	assert.NotNil(t, navSchema.Properties.GetOrZero("label"))
	assert.NotNil(t, navSchema.Properties.GetOrZero("children"))
}

func TestBuildModel_NavigationServiceSpec(t *testing.T) {
	// Test with the actual navigation service spec that caused the stack overflow
	data, err := os.ReadFile("../../resources/data/services/navigationservice_e_spirit_cloud/setup/openapi.yml")
	if err != nil {
		t.Skip("Navigation service spec not found")
	}

	doc, err := libopenapilib.NewDocument(data)
	assert.NoError(t, err)

	// This should not cause stack overflow
	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, model)

	// Verify key schemas exist
	navElementV1Request := model.Components.Schemas.GetOrZero("NavElementV1Request").Schema()
	assert.NotNil(t, navElementV1Request)

	navElementV1Response := model.Components.Schemas.GetOrZero("NavElementV1Response").Schema()
	assert.NotNil(t, navElementV1Response)

	// Both should have children property with circular reference
	assert.NotNil(t, navElementV1Request.Properties.GetOrZero("children"))
	assert.NotNil(t, navElementV1Response.Properties.GetOrZero("children"))

	// Verify NavigationFound response union is simplified
	navigationFoundResp := model.Components.Responses.GetOrZero("NavigationFound")
	assert.NotNil(t, navigationFoundResp)

	jsonContent := navigationFoundResp.Content.GetOrZero("application/json")
	assert.NotNil(t, jsonContent)

	schema := jsonContent.Schema.Schema()
	assert.NotNil(t, schema)

	// The oneOf should be simplified to a single element
	assert.Len(t, schema.OneOf, 1, "NavigationFound response oneOf should be simplified to single element")
	assert.Empty(t, schema.AnyOf, "NavigationFound response should not have anyOf")
}

func TestMergeSchemaProperties(t *testing.T) {
	t.Run("nil source does nothing", func(t *testing.T) {
		dst := &base.Schema{
			Type: []string{"object"},
		}
		mergeSchemaProperties(dst, nil)
		assert.Equal(t, []string{"object"}, dst.Type)
	})

	t.Run("merges type when dst is empty", func(t *testing.T) {
		dst := &base.Schema{}
		src := &base.Schema{Type: []string{"string"}}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, []string{"string"}, dst.Type)
	})

	t.Run("does not override existing type", func(t *testing.T) {
		dst := &base.Schema{Type: []string{"object"}}
		src := &base.Schema{Type: []string{"string"}}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, []string{"object"}, dst.Type)
	})

	t.Run("merges format when dst is empty", func(t *testing.T) {
		dst := &base.Schema{}
		src := &base.Schema{Format: "date-time"}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, "date-time", dst.Format)
	})

	t.Run("does not override existing format", func(t *testing.T) {
		dst := &base.Schema{Format: "date"}
		src := &base.Schema{Format: "date-time"}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, "date", dst.Format)
	})

	t.Run("merges enum when dst is empty", func(t *testing.T) {
		dst := &base.Schema{}
		src := &base.Schema{Enum: []*yaml.Node{{Value: "a"}, {Value: "b"}}}
		mergeSchemaProperties(dst, src)
		assert.Len(t, dst.Enum, 2)
	})

	t.Run("does not override existing enum", func(t *testing.T) {
		dst := &base.Schema{Enum: []*yaml.Node{{Value: "x"}}}
		src := &base.Schema{Enum: []*yaml.Node{{Value: "a"}, {Value: "b"}}}
		mergeSchemaProperties(dst, src)
		assert.Len(t, dst.Enum, 1)
	})

	t.Run("merges required without duplicates", func(t *testing.T) {
		dst := &base.Schema{Required: []string{"a", "b"}}
		src := &base.Schema{Required: []string{"b", "c"}}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, []string{"a", "b", "c"}, dst.Required)
	})

	t.Run("merges items when dst is nil", func(t *testing.T) {
		srcItems := &base.DynamicValue[*base.SchemaProxy, bool]{}
		dst := &base.Schema{}
		src := &base.Schema{Items: srcItems}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, srcItems, dst.Items)
	})

	t.Run("does not override existing items", func(t *testing.T) {
		dstItems := &base.DynamicValue[*base.SchemaProxy, bool]{}
		srcItems := &base.DynamicValue[*base.SchemaProxy, bool]{}
		dst := &base.Schema{Items: dstItems}
		src := &base.Schema{Items: srcItems}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, dstItems, dst.Items)
	})

	t.Run("merges additionalProperties when dst is nil", func(t *testing.T) {
		srcAdditional := &base.DynamicValue[*base.SchemaProxy, bool]{}
		dst := &base.Schema{}
		src := &base.Schema{AdditionalProperties: srcAdditional}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, srcAdditional, dst.AdditionalProperties)
	})

	t.Run("does not override existing additionalProperties", func(t *testing.T) {
		dstAdditional := &base.DynamicValue[*base.SchemaProxy, bool]{}
		srcAdditional := &base.DynamicValue[*base.SchemaProxy, bool]{}
		dst := &base.Schema{AdditionalProperties: dstAdditional}
		src := &base.Schema{AdditionalProperties: srcAdditional}
		mergeSchemaProperties(dst, src)
		assert.Equal(t, dstAdditional, dst.AdditionalProperties)
	})
}

func TestBuildModel_PathOperations(t *testing.T) {
	spec, err := os.ReadFile("../../testdata/path-operations-unions.yml")
	assert.NoError(t, err)

	doc, err := libopenapilib.NewDocument(spec)
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, model)

	// Verify the path exists
	pathItem := model.Paths.PathItems.GetOrZero("/items/{id}")
	assert.NotNil(t, pathItem)

	// Verify GET operation parameter schema is simplified
	getOp := pathItem.Get
	assert.NotNil(t, getOp)
	assert.Len(t, getOp.Parameters, 1)
	paramSchema := getOp.Parameters[0].Schema.Schema()
	assert.NotNil(t, paramSchema)
	// oneOf should be removed and first variant merged
	assert.Empty(t, paramSchema.OneOf)
	// First variant was string type
	assert.Contains(t, paramSchema.Type, "string")

	// Verify GET response schema is simplified
	resp200 := getOp.Responses.Codes.GetOrZero("200")
	assert.NotNil(t, resp200)
	respSchema := resp200.Content.GetOrZero("application/json").Schema.Schema()
	assert.NotNil(t, respSchema)
	// oneOf should be removed and first variant merged
	assert.Empty(t, respSchema.OneOf)
	// First variant was object with name property
	assert.Contains(t, respSchema.Type, "object")
	assert.NotNil(t, respSchema.Properties.GetOrZero("name"))

	// Verify POST request body schema is simplified
	postOp := pathItem.Post
	assert.NotNil(t, postOp)
	reqBodySchema := postOp.RequestBody.Content.GetOrZero("application/json").Schema.Schema()
	assert.NotNil(t, reqBodySchema)
	// anyOf should be removed and first variant merged
	assert.Empty(t, reqBodySchema.AnyOf)
	// First variant was object with title property
	assert.Contains(t, reqBodySchema.Type, "object")
	assert.NotNil(t, reqBodySchema.Properties.GetOrZero("title"))
}

func TestBuildModel_ArrayItems(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths: {}
components:
  schemas:
    ItemList:
      type: array
      items:
        oneOf:
          - type: string
          - type: integer
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	assert.NoError(t, err)

	itemListSchema := model.Components.Schemas.GetOrZero("ItemList").Schema()
	assert.NotNil(t, itemListSchema)
	assert.Equal(t, []string{"array"}, itemListSchema.Type)

	// Items should have oneOf simplified
	itemsSchema := itemListSchema.Items.A.Schema()
	assert.NotNil(t, itemsSchema)
	assert.Empty(t, itemsSchema.OneOf)
	assert.Contains(t, itemsSchema.Type, "string")
}

func TestBuildModel_AdditionalProperties(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths: {}
components:
  schemas:
    DynamicMap:
      type: object
      additionalProperties:
        anyOf:
          - type: string
          - type: number
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	assert.NoError(t, err)

	mapSchema := model.Components.Schemas.GetOrZero("DynamicMap").Schema()
	assert.NotNil(t, mapSchema)
	assert.Equal(t, []string{"object"}, mapSchema.Type)

	// AdditionalProperties should have anyOf simplified
	addlPropsSchema := mapSchema.AdditionalProperties.A.Schema()
	assert.NotNil(t, addlPropsSchema)
	assert.Empty(t, addlPropsSchema.AnyOf)
	assert.Contains(t, addlPropsSchema.Type, "string")
}

func TestBuildModel_Error(t *testing.T) {
	// Test with invalid document
	invalidSpec := []byte(`invalid yaml: [`)
	_, err := libopenapilib.NewDocument(invalidSpec)
	assert.Error(t, err)
}

func TestBuildModel_ComponentResponses(t *testing.T) {
	spec := `
openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /test:
    get:
      responses:
        '200':
          $ref: '#/components/responses/TestResponse'
components:
  responses:
    TestResponse:
      description: Success
      content:
        application/json:
          schema:
            type: object
            anyOf:
              - type: object
                properties:
                  name:
                    type: string
              - type: object
                properties:
                  age:
                    type: integer
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	if !assert.NoError(t, err) {
		return
	}

	// Build with simplify enabled
	model, err := BuildModel(doc, true, nil)
	if !assert.NoError(t, err) {
		return
	}

	// Check that component response schema was simplified
	resp := model.Components.Responses.GetOrZero("TestResponse")
	if !assert.NotNil(t, resp) {
		return
	}
	mediaType := resp.Content.GetOrZero("application/json")
	if !assert.NotNil(t, mediaType) {
		return
	}
	schema := mediaType.Schema.Schema()
	if !assert.NotNil(t, schema) {
		return
	}

	// Union should be simplified - anyOf should be empty
	assert.Empty(t, schema.AnyOf)
}

func TestBuildModel_WithoutSimplify(t *testing.T) {
	spec := `
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
        optionalUnion:
          anyOf:
            - type: string
            - type: integer
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	// Build without simplification
	model, err := BuildModel(doc, false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, model)

	// Union should still be present
	userSchema := model.Components.Schemas.GetOrZero("User").Schema()
	assert.NotNil(t, userSchema)
	optUnion := userSchema.Properties.GetOrZero("optionalUnion").Schema()
	assert.NotNil(t, optUnion)
	assert.Len(t, optUnion.AnyOf, 2)
}

func TestBuildModel_NilOptionalConfig_KeepsAllOptional(t *testing.T) {
	spec := `
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
        - name
      properties:
        name:
          type: string
        optional1:
          type: string
        optional2:
          type: string
        optional3:
          type: string
        optional4:
          type: string
        optional5:
          type: string
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	// nil config means keep all optional properties
	model, err := BuildModel(doc, true, nil)
	assert.NoError(t, err)

	userSchema := model.Components.Schemas.GetOrZero("User").Schema()
	assert.NotNil(t, userSchema)
	assert.Equal(t, 6, userSchema.Properties.Len(), "All 6 properties (1 required + 5 optional) should be kept")
}

func TestBuildModel_ZeroOptionalConfig_RemovesAllOptional(t *testing.T) {
	spec := `
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
        - name
      properties:
        name:
          type: string
        optional1:
          type: string
        optional2:
          type: string
        optional3:
          type: string
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	// {0, 0} removes all optional properties
	opts := &config.OptionalProperties{Min: 0, Max: 0}
	model, err := BuildModel(doc, true, opts)
	assert.NoError(t, err)

	userSchema := model.Components.Schemas.GetOrZero("User").Schema()
	assert.NotNil(t, userSchema)
	assert.Equal(t, 1, userSchema.Properties.Len(), "Only required property should remain")
	assert.NotNil(t, userSchema.Properties.GetOrZero("name"))
}

func TestBuildModel_WithOptionalPropertiesRandomized(t *testing.T) {
	spec := `
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
        - name
      properties:
        name:
          type: string
        optional1:
          type: string
        optional2:
          type: string
`
	doc, err := libopenapilib.NewDocument([]byte(spec))
	assert.NoError(t, err)

	model, err := BuildModel(doc, true, &config.OptionalProperties{Min: 0, Max: 1})
	assert.NoError(t, err)
	assert.NotNil(t, model)
}
