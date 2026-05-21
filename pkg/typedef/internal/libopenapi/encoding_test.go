package libopenapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoding_RequestBodyEncodingPopulated(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    post:
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                tags: {type: array, items: {type: string}}
            encoding:
              tags:
                style: form
                explode: false
                contentType: text/plain
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "POST")
	require.NotNil(t, op)
	require.Contains(t, op.BodyEncoding, "tags")

	enc := op.BodyEncoding["tags"]
	assert.Equal(t, "form", enc.Style)
	require.NotNil(t, enc.Explode)
	assert.False(t, *enc.Explode)
	assert.Equal(t, "text/plain", enc.ContentType)
}

func TestEncoding_ParameterEncodingForStyledQuery(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      parameters:
        - name: filter
          in: query
          style: deepObject
          explode: true
          schema: {type: object}
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)

	q := op.Query["filter"]
	require.NotNil(t, q)
	require.NotNil(t, q.Encoding)
	assert.Equal(t, "deepObject", q.Encoding.Style)
	require.NotNil(t, q.Encoding.Explode)
	assert.True(t, *q.Encoding.Explode)
}

func TestEncoding_NoEncodingNoMap(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      parameters:
        - name: filter
          in: query
          schema: {type: string}
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	q := op.Query["filter"]
	require.NotNil(t, q)
	assert.Nil(t, q.Encoding, "no style/explode means no encoding entry")
}

func TestConvertRequestBodyEncoding_Nil(t *testing.T) {
	assert.Nil(t, convertRequestBodyEncoding(nil))
}

func TestConvertParameterEncoding_NilOrUnstyled(t *testing.T) {
	assert.Nil(t, convertParameterEncoding(nil))
}
