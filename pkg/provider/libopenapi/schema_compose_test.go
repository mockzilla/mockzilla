package libopenapi

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompose_AllOfMergesPropertiesAndRequired(t *testing.T) {
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
                allOf:
                  - type: object
                    required: [a]
                    properties:
                      a: {type: string}
                  - type: object
                    required: [b]
                    properties:
                      b: {type: integer}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeObject, s.Type)
	assert.Contains(t, s.Properties, "a")
	assert.Contains(t, s.Properties, "b")
	assert.ElementsMatch(t, []string{"a", "b"}, s.Required)
}

func TestCompose_AllOfEnumIntersection(t *testing.T) {
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
                allOf:
                  - type: object
                    properties:
                      kind:
                        type: string
                        enum: [a, b, c]
                  - type: object
                    properties:
                      kind:
                        type: string
                        enum: [b, c, d]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	prop, ok := s.Properties["kind"]
	require.True(t, ok)
	keys := map[string]bool{}
	for _, v := range prop.Enum {
		keys[scalarKey(v)] = true
	}
	assert.True(t, keys["b"], "b is in both allOf branches")
	assert.True(t, keys["c"], "c is in both allOf branches")
	assert.False(t, keys["a"], "a only in first branch and must be dropped")
	assert.False(t, keys["d"], "d only in second branch and must be dropped")
}

func TestCompose_OneOfPicksFirstNonNullBranch(t *testing.T) {
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
                oneOf:
                  - type: "null"
                  - type: object
                    properties:
                      label: {type: string}
                    required: [label]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeObject, s.Type)
	assert.Contains(t, s.Properties, "label")
	assert.ElementsMatch(t, []string{"label"}, s.Required)
}

func TestCompose_OneOfDiscriminatorInjectsValue(t *testing.T) {
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
                oneOf:
                  - $ref: "#/components/schemas/Dog"
                  - $ref: "#/components/schemas/Cat"
                discriminator:
                  propertyName: kind
                  mapping:
                    dog: "#/components/schemas/Dog"
                    cat: "#/components/schemas/Cat"
components:
  schemas:
    Dog:
      type: object
      properties:
        kind: {type: string}
        name: {type: string}
      required: [kind, name]
    Cat:
      type: object
      properties:
        kind: {type: string}
        whiskers: {type: integer}
      required: [kind, whiskers]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s.Discriminator)
	assert.Equal(t, "kind", s.Discriminator.PropertyName)

	prop, ok := s.Properties["kind"]
	require.True(t, ok)
	require.Len(t, prop.Enum, 1, "discriminator must pin kind to the picked branch")
	assert.Equal(t, "dog", scalarKey(prop.Enum[0]))
}

func TestCompose_AnyOfPatternFallback(t *testing.T) {
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
                anyOf:
                  - type: string
                    pattern: "^pat-A$"
                  - type: string
                    pattern: "^pat-B$"
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, "^pat-A$", s.Pattern, "outer-empty pattern falls back to first anyOf branch")
}

func TestCompose_RecursiveAllOfCycle(t *testing.T) {
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
                $ref: "#/components/schemas/A"
components:
  schemas:
    A:
      type: object
      allOf:
        - $ref: "#/components/schemas/B"
      properties:
        name: {type: string}
    B:
      type: object
      allOf:
        - $ref: "#/components/schemas/A"
      properties:
        ref:
          $ref: "#/components/schemas/A"
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeObject, s.Type)
	assert.Contains(t, s.Properties, "name")
}
