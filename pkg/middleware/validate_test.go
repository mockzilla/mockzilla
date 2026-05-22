package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validateTestSpec = `openapi: 3.0.0
info:
  title: Test
  version: 1.0.0
paths:
  /pets:
    post:
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/PetInput'
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Pet'
components:
  schemas:
    PetInput:
      type: object
      required: [name]
      properties:
        name:
          type: string
    Pet:
      type: object
      required: [id, name]
      properties:
        id:
          type: integer
        name:
          type: string
`

func newValidatorFromSpec(t *testing.T, spec string) validator.Validator {
	t.Helper()
	doc, err := libopenapi.NewDocument([]byte(spec))
	require.NoError(t, err)
	v, errs := validator.NewValidator(doc)
	require.Empty(t, errs)
	return v
}

func boolPtr(b bool) *bool { return &b }

func TestCreateValidationMiddleware(t *testing.T) {
	v := newValidatorFromSpec(t, validateTestSpec)

	// validHandler echoes a spec-compliant Pet body, allowing response
	// validation to pass.
	validHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"name":"rex"}`))
	})

	// invalidHandler returns a response that violates the Pet schema
	// (missing required `id`). Used to exercise the response validation
	// failure path.
	invalidHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"rex"}`))
	})

	tests := []struct {
		name       string
		cfg        *config.ValidateConfig
		handler    http.Handler
		reqBody    string
		wantStatus int
		wantBody   string // substring match
	}{
		{
			name:       "nil config: both validations off, invalid request passes through",
			cfg:        nil,
			handler:    validHandler,
			reqBody:    `{}`, // would fail request validation if it ran
			wantStatus: http.StatusOK,
			wantBody:   `"id":1`,
		},
		{
			name:       "request:true catches invalid request with 400",
			cfg:        &config.ValidateConfig{Request: boolPtr(true)},
			handler:    validHandler,
			reqBody:    `{}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `request validation failed`,
		},
		{
			name:       "request:true on valid request passes through",
			cfg:        &config.ValidateConfig{Request: boolPtr(true)},
			handler:    validHandler,
			reqBody:    `{"name":"rex"}`,
			wantStatus: http.StatusOK,
			wantBody:   `"id":1`,
		},
		{
			name:       "nil config: invalid response body passes through (response off by default)",
			cfg:        nil,
			handler:    invalidHandler,
			reqBody:    `{"name":"rex"}`,
			wantStatus: http.StatusOK,
			wantBody:   `"name":"rex"`,
		},
		{
			name:       "response:true catches invalid response body with 500",
			cfg:        &config.ValidateConfig{Response: boolPtr(true)},
			handler:    invalidHandler,
			reqBody:    `{"name":"rex"}`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `response validation failed`,
		},
		{
			name:       "response:false explicit (matches default); invalid body still passes",
			cfg:        &config.ValidateConfig{Response: boolPtr(false)},
			handler:    invalidHandler,
			reqBody:    `{"name":"rex"}`,
			wantStatus: http.StatusOK,
			wantBody:   `"name":"rex"`,
		},
		{
			name:       "both explicit false skips everything",
			cfg:        &config.ValidateConfig{Request: boolPtr(false), Response: boolPtr(false)},
			handler:    invalidHandler,
			reqBody:    `{}`,
			wantStatus: http.StatusOK,
			wantBody:   `"name":"rex"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.ServiceConfig{Name: "test", Validate: tc.cfg}
			params := newTestParams(cfg)
			mw := CreateValidationMiddleware(params, func() validator.Validator { return v }, nil)

			req := httptest.NewRequest(http.MethodPost, "/pets", strings.NewReader(tc.reqBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			mw(tc.handler).ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.wantBody)
		})
	}
}

func TestCreateValidationMiddleware_NilValidator(t *testing.T) {
	// Source returning nil means validation is silently skipped: a bad
	// spec at startup shouldn't make every request fail.
	params := newTestParams(nil)
	mw := CreateValidationMiddleware(params, func() validator.Validator { return nil }, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("nope"))
	})

	req := httptest.NewRequest(http.MethodPost, "/pets", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "nope", rec.Body.String())
}

func TestCreateValidationMiddleware_RequestBodyForwarded(t *testing.T) {
	// Request validation reads the body; the handler downstream must
	// still see the original bytes intact.
	v := newValidatorFromSpec(t, validateTestSpec)
	params := newTestParams(nil)
	mw := CreateValidationMiddleware(params, func() validator.Validator { return v }, nil)

	var got []byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"name":"rex"}`))
	})

	original := `{"name":"rex"}`
	req := httptest.NewRequest(http.MethodPost, "/pets", strings.NewReader(original))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)

	assert.Equal(t, original, string(got))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCreateValidationMiddleware_NonSuccessResponseSkipsValidation(t *testing.T) {
	// Error responses (4xx/5xx) often don't match the OpenAPI success
	// schema. The middleware should let them through unmodified rather
	// than wrap them in another 500.
	v := newValidatorFromSpec(t, validateTestSpec)
	params := newTestParams(nil)
	mw := CreateValidationMiddleware(params, func() validator.Validator { return v }, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	})

	req := httptest.NewRequest(http.MethodPost, "/pets", strings.NewReader(`{"name":"rex"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "upstream unavailable")
}

func TestValidationErrorPayload_Encoding(t *testing.T) {
	// Sanity check: the JSON shape contains both top-level message and
	// the detail list, with libopenapi-validator's ValidationError
	// fields preserved.
	v := newValidatorFromSpec(t, validateTestSpec)
	boolTrue := true
	params := newTestParams(&config.ServiceConfig{
		Name:     "test",
		Validate: &config.ValidateConfig{Request: &boolTrue},
	})
	mw := CreateValidationMiddleware(params, func() validator.Validator { return v }, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached on request failure")
	})

	req := httptest.NewRequest(http.MethodPost, "/pets", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mw(handler).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var payload validationErrorPayload
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	assert.Equal(t, "request validation failed", payload.Error)
	assert.NotEmpty(t, payload.Details)
}

func TestServicePrefix(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.ServiceConfig
		want string
	}{
		{"mount with leading slash", &config.ServiceConfig{Mount: "/foo/bar"}, "/foo/bar"},
		{"mount without leading slash", &config.ServiceConfig{Mount: "foo/bar"}, "/foo/bar"},
		{"name only", &config.ServiceConfig{Name: "pets"}, "/pets"},
		{"empty name and mount", &config.ServiceConfig{}, "/"},
		{"mount wins over name", &config.ServiceConfig{Name: "pets", Mount: "/v2"}, "/v2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, servicePrefix(tc.cfg))
		})
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		prefix string
		want   string
	}{
		{"exact match returns root", "/foo/bar", "/foo/bar", "/"},
		{"with trailing slash", "/foo/bar/", "/foo/bar", "/"},
		{"strips multi-segment", "/foo/bar/pets", "/foo/bar", "/pets"},
		{"single-segment prefix", "/pets/42", "/pets", "/42"},
		{"prefix not present", "/other/path", "/foo/bar", "/other/path"},
		{"mid-segment match left alone", "/foobar/pets", "/foo", "/foobar/pets"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, stripPrefix(tc.path, tc.prefix))
		})
	}
}

func TestAllPathMissing(t *testing.T) {
	pathMissing := &errors.ValidationError{ValidationType: "path", ValidationSubType: "missing"}
	schemaErr := &errors.ValidationError{ValidationType: "response", ValidationSubType: "schema"}

	t.Run("empty slice is not all-path-missing", func(t *testing.T) {
		assert.False(t, allPathMissing(nil))
	})
	t.Run("all path/missing", func(t *testing.T) {
		assert.True(t, allPathMissing([]*errors.ValidationError{pathMissing, pathMissing}))
	})
	t.Run("mixed types", func(t *testing.T) {
		assert.False(t, allPathMissing([]*errors.ValidationError{pathMissing, schemaErr}))
	})
}

func TestAllSchemaRenderFailure(t *testing.T) {
	render := &errors.ValidationError{
		Message: "200 response body for '/x' failed schema rendering",
		Reason:  "schema render failure, circular reference: `#/components/schemas/Foo`",
	}
	compile := &errors.ValidationError{
		Message: "200 response body for '/x' failed schema compilation",
		Reason:  "The response schema for status code '200' failed to compile: JSON schema compile failed: ...",
	}
	normal := &errors.ValidationError{
		Message: "200 response body for '/x' failed to validate schema",
		Reason:  "minProperties: got 0, want 1",
	}

	t.Run("empty slice is not all-render-failure", func(t *testing.T) {
		assert.False(t, allSchemaRenderFailure(nil))
	})
	t.Run("all render failures", func(t *testing.T) {
		assert.True(t, allSchemaRenderFailure([]*errors.ValidationError{render}))
	})
	t.Run("schema compile failure counts", func(t *testing.T) {
		assert.True(t, allSchemaRenderFailure([]*errors.ValidationError{compile}))
	})
	t.Run("mixed render and compile", func(t *testing.T) {
		assert.True(t, allSchemaRenderFailure([]*errors.ValidationError{render, compile}))
	})
	t.Run("real failure not classified", func(t *testing.T) {
		assert.False(t, allSchemaRenderFailure([]*errors.ValidationError{render, normal}))
	})
}

func TestAllUnsatisfiableSchema(t *testing.T) {
	const stream = `properties:
  syncCatalog:
    properties:
      streams:
        items:
          properties:
            stream:
              additionalProperties: false
              required:
                - json_schema
              properties:
                jsonSchema:
                  type: string
`
	missingRequired := &errors.ValidationError{
		Message: "200 response body failed",
		SchemaValidationErrors: []*errors.SchemaValidationFailure{{
			Reason:          "missing property 'json_schema'",
			KeywordLocation: "/properties/syncCatalog/properties/streams/items/properties/stream/required",
			ReferenceSchema: stream,
		}},
	}
	additionalProperty := &errors.ValidationError{
		Message: "200 response body failed",
		SchemaValidationErrors: []*errors.SchemaValidationFailure{{
			Reason:          "additional properties 'json_schema' not allowed",
			KeywordLocation: "/properties/syncCatalog/properties/streams/items/properties/stream/additionalProperties",
			ReferenceSchema: stream,
		}},
	}
	otherError := &errors.ValidationError{
		Message: "200 response body failed",
		SchemaValidationErrors: []*errors.SchemaValidationFailure{{
			Reason:          "minProperties: got 0, want 1",
			KeywordLocation: "/properties/syncCatalog/minProperties",
			ReferenceSchema: stream,
		}},
	}

	t.Run("empty slice is not unsatisfiable", func(t *testing.T) {
		assert.False(t, allUnsatisfiableSchema(nil))
	})
	t.Run("missing required key absent from properties", func(t *testing.T) {
		assert.True(t, allUnsatisfiableSchema([]*errors.ValidationError{missingRequired}))
	})
	t.Run("additional property that is also required", func(t *testing.T) {
		assert.True(t, allUnsatisfiableSchema([]*errors.ValidationError{additionalProperty}))
	})
	t.Run("unrelated failure not classified", func(t *testing.T) {
		assert.False(t, allUnsatisfiableSchema([]*errors.ValidationError{otherError}))
	})
	t.Run("mixed unsatisfiable + other fails", func(t *testing.T) {
		assert.False(t, allUnsatisfiableSchema([]*errors.ValidationError{missingRequired, otherError}))
	})
}

func TestExtractPropertyName(t *testing.T) {
	assert.Equal(t, "foo", extractPropertyName("missing property 'foo'"))
	assert.Equal(t, "foo", extractPropertyName("additional properties 'foo' not allowed"))
	assert.Equal(t, "", extractPropertyName("minProperties: got 0, want 1"))
}

func TestResolveJSONPointer(t *testing.T) {
	root := map[string]any{
		"properties": map[string]any{
			"foo": map[string]any{
				"type": "string",
				"enum": []any{"a", "b"},
			},
		},
	}
	t.Run("walks into nested map", func(t *testing.T) {
		got := resolveJSONPointer(root, "/properties/foo/type")
		assert.Equal(t, "string", got)
	})
	t.Run("walks into array by index", func(t *testing.T) {
		got := resolveJSONPointer(root, "/properties/foo/enum/1")
		assert.Equal(t, "b", got)
	})
	t.Run("empty pointer returns root", func(t *testing.T) {
		assert.Equal(t, root, resolveJSONPointer(root, ""))
	})
	t.Run("missing key returns nil", func(t *testing.T) {
		assert.Nil(t, resolveJSONPointer(root, "/properties/bar"))
	})
}

func TestAllContentTypeParamsOnly(t *testing.T) {
	paramsOnly := &errors.ValidationError{
		ValidationType:    "response",
		ValidationSubType: "contentType",
		Message:           "operation response content type 'application/json' does not exist",
		HowToFix:          "Use one of the 1 supported types for this operation: application/json; charset=utf-8",
	}
	differentMediaType := &errors.ValidationError{
		ValidationType:    "response",
		ValidationSubType: "contentType",
		Message:           "operation response content type 'application/json' does not exist",
		HowToFix:          "Use one of the 1 supported types for this operation: text/html",
	}
	wildcard := &errors.ValidationError{
		ValidationType:    "response",
		ValidationSubType: "contentType",
		Message:           "operation response content type 'application/json' does not exist",
		HowToFix:          "Use one of the 1 supported types for this operation: */*",
	}
	other := &errors.ValidationError{ValidationType: "response", ValidationSubType: "schema"}

	t.Run("empty slice", func(t *testing.T) {
		assert.False(t, allContentTypeParamsOnly(nil))
	})
	t.Run("only differs by parameters", func(t *testing.T) {
		assert.True(t, allContentTypeParamsOnly([]*errors.ValidationError{paramsOnly}))
	})
	t.Run("different media type not classified", func(t *testing.T) {
		assert.False(t, allContentTypeParamsOnly([]*errors.ValidationError{differentMediaType}))
	})
	t.Run("wildcard not classified (separate path)", func(t *testing.T) {
		assert.False(t, allContentTypeParamsOnly([]*errors.ValidationError{wildcard}))
	})
	t.Run("non-contentType error", func(t *testing.T) {
		assert.False(t, allContentTypeParamsOnly([]*errors.ValidationError{other}))
	})
}

func TestAllWildcardContentType(t *testing.T) {
	wildcard := &errors.ValidationError{
		ValidationType:    "response",
		ValidationSubType: "contentType",
		Message:           "operation response content type 'application/json' does not exist",
		HowToFix:          "Use one of the 1 supported types for this operation: */*",
	}
	concrete := &errors.ValidationError{
		ValidationType:    "response",
		ValidationSubType: "contentType",
		Message:           "operation response content type 'application/xml' does not exist",
		HowToFix:          "Use one of the 1 supported types for this operation: application/json",
	}
	other := &errors.ValidationError{
		ValidationType:    "response",
		ValidationSubType: "schema",
	}

	t.Run("empty slice", func(t *testing.T) {
		assert.False(t, allWildcardContentType(nil))
	})
	t.Run("wildcard mismatch", func(t *testing.T) {
		assert.True(t, allWildcardContentType([]*errors.ValidationError{wildcard}))
	})
	t.Run("concrete content type mismatch is not classified", func(t *testing.T) {
		assert.False(t, allWildcardContentType([]*errors.ValidationError{concrete}))
	})
	t.Run("non-contentType error", func(t *testing.T) {
		assert.False(t, allWildcardContentType([]*errors.ValidationError{other}))
	})
}

func TestValidatorCannotLookup(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"plain path", "/users/{id}", false},
		{"root path", "/", false},
		{"empty path", "", false},
		{"discriminator suffix", "/foo/{id}#qparam", true},
		{"space in literal segment", "/Your Pull DOC Request API Path", true},
		{"reserved char in segment", "/foo/bar baz", true},
		{"compound placeholder segment", "/media/{id}.{extension}", true},
		{"placeholder with literal suffix", "/files/{name}-{ext}", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, validatorCannotLookup(tc.path))
		})
	}
}

func TestIsAmbiguousOneOfReason(t *testing.T) {
	t.Run("subschemas matched is ambiguous", func(t *testing.T) {
		assert.True(t, isAmbiguousOneOfReason("'oneOf' failed, subschemas 0, 1 matched"))
	})
	t.Run("none matched is a real failure", func(t *testing.T) {
		assert.False(t, isAmbiguousOneOfReason("'oneOf' failed, none matched"))
	})
	t.Run("unrelated failure", func(t *testing.T) {
		assert.False(t, isAmbiguousOneOfReason("minProperties: got 0, want 1"))
	})
}

func TestAllAmbiguousOneOf(t *testing.T) {
	ambiguous := &errors.ValidationError{
		Message: "200 response body failed to validate schema",
		Reason:  "The response body for status code '200' is defined as an object. However, it does not meet the schema requirements of the specification",
		SchemaValidationErrors: []*errors.SchemaValidationFailure{
			{Reason: "'oneOf' failed, subschemas 0, 1 matched", KeywordLocation: "/properties/x/oneOf"},
		},
	}
	noneMatched := &errors.ValidationError{
		Message: "200 response body failed to validate schema",
		SchemaValidationErrors: []*errors.SchemaValidationFailure{
			{Reason: "'oneOf' failed, none matched", KeywordLocation: "/properties/x/oneOf"},
		},
	}
	ambiguousWithChild := &errors.ValidationError{
		Message: "200 response body failed to validate schema",
		// Real-world libopenapi-validator output: the ambiguous oneOf
		// appears at /properties/x/oneOf, and each matched branch
		// contributes child errors nested deeper under the same path
		// (here, enum failure inside branch 1).
		SchemaValidationErrors: []*errors.SchemaValidationFailure{
			{Reason: "'oneOf' failed, subschemas 0, 1 matched", KeywordLocation: "/properties/x/oneOf"},
			{Reason: "value must be 'Page Break'", KeywordLocation: "/properties/x/oneOf/1/properties/Type/enum"},
		},
	}
	ambiguousPlusUnrelated := &errors.ValidationError{
		Message: "200 response body failed to validate schema",
		// A non-child error (different prefix) means a real failure is
		// mixed in - skipping would swallow it.
		SchemaValidationErrors: []*errors.SchemaValidationFailure{
			{Reason: "'oneOf' failed, subschemas 0, 1 matched", KeywordLocation: "/properties/x/oneOf"},
			{Reason: "minProperties: got 0, want 1", KeywordLocation: "/properties/y/minProperties"},
		},
	}
	noNested := &errors.ValidationError{
		Message: "request validation failed",
		Reason:  "minProperties: got 0, want 1",
	}
	siblingAmbiguous := &errors.ValidationError{
		Message: "200 response body failed to validate schema",
		// Two unrelated fields each have an ambiguous oneOf. The
		// SVEs share no /anyOf or /oneOf prefix, but every SVE is
		// itself ambiguous - all failures are spec ambiguity.
		SchemaValidationErrors: []*errors.SchemaValidationFailure{
			{Reason: "'oneOf' failed, subschemas 0, 1 matched", KeywordLocation: "/properties/x/oneOf"},
			{Reason: "'oneOf' failed, subschemas 0, 1 matched", KeywordLocation: "/properties/y/oneOf"},
		},
	}

	t.Run("empty slice is not all-ambiguous", func(t *testing.T) {
		assert.False(t, allAmbiguousOneOf(nil))
	})
	t.Run("solo ambiguous oneOf", func(t *testing.T) {
		assert.True(t, allAmbiguousOneOf([]*errors.ValidationError{ambiguous}))
	})
	t.Run("ambiguous oneOf with child explanation", func(t *testing.T) {
		assert.True(t, allAmbiguousOneOf([]*errors.ValidationError{ambiguousWithChild}))
	})
	t.Run("sibling ambiguous oneOfs at unrelated paths", func(t *testing.T) {
		assert.True(t, allAmbiguousOneOf([]*errors.ValidationError{siblingAmbiguous}))
	})
	t.Run("none-matched is not ambiguous", func(t *testing.T) {
		assert.False(t, allAmbiguousOneOf([]*errors.ValidationError{noneMatched}))
	})
	t.Run("ambiguous mixed with unrelated failure fails", func(t *testing.T) {
		assert.False(t, allAmbiguousOneOf([]*errors.ValidationError{ambiguousPlusUnrelated}))
	})
	t.Run("error without nested schema failures is not ambiguous", func(t *testing.T) {
		assert.False(t, allAmbiguousOneOf([]*errors.ValidationError{noNested}))
	})
}

func TestAllAmbiguousOneOfSVE(t *testing.T) {
	allAmbiguous := []*errors.SchemaValidationFailure{
		{Reason: "'oneOf' failed, subschemas 0, 1 matched"},
		{Reason: "'oneOf' failed, subschemas 0, 2 matched"},
	}
	mixed := []*errors.SchemaValidationFailure{
		{Reason: "'oneOf' failed, subschemas 0, 1 matched"},
		{Reason: "minProperties: got 0, want 1"},
	}
	allNone := []*errors.SchemaValidationFailure{
		{Reason: "'oneOf' failed, none matched"},
	}

	t.Run("all ambiguous", func(t *testing.T) {
		assert.True(t, allAmbiguousOneOfSVE(allAmbiguous))
	})
	t.Run("mixed with non-ambiguous fails", func(t *testing.T) {
		assert.False(t, allAmbiguousOneOfSVE(mixed))
	})
	t.Run("none-matched is not ambiguous", func(t *testing.T) {
		assert.False(t, allAmbiguousOneOfSVE(allNone))
	})
}

func TestIsValidatorTimeout(t *testing.T) {
	t.Run("nil slice is not a timeout", func(t *testing.T) {
		assert.False(t, isValidatorTimeout(nil))
	})
	t.Run("regular failure is not a timeout", func(t *testing.T) {
		assert.False(t, isValidatorTimeout([]*errors.ValidationError{
			{ValidationType: "schema"},
		}))
	})
	t.Run("synthetic timeout error is detected", func(t *testing.T) {
		assert.True(t, isValidatorTimeout([]*errors.ValidationError{
			{ValidationType: "timeout"},
		}))
	})
	t.Run("mixed - any timeout entry trips the check", func(t *testing.T) {
		assert.True(t, isValidatorTimeout([]*errors.ValidationError{
			{ValidationType: "schema"},
			{ValidationType: "timeout"},
		}))
	})
}

func TestTimeoutValidationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	e := timeoutValidationError(req, "response")
	assert.Equal(t, "timeout", e.ValidationType)
	assert.Equal(t, "response", e.ValidationSubType)
	assert.Equal(t, http.MethodGet, e.RequestMethod)
	assert.Equal(t, "/foo", e.RequestPath)
	assert.Contains(t, e.Message, validationTimeout.String())
}
