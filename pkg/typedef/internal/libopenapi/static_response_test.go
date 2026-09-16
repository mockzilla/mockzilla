package libopenapi

import (
	"fmt"
	"strconv"
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

func TestStaticResponse_HeaderValue(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "201":
          description: created
          headers:
            Location:
              schema: {type: string}
              x-static-response: /users/42
            X-Generated:
              schema: {type: string}
            X-Blank:
              schema: {type: string}
              x-static-response: "   "
          content:
            application/json:
              schema: {type: object}
              x-static-response: '{"id":42}'
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)

	headers := op.Response.GetResponse(201).Headers
	require.NotNil(t, headers["Location"])
	assert.Equal(t, "/users/42", headers["Location"].StaticContent)
	require.NotNil(t, headers["X-Generated"])
	assert.Empty(t, headers["X-Generated"].StaticContent)
	require.NotNil(t, headers["X-Blank"])
	assert.Empty(t, headers["X-Blank"].StaticContent)
}

func TestStaticResponse_BodilessMarker(t *testing.T) {
	const spec = `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    delete:
      responses:
        "%d":
          description: done
          x-static-response: true
          headers:
            X-Deleted:
              schema: {type: string}
              x-static-response: "yes"
`
	for _, code := range []int{204, 304} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			reg, err := NewRegistry([]byte(fmt.Sprintf(spec, code)), Options{})
			require.NoError(t, err)
			require.True(t, reg.GetRouteInfo()[0].IsStatic)

			op := reg.FindOperation("/", "DELETE")
			require.NotNil(t, op)
			assert.Equal(t, code, op.Response.SuccessCode)

			success := op.Response.GetSuccess()
			require.NotNil(t, success)
			assert.Nil(t, success.Content)
			assert.Equal(t, "yes", success.Headers["X-Deleted"].StaticContent)
		})
	}
}
