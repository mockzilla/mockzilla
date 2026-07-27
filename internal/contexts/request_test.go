package contexts

import (
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestHasRequestRefs(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("no contexts", func(t *testing.T) {
		assert.False(HasRequestRefs(nil))
	})

	t.Run("without refs", func(t *testing.T) {
		assert.False(HasRequestRefs([]map[string]any{
			{"name": "Jane", "nested": map[string]any{"id": 1}},
		}))
	})

	t.Run("root level ref", func(t *testing.T) {
		assert.True(HasRequestRefs([]map[string]any{
			{"name": "Jane"},
			{"currency": RequestRef{Path: "order.currency"}},
		}))
	})

	t.Run("nested ref", func(t *testing.T) {
		assert.True(HasRequestRefs([]map[string]any{
			{
				"in-response": map[string]any{
					"charge": map[string]any{
						"currency": RequestRef{Path: "order.currency"},
					},
				},
			},
		}))
	})
}
