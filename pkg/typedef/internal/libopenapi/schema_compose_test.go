package libopenapi

import (
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ = slices.Sorted[string] // keep slices import if maps.Keys collapses

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

func TestComposeSchema_NoCompositionReturnsNil(t *testing.T) {
	assert.Nil(t, composeSchema(nil, newConvertCtx()))
	assert.Nil(t, composeSchema(&base.Schema{}, newConvertCtx()))
}

func TestApplyDiscriminator_NoOpWithoutOneOf(t *testing.T) {
	properties := map[string]*schema.Schema{}
	s := &base.Schema{Discriminator: &base.Discriminator{PropertyName: "kind"}}
	applyDiscriminator(properties, s, &composedShape{})
	assert.NotContains(t, properties, "kind")
}

func TestApplyDiscriminator_NoMappingReturnsEarly(t *testing.T) {
	properties := map[string]*schema.Schema{
		"kind": {Type: types.TypeString},
	}
	s := &base.Schema{Discriminator: &base.Discriminator{PropertyName: "kind"}}
	composed := &composedShape{}
	applyDiscriminator(properties, s, composed)
	assert.Empty(t, properties["kind"].Enum, "no mapping -> no enum injection")
}

func TestDiscriminatorValueFor_NoMapping(t *testing.T) {
	assert.Equal(t, "", discriminatorValueFor(nil, nil))
	assert.Equal(t, "", discriminatorValueFor(&base.Discriminator{}, nil))
}

func TestFirstNonNullBranch_AllNullAndNil(t *testing.T) {
	assert.Nil(t, firstNonNullBranch(nil))
	assert.Nil(t, firstNonNullBranch([]*base.SchemaProxy{nil}))
}

func TestFirstPatternFromBranches_NilEntries(t *testing.T) {
	assert.Equal(t, "", firstPatternFromBranches(nil))
	assert.Equal(t, "", firstPatternFromBranches([]*base.SchemaProxy{nil}))
}

func TestOneOfPrimitiveEnumRecovery(t *testing.T) {
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
                oneOf:
                  - type: string
                    enum: ["x", "y"]
                  - type: string
                    enum: ["z"]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s)
	assert.Equal(t, types.TypeString, s.Type)
	require.Len(t, s.Enum, 2, "first branch's enum wins")
	keys := mapToStrings(s.Enum)
	assert.ElementsMatch(t, []string{"x", "y"}, keys)
}

// TestCompose_NestedAllOfRefMergesWithInlineBranch covers the beezup
// orderLinks shape: a $ref to a schema that declares some properties +
// an inline allOf branch that adds more properties and bumps required.
// The merged schema must carry properties from both branches so the
// generator can emit every required key.
func TestCompose_NestedAllOfRefMergesWithInlineBranch(t *testing.T) {
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
                $ref: "#/components/schemas/Links"
components:
  schemas:
    HeaderLinks:
      type: object
      required: [self]
      properties:
        self: {type: object, properties: {href: {type: string}}}
    Links:
      allOf:
        - $ref: "#/components/schemas/HeaderLinks"
        - type: object
          required: [self, history, harvest]
          properties:
            history: {type: object, properties: {href: {type: string}}}
            harvest: {type: object, properties: {href: {type: string}}}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Contains(t, s.Properties, "self", "self from $ref branch")
	assert.Contains(t, s.Properties, "history", "history from inline branch")
	assert.Contains(t, s.Properties, "harvest", "harvest from inline branch")
	assert.ElementsMatch(t, []string{"self", "history", "harvest"}, s.Required)
}

// TestCompose_BeezupLinksHierarchy reproduces the exact orderWithLinks
// nesting from testdata/specs/3.0/misc/beezup.com.yml: links appears in
// two allOf branches (once via $ref order, once via $ref orderWithLinks
// inline), and the linked schema is itself allOf-of-$ref-plus-inline.
// The merged response.links must surface every property the inline
// branch contributes (history, harvest), not just self from the $ref.
func TestCompose_BeezupLinksHierarchy(t *testing.T) {
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
                $ref: "#/components/schemas/OrderWithLinks"
components:
  schemas:
    HeaderLinks:
      type: object
      required: [self]
      properties:
        self: {type: object, properties: {href: {type: string}}}
    HistoryLink:
      type: object
      properties: {href: {type: string}}
    HarvestLink:
      type: object
      properties: {href: {type: string}}
    OrderLinks:
      allOf:
        - $ref: "#/components/schemas/HeaderLinks"
        - type: object
          required: [self, history, harvest]
          properties:
            history: {$ref: "#/components/schemas/HistoryLink"}
            harvest: {$ref: "#/components/schemas/HarvestLink"}
    OrderHeader:
      type: object
      properties:
        id: {type: string}
    Order:
      allOf:
        - $ref: "#/components/schemas/OrderHeader"
        - type: object
          properties:
            links: {$ref: "#/components/schemas/OrderLinks"}
    OrderWithLinks:
      allOf:
        - $ref: "#/components/schemas/Order"
        - type: object
          required: [links]
          properties:
            links: {$ref: "#/components/schemas/OrderLinks"}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s.Properties["links"])
	linksProp := s.Properties["links"]
	assert.Contains(t, linksProp.Properties, "self", "self from HeaderLinks")
	assert.Contains(t, linksProp.Properties, "history", "history from OrderLinks inline branch")
	assert.Contains(t, linksProp.Properties, "harvest", "harvest from OrderLinks inline branch")
	assert.ElementsMatch(t, []string{"self", "history", "harvest"}, linksProp.Required)
}

// TestCompose_BoxOneOfInsideAllOf covers box.com's Collaboration.item
// pattern: `allOf: [{oneOf: [$ref A, $ref B]}, {description}]` where
// each oneOf branch is itself allOf-of-$ref-plus-inline. The merged
// shape must carry properties from the chosen oneOf branch's entire
// allOf chain (including required keys declared at the outer schema
// level), not just the first $ref in the chain.
func TestCompose_BoxOneOfInsideAllOf(t *testing.T) {
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
                $ref: "#/components/schemas/Collab"
components:
  schemas:
    FileBase:
      type: object
      required: [id]
      properties:
        id: {type: string}
        etag: {type: string}
    FileMini:
      allOf:
        - $ref: "#/components/schemas/FileBase"
        - type: object
          properties:
            name: {type: string}
            sequence_id: {type: string}
            sha1: {type: string}
      required:
        - sequence_id
        - sha1
      type: object
    File:
      allOf:
        - $ref: "#/components/schemas/FileMini"
        - type: object
          properties:
            description: {type: string}
    Folder:
      type: object
      required: [id]
      properties:
        id: {type: string}
        kind: {type: string, enum: [folder]}
    Collab:
      type: object
      properties:
        item:
          allOf:
            - oneOf:
                - $ref: "#/components/schemas/File"
                - $ref: "#/components/schemas/Folder"
            - description: chosen item
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s.Properties["item"])
	item := s.Properties["item"]
	assert.Contains(t, item.Properties, "id", "id from FileBase $ref")
	assert.Contains(t, item.Properties, "sequence_id", "sequence_id from FileMini inline branch")
	assert.Contains(t, item.Properties, "sha1", "sha1 from FileMini inline branch")
	assert.Contains(t, item.Required, "sequence_id", "FileMini's required must propagate")
	assert.Contains(t, item.Required, "sha1", "FileMini's required must propagate")
}

// TestCompose_BoxRealSpec_CollaborationsItem dumps what the registry
// produces for box.com's /collaborations GET response, specifically the
// entries[].item property. This is a diagnostic test: it doesn't assert
// success, it logs the converted schema so we can see exactly what the
// generator gets.
func TestCompose_BeezupRealSpec_OrderLinks(t *testing.T) {
	t.Skip("diagnostic only; un-skip to inspect beezup.com schema conversion")
	specBytes, err := os.ReadFile("../../../../testdata/specs/3.0/misc/beezup.com.yml")
	require.NoError(t, err)
	reg, err := NewRegistry(specBytes, Options{})
	require.NoError(t, err)

	// Look at orderLinks via the model directly, bypassing the cached
	// convert path the operation walk uses, to see what compose
	// produces on a fresh conversion.
	freshCtx := newConvertCtx()
	model := reg.model
	for name, proxy := range model.Components.Schemas.FromOldest() {
		if name != "orderLinks" {
			continue
		}
		converted := convertProxy(proxy, freshCtx)
		require.NotNil(t, converted)
		t.Logf("orderLinks (fresh) Type=%q Required=%v Properties=%v",
			converted.Type, converted.Required, slices.Sorted(maps.Keys(converted.Properties)))
	}

	op := reg.FindOperation("/orders/v3/{marketplaceTechnicalCode}/{accountId}/{beezUPOrderId}", "GET")
	require.NotNil(t, op)
	success := op.Response.GetSuccess()
	root := success.Content
	links := root.Properties["links"]
	require.NotNil(t, links)
	t.Logf("links (via operation) Type=%q Required=%v Properties=%v",
		links.Type, links.Required, slices.Sorted(maps.Keys(links.Properties)))
}

func TestCompose_BoxRealSpec_CollaborationsItem(t *testing.T) {
	t.Skip("diagnostic only; un-skip to inspect box.com schema conversion")
	specBytes, err := os.ReadFile("../../../../testdata/specs/3.0/misc/box.com.yml")
	require.NoError(t, err)
	reg, err := NewRegistry(specBytes, Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/collaborations", "GET")
	require.NotNil(t, op)
	require.NotNil(t, op.Response)
	success := op.Response.GetSuccess()
	require.NotNil(t, success)

	// Walk Collaborations.entries.items.item
	collabs := success.Content
	t.Logf("collabs.Properties keys: %v", slices.Sorted(maps.Keys(collabs.Properties)))
	entries := collabs.Properties["entries"]
	require.NotNil(t, entries)
	require.NotNil(t, entries.Items)
	t.Logf("entries.Items.Properties keys: %v", slices.Sorted(maps.Keys(entries.Items.Properties)))
	item := entries.Items.Properties["item"]
	require.NotNil(t, item)
	t.Logf("item.Type=%q, Required=%v, Properties=%v", item.Type, item.Required, slices.Sorted(maps.Keys(item.Properties)))
}

func TestAllOfMergeArrayItems(t *testing.T) {
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
                allOf:
                  - type: array
                    items:
                      type: integer
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeArray, s.Type)
	require.NotNil(t, s.Items)
	assert.Equal(t, types.TypeInteger, s.Items.Type)
}
