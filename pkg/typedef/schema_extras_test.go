package typedef

import (
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

func proxy(s *base.Schema) *base.SchemaProxy {
	return base.CreateSchemaProxy(s)
}

func TestEnumContains(t *testing.T) {
	assert.True(t, enumContains([]any{"a", "b"}, "a"))
	assert.True(t, enumContains([]any{1, 2}, "1"))
	assert.False(t, enumContains([]any{"a"}, "b"))
	assert.False(t, enumContains(nil, "a"))
}

func TestEnumValueKey(t *testing.T) {
	assert.Equal(t, "hello", enumValueKey("hello"))
	assert.Equal(t, "42", enumValueKey(42))
	assert.Equal(t, "3.14", enumValueKey(3.14))
	assert.Equal(t, "0", enumValueKey(nil))
}

func TestFirstPatternFromBranches(t *testing.T) {
	t.Run("empty branches", func(t *testing.T) {
		assert.Equal(t, "", firstPatternFromBranches(nil))
	})

	t.Run("first non-empty wins", func(t *testing.T) {
		branches := []*base.SchemaProxy{
			nil,
			proxy(&base.Schema{Pattern: ""}),
			proxy(&base.Schema{Pattern: "^[a-z]+$"}),
			proxy(&base.Schema{Pattern: "^[0-9]+$"}),
		}
		assert.Equal(t, "^[a-z]+$", firstPatternFromBranches(branches))
	})

	t.Run("none have patterns", func(t *testing.T) {
		branches := []*base.SchemaProxy{
			proxy(&base.Schema{}),
			proxy(&base.Schema{}),
		}
		assert.Equal(t, "", firstPatternFromBranches(branches))
	})
}

func TestEnumsFromUnionBranches(t *testing.T) {
	mkEnum := func(vals ...string) []*yaml.Node {
		out := make([]*yaml.Node, len(vals))
		for i, v := range vals {
			out[i] = &yaml.Node{Tag: "!!str", Value: v}
		}
		return out
	}

	t.Run("no branches", func(t *testing.T) {
		vals, typ := enumsFromUnionBranches(nil, "string")
		assert.Nil(t, vals)
		assert.Equal(t, "", typ)
	})

	t.Run("branch matching type returns immediately", func(t *testing.T) {
		branches := []*base.SchemaProxy{
			proxy(&base.Schema{Type: []string{"string"}, Enum: mkEnum("a", "b")}),
		}
		vals, typ := enumsFromUnionBranches(branches, "string")
		assert.Equal(t, []any{"a", "b"}, vals)
		assert.Equal(t, "string", typ)
	})

	t.Run("null-only branch skipped", func(t *testing.T) {
		branches := []*base.SchemaProxy{
			proxy(&base.Schema{Type: []string{"null"}, Enum: mkEnum("ignored")}),
			proxy(&base.Schema{Type: []string{"string"}, Enum: mkEnum("kept")}),
		}
		vals, typ := enumsFromUnionBranches(branches, "string")
		assert.Equal(t, []any{"kept"}, vals)
		assert.Equal(t, "string", typ)
	})

	t.Run("fallback branch with disagreeing type", func(t *testing.T) {
		branches := []*base.SchemaProxy{
			proxy(&base.Schema{Type: []string{"integer"}, Enum: mkEnum("1", "2")}),
		}
		vals, typ := enumsFromUnionBranches(branches, "string")
		assert.NotNil(t, vals)
		assert.Equal(t, "integer", typ)
	})

	t.Run("nil and enum-less branches skipped", func(t *testing.T) {
		branches := []*base.SchemaProxy{
			nil,
			proxy(&base.Schema{Type: []string{"string"}}),
			proxy(&base.Schema{Type: []string{"string"}, Enum: mkEnum("x")}),
		}
		vals, _ := enumsFromUnionBranches(branches, "string")
		assert.Equal(t, []any{"x"}, vals)
	})
}

func TestDiscriminatorFromInner(t *testing.T) {
	t.Run("codegen discriminator with mapping", func(t *testing.T) {
		d := &codegen.Discriminator{
			Property: "kind",
			Mapping:  map[string]string{"a": "TypeA", "b": "TypeB"},
		}
		out := discriminatorFromInner(d, nil)
		assert.Equal(t, "kind", out.PropertyName)
		assert.Equal(t, map[string]string{"a": "TypeA", "b": "TypeB"}, out.Mapping)
	})

	t.Run("codegen discriminator with no property", func(t *testing.T) {
		out := discriminatorFromInner(&codegen.Discriminator{}, nil)
		assert.Nil(t, out)
	})

	t.Run("fallback to inner discriminator", func(t *testing.T) {
		inner := &base.Schema{Discriminator: &base.Discriminator{PropertyName: "type"}}
		out := discriminatorFromInner(nil, inner)
		assert.Equal(t, "type", out.PropertyName)
		assert.Nil(t, out.Mapping)
	})

	t.Run("nil everywhere", func(t *testing.T) {
		assert.Nil(t, discriminatorFromInner(nil, nil))
	})

	t.Run("inner without discriminator", func(t *testing.T) {
		assert.Nil(t, discriminatorFromInner(nil, &base.Schema{}))
	})
}

func TestConvertEnumNode(t *testing.T) {
	t.Run("nil node", func(t *testing.T) {
		assert.Nil(t, convertEnumNode(nil, "string"))
	})

	t.Run("yaml null", func(t *testing.T) {
		assert.Nil(t, convertEnumNode(&yaml.Node{Tag: "!!null"}, "string"))
	})

	t.Run("malformed empty-value non-scalar", func(t *testing.T) {
		assert.Nil(t, convertEnumNode(&yaml.Node{Tag: "!!map", Value: ""}, "string"))
	})

	t.Run("string schema with int tag returns int64", func(t *testing.T) {
		n := &yaml.Node{Tag: "!!int", Value: "42"}
		assert.Equal(t, int64(42), convertEnumNode(n, "string"))
	})

	t.Run("string schema with float tag returns float64", func(t *testing.T) {
		n := &yaml.Node{Tag: "!!float", Value: "3.14"}
		assert.Equal(t, 3.14, convertEnumNode(n, "string"))
	})

	t.Run("string schema with bool tag returns bool", func(t *testing.T) {
		n := &yaml.Node{Tag: "!!bool", Value: "true"}
		assert.Equal(t, true, convertEnumNode(n, "string"))
	})

	t.Run("string schema with unparseable int falls back to string", func(t *testing.T) {
		n := &yaml.Node{Tag: "!!int", Value: "not-an-int"}
		assert.Equal(t, "not-an-int", convertEnumNode(n, "string"))
	})

	t.Run("integer schema reads value", func(t *testing.T) {
		n := &yaml.Node{Tag: "!!int", Value: "7"}
		assert.Equal(t, int64(7), convertEnumNode(n, "integer"))
	})
}

func TestAllOfDeclaresOnlyObjects(t *testing.T) {
	t.Run("nil schema", func(t *testing.T) {
		assert.False(t, allOfDeclaresOnlyObjects(nil))
	})

	t.Run("empty allOf", func(t *testing.T) {
		assert.False(t, allOfDeclaresOnlyObjects(&base.Schema{}))
	})

	t.Run("all branches are objects", func(t *testing.T) {
		s := &base.Schema{
			AllOf: []*base.SchemaProxy{
				proxy(&base.Schema{Type: []string{"object"}}),
				proxy(&base.Schema{Type: []string{"OBJECT"}}),
			},
		}
		assert.True(t, allOfDeclaresOnlyObjects(s))
	})

	t.Run("one non-object branch", func(t *testing.T) {
		s := &base.Schema{
			AllOf: []*base.SchemaProxy{
				proxy(&base.Schema{Type: []string{"object"}}),
				proxy(&base.Schema{Type: []string{"string"}}),
			},
		}
		assert.False(t, allOfDeclaresOnlyObjects(s))
	})

	t.Run("nil branch fails", func(t *testing.T) {
		s := &base.Schema{AllOf: []*base.SchemaProxy{nil}}
		assert.False(t, allOfDeclaresOnlyObjects(s))
	})
}

func TestFindItemsInAllOf(t *testing.T) {
	t.Run("nil and empty", func(t *testing.T) {
		assert.Nil(t, findItemsInAllOf(nil))
		assert.Nil(t, findItemsInAllOf(&base.Schema{}))
	})

	t.Run("direct items", func(t *testing.T) {
		items := &base.Schema{Type: []string{"string"}}
		itemsDV := &base.DynamicValue[*base.SchemaProxy, bool]{N: 0, A: proxy(items)}
		s := &base.Schema{AllOf: []*base.SchemaProxy{
			proxy(&base.Schema{Items: itemsDV}),
		}}
		got := findItemsInAllOf(s)
		assert.NotNil(t, got)
		assert.Equal(t, []string{"string"}, got.Type)
	})

	t.Run("nested allOf items", func(t *testing.T) {
		items := &base.Schema{Type: []string{"integer"}}
		itemsDV := &base.DynamicValue[*base.SchemaProxy, bool]{N: 0, A: proxy(items)}
		inner := &base.Schema{AllOf: []*base.SchemaProxy{proxy(&base.Schema{Items: itemsDV})}}
		s := &base.Schema{AllOf: []*base.SchemaProxy{proxy(inner)}}
		got := findItemsInAllOf(s)
		assert.NotNil(t, got)
		assert.Equal(t, []string{"integer"}, got.Type)
	})

	t.Run("no items anywhere", func(t *testing.T) {
		s := &base.Schema{AllOf: []*base.SchemaProxy{
			proxy(&base.Schema{Type: []string{"object"}}),
		}}
		assert.Nil(t, findItemsInAllOf(s))
	})
}

func TestAdditionalPropertiesIsPlaceholder(t *testing.T) {
	t.Run("nil is placeholder", func(t *testing.T) {
		assert.True(t, additionalPropertiesIsPlaceholder(nil))
	})

	t.Run("non-string type is not", func(t *testing.T) {
		assert.False(t, additionalPropertiesIsPlaceholder(&schema.Schema{Type: "object"}))
	})

	t.Run("bare string is placeholder", func(t *testing.T) {
		assert.True(t, additionalPropertiesIsPlaceholder(&schema.Schema{Type: "string"}))
	})

	t.Run("string with constraints is not", func(t *testing.T) {
		assert.False(t, additionalPropertiesIsPlaceholder(&schema.Schema{Type: "string", Pattern: "^foo$"}))
	})
}

func TestMergeAllOfUnionPropertiesEdges(t *testing.T) {
	t.Run("nil and empty allOf", func(t *testing.T) {
		props, req := mergeAllOfUnionProperties(nil, nil, nil)
		assert.Nil(t, props)
		assert.Nil(t, req)

		props, req = mergeAllOfUnionProperties(&base.Schema{}, nil, nil)
		assert.Nil(t, props)
		assert.Nil(t, req)
	})

	t.Run("allOf without oneOf branch", func(t *testing.T) {
		s := &base.Schema{AllOf: []*base.SchemaProxy{
			proxy(&base.Schema{Type: []string{"object"}}),
			nil,
		}}
		props, req := mergeAllOfUnionProperties(s, nil, &schemaContext{cache: map[string]*schema.Schema{}, depthTrack: map[string]int{}})
		assert.Empty(t, props)
		assert.Empty(t, req)
	})
}

func TestComponentNameFromRef(t *testing.T) {
	assert.Equal(t, "Foo", componentNameFromRef("#/components/schemas/Foo"))
	assert.Equal(t, "FooBar", componentNameFromRef("#/components/schemas/Foo.Bar"))
	assert.Equal(t, "", componentNameFromRef("not-a-ref"))
	assert.Equal(t, "", componentNameFromRef("#/definitions/Foo"))
}

func TestInnerHasObjectShape(t *testing.T) {
	assert.False(t, innerHasObjectShape(nil))
	assert.False(t, innerHasObjectShape(&base.Schema{Type: []string{"string"}}))
	assert.True(t, innerHasObjectShape(&base.Schema{Type: []string{"object"}}))
	assert.True(t, innerHasObjectShape(&base.Schema{Type: []string{"OBJECT"}}))

	props := orderedmap.New[string, *base.SchemaProxy]()
	props.Set("name", proxy(&base.Schema{Type: []string{"string"}}))
	assert.True(t, innerHasObjectShape(&base.Schema{Properties: props}))
}

func TestCollectAllOfBranchProperties(t *testing.T) {
	ctx := &schemaContext{
		cache:           map[string]*schema.Schema{},
		depthTrack:      map[string]int{},
		expandingUnions: map[string]bool{},
	}

	t.Run("nil schema", func(t *testing.T) {
		props, req := collectAllOfBranchProperties(nil, nil, ctx)
		assert.Nil(t, props)
		assert.Nil(t, req)
	})

	t.Run("properties and required collected", func(t *testing.T) {
		props := orderedmap.New[string, *base.SchemaProxy]()
		props.Set("name", proxy(&base.Schema{Type: []string{"string"}}))
		s := &base.Schema{
			Required:   []string{"name"},
			Properties: props,
		}
		got, req := collectAllOfBranchProperties(s, nil, ctx)
		assert.Contains(t, got, "name")
		assert.Equal(t, []string{"name"}, req)
	})

	t.Run("nested allOf merged", func(t *testing.T) {
		nestedProps := orderedmap.New[string, *base.SchemaProxy]()
		nestedProps.Set("inner", proxy(&base.Schema{Type: []string{"integer"}}))
		nested := &base.Schema{Required: []string{"inner"}, Properties: nestedProps}

		outerProps := orderedmap.New[string, *base.SchemaProxy]()
		outerProps.Set("outer", proxy(&base.Schema{Type: []string{"string"}}))
		s := &base.Schema{
			Required:   []string{"outer"},
			Properties: outerProps,
			AllOf:      []*base.SchemaProxy{proxy(nested)},
		}
		got, req := collectAllOfBranchProperties(s, nil, ctx)
		assert.Contains(t, got, "outer")
		assert.Contains(t, got, "inner")
		assert.ElementsMatch(t, []string{"outer", "inner"}, req)
	})
}
