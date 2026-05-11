package simplify

import (
	"strings"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const demoSpec = `openapi: 3.0.3
info:
  title: Test
  version: 1.0.0
paths:
  /things:
    get:
      operationId: getThings
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Thing'
components:
  schemas:
    Thing:
      type: object
      x-internal-marker: should-be-stripped-from-schema
      required:
        - id
        - status
      properties:
        id:
          type: string
        status:
          anyOf:
            - type: string
            - type: integer
        metadata:
          oneOf:
            - type: string
            - type: object
        keep_me:
          type: string
`

// TestSimplify_Transforms covers the documented behavior end-to-end on a tiny
// hand-crafted spec: required-union flattening, optional-union removal,
// schema-level x-* stripping, and source-indent preservation. Examples are
// deliberately preserved (see pkg/typedef/simplify_spec.go:205).
func TestSimplify_Transforms(t *testing.T) {
	out, err := Simplify([]byte(demoSpec), Options{})
	require.NoError(t, err)
	s := string(out)

	t.Run("strips schema-level x-*", func(t *testing.T) {
		assert.NotContains(t, s, "x-internal-marker")
	})
	t.Run("flattens required unions to first variant", func(t *testing.T) {
		assert.NotContains(t, s, "anyOf")
		assert.Contains(t, s, "status:")
	})
	t.Run("drops optional union properties entirely", func(t *testing.T) {
		assert.NotContains(t, s, "oneOf")
		assert.NotContains(t, s, "metadata:")
	})
	t.Run("preserves source 2-space indent", func(t *testing.T) {
		assert.True(t,
			strings.Contains(s, "\n  title:"),
			"expected 2-space indent under 'info:'; got:\n%s", s)
	})
}

func TestSimplify_OptionalPropertyConfig(t *testing.T) {
	t.Run("nil keeps all optional properties", func(t *testing.T) {
		out, err := Simplify([]byte(demoSpec), Options{})
		require.NoError(t, err)
		assert.Contains(t, string(out), "keep_me:")
	})

	t.Run("Min:0 Max:0 drops all optional", func(t *testing.T) {
		out, err := Simplify([]byte(demoSpec), Options{
			OptionalProperties: &typedef.OptionalPropertyConfig{Min: 0, Max: 0},
		})
		require.NoError(t, err)
		assert.NotContains(t, string(out), "keep_me:")
	})

	t.Run("Min:N Max:N keeps exactly N", func(t *testing.T) {
		// Thing has only one optional property after union removal (keep_me),
		// so requesting 5 yields 1.
		out, err := Simplify([]byte(demoSpec), Options{
			OptionalProperties: &typedef.OptionalPropertyConfig{Min: 5, Max: 5},
		})
		require.NoError(t, err)
		assert.Contains(t, string(out), "keep_me:")
	})
}

func TestSimplify_WithConfigYAML(t *testing.T) {
	const spec = `openapi: 3.0.0
info:
  title: T
  version: 1.0.0
paths:
  /keep:
    get:
      operationId: keepMe
      responses:
        '200':
          description: ok
  /drop:
    get:
      operationId: dropMe
      responses:
        '200':
          description: ok
`
	const cfg = `filter:
  include:
    paths:
      - /keep
`
	out, err := Simplify([]byte(spec), Options{ConfigYAML: []byte(cfg)})
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "/keep:")
	assert.NotContains(t, s, "/drop:")
}

func TestSimplify_Errors(t *testing.T) {
	t.Run("malformed spec", func(t *testing.T) {
		_, err := Simplify([]byte("not: valid: openapi: :::"), Options{})
		require.Error(t, err)
	})

	t.Run("malformed config YAML", func(t *testing.T) {
		_, err := Simplify([]byte(demoSpec), Options{
			ConfigYAML: []byte("filter: [this is not a map"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing config")
	})
}
