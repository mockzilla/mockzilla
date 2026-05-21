package libopenapi

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

func TestExtractLeadingNumber(t *testing.T) {
	cases := map[string]string{
		"0 (User)": "0",
		"-3.14abc": "-3.14",
		"":         "",
		"alpha":    "",
		"42":       "42",
	}
	for in, want := range cases {
		assert.Equal(t, want, extractLeadingNumber(in), "input %q", in)
	}
}

func TestParseTypedValue(t *testing.T) {
	cases := []struct {
		value, schemaType string
		want              any
	}{
		{"42", types.TypeInteger, int64(42)},
		{"0 (label)", types.TypeInteger, int64(0)},
		{"non-numeric", types.TypeInteger, "non-numeric"},
		{"3.14", types.TypeNumber, 3.14},
		{"7", types.TypeNumber, float64(7)},
		{"true", types.TypeBoolean, true},
		{"foo", types.TypeBoolean, "foo"},
		{"1", types.TypeString, "1"},
		{"raw", "unknown-type", "raw"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, parseTypedValue(c.value, c.schemaType), "value=%q type=%q", c.value, c.schemaType)
	}
}

func TestInferTypeFromNode(t *testing.T) {
	cases := map[string]string{
		"!!str":   types.TypeString,
		"!!int":   types.TypeInteger,
		"!!float": types.TypeNumber,
		"!!bool":  types.TypeBoolean,
		"!!null":  "",
	}
	for tag, want := range cases {
		n := &yaml.Node{Tag: tag, Value: "x"}
		assert.Equal(t, want, inferTypeFromNode(n), "tag=%s", tag)
	}
	assert.Equal(t, "", inferTypeFromNode(nil))
}

func TestScalarKey(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{int64(42), "42"},
		{int(7), "7"},
		{3.14, "3.14"},
		{true, "true"},
		{[]int{}, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, scalarKey(c.in))
	}
}

func TestEnumContainsKey(t *testing.T) {
	enum := []any{"a", int64(1), true}
	assert.True(t, enumContainsKey(enum, "a"))
	assert.True(t, enumContainsKey(enum, "1"))
	assert.True(t, enumContainsKey(enum, "true"))
	assert.False(t, enumContainsKey(enum, "zzz"))
}

func TestConvertScalarNode_NullAndMalformed(t *testing.T) {
	assert.Nil(t, convertScalarNode(nil, ""))
	assert.Nil(t, convertScalarNode(&yaml.Node{Tag: "!!null", Value: "null"}, ""))
	assert.Nil(t, convertScalarNode(&yaml.Node{Tag: "!!map", Value: ""}, types.TypeString))
}

func TestConvertScalarNode_StringTypeNumericValue(t *testing.T) {
	n := &yaml.Node{Tag: "!!int", Value: "42"}
	assert.Equal(t, int64(42), convertScalarNode(n, types.TypeString))

	f := &yaml.Node{Tag: "!!float", Value: "3.14"}
	assert.Equal(t, 3.14, convertScalarNode(f, types.TypeString))

	b := &yaml.Node{Tag: "!!bool", Value: "true"}
	assert.Equal(t, true, convertScalarNode(b, types.TypeString))
}

func TestMergeStringLists(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, mergeStringLists([]string{"a"}, []string{"b"}))
	assert.Equal(t, []string{"a"}, mergeStringLists(nil, []string{"a"}))
	assert.Equal(t, []string{"a"}, mergeStringLists([]string{"a"}, nil))
	assert.Equal(t, []string{"a", "b"}, mergeStringLists([]string{"a"}, []string{"a", "b"}))
}

func TestInferType(t *testing.T) {
	assert.Equal(t, types.TypeArray, inferType(&schema.Schema{}, nil, nil, nil))
	assert.Equal(t, types.TypeObject, inferType(nil, map[string]*schema.Schema{"k": nil}, nil, nil))
	assert.Equal(t, types.TypeObject, inferType(nil, nil, &schema.Schema{}, nil))
	assert.Equal(t, types.TypeString, inferType(nil, nil, nil, []any{"x"}))
	assert.Equal(t, types.TypeString, inferType(nil, nil, nil, nil), "empty schema defaults to string so the generator has a concrete type")
}

func TestInferTypeFromEnum(t *testing.T) {
	assert.Equal(t, types.TypeString, inferTypeFromEnum([]any{"a"}))
	assert.Equal(t, types.TypeInteger, inferTypeFromEnum([]any{int64(1)}))
	assert.Equal(t, types.TypeInteger, inferTypeFromEnum([]any{int(1)}))
	assert.Equal(t, types.TypeNumber, inferTypeFromEnum([]any{1.5}))
	assert.Equal(t, types.TypeBoolean, inferTypeFromEnum([]any{true}))
	assert.Equal(t, types.TypeString, inferTypeFromEnum([]any{struct{}{}}))
}

func TestFirstNonNullType(t *testing.T) {
	assert.Equal(t, "string", firstNonNullType([]string{"null", "string"}))
	assert.Equal(t, "", firstNonNullType([]string{"null"}))
	assert.Equal(t, "", firstNonNullType(nil))
}

func TestShortName(t *testing.T) {
	assert.Equal(t, "Foo", shortName("#/components/schemas/Foo"))
	assert.Equal(t, "bare", shortName("bare"))
	assert.Equal(t, "", shortName(""))
}

func TestAppendUnique(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, appendUnique([]string{"a"}, "b"))
	assert.Equal(t, []string{"a"}, appendUnique([]string{"a"}, "a"))
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

func TestIsAllNullType(t *testing.T) {
	assert.False(t, isAllNullType(nil))
	assert.True(t, isAllNullType([]string{"null"}))
	assert.True(t, isAllNullType([]string{"NULL", "Null"}))
	assert.False(t, isAllNullType([]string{"null", "string"}))
}

func TestDecodeNode(t *testing.T) {
	assert.Nil(t, decodeNode(nil))
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"}
	assert.EqualValues(t, 5, decodeNode(n))
}

func TestDecodeNodes(t *testing.T) {
	assert.Nil(t, decodeNodes(nil))
	out := decodeNodes([]*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "a"},
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "b"},
	})
	assert.Equal(t, []any{"a", "b"}, out)
}

func TestPickSuccessCode_FallthroughBranches(t *testing.T) {
	items := func(codes ...int) map[int]*schema.ResponseItem {
		out := map[int]*schema.ResponseItem{}
		for _, c := range codes {
			out[c] = &schema.ResponseItem{StatusCode: c}
		}
		return out
	}
	assert.Equal(t, 200, pickSuccessCode(items(200, 404, 500)))
	assert.Equal(t, 302, pickSuccessCode(items(302, 500)))
	assert.Equal(t, 404, pickSuccessCode(items(404, 500)))
	assert.Equal(t, 503, pickSuccessCode(items(503)))
	assert.Equal(t, 0, pickSuccessCode(items()))
}

func TestParseStatusCode(t *testing.T) {
	cases := []struct {
		in   string
		code int
		ok   bool
	}{
		{"200", 200, true},
		{"404", 404, true},
		{"2XX", 200, true},
		{"3xx", 300, true},
		{"4XX", 400, true},
		{"5XX", 500, true},
		{"default", 0, false},
		{"", 0, false},
		{"abc", 0, false},
		{"6XX", 0, false},
	}
	for _, c := range cases {
		code, ok := parseStatusCode(c.in)
		assert.Equal(t, c.ok, ok, "input=%q", c.in)
		if ok {
			assert.Equal(t, c.code, code, "input=%q", c.in)
		}
	}
}

func TestIsMediaTypeJSON(t *testing.T) {
	cases := map[string]bool{
		"application/json":         true,
		"application/vnd.api+json": true,
		"application/xml":          false,
		"text/plain":               false,
		"":                         false,
	}
	for mt, want := range cases {
		assert.Equal(t, want, isMediaTypeJSON(mt), "media-type %q", mt)
	}
}
