package generator

import (
	"net/http"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	assert2 "github.com/stretchr/testify/assert"
)

func TestGenerateHeaders(t *testing.T) {
	assert := assert2.New(t)

	t.Run("basic case", func(t *testing.T) {
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "x-rate-limit-limit":
				return 100
			case "x-rate-limit-remaining":
				return 80
			}
			return nil
		}

		headers := map[string]*schema.Schema{
			"X-Rate-Limit-Limit":     createSchemaFromString(t, `{"type": "integer"}`),
			"X-Rate-Limit-Remaining": createSchemaFromString(t, `{"type": "integer"}`),
		}

		expected := http.Header{
			"X-Rate-Limit-Limit":     []string{"100"},
			"X-Rate-Limit-Remaining": []string{"80"},
		}

		res := generateHeaders(headers, valueReplacer)
		assert.Equal(expected, res)
	})

	t.Run("filters out transport-managed headers", func(t *testing.T) {
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "x-custom-header":
				return "custom-value"
			case "content-encoding":
				return "gzip"
			case "content-length":
				return "1234"
			case "transfer-encoding":
				return "chunked"
			}
			return nil
		}

		headers := map[string]*schema.Schema{
			"X-Custom-Header":   createSchemaFromString(t, `{"type": "string"}`),
			"Content-Encoding":  createSchemaFromString(t, `{"type": "string"}`),
			"Content-Length":    createSchemaFromString(t, `{"type": "string"}`),
			"Transfer-Encoding": createSchemaFromString(t, `{"type": "string"}`),
		}

		res := generateHeaders(headers, valueReplacer)

		// Transport-managed headers should be filtered out
		assert.Equal("", res.Get("Content-Encoding"))
		assert.Equal("", res.Get("content-encoding"))
		assert.Equal("", res.Get("Content-Length"))
		assert.Equal("", res.Get("content-length"))
		assert.Equal("", res.Get("Transfer-Encoding"))
		assert.Equal("", res.Get("transfer-encoding"))

		// Other headers should be present
		assert.Equal("custom-value", res.Get("X-Custom-Header"))
	})
}
