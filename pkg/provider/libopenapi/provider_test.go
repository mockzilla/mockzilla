package libopenapi

import (
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
	spec := readSpec(t, filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

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
	spec := readSpec(t, filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

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
	spec := readSpec(t, filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{
		SpecOptions: &config.SpecOptions{LazyLoad: false},
	})
	require.NoError(t, err)

	assert.Len(t, reg.Operations(), 3, "eager mode pre-converts every op")
}

func TestNewRegistry_MissingOperationReturnsNil(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	assert.Nil(t, reg.FindOperation("/no-such-path", "GET"))
	assert.Nil(t, reg.GetResponseSchema("/no-such-path", "GET"))
}

func TestNewRegistry_MethodCaseInsensitive(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	upper := reg.FindOperation("/pets", "GET")
	lower := reg.FindOperation("/pets", "get")
	require.NotNil(t, upper)
	assert.Same(t, upper, lower, "method case must not change the entry returned")
}

func TestNewRegistry_DocumentProviderShared(t *testing.T) {
	spec := readSpec(t, filepath.Join("..", "..", "..", "internal", "portable", "testdata", "petstore.yml"))

	reg, err := NewRegistry(spec, Options{})
	require.NoError(t, err)

	provider, ok := reg.(*Registry)
	require.True(t, ok)
	doc, err := provider.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestNewRegistry_RejectsInvalidSpec(t *testing.T) {
	_, err := NewRegistry([]byte("this: is: not: valid: openapi"), Options{})
	require.Error(t, err)
}
