package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/stretchr/testify/assert"
)

func TestPanicValidationError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	out := panicValidationError("boom", []byte("trace"), req, "request")
	assert.Equal(t, "validator panicked during request validation", out.Message)
	assert.Contains(t, out.Reason, "boom")
	assert.Contains(t, out.Reason, "trace")
	assert.Equal(t, "panic", out.ValidationType)
	assert.Equal(t, "request", out.ValidationSubType)
	assert.Equal(t, "/foo/bar", out.RequestPath)
	assert.Equal(t, http.MethodGet, out.RequestMethod)
}

func TestAllErrorsForDifferentSpecPath(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		assert.False(t, allErrorsForDifferentSpecPath(nil, "/users/{id}"))
	})

	t.Run("empty router path", func(t *testing.T) {
		errs := []*errors.ValidationError{{SpecPath: "/x"}}
		assert.False(t, allErrorsForDifferentSpecPath(errs, ""))
	})

	t.Run("all match router path", func(t *testing.T) {
		errs := []*errors.ValidationError{
			{SpecPath: "/users/{id}"},
			{SpecPath: "/users/{id}"},
		}
		assert.False(t, allErrorsForDifferentSpecPath(errs, "/users/{id}"))
	})

	t.Run("all differ from router path", func(t *testing.T) {
		errs := []*errors.ValidationError{
			{SpecPath: "/other/{x}"},
			{SpecPath: "/other/{x}"},
		}
		assert.True(t, allErrorsForDifferentSpecPath(errs, "/users/{id}"))
	})

	t.Run("one matches router path", func(t *testing.T) {
		errs := []*errors.ValidationError{
			{SpecPath: "/users/{id}"},
			{SpecPath: "/other/{x}"},
		}
		assert.False(t, allErrorsForDifferentSpecPath(errs, "/users/{id}"))
	})

	t.Run("error with empty SpecPath disqualifies", func(t *testing.T) {
		errs := []*errors.ValidationError{
			{SpecPath: ""},
			{SpecPath: "/other"},
		}
		assert.False(t, allErrorsForDifferentSpecPath(errs, "/users"))
	})
}

func TestAllRouterAmbiguity(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		assert.False(t, allRouterAmbiguity(nil, "text/html"))
	})

	t.Run("empty respCT", func(t *testing.T) {
		errs := []*errors.ValidationError{{ValidationType: "response", ValidationSubType: "contentType"}}
		assert.False(t, allRouterAmbiguity(errs, ""))
	})

	t.Run("JSON response is not ambiguity", func(t *testing.T) {
		errs := []*errors.ValidationError{{ValidationType: "response", ValidationSubType: "contentType"}}
		assert.False(t, allRouterAmbiguity(errs, "application/json"))
	})

	t.Run("non-JSON response with all-contentType errors", func(t *testing.T) {
		errs := []*errors.ValidationError{
			{ValidationType: "response", ValidationSubType: "contentType"},
			{ValidationType: "response", ValidationSubType: "contentType"},
		}
		assert.True(t, allRouterAmbiguity(errs, "text/html"))
	})

	t.Run("non-contentType error disqualifies", func(t *testing.T) {
		errs := []*errors.ValidationError{
			{ValidationType: "response", ValidationSubType: "contentType"},
			{ValidationType: "response", ValidationSubType: "schema"},
		}
		assert.False(t, allRouterAmbiguity(errs, "text/html"))
	})

	t.Run("unparseable content-type", func(t *testing.T) {
		errs := []*errors.ValidationError{{ValidationType: "response", ValidationSubType: "contentType"}}
		assert.False(t, allRouterAmbiguity(errs, "  "))
	})
}

func TestReasonIsJSLiteralPattern(t *testing.T) {
	assert.False(t, reasonIsJSLiteralPattern(""))
	assert.False(t, reasonIsJSLiteralPattern("no marker here"))
	assert.False(t, reasonIsJSLiteralPattern("does not match pattern 'no-end-quote"))
	assert.False(t, reasonIsJSLiteralPattern("does not match pattern '^[a-z]+$'"))
	assert.True(t, reasonIsJSLiteralPattern("does not match pattern '/^[0-9]+$/'"))
}

func TestReasonIsDescriptivePattern(t *testing.T) {
	assert.False(t, reasonIsDescriptivePattern(""))
	assert.False(t, reasonIsDescriptivePattern("no marker"))
	assert.False(t, reasonIsDescriptivePattern("does not match pattern 'no-end-quote"))
	assert.False(t, reasonIsDescriptivePattern("does not match pattern '^[a-z]+$'"))
	assert.True(t, reasonIsDescriptivePattern("does not match pattern 'a-z, A-Z, 0-9'"))
}

func TestExtractQuotedToken(t *testing.T) {
	assert.Equal(t, "value", extractQuotedToken("foo 'value' bar"))
	assert.Equal(t, "", extractQuotedToken("no quotes here"))
	assert.Equal(t, "", extractQuotedToken("only one ' quote"))
}

func TestExtractAfter(t *testing.T) {
	assert.Equal(t, "result", extractAfter("prefix: result", "prefix: "))
	assert.Equal(t, "", extractAfter("no-marker", "missing: "))
	assert.Equal(t, "trimmed", extractAfter("a:   trimmed   ", "a:"))
}

func TestSnapshotBodyNilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	body, rc, err := snapshotBody(req)
	assert.NoError(t, err)
	assert.Nil(t, body)
	assert.Equal(t, http.NoBody, rc)
}

func TestSnapshotBodyHttpNoBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = http.NoBody
	body, rc, err := snapshotBody(req)
	assert.NoError(t, err)
	assert.Nil(t, body)
	assert.Equal(t, http.NoBody, rc)
}
