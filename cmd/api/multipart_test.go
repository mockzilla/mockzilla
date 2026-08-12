package api

import (
	"testing"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInlineMultipartBodies(t *testing.T) {
	for _, tc := range []struct {
		name        string
		contentType string
		body        string
		want        []string
		wantSame    bool
	}{
		{
			name:     "inline properties are left alone",
			body:     "type: object\n              properties:\n                kind: {type: string}",
			wantSame: true,
		},
		{
			name:        "json body is left alone",
			contentType: "application/json",
			body:        "$ref: '#/components/schemas/uploader'",
			wantSame:    true,
		},
		{
			name: "referenced schema is inlined",
			body: "$ref: '#/components/schemas/uploader'",
			want: []string{"kind", "file"},
		},
		{
			name: "allOf members are merged",
			body: "$ref: '#/components/schemas/composed'",
			want: []string{"kind", "file", "extra"},
		},
		{
			name: "allOf wrapping a reference is merged",
			body: "allOf:\n                - $ref: '#/components/schemas/uploader'",
			want: []string{"kind", "file"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentType := tc.contentType
			if contentType == "" {
				contentType = "multipart/form-data"
			}
			spec := []byte(uploadSpec(contentType, tc.body))

			got, err := inlineMultipartBodies(spec)
			require.NoError(t, err)

			if tc.wantSame {
				assert.Equal(t, spec, got, "spec should reach the generator unchanged")
				return
			}

			schema := multipartSchema(t, got)
			require.NotNil(t, schema)
			assert.False(t, schema.IsReference(), "body should no longer be a reference")

			var names []string
			for name := range schema.Schema().Properties.FromOldest() {
				names = append(names, name)
			}
			assert.ElementsMatch(t, tc.want, names)
		})
	}
}

func TestInlineMultipartBodiesKeepsRequired(t *testing.T) {
	spec := []byte(uploadSpec("multipart/form-data", "$ref: '#/components/schemas/composed'"))

	got, err := inlineMultipartBodies(spec)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"file", "kind"}, multipartSchema(t, got).Schema().Required)
}

func TestInlineMultipartBodiesRejectsUnparseableSpec(t *testing.T) {
	_, err := inlineMultipartBodies([]byte("not: [an, openapi, document"))
	assert.Error(t, err)
}

func TestAllPathItems(t *testing.T) {
	assert.Nil(t, allPathItems(&v3high.Document{}))

	model := buildModel(t, []byte(uploadSpec("multipart/form-data", "type: object")))
	assert.Len(t, allPathItems(model), 1)
}

func TestInlineOperationBody(t *testing.T) {
	assert.False(t, inlineOperationBody(nil))
	assert.False(t, inlineOperationBody(&v3high.Operation{}))
	assert.False(t, inlineOperationBody(&v3high.Operation{RequestBody: &v3high.RequestBody{}}))

	inline := uploadOperation(t, "multipart/form-data", "type: object\n              properties:\n                kind: {type: string}")
	assert.False(t, inlineOperationBody(inline), "inline properties need no rewrite")

	referenced := uploadOperation(t, "multipart/form-data", "$ref: '#/components/schemas/uploader'")
	assert.True(t, inlineOperationBody(referenced))
}

func TestInlineSchema(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		props []string
	}{
		{
			name: "inline object needs no stand-in",
			body: "type: object\n              properties:\n                kind: {type: string}",
		},
		{
			name: "reference to a schema without properties needs no stand-in",
			body: "$ref: '#/components/schemas/empty'",
		},
		{
			name:  "reference is flattened",
			body:  "$ref: '#/components/schemas/uploader'",
			props: []string{"kind", "file"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := uploadOperation(t, "multipart/form-data", tc.body)
			got := inlineSchema(op.RequestBody.Content.GetOrZero("multipart/form-data").Schema)

			if tc.props == nil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			var names []string
			for name := range got.Schema().Properties.FromOldest() {
				names = append(names, name)
			}
			assert.ElementsMatch(t, tc.props, names)
		})
	}
}

func TestCollectProperties(t *testing.T) {
	t.Run("merges nested allOf and dedupes required", func(t *testing.T) {
		model := buildModel(t, []byte(uploadSpec("multipart/form-data", "type: object")))
		schema := model.Components.Schemas.GetOrZero("nested").Schema()

		props := orderedmap.New[string, *base.SchemaProxy]()
		var required []string
		collectProperties(schema, props, &required, make(map[*base.Schema]bool))

		var names []string
		for name := range props.FromOldest() {
			names = append(names, name)
		}
		assert.ElementsMatch(t, []string{"kind", "file", "extra", "own"}, names)
		assert.ElementsMatch(t, []string{"file", "kind"}, required)
	})

	t.Run("stops on a cycle", func(t *testing.T) {
		schema := &base.Schema{Properties: orderedmap.New[string, *base.SchemaProxy]()}
		schema.Properties.Set("kind", base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}))
		schema.AllOf = []*base.SchemaProxy{base.CreateSchemaProxy(schema)}

		props := orderedmap.New[string, *base.SchemaProxy]()
		var required []string
		collectProperties(schema, props, &required, make(map[*base.Schema]bool))

		assert.Equal(t, 1, props.Len())
	})
}

// uploadSpec renders a one-operation spec whose request body carries the given
// schema, indented to sit under the content type.
func uploadSpec(contentType, bodySchema string) string {
	return `openapi: 3.0.3
info:
  title: upload
  version: "1"
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          ` + contentType + `:
            schema:
              ` + bodySchema + `
      responses:
        "201":
          description: created
components:
  schemas:
    empty:
      type: object
    uploader:
      type: object
      required: [file]
      properties:
        kind: {type: string}
        file: {type: string, format: binary}
    composed:
      allOf:
        - $ref: '#/components/schemas/uploader'
        - type: object
          required: [kind, file]
          properties:
            extra: {type: string}
    nested:
      allOf:
        - $ref: '#/components/schemas/composed'
      properties:
        own: {type: string}
`
}

func buildModel(t *testing.T, spec []byte) *v3high.Document {
	t.Helper()

	doc, err := libopenapi.NewDocumentWithConfiguration(spec, &datamodel.DocumentConfiguration{
		SkipCircularReferenceCheck: true,
	})
	require.NoError(t, err)

	model, errs := doc.BuildV3Model()
	require.Nil(t, errs)
	return &model.Model
}

func uploadOperation(t *testing.T, contentType, bodySchema string) *v3high.Operation {
	t.Helper()

	model := buildModel(t, []byte(uploadSpec(contentType, bodySchema)))
	return model.Paths.PathItems.GetOrZero("/upload").Post
}

func multipartSchema(t *testing.T, spec []byte) *base.SchemaProxy {
	t.Helper()

	model := buildModel(t, spec)
	return model.Paths.PathItems.GetOrZero("/upload").Post.
		RequestBody.Content.GetOrZero("multipart/form-data").Schema
}
