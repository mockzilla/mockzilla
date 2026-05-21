package libopenapi

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperation_ParametersGroupedByLocation(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: integer}
        - name: token
          in: header
          required: true
          schema: {type: string}
        - name: include
          in: query
          required: false
          schema: {type: string, enum: [profile, billing]}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: object}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/users/{id}", "GET")
	require.NotNil(t, op)

	require.NotNil(t, op.PathParams)
	assert.Equal(t, types.TypeObject, op.PathParams.Type)
	assert.Contains(t, op.PathParams.Properties, "id")
	assert.ElementsMatch(t, []string{"id"}, op.PathParams.Required)

	require.NotNil(t, op.Headers)
	assert.Contains(t, op.Headers.Properties, "token")

	require.NotNil(t, op.Query)
	include, ok := op.Query["include"]
	require.True(t, ok)
	assert.False(t, include.Required)
	require.NotNil(t, include.Schema)
	assert.ElementsMatch(t, []string{"profile", "billing"}, mapToStrings(include.Schema.Enum))
}

func TestOperation_BodyJSONNormalisation(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    post:
      requestBody:
        content:
          application/vnd.api+json:
            schema: {type: object}
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "POST")
	require.NotNil(t, op)
	assert.Equal(t, "application/json", op.ContentType, "JSON-shaped media types normalise to application/json")
}

func TestOperation_BodyNonJSONPreserved(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    post:
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema: {type: object}
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "POST")
	require.NotNil(t, op)
	assert.Equal(t, "application/x-www-form-urlencoded", op.ContentType)
}

func TestOperation_SyntheticIDForOperationsWithoutOperationId(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /widgets/{id}:
    get:
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/widgets/{id}", "GET")
	require.NotNil(t, op)
	assert.Equal(t, "get__widgets_id", op.ID, "synthesised ID matches typedef/registry.go's format")
}

func TestOperation_DuplicateSynthesisedIDsDisambiguated(t *testing.T) {
	// Without operationIds, two operations colliding on the synthesised ID
	// should not share the same op.ID after registration.
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /a:
    get:
      responses:
        "200": {description: ok}
    post:
      responses:
        "200": {description: ok}
  /b:
    get:
      responses:
        "200": {description: ok}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, route := range reg.GetRouteInfo() {
		op := reg.FindOperation(route.Path, route.Method)
		require.NotNil(t, op)
		assert.False(t, seen[op.ID], "operation ID %q was repeated", op.ID)
		seen[op.ID] = true
	}
}

func TestOperation_StatusCodeSelection(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "404": {description: missing}
        "201": {description: created}
        "500": {description: oops}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	require.NotNil(t, op.Response)
	assert.Equal(t, 201, op.Response.SuccessCode, "lowest declared 2xx wins")
}

func TestOperation_StaticResponseWired(t *testing.T) {
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
              x-static-response: '{"ping":"pong"}'
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	success := op.Response.GetSuccess()
	require.NotNil(t, success)
	require.NotNil(t, success.Content)
	assert.Equal(t, `{"ping":"pong"}`, success.Content.StaticContent)
}

func TestOperation_ResponseHeaders(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          headers:
            X-Trace:
              schema: {type: string}
          content:
            application/json:
              schema: {type: object}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	success := op.Response.GetSuccess()
	require.NotNil(t, success)
	require.Contains(t, success.Headers, "X-Trace")
	assert.Equal(t, types.TypeString, success.Headers["X-Trace"].Type)
}

func TestOperation_GetResponseSchema(t *testing.T) {
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
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	rs := reg.GetResponseSchema("/", "GET")
	require.NotNil(t, rs)
	assert.Equal(t, "application/json", rs.ContentType)
	require.NotNil(t, rs.Body)
	assert.Equal(t, types.TypeObject, rs.Body.Type)
}

func mapToStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, scalarKey(v))
	}
	return out
}

// guard so the import of schema stays used when no other test in this file
// touches it directly through a method.
var _ = (&schema.Operation{})
