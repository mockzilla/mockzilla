package libopenapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticResponse_ExtractedAcrossCodes(t *testing.T) {
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
              schema: {type: object}
              x-static-response: '{"v":"200"}'
        "404":
          description: missing
          content:
            application/json:
              schema: {type: object}
              x-static-response: '{"v":"404"}'
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)

	require.NotNil(t, op.Response.GetResponse(200))
	require.NotNil(t, op.Response.GetResponse(200).Content)
	assert.Equal(t, `{"v":"200"}`, op.Response.GetResponse(200).Content.StaticContent)

	require.NotNil(t, op.Response.GetResponse(404))
	require.NotNil(t, op.Response.GetResponse(404).Content)
	assert.Equal(t, `{"v":"404"}`, op.Response.GetResponse(404).Content.StaticContent)
}

func TestStaticResponse_BlankExtensionIgnored(t *testing.T) {
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
              schema: {type: object}
              x-static-response: "   "
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	require.NotNil(t, op.Response.GetResponse(200))
	require.NotNil(t, op.Response.GetResponse(200).Content)
	assert.Empty(t, op.Response.GetResponse(200).Content.StaticContent)
}

func TestStaticResponse_NonNumericCodeSkipped(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        default:
          description: anything
          content:
            application/json:
              schema: {type: object}
              x-static-response: '{"never":"applied"}'
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	require.NotNil(t, op.Response.GetResponse(200))
	assert.Empty(t, op.Response.GetResponse(200).Content.StaticContent)
}
