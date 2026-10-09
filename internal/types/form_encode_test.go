package types

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestEncodeFormData(t *testing.T) {
	t.Run("simple object", func(t *testing.T) {
		data := map[string]any{
			"name": "John",
			"age":  30,
		}

		result, err := EncodeFormData(data, nil)
		assert.NoError(t, err)
		assert.Contains(t, result, "name=John")
		assert.Contains(t, result, "age=30")
	})

	t.Run("nested object with deepObject style", func(t *testing.T) {
		data := map[string]any{
			"flow_data": map[string]any{
				"subscription_update_confirm": map[string]any{
					"subscription": "sub_123",
					"items": []any{
						map[string]any{
							"id":       "item_1",
							"price":    "price_1",
							"quantity": 1,
						},
					},
					"discounts": []any{
						map[string]any{
							"coupon":         "coupon_1",
							"promotion_code": "promo_1",
						},
					},
				},
			},
		}

		encoding := map[string]schema.RequestBodyEncoding{
			"flow_data": {
				Style:   "deepObject",
				Explode: boolPtr(true),
			},
		}

		result, err := EncodeFormData(data, encoding)
		assert.NoError(t, err)
		t.Logf("Encoded form data: %s", result)

		// With deepObject, nested structures should be encoded with brackets (URL-encoded as %5B and %5D)
		assert.Contains(t, result, "flow_data%5B")
	})

	t.Run("array of objects", func(t *testing.T) {
		data := map[string]any{
			"items": []any{
				map[string]any{
					"id":    "1",
					"price": "100",
				},
				map[string]any{
					"id":    "2",
					"price": "200",
				},
			},
		}

		result, err := EncodeFormData(data, nil)
		assert.NoError(t, err)
		t.Logf("Encoded array: %s", result)
		// Arrays should be encoded properly, not as "map[...]" strings
		assert.NotContains(t, result, "map[")
	})
}

func boolPtr(b bool) *bool {
	return &b
}
