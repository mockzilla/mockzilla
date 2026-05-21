package libopenapi

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSpec loads a YAML/JSON spec from the project tree relative to the
// caller; used by all the table tests in this file to keep fixtures off
// disk-local to the package.
func readSpec(t *testing.T, rel string) []byte {
	t.Helper()
	bytes, err := os.ReadFile(rel)
	require.NoError(t, err, "reading %s", rel)
	return bytes
}

func TestNewRegistry_BuildsOperationIndex(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	routes := reg.GetRouteInfo()
	assert.Len(t, routes, 3, "petstore declares three operations")

	op := reg.FindOperation("/pets", "GET")
	require.NotNil(t, op)
	assert.Equal(t, "listPets", op.ID)
	assert.Equal(t, "GET", op.Method)
	require.NotNil(t, op.Response)
	assert.Equal(t, 200, op.Response.SuccessCode)

	success := op.Response.GetSuccess()
	require.NotNil(t, success)
	require.NotNil(t, success.Content)
	assert.Equal(t, types.TypeArray, success.Content.Type)
	require.NotNil(t, success.Content.Items)
	assert.Equal(t, types.TypeObject, success.Content.Items.Type)
}

func TestNewRegistry_LazyDoesNotPreConvert(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{
		SpecOptions: &config.SpecOptions{LazyLoad: true},
	})
	require.NoError(t, err)

	assert.Empty(t, reg.Operations(), "lazy mode should not pre-convert any op")

	op := reg.FindOperation("/pets/{petId}", "GET")
	require.NotNil(t, op)
	assert.Equal(t, "getPet", op.ID)

	assert.Len(t, reg.Operations(), 1, "first FindOperation should cache one op")
}

func TestNewRegistry_EagerPreConvertsAll(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{
		SpecOptions: &config.SpecOptions{LazyLoad: false},
	})
	require.NoError(t, err)

	assert.Len(t, reg.Operations(), 3, "eager mode pre-converts every op")
}

func TestNewRegistry_MissingOperationReturnsNil(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	assert.Nil(t, reg.FindOperation("/no-such-path", "GET"))
	assert.Nil(t, reg.GetResponseSchema("/no-such-path", "GET"))
}

func TestNewRegistry_MethodCaseInsensitive(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	upper := reg.FindOperation("/pets", "GET")
	lower := reg.FindOperation("/pets", "get")
	require.NotNil(t, upper)
	assert.Same(t, upper, lower, "method case must not change the entry returned")
}

func TestNewRegistry_DocumentProviderShared(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	doc, err := reg.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestNewRegistry_RejectsInvalidSpec(t *testing.T) {
	_, err := NewRegistry([]byte("this: is: not: valid: openapi"), Options{})
	require.Error(t, err)
}

func TestNewRegistry_LoggerPropagates(t *testing.T) {
	spec := readSpec(t, "../../../../internal/portable/testdata/petstore.yml")
	reg, err := NewRegistry(spec, Options{Logger: slog.Default()})
	require.NoError(t, err)
	doc, err := reg.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestNewRegistry_SimplifyAndOptionalProperties(t *testing.T) {
	spec := readSpec(t, "../../../../internal/portable/testdata/petstore.yml")
	reg, err := NewRegistry(spec, Options{
		SpecOptions: &config.SpecOptions{
			Simplify: true,
			OptionalProperties: &config.OptionalProperties{
				Min: 0,
				Max: 1,
			},
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, reg)
}

func TestNewRegistry_EmptyPaths(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths: {}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	assert.Empty(t, reg.GetRouteInfo())
	assert.Empty(t, reg.Operations())
}

func TestRegistry_UniqueOperationID_EmptyStringPassthrough(t *testing.T) {
	r := &Registry{}
	assert.Equal(t, "", r.uniqueOperationID(""))
}

func TestRegistry_UniqueOperationID_DuplicateCounter(t *testing.T) {
	r := &Registry{}
	assert.Equal(t, "id", r.uniqueOperationID("id"))
	assert.Equal(t, "id2", r.uniqueOperationID("id"))
	assert.Equal(t, "id3", r.uniqueOperationID("id"))
}

func TestRegistry_ConvertResponseItem_NilResponse(t *testing.T) {
	r := &Registry{staticResponses: map[staticResponseKey]string{}}
	ctx := newConvertCtx()
	item := r.convertResponseItem(204, nil, ctx)
	require.NotNil(t, item)
	assert.Equal(t, 204, item.StatusCode)
}

func TestRegistry_ConvertResponses_NilResponsesFabricates204(t *testing.T) {
	r := &Registry{staticResponses: map[staticResponseKey]string{}}
	resp := r.convertResponses(nil, "GET", "/", newConvertCtx())
	require.NotNil(t, resp, "nil responses should still yield a Response with fabricated 204")
	assert.Equal(t, 204, resp.SuccessCode)
	assert.NotNil(t, resp.GetSuccess())
}

func TestRegistry_ConvertResponses_Only3xxFabricates204(t *testing.T) {
	// 3xx-only operations must not surface the real Location header on the
	// synthesised 204 entry, or clients would chase a redirect loop.
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /redir:
    get:
      responses:
        '302':
          description: redirected
          headers:
            Location:
              schema: {type: string}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/redir", "GET")
	require.NotNil(t, op)
	assert.Equal(t, 204, op.Response.SuccessCode, "no 2xx declared => SuccessCode synthesised as 204")
	require.NotNil(t, op.Response.GetSuccess())
	assert.Empty(t, op.Response.GetSuccess().Headers, "synthesised 204 must not surface 3xx Location header")
	require.NotNil(t, op.Response.GetResponse(302), "the real 302 entry stays available for factory.SuccessStatusCode")
}

func TestRegistry_ConvertResponses_DefaultResponseInheritedIntoSyntheticEntry(t *testing.T) {
	// default-only operations need the default's content-type to flow into
	// the synthetic 204 so the response writer picks the right content-type
	// instead of defaulting to application/json.
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /timezone.txt:
    get:
      responses:
        default:
          description: list of timezones
          content:
            text/plain:
              schema: {type: string}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/timezone.txt", "GET")
	require.NotNil(t, op)
	require.NotNil(t, op.Response.GetSuccess())
	assert.Equal(t, "text/plain", op.Response.GetSuccess().ContentType)
}
