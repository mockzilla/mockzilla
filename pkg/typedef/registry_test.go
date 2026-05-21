package typedef

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	pblibopenapi "github.com/pb33f/libopenapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const minimalSpec = `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: array, items: {type: string}}
`

func TestNewRegistry_ForwardsAndImplementsInterfaces(t *testing.T) {
	reg, err := NewRegistry([]byte(minimalSpec), RegistryOptions{})
	require.NoError(t, err)
	require.NotNil(t, reg)

	op := reg.FindOperation("/pets", "GET")
	require.NotNil(t, op)
	assert.Equal(t, "listPets", op.ID)

	routes := reg.GetRouteInfo()
	require.Len(t, routes, 1)
	assert.Equal(t, "GET", routes[0].Method)
	assert.Equal(t, "/pets", routes[0].Path)

	dp, ok := reg.(DocumentProvider)
	require.True(t, ok, "registry must satisfy DocumentProvider")
	doc, err := dp.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestNewRegistry_RejectsInvalidSpec(t *testing.T) {
	_, err := NewRegistry([]byte("this: is: not: valid: openapi"), RegistryOptions{})
	require.Error(t, err)
}

func TestBuildModel_Forwards(t *testing.T) {
	doc, err := pblibopenapi.NewDocument([]byte(minimalSpec))
	require.NoError(t, err)

	model, err := BuildModel(doc, true, &config.OptionalProperties{Min: 0, Max: 0})
	require.NoError(t, err)
	require.NotNil(t, model)
	require.NotNil(t, model.Paths)

	pathItem, ok := model.Paths.PathItems.Get("/pets")
	require.True(t, ok)
	assert.NotNil(t, pathItem.Get)
}
