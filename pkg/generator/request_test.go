package generator

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	assert2 "github.com/stretchr/testify/assert"
)

const testRequestBody = `{
	"order": {
		"payment": {"amount": {"currency": "EUR", "value": 10.5}},
		"amounts": [{"currency": "USD"}, {"currency": "GBP"}]
	}
}`

func TestGenerator_ResponseWithRequest(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	gen, err := NewGenerator(LoadServiceContext([]byte(`
charge:
  currency: "request:order.payment.amount.currency"
  amount: "request:order.payment.amount.value"
  first: "request:order.amounts[0].currency"
  second: "request:order.amounts[1].currency"
  missing: "request:order.payment.missing"
`), nil), nil)
	assert.NoError(err)

	respSchema := &schema.ResponseSchema{
		ContentType: "application/json",
		Body: &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"charge": {
					Type: "object",
					Properties: map[string]*schema.Schema{
						"currency": {Type: "string"},
						"amount":   {Type: "number"},
						"first":    {Type: "string"},
						"second":   {Type: "string"},
						"missing":  {Type: "string", Enum: []any{"FALLBACK"}},
					},
				},
			},
		},
	}

	charge := func(res schema.ResponseData) map[string]any {
		var body map[string]any
		assert.NoError(json.Unmarshal(res.Body, &body))
		return body["charge"].(map[string]any)
	}

	t.Run("values taken from the request payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(testRequestBody))
		req.Header.Set("Content-Type", "application/json")

		res := charge(gen.Response(respSchema, nil, WithRequest(req)))
		assert.Equal("EUR", res["currency"])
		assert.Equal(10.5, res["amount"])
		assert.Equal("USD", res["first"])
		assert.Equal("GBP", res["second"])
		assert.Equal("FALLBACK", res["missing"])
	})

	t.Run("body stays readable for the caller", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(testRequestBody))
		req.Header.Set("Content-Type", "application/json")

		gen.Response(respSchema, nil, WithRequest(req))

		body, err := io.ReadAll(req.Body)
		assert.NoError(err)
		assert.Equal(testRequestBody, string(body))
	})

	t.Run("response headers take values from the request payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(testRequestBody))
		req.Header.Set("Content-Type", "application/json")

		withHeaders := &schema.ResponseSchema{
			ContentType: "application/json",
			Body:        respSchema.Body,
			Headers: map[string]*schema.Schema{
				"X-Currency": {Type: "string"},
			},
		}

		res := gen.Response(withHeaders, map[string]any{
			"in-header": map[string]any{
				"x-currency": "request:order.payment.amount.currency",
			},
		}, WithRequest(req))
		assert.Equal("EUR", res.Headers.Get("x-currency"))
	})

	t.Run("without a request the schema value is generated", func(t *testing.T) {
		res := charge(gen.Response(respSchema, nil))
		assert.NotEqual("EUR", res["currency"])
		assert.Equal("FALLBACK", res["missing"])
	})

	t.Run("user context can reference the request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(testRequestBody))
		req.Header.Set("Content-Type", "application/json")

		plain, err := NewGenerator(nil, nil)
		assert.NoError(err)

		res := charge(plain.Response(respSchema, map[string]any{
			"currency": "request:order.amounts[1].currency",
		}, WithRequest(req)))
		assert.Equal("GBP", res["currency"])
	})
}

func TestRequestPayload(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	newRequest := func(contentType, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		return req
	}

	t.Run("nil request", func(t *testing.T) {
		assert.Nil(requestPayload(nil))
	})

	t.Run("empty body", func(t *testing.T) {
		assert.Nil(requestPayload(newRequest("application/json", "")))
	})

	t.Run("invalid json", func(t *testing.T) {
		assert.Nil(requestPayload(newRequest("application/json", "not json")))
	})

	t.Run("unsupported content type", func(t *testing.T) {
		assert.Nil(requestPayload(newRequest("application/octet-stream", testRequestBody)))
	})

	t.Run("json object", func(t *testing.T) {
		payload, ok := requestPayload(newRequest("application/json", testRequestBody)).(map[string]any)
		assert.True(ok)
		assert.Contains(payload, "order")
	})

	t.Run("json array", func(t *testing.T) {
		payload, ok := requestPayload(newRequest("application/json", `[{"currency":"USD"}]`)).([]any)
		assert.True(ok)
		assert.Len(payload, 1)
	})

	t.Run("form encoded", func(t *testing.T) {
		payload, ok := requestPayload(newRequest(
			"application/x-www-form-urlencoded; charset=utf-8",
			"currency=USD&amount=10")).(map[string]any)
		assert.True(ok)
		assert.Equal("USD", payload["currency"])
		assert.Equal(float64(10), payload["amount"])
	})

	t.Run("form encoded deep object", func(t *testing.T) {
		payload := requestPayload(newRequest(
			"application/x-www-form-urlencoded",
			"order[payment][currency]=EUR&amounts[0][currency]=USD&amounts[1][currency]=GBP"))

		assert.Equal("EUR", types.GetValueByJSONPath(payload, "order.payment.currency"))
		assert.Equal("USD", types.GetValueByJSONPath(payload, "amounts[0].currency"))
		assert.Equal("GBP", types.GetValueByJSONPath(payload, "amounts[1].currency"))
	})

	t.Run("form encoded repeated key", func(t *testing.T) {
		payload := requestPayload(newRequest(
			"application/x-www-form-urlencoded",
			"currency=USD&currency=GBP"))

		assert.Equal("GBP", types.GetValueByJSONPath(payload, "currency[1]"))
	})
}
