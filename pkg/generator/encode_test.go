package generator

import (
	"encoding/json"
	"encoding/xml"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

func TestEncodeContent(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("nil content returns nil", func(t *testing.T) {
		result, err := encodeContent(nil, "application/json")
		assert.NoError(err)
		assert.Nil(result)
	})

	t.Run("application/json encoding", func(t *testing.T) {
		content := map[string]any{
			"name": "John",
			"age":  30,
		}
		result, err := encodeContent(content, "application/json")
		assert.NoError(err)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("John", decoded["name"])
		assert.Equal(float64(30), decoded["age"])
	})

	t.Run("application/x-www-form-urlencoded with data", func(t *testing.T) {
		content := map[string]any{
			"username": "john",
			"password": "secret",
		}
		result, err := encodeContent(content, "application/x-www-form-urlencoded")
		assert.NoError(err)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("john", decoded["username"])
		assert.Equal("secret", decoded["password"])
	})

	t.Run("application/x-www-form-urlencoded with empty object", func(t *testing.T) {
		content := map[string]any{}
		result, err := encodeContent(content, "application/x-www-form-urlencoded")
		assert.NoError(err)
		assert.Equal([]byte(""), result)
	})

	t.Run("multipart/form-data with data", func(t *testing.T) {
		content := map[string]any{
			"file": "data",
		}
		result, err := encodeContent(content, "multipart/form-data")
		assert.NoError(err)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("data", decoded["file"])
	})

	t.Run("multipart/form-data with empty object", func(t *testing.T) {
		content := map[string]any{}
		result, err := encodeContent(content, "multipart/form-data")
		assert.NoError(err)
		assert.Equal([]byte(""), result)
	})

	t.Run("application/xml encoding", func(t *testing.T) {
		type Person struct {
			Name string `xml:"name"`
			Age  int    `xml:"age"`
		}
		content := Person{Name: "John", Age: 30}
		result, err := encodeContent(content, "application/xml")
		assert.NoError(err)

		var decoded Person
		err = xml.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("John", decoded.Name)
		assert.Equal(30, decoded.Age)
	})

	t.Run("application/xml encodes map with root name", func(t *testing.T) {
		content := map[string]any{
			"eva":     8000105,
			"station": "Frankfurt",
		}
		result, err := encodeContent(content, "application/xml", "timetable")
		assert.NoError(err)
		assert.Equal(
			`<timetable><eva>8000105</eva><station>Frankfurt</station></timetable>`,
			string(result),
		)
	})

	t.Run("application/xml encodes nested map and array", func(t *testing.T) {
		content := map[string]any{
			"eva": 1,
			"s": []any{
				map[string]any{"id": "a"},
				map[string]any{"id": "b"},
			},
		}
		result, err := encodeContent(content, "application/xml", "timetable")
		assert.NoError(err)
		assert.Equal(
			`<timetable><eva>1</eva><s><id>a</id></s><s><id>b</id></s></timetable>`,
			string(result),
		)
	})

	t.Run("application/xml escapes special characters", func(t *testing.T) {
		content := map[string]any{"msg": "<hi> & bye"}
		result, err := encodeContent(content, "application/xml", "root")
		assert.NoError(err)
		assert.Equal(`<root><msg>&lt;hi&gt; &amp; bye</msg></root>`, string(result))
	})

	t.Run("application/xml falls back to xml.Marshal without root name", func(t *testing.T) {
		content := map[string]any{"k": "v"}
		_, err := encodeContent(content, "application/xml")
		assert.Error(err)
		assert.Contains(err.Error(), "unsupported type")
	})

	t.Run("application/x-yaml encoding", func(t *testing.T) {
		content := map[string]any{
			"name": "John",
			"age":  30,
		}
		result, err := encodeContent(content, "application/x-yaml")
		assert.NoError(err)

		var decoded map[string]any
		err = yaml.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("John", decoded["name"])
		assert.Equal(30, decoded["age"])
	})

	t.Run("unknown content type with byte slice", func(t *testing.T) {
		content := []byte("raw data")
		result, err := encodeContent(content, "text/plain")
		assert.NoError(err)
		assert.Equal([]byte("raw data"), result)
	})

	t.Run("unknown content type with string", func(t *testing.T) {
		content := "plain text"
		result, err := encodeContent(content, "text/plain")
		assert.NoError(err)
		assert.Equal([]byte("plain text"), result)
	})

	t.Run("unknown content type with unsupported type", func(t *testing.T) {
		content := 12345
		result, err := encodeContent(content, "text/plain")
		assert.Error(err)
		assert.Nil(result)
		assert.Contains(err.Error(), "cannot encode type int")
		assert.Contains(err.Error(), "text/plain")
	})

	t.Run("empty content type defaults to JSON", func(t *testing.T) {
		content := map[string]any{"key": "value"}
		result, err := encodeContent(content, "")
		assert.NoError(err)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("value", decoded["key"])
	})

	t.Run("multipart/formdata variant", func(t *testing.T) {
		content := map[string]any{"field": "data"}
		result, err := encodeContent(content, "multipart/formdata")
		assert.NoError(err)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("data", decoded["field"])
	})

	t.Run("form-data with unmarshalable content returns error", func(t *testing.T) {
		// json.Marshal fails on channels
		content := make(chan int)
		result, err := encodeContent(content, "application/x-www-form-urlencoded")
		assert.Error(err)
		assert.Nil(result)
	})

	t.Run("json with unmarshalable content returns error", func(t *testing.T) {
		// json.Marshal fails on channels
		content := make(chan int)
		result, err := encodeContent(content, "application/json")
		assert.Error(err)
		assert.Nil(result)
	})

	t.Run("json.RawMessage passes through for text/html", func(t *testing.T) {
		content := json.RawMessage("<html><body>hi</body></html>")
		result, err := encodeContent(content, "text/html")
		assert.NoError(err)
		assert.Equal([]byte("<html><body>hi</body></html>"), result)
	})

	t.Run("json.RawMessage passes through for application/xml", func(t *testing.T) {
		content := json.RawMessage(`<root><name>John</name></root>`)
		result, err := encodeContent(content, "application/xml")
		assert.NoError(err)
		assert.Equal([]byte(`<root><name>John</name></root>`), result)
	})

	t.Run("json.RawMessage passes through for application/json", func(t *testing.T) {
		content := json.RawMessage(`{"name":"John"}`)
		result, err := encodeContent(content, "application/json")
		assert.NoError(err)
		assert.Equal([]byte(`{"name":"John"}`), result)
	})

	t.Run("json.RawMessage passes through for text/plain", func(t *testing.T) {
		content := json.RawMessage("plain text body")
		result, err := encodeContent(content, "text/plain")
		assert.NoError(err)
		assert.Equal([]byte("plain text body"), result)
	})
}
