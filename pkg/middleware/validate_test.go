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
			name:       "valid request passes through (default: req on, resp off)",
			cfg:        nil,
			handler:    validHandler,
			reqBody:    `{"name":"rex"}`,
			wantStatus: http.StatusOK,
			wantBody:   `"id":1`,
		},
		{
			name:       "invalid request body returns 400 by default",
			cfg:        nil,
			handler:    validHandler,
			reqBody:    `{}`, // missing required name
			wantStatus: http.StatusBadRequest,
			wantBody:   `request validation failed`,
		},
		{
			name:       "invalid response body passes through by default (response opt-in)",
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
			name:       "request:false skips request validation",
			cfg:        &config.ValidateConfig{Request: boolPtr(false)},
			handler:    validHandler,
			reqBody:    `{}`, // would normally fail
			wantStatus: http.StatusOK,
			wantBody:   `"id":1`,
		},
		{
			name:       "response:false explicit (matches default) — invalid body still passes",
			cfg:        &config.ValidateConfig{Response: boolPtr(false)},
			handler:    invalidHandler,
			reqBody:    `{"name":"rex"}`,
			wantStatus: http.StatusOK,
			wantBody:   `"name":"rex"`,
		},
		{
			name:       "both disabled skips everything",
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
	params := newTestParams(nil)
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
	t.Run("mixed", func(t *testing.T) {
		assert.False(t, allSchemaRenderFailure([]*errors.ValidationError{render, normal}))
	})
}
