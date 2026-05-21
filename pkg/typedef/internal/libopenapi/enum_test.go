package libopenapi

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
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

func TestInferTypeFromEnum(t *testing.T) {
	assert.Equal(t, types.TypeString, inferTypeFromEnum([]any{"a"}))
	assert.Equal(t, types.TypeInteger, inferTypeFromEnum([]any{int64(1)}))
	assert.Equal(t, types.TypeInteger, inferTypeFromEnum([]any{int(1)}))
	assert.Equal(t, types.TypeNumber, inferTypeFromEnum([]any{1.5}))
	assert.Equal(t, types.TypeBoolean, inferTypeFromEnum([]any{true}))
	assert.Equal(t, types.TypeString, inferTypeFromEnum([]any{struct{}{}}))
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
