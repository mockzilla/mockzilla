package libopenapi

import (
	"log/slog"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_LoggerPropagates(t *testing.T) {
	spec := readSpec(t, "../../../internal/portable/testdata/petstore.yml")
	reg, err := NewRegistry(spec, Options{Logger: slog.Default()})
	require.NoError(t, err)
	provider := reg.(*Registry)
	doc, err := provider.Document()
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestNewRegistry_SimplifyAndOptionalProperties(t *testing.T) {
	spec := readSpec(t, "../../../internal/portable/testdata/petstore.yml")
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

func TestEmptyRegistry_NoPaths(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths: {}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	assert.Empty(t, reg.GetRouteInfo())
	assert.Empty(t, reg.Operations())
}

func TestUniqueOperationID_EmptyStringPassthrough(t *testing.T) {
	r := &Registry{}
	assert.Equal(t, "", r.uniqueOperationID(""))
}

func TestUniqueOperationID_DuplicateCounter(t *testing.T) {
	r := &Registry{}
	assert.Equal(t, "id", r.uniqueOperationID("id"))
	assert.Equal(t, "id2", r.uniqueOperationID("id"))
	assert.Equal(t, "id3", r.uniqueOperationID("id"))
}

func TestConvertParameters_NilEntriesIgnored(t *testing.T) {
	ctx := newConvertCtx()
	pathS, hdr, q := convertParameters(nil, ctx)
	assert.Nil(t, pathS)
	assert.Nil(t, hdr)
	assert.Nil(t, q)
}

func TestConvertResponseItem_NilResponse(t *testing.T) {
	r := &Registry{staticResponses: map[staticResponseKey]string{}}
	ctx := newConvertCtx()
	item := r.convertResponseItem(204, nil, ctx)
	require.NotNil(t, item)
	assert.Equal(t, 204, item.StatusCode)
}

func TestConvertResponses_NilResponsesFabricates204(t *testing.T) {
	r := &Registry{staticResponses: map[staticResponseKey]string{}}
	resp := r.convertResponses(nil, "GET", "/", newConvertCtx())
	require.NotNil(t, resp, "nil responses should still yield a Response with fabricated 204")
	assert.Equal(t, 204, resp.SuccessCode)
	assert.NotNil(t, resp.GetSuccess())
}

func TestConvertResponses_Only3xxFabricates204(t *testing.T) {
	// When only a 3xx response is declared, op.Response.SuccessCode
	// becomes 204 (synthetic) so the handler doesn't pull a real 3xx
	// entry's Location header into the response and create a redirect
	// loop in clients.
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

func TestConvertResponses_DefaultResponseInheritedIntoSyntheticEntry(t *testing.T) {
	// Specs that declare only a `default` response (e.g. worldtimeapi)
	// need the default's content-type to flow into the synthetic 204 so
	// the response writer picks the right content-type instead of
	// defaulting to application/json.
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

func TestConvertProxy_CycleEmitsRecursiveMarker(t *testing.T) {
	// Direct self-reference must not produce an infinite tree.
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
              schema:
                $ref: "#/components/schemas/Node"
components:
  schemas:
    Node:
      type: object
      properties:
        next: {$ref: "#/components/schemas/Node"}
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s)
	require.NotNil(t, s.Properties)
	require.Contains(t, s.Properties, "next")
	assert.True(t, s.Properties["next"].Recursive, "self-ref should be marked Recursive")
}

func TestPickContent_Empty(t *testing.T) {
	mt, ct := pickContent(nil)
	assert.Equal(t, "", mt)
	assert.Nil(t, ct)
}

func TestApplyDiscriminator_NoOpWithoutOneOf(t *testing.T) {
	properties := map[string]*schema.Schema{}
	s := &base.Schema{Discriminator: &base.Discriminator{PropertyName: "kind"}}
	applyDiscriminator(properties, s, &composedShape{})
	assert.NotContains(t, properties, "kind")
}

func TestApplyDiscriminator_NoMappingReturnsEarly(t *testing.T) {
	// composedShape present but discriminator has no mapping entries.
	properties := map[string]*schema.Schema{
		"kind": {Type: types.TypeString},
	}
	s := &base.Schema{Discriminator: &base.Discriminator{PropertyName: "kind"}}
	composed := &composedShape{}
	applyDiscriminator(properties, s, composed)
	assert.Empty(t, properties["kind"].Enum, "no mapping -> no enum injection")
}

func TestDiscriminatorValueFor_NoMapping(t *testing.T) {
	assert.Equal(t, "", discriminatorValueFor(nil, nil))
	assert.Equal(t, "", discriminatorValueFor(&base.Discriminator{}, nil))
}

func TestFirstNonNullBranch_AllNullAndNil(t *testing.T) {
	assert.Nil(t, firstNonNullBranch(nil))
	assert.Nil(t, firstNonNullBranch([]*base.SchemaProxy{nil}))
}

func TestFirstPatternFromBranches_NilEntries(t *testing.T) {
	assert.Equal(t, "", firstPatternFromBranches(nil))
	assert.Equal(t, "", firstPatternFromBranches([]*base.SchemaProxy{nil}))
}

func TestComposeSchema_NoCompositionReturnsNil(t *testing.T) {
	assert.Nil(t, composeSchema(nil, newConvertCtx()))
	assert.Nil(t, composeSchema(&base.Schema{}, newConvertCtx()))
}

func TestConvertSchema_NilReturnsNil(t *testing.T) {
	assert.Nil(t, convertSchema(nil, newConvertCtx()))
}

func TestConvertProxy_NilProxyReturnsNil(t *testing.T) {
	assert.Nil(t, convertProxy(nil, newConvertCtx()))
}

func TestConvertProperties_NilProperties(t *testing.T) {
	props, nullOnly := convertProperties(nil, newConvertCtx())
	assert.Empty(t, props)
	assert.Empty(t, nullOnly)
}

func TestConvertRequestBodyEncoding_Nil(t *testing.T) {
	assert.Nil(t, convertRequestBodyEncoding(nil))
}

func TestConvertParameterEncoding_NilOrUnstyled(t *testing.T) {
	assert.Nil(t, convertParameterEncoding(nil))
}

func TestOneOfPrimitiveEnumRecovery(t *testing.T) {
	// oneOf branches with primitive enums should yield the enum value list
	// when the outer schema only declares a type.
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
              schema:
                type: string
                oneOf:
                  - type: string
                    enum: ["x", "y"]
                  - type: string
                    enum: ["z"]
`
	s := loadResponseSchema(t, spec, "/", "GET")
	require.NotNil(t, s)
	assert.Equal(t, types.TypeString, s.Type)
	require.Len(t, s.Enum, 2, "first branch's enum wins")
	keys := mapToStrings(s.Enum)
	assert.ElementsMatch(t, []string{"x", "y"}, keys)
}

func TestAllOfMergeArrayItems(t *testing.T) {
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
              schema:
                allOf:
                  - type: array
                    items:
                      type: integer
`
	s := loadResponseSchema(t, spec, "/", "GET")
	assert.Equal(t, types.TypeArray, s.Type)
	require.NotNil(t, s.Items)
	assert.Equal(t, types.TypeInteger, s.Items.Type)
}

func TestNonJSONContentSelectsFirst(t *testing.T) {
	spec := `openapi: 3.0.0
info: {title: t, version: 1}
paths:
  /:
    get:
      responses:
        "200":
          description: ok
          content:
            text/plain:
              schema: {type: string}
            text/csv:
              schema: {type: string}
`
	reg, err := NewRegistry([]byte(spec), Options{})
	require.NoError(t, err)
	op := reg.FindOperation("/", "GET")
	require.NotNil(t, op)
	success := op.Response.GetSuccess()
	require.NotNil(t, success)
	assert.Contains(t, []string{"text/plain", "text/csv"}, success.ContentType)
}
