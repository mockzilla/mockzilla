package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRule_ArrayEnumScalars(t *testing.T) {
	spec := writeSpec(t, "dracoon.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  rules:
                    type: array
                    enum: [alpha, beta, gamma]
                    items: {type: string}
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	require.Len(t, defects, 1)
	assert.Equal(t, "array-enum-scalars", defects[0].Rule)
	assert.Contains(t, defects[0].Path, "rules")
}

func TestRule_AdditionalPropertiesFalseWithOneOf(t *testing.T) {
	spec := writeSpec(t, "atlassian.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                additionalProperties: false
                oneOf:
                  - type: object
                    properties:
                      kind: {type: string}
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	require.Len(t, defects, 1)
	assert.Equal(t, "additional-props-false-with-oneof", defects[0].Rule)
}

// TestRule_AdditionalPropertiesTrueWithOneOf confirms the rule doesn't fire
// for the boolean-true form (which permits arbitrary additional fields and
// is therefore satisfiable). Without this guard the rule risks flagging
// schemas that pass validators in practice.
func TestRule_AdditionalPropertiesTrueWithOneOf(t *testing.T) {
	spec := writeSpec(t, "permissive.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                additionalProperties: true
                oneOf:
                  - type: object
                    properties:
                      kind: {type: string}
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	assert.Empty(t, defects)
}

func TestRule_AllOfDisjointEnums(t *testing.T) {
	spec := writeSpec(t, "motaword.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                allOf:
                  - type: object
                    properties:
                      status:
                        type: string
                        enum: [success, error]
                  - type: object
                    properties:
                      status:
                        type: string
                        enum: [already_confirmed]
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	require.Len(t, defects, 1)
	assert.Equal(t, "allof-non-overlapping-enums", defects[0].Rule)
	assert.Contains(t, defects[0].Path, "status")
}

// TestRule_AllOfOverlappingEnumsAreClean keeps the rule honest: when the
// intersection is non-empty (success + error overlap), no defect.
func TestRule_AllOfOverlappingEnumsAreClean(t *testing.T) {
	spec := writeSpec(t, "overlap.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                allOf:
                  - type: object
                    properties:
                      status:
                        type: string
                        enum: [success, error, pending]
                  - type: object
                    properties:
                      status:
                        type: string
                        enum: [success, error]
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	assert.Empty(t, defects)
}

func TestRule_AllOfAdditionalPropertiesClash(t *testing.T) {
	spec := writeSpec(t, "zuora.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                allOf:
                  - type: object
                    properties:
                      success: {type: boolean}
                  - type: object
                    additionalProperties: {type: object}
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	require.Len(t, defects, 1)
	assert.Equal(t, "allof-additional-props-conflicts-sibling", defects[0].Rule)
}

// TestRule_AllOfAdditionalPropertiesSiblingFromTopLevel exercises the
// Azure/Atlassian shape: a base schema with additionalProperties is `$ref`'d
// under allOf, and the derived type declares conflicting top-level fields.
// The schema's own properties act as a sibling allOf branch.
func TestRule_AllOfAdditionalPropertiesSiblingFromTopLevel(t *testing.T) {
	spec := writeSpec(t, "azure.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Derived"
components:
  schemas:
    Base:
      type: object
      additionalProperties: {type: object}
      properties:
        objectType: {type: string}
    Derived:
      allOf:
        - $ref: "#/components/schemas/Base"
      type: object
      properties:
        # conflicts: Base sees this as additional and wants object, we declare boolean
        isActive: {type: boolean}
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	// Walker visits each schema once but the rule fires per-schema; both
	// Derived and the in-response reference resolve to the same instance,
	// so we expect exactly one defect.
	require.NotEmpty(t, defects)
	saw := false
	for _, d := range defects {
		if d.Rule == "allof-additional-props-conflicts-sibling" {
			saw = true
			break
		}
	}
	assert.True(t, saw, "expected allof-additional-props-conflicts-sibling defect; got %v", defects)
}

func TestRule_PatternUnicodeCircumflex(t *testing.T) {
	spec := writeSpec(t, "stark.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  bankCode:
                    type: string
                    pattern: "ˆ^\\d{1,8}$"
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	require.Len(t, defects, 1)
	assert.Equal(t, "pattern-unicode-circumflex", defects[0].Rule)
}

// TestRule_PatternASCIICaret confirms a plain caret pattern is clean.
// Without this, a careless tweak to ruleA could flag every anchored regex.
func TestRule_PatternASCIICaret(t *testing.T) {
	spec := writeSpec(t, "clean-pattern.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /x:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  code:
                    type: string
                    pattern: "^[A-Z]{2}$"
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	assert.Empty(t, defects)
}
