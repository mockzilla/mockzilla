package inspect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	t.Run("returns title, version, and endpoint counts", func(t *testing.T) {
		spec := []byte(`
openapi: 3.0.3
info:
  title: Petstore
  version: "1.0"
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        '200':
          description: ok
    post:
      operationId: createPet
      responses:
        '201':
          description: created
  /pets/{id}:
    get:
      operationId: getPet
      responses:
        '200':
          description: ok
`)

		summary, err := Summarize(spec)
		require.NoError(t, err)

		assert.Equal(t, "Petstore", summary.Title)
		assert.Equal(t, "1.0", summary.Version)
		assert.Equal(t, "3.0.3", summary.OpenAPIVersion)
		assert.Equal(t, 3, summary.EndpointCount)
		assert.Len(t, summary.Paths, 3)

		// Methods are upper-cased; operation IDs are preserved.
		methods := map[string]string{}
		for _, ep := range summary.Paths {
			methods[ep.OperationID] = ep.Method
		}
		assert.Equal(t, "GET", methods["listPets"])
		assert.Equal(t, "POST", methods["createPet"])
		assert.Equal(t, "GET", methods["getPet"])
	})

	t.Run("handles spec with no paths", func(t *testing.T) {
		spec := []byte(`
openapi: 3.1.0
info:
  title: Empty
  version: "0.1"
paths: {}
`)
		summary, err := Summarize(spec)
		require.NoError(t, err)
		assert.Equal(t, 0, summary.EndpointCount)
		assert.Empty(t, summary.Paths)
		assert.Equal(t, "3.1.0", summary.OpenAPIVersion)
	})

	t.Run("returns an error on unparseable input", func(t *testing.T) {
		_, err := Summarize([]byte("not: a valid: openapi: doc:"))
		assert.Error(t, err)
	})
}
