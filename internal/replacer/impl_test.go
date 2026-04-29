//go:build !integration

package replacer

import (
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/jaswdr/faker/v2"
	"github.com/mockzilla/mockzilla/v2/internal/contexts"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	assert2 "github.com/stretchr/testify/assert"
)

// ptr is a helper function to create pointers to values for tests
func ptr[T any](v T) *T {
	return &v
}

func TestIsMatchSchemaReadWriteToState(t *testing.T) {
	tests := []struct {
		name     string
		schema   *schema.Schema
		state    *ReplaceState
		expected bool
	}{
		{"nil schema", nil, NewReplaceState(), true},
		{"nil state", &schema.Schema{Type: types.TypeString}, nil, true},
		{"readOnly schema with readOnly state", &schema.Schema{Type: types.TypeString, ReadOnly: true}, NewReplaceState(WithReadOnly()), true},
		{"readOnly schema without readOnly state", &schema.Schema{Type: types.TypeString, ReadOnly: true}, NewReplaceState(), false},
		{"writeOnly schema with writeOnly state", &schema.Schema{Type: types.TypeString, WriteOnly: true}, NewReplaceState(WithWriteOnly()), true},
		{"writeOnly schema without writeOnly state", &schema.Schema{Type: types.TypeString, WriteOnly: true}, NewReplaceState(), false},
		{"path param ignores readOnly", &schema.Schema{Type: types.TypeInteger, ReadOnly: true}, NewReplaceState(WithPath()), true},
		{"path param ignores writeOnly", &schema.Schema{Type: types.TypeInteger, WriteOnly: true}, NewReplaceState(WithPath()), true},
		{"regular schema matches", &schema.Schema{Type: types.TypeString}, NewReplaceState(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMatchSchemaReadWriteToState(tt.schema, tt.state)
			assert2.Equal(t, tt.expected, result)
		})
	}
}

func TestHasCorrectSchemaValue(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	// Test nil schema in context
	t.Run("nil-schema-in-context", func(t *testing.T) {
		ctx := &ReplaceContext{faker: fake, schema: nil}
		res := hasCorrectSchemaValue(ctx, "nice")
		assert.True(res)
	})

	// Table-driven tests for basic type validation
	basicTests := []struct {
		name     string
		schema   *schema.Schema
		value    any
		expected bool
	}{
		{"nil-schema", nil, "nice", true},
		{"string-type-ok", &schema.Schema{Type: types.TypeString}, "nice", true},
		{"string-type-error", &schema.Schema{Type: types.TypeString}, 123, false},
		{"int32-ok", &schema.Schema{Type: types.TypeNumber, Format: "int32"}, 123, true},
		{"int64-ok-small", &schema.Schema{Type: types.TypeNumber, Format: "int64"}, 123, true},
		{"int64-bad", &schema.Schema{Type: types.TypeNumber, Format: "int64"}, 123.1, false},
		// String type with int formats (common pattern for large integers in JSON)
		{"string-int32-ok", &schema.Schema{Type: types.TypeString, Format: "int32"}, "123", true},
		{"string-int32-bad", &schema.Schema{Type: types.TypeString, Format: "int32"}, "not-a-number", false},
		{"string-int32-overflow", &schema.Schema{Type: types.TypeString, Format: "int32"}, "9999999999999999999", false},
		{"string-int64-ok", &schema.Schema{Type: types.TypeString, Format: "int64"}, "954509091344887077", true},
		{"string-int64-bad", &schema.Schema{Type: types.TypeString, Format: "int64"}, "not-a-number", false},
		{"string-int64-non-string", &schema.Schema{Type: types.TypeString, Format: "int64"}, 123, false},
		{"string-date-ok", &schema.Schema{Type: types.TypeString, Format: "date"}, "2020-01-01", true},
		{"string-date-bad", &schema.Schema{Type: types.TypeString, Format: "date"}, "2020-13-01", false},
		{"email-valid", &schema.Schema{Type: types.TypeString, Format: "email"}, "adams.judd@gmail.com", true},
		{"email-invalid", &schema.Schema{Type: types.TypeString, Format: "email"}, "not-an-email", false},
		{"email-non-string-any-type", &schema.Schema{Type: "any", Format: "email"}, 12345, false},
		{"string-datetime-ok", &schema.Schema{Type: types.TypeString, Format: "date-time"}, "2020-01-01T15:04:05.000Z", true},
		{"string-datetime-bad", &schema.Schema{Type: types.TypeString, Format: "date-time"}, "2020-01-01T25:04:05.000Z", false},
		// Integer date/datetime formats (Unix timestamps - common in APIs like Intercom)
		{"int-date-unix-timestamp", &schema.Schema{Type: types.TypeInteger, Format: "date"}, int64(1672531200), true},
		{"int-datetime-unix-timestamp", &schema.Schema{Type: types.TypeInteger, Format: "date-time"}, int64(1672531200), true},
		{"int32-datetime-unix-timestamp", &schema.Schema{Type: types.TypeInteger, Format: "date-time"}, int32(1672531200), true},
		{"string-with-unknown-format", &schema.Schema{Type: types.TypeString, Format: "x"}, "xxx", true},
		{"uuid-valid", &schema.Schema{Type: types.TypeString, Format: "uuid"}, "550e8400-e29b-41d4-a716-446655440000", true},
		{"uuid-invalid-length", &schema.Schema{Type: types.TypeString, Format: "uuid"}, "8D4176FA78D5A7Fffa91e9edc694ec5858be9a1b109507c", false},
		{"uuid-invalid-format", &schema.Schema{Type: types.TypeString, Format: "uuid"}, "not-a-uuid", false},
		{"uuid-non-string-any-type", &schema.Schema{Type: "any", Format: "uuid"}, 12345, false},
	}

	for _, tc := range basicTests {
		t.Run(tc.name, func(t *testing.T) {
			res := hasCorrectSchemaValue(newTestReplaceContext(tc.schema), tc.value)
			assert.Equal(tc.expected, res)
		})
	}

	// Test with non-schema type
	t.Run("not-a-schema-type", func(t *testing.T) {
		res := hasCorrectSchemaValue(newTestReplaceContext("not-a-schema"), "nice")
		assert.True(res)
	})

	// Tests requiring faker
	t.Run("int32-bad", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Format: "int32"}
		res := hasCorrectSchemaValue(newTestReplaceContext(s), fake.Int64())
		assert.False(res)
	})

	t.Run("int64-ok-big", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Format: "int64"}
		res := hasCorrectSchemaValue(newTestReplaceContext(s), fake.Int64())
		assert.True(res)
	})
}

func TestReplaceInRequest(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	t.Run("skipped-when-not-write-only", func(t *testing.T) {
		state := NewReplaceState(WithName("id"), WithReadOnly())
		res := replaceInRequest(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-request": map[string]any{
						"id": "abc-123",
					},
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("request-header-area", func(t *testing.T) {
		state := NewReplaceState(WithName("authorization"), WithHeader(), WithWriteOnly())
		res := replaceInRequest(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-request-header": map[string]any{
						"authorization": "Bearer tok",
					},
					"in-request": map[string]any{
						"authorization": "should-not-match",
					},
				},
			},
		})
		assert.Equal("Bearer tok", res)
	})

	t.Run("general-request-area", func(t *testing.T) {
		state := NewReplaceState(WithName("id"), WithWriteOnly())
		res := replaceInRequest(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-request": map[string]any{
						"id": "abc-123",
					},
				},
			},
		})
		assert.Equal("abc-123", res)
	})

	t.Run("falls-through-when-no-match", func(t *testing.T) {
		state := NewReplaceState(WithName("unknown"), WithWriteOnly())
		res := replaceInRequest(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-request": map[string]any{
						"id": "abc-123",
					},
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("header-falls-through-to-general-request", func(t *testing.T) {
		state := NewReplaceState(WithName("x-token"), WithHeader(), WithWriteOnly())
		res := replaceInRequest(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-request": map[string]any{
						"x-token": "general-request-val",
					},
				},
			},
		})
		assert.Equal("general-request-val", res)
	})
}

func TestReplaceInResponse(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	t.Run("skipped-when-not-read-only", func(t *testing.T) {
		state := NewReplaceState(WithName("id"), WithWriteOnly())
		res := replaceInResponse(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-response": map[string]any{
						"id": 100,
					},
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("response-header-area", func(t *testing.T) {
		state := NewReplaceState(WithName("x-request-id"), WithHeader(), WithReadOnly())
		res := replaceInResponse(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-response-header": map[string]any{
						"x-request-id": "fixed",
					},
					"in-response": map[string]any{
						"x-request-id": "should-not-match",
					},
				},
			},
		})
		assert.Equal("fixed", res)
	})

	t.Run("general-response-area", func(t *testing.T) {
		state := NewReplaceState(WithName("id"), WithReadOnly())
		res := replaceInResponse(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-response": map[string]any{
						"id": 100,
					},
				},
			},
		})
		assert.Equal(100, res)
	})

	t.Run("falls-through-when-no-match", func(t *testing.T) {
		state := NewReplaceState(WithName("unknown"), WithReadOnly())
		res := replaceInResponse(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-response": map[string]any{
						"id": 100,
					},
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("header-falls-through-to-general-response", func(t *testing.T) {
		state := NewReplaceState(WithName("x-token"), WithHeader(), WithReadOnly())
		res := replaceInResponse(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-response": map[string]any{
						"x-token": "general-response-val",
					},
				},
			},
		})
		assert.Equal("general-response-val", res)
	})
}

func TestReplaceInHeaders(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	t.Run("not-a-header", func(t *testing.T) {
		state := NewReplaceStateWithName("userID")
		res := replaceInHeaders(&ReplaceContext{
			faker: fake,
			state: state,
			data: []map[string]any{
				{
					"userID": "1234",
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("basic authorization header uses corresponding context with base64", func(t *testing.T) {
		state := NewReplaceState(WithName("authorization"), WithHeader())
		res := replaceInHeaders(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-header": map[string]any{
						"authorization": "user:password",
					},
				},
			},
			schema: &schema.Schema{
				Type:   types.TypeString,
				Format: "basic",
			},
		})
		assert.Equal("Basic dXNlcjpwYXNzd29yZA==", res)
	})

	t.Run("basic authorization header without context uses random password", func(t *testing.T) {
		state := NewReplaceState(WithName("authorization"), WithHeader())
		res, ok := replaceInHeaders(&ReplaceContext{
			faker: fake,
			state: state,
			schema: &schema.Schema{
				Type:   types.TypeString,
				Format: "basic",
			},
		}).(string)

		assert.True(ok)
		assert.True(strings.HasPrefix(res, "Basic "))
		assert.Greater(len(res), 10)
	})

	t.Run("bearer authorization header uses corresponding context", func(t *testing.T) {
		state := NewReplaceState(WithName("authorization"), WithHeader())
		res := replaceInHeaders(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-header": map[string]string{
						"authorization": "token",
					},
				},
			},
			schema: &schema.Schema{
				Type:   types.TypeString,
				Format: "bearer",
			},
		})
		assert.Equal("Bearer token", res)
	})

	t.Run("bearer authorization header without context uses random password", func(t *testing.T) {
		state := NewReplaceState(WithName("authorization"), WithHeader())
		res, ok := replaceInHeaders(&ReplaceContext{
			faker: fake,
			state: state,
			schema: &schema.Schema{
				Type:   types.TypeString,
				Format: "bearer",
			},
		}).(string)

		assert.True(ok)
		assert.True(strings.HasPrefix(res, "Bearer "))
		assert.Greater(len(res), 10)
	})

	t.Run("custom authorization header uses value from the context", func(t *testing.T) {
		state := NewReplaceState(WithName("authorization"), WithHeader())
		ctx := &ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"in-header": map[string]any{
						"authorization": "custom-token",
					},
				},
			},
			schema: &schema.Schema{
				Type:   types.TypeString,
				Format: "custom",
			},
		}

		res, ok := replaceInHeaders(ctx).(string)

		assert.True(ok)
		assert.Equal("custom-token", res)
	})

	t.Run("custom header uses corresponding context value", func(t *testing.T) {
		state := NewReplaceStateWithName("userID").WithOptions(WithHeader())
		res := replaceInHeaders(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"userID": "1234",
					"in-header": map[string]string{
						"userID": "5678",
					},
				},
			},
		})
		assert.Equal("5678", res)
	})
}

func TestReplaceInPath(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	t.Run("not-a-path", func(t *testing.T) {
		state := NewReplaceStateWithName("userID")
		res := replaceInPath(&ReplaceContext{
			faker: fake,
			state: state,
			data: []map[string]any{
				{
					"userID": "1234",
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("in-path", func(t *testing.T) {
		state := NewReplaceStateWithName("userID").WithOptions(WithPath())
		res := replaceInPath(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"userID": "1234",
					"in-path": map[string]string{
						"userID": "5678",
					},
				},
			},
		})
		assert.Equal("5678", res)
	})

	t.Run("not-in-ctx", func(t *testing.T) {
		state := NewReplaceStateWithName("userID").WithOptions(WithPath())
		res := replaceInPath(&ReplaceContext{
			faker:      fake,
			state:      state,
			areaPrefix: "in-",
			data: []map[string]any{
				{
					"userID": "1234",
				},
			},
		})
		assert.Nil(res)
	})
}

func TestReplaceInArea(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	t.Run("missing-prefix", func(t *testing.T) {
		state := NewReplaceStateWithName("userID").WithOptions(WithPath())
		res := replaceInArea(&ReplaceContext{
			faker: fake,
			state: state,
			data: []map[string]any{
				{
					"userID": "1234",
				},
			},
		}, "path")
		assert.Nil(res)
	})
}

func TestReplaceFromContext(t *testing.T) {
	assert := assert2.New(t)
	fake := faker.New()

	t.Run("happy-path", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeString,
		}
		state := NewReplaceStateWithName("Person").WithOptions(WithName("dateOfBirth"))

		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"Person": map[string]any{
						"dateOfBirth": "1980-01-01",
					},
				},
			},
		})
		assert.Equal("1980-01-01", res)
	})

	t.Run("not-found-in-path", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeString,
		}
		state := NewReplaceStateWithName("Person").WithOptions(WithName("dateOfBirth"))
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"people": map[string]any{
						"dateOfBirth": "1980-01-01",
					},
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("uuid-format-ignores-context-value", func(t *testing.T) {
		// This tests the bug fix: when field has format:uuid, context values should be ignored
		s := &schema.Schema{
			Type:   types.TypeString,
			Format: "uuid",
		}
		state := NewReplaceStateWithName("status").WithOptions(WithName("id"))
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"status": []string{"success", "pending", "failed"},
				},
			},
		})
		// Should return nil so replaceFromSchemaFormat can generate a UUID
		assert.Nil(res)
	})

	t.Run("zero-integer-from-context-is-returned", func(t *testing.T) {
		// replaceFromContext returns the value as-is; constraints are applied by caller
		s := &schema.Schema{
			Type: types.TypeInteger,
		}
		state := NewReplaceStateWithName("amount")
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"amount": 0,
				},
			},
		})
		// Returns 0 - caller (CreateValueReplacer) will apply constraints
		assert.Equal(0, res)
	})

	t.Run("non-zero-integer-from-context-is-returned", func(t *testing.T) {
		// When context returns non-zero integer, should return it
		s := &schema.Schema{
			Type: types.TypeInteger,
		}
		state := NewReplaceStateWithName("amount")
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"amount": 100,
				},
			},
		})
		assert.Equal(100, res)
	})

	t.Run("zero-int32-from-context-returns-nil", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeInteger,
			Format: "int32",
		}
		state := NewReplaceStateWithName("count")
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"count": int32(0),
				},
			},
		})
		assert.Nil(res)
	})

	t.Run("zero-int64-from-context-returns-nil", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeInteger,
			Format: "int64",
		}
		state := NewReplaceStateWithName("total")
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"total": int64(0),
				},
			},
		})
		assert.Nil(res)
	})
}

func TestCastToSchemaFormat(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no-schema", func(t *testing.T) {
		res := castToSchemaFormat(newTestReplaceContext(nil), 123)
		assert.Equal(123, res)
	})

	t.Run("uuid-format-returns-nil", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, Format: "uuid"}
		res := castToSchemaFormat(newTestReplaceContext(s), "some-context-value")
		assert.Nil(res)
	})

	// Table-driven tests for integer format conversions
	intFormatTests := []struct {
		name     string
		format   string
		input    any
		expected any
	}{
		{"int32-ok", "int32", 123.0, int32(123)},
		{"int32-not-convertible", "int32", 123.4, 123.4},
		{"int64-ok", "int64", 123.0, int64(123)},
		{"int64-not-convertible", "int64", 123.4, 123.4},
		{"uint8-ok", "uint8", 200.0, uint8(200)},
		{"uint8-overflow", "uint8", 300.0, 300.0},
		{"uint16-ok", "uint16", 50000.0, uint16(50000)},
		{"uint16-overflow", "uint16", 70000.0, 70000.0},
		{"uint32-ok", "uint32", 3000000000.0, uint32(3000000000)},
		{"uint32-overflow", "uint32", 5000000000.0, 5000000000.0},
		{"uint64-ok", "uint64", 1000000.0, uint64(1000000)},
		{"uint64-not-convertible", "uint64", 123.4, 123.4},
		{"unknown-format", "unknown", 123.0, 123.0},
	}

	for _, tc := range intFormatTests {
		t.Run(tc.name, func(t *testing.T) {
			s := &schema.Schema{Type: types.TypeInteger, Format: tc.format}
			res := castToSchemaFormat(newTestReplaceContext(s), tc.input)
			assert.Equal(tc.expected, res)
		})
	}
}

func TestReplaceValueWithContext(t *testing.T) {
	assert := assert2.New(t)

	t.Run("happy-path", func(t *testing.T) {
		context := map[string]interface{}{
			"user": map[string]interface{}{
				"age": 30,
				"country": map[string]interface{}{
					"name": "Germany",
					"code": "DE",
				},
			},
		}
		namePath := []string{"user", "country", "name"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal("Germany", res)
	})

	t.Run("happy-path-with-ints", func(t *testing.T) {
		context := map[string]any{
			"user": map[string]any{
				"name": "Jane Doe",
				"age":  30,
			},
		}
		namePath := []string{"user", "age"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal(30, res)
	})

	t.Run("unmapped-type", func(t *testing.T) {
		namePath := []string{"rank"}
		ctx := map[string]int64{
			"rank": 123,
		}
		res := replaceValueWithContext(namePath, ctx)
		assert.Nil(res)
	})

	t.Run("has-name-prefix", func(t *testing.T) {
		context := map[string]interface{}{
			"user": map[string]interface{}{
				"country": map[string]interface{}{
					"^name": "Germany",
				},
			},
		}
		namePath := []string{"user", "country", "name"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal("Germany", res)
	})

	t.Run("single-namepath-has-name-prefix", func(t *testing.T) {
		context := map[string]interface{}{
			"^name": "Jane Doe",
		}
		namePath := []string{"name"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal("Jane Doe", res)
	})

	t.Run("random-slice-value", func(t *testing.T) {
		names := []string{"Jane", "John", "Zena"}
		context := map[string]interface{}{
			"user": map[string]interface{}{
				"name": names,
			},
		}
		namePath := []string{"user", "name"}
		res := replaceValueWithContext(namePath, context)

		assert.Contains(names, res)
	})

	t.Run("with-map-of-strings-ctx", func(t *testing.T) {
		context := map[string]string{
			"name": "Jane Doe",
		}
		namePath := []string{"name"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal("Jane Doe", res)
	})

	t.Run("with-map-of-ints-ctx", func(t *testing.T) {
		context := map[string]int{
			"age": 30,
		}
		namePath := []string{"name", "age"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal(30, res)
	})

	t.Run("with-map-of-float64s-ctx", func(t *testing.T) {
		id := float64(123)
		context := map[string]float64{
			"rank": id,
		}
		namePath := []string{"name", "rank"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal(id, res)
	})

	t.Run("with-map-of-bools-ctx", func(t *testing.T) {
		context := map[string]bool{
			"is_married": true,
		}
		namePath := []string{"name", "is_married"}
		res := replaceValueWithContext(namePath, context)

		assert.Equal(true, res)
	})

	t.Run("with-fake-func-ctx", func(t *testing.T) {
		fn := contexts.FakeFunc(func() contexts.MixedValue {
			return contexts.IntValue(123)
		})
		namePath := []string{"name", "rank"}
		res := replaceValueWithContext(namePath, fn)

		assert.Equal(int64(123), res)
	})

	t.Run("with-string-ctx", func(t *testing.T) {
		namePath := []string{"name"}
		res := replaceValueWithContext(namePath, "Jane")
		assert.Equal("Jane", res)
	})

	t.Run("with-int-ctx", func(t *testing.T) {
		namePath := []string{"age"}
		res := replaceValueWithContext(namePath, 30)
		assert.Equal(30, res)
	})

	t.Run("with-float64-ctx", func(t *testing.T) {
		namePath := []string{"rank"}
		res := replaceValueWithContext(namePath, 123.0)
		assert.Equal(123.0, res)
	})

	t.Run("with-bool-ctx", func(t *testing.T) {
		namePath := []string{"is_married"}
		res := replaceValueWithContext(namePath, true)
		assert.Equal(true, res)
	})

	t.Run("with-string-slice-ctx", func(t *testing.T) {
		namePath := []string{"name"}
		values := []string{"Jane", "John"}
		res := replaceValueWithContext(namePath, values)
		assert.Contains(values, res)
	})

	t.Run("with-int-slice-ctx", func(t *testing.T) {
		namePath := []string{"age"}
		values := []int{30, 40}
		res := replaceValueWithContext(namePath, values)
		assert.Contains(values, res)
	})

	t.Run("with-bool-slice-ctx", func(t *testing.T) {
		namePath := []string{"is_married"}
		values := []bool{true, false}
		res := replaceValueWithContext(namePath, values)
		assert.Contains(values, res)
	})

	t.Run("with-float64-slice-ctx", func(t *testing.T) {
		namePath := []string{"rank"}
		values := []float64{123.0, 1.0, 12.0}
		res := replaceValueWithContext(namePath, values)
		assert.Contains(values, res)
	})

	t.Run("with-any-slice-ctx", func(t *testing.T) {
		namePath := []string{"nickname"}
		values := []any{"j", 1}
		res := replaceValueWithContext(namePath, values)
		assert.Contains(values, res)
	})
}

func TestReplaceValueWithMapContext(t *testing.T) {
	assert := assert2.New(t)

	t.Run("empty-path", func(t *testing.T) {
		res := replaceValueWithMapContext[string]([]string{}, map[string]string{})
		assert.Nil(res)
	})

	t.Run("direct-match", func(t *testing.T) {
		path := []string{"name"}
		data := map[string]string{
			"name": "Jane Doe",
		}
		res := replaceValueWithMapContext[string](path, data)
		assert.Equal("Jane Doe", res)
	})

	t.Run("no-match", func(t *testing.T) {
		path := []string{"user"}
		data := map[string]string{
			"name": "Jane Doe",
		}
		res := replaceValueWithMapContext[string](path, data)
		assert.Nil(res)
	})

	t.Run("pattern-match-with-suffix", func(t *testing.T) {
		// Test pattern like _amount$ or id$
		path := []string{"total_amount"}
		data := map[string][]string{
			"_amount$": {"100", "200", "300"},
		}
		res := replaceValueWithMapContext[[]string](path, data)
		assert.NotNil(res)
		assert.Contains([]string{"100", "200", "300"}, res)

		// Should match fields ending with id
		path = []string{"user_id"}
		data = map[string][]string{
			"id$": {"uuid1", "uuid2", "uuid3"},
		}
		res = replaceValueWithMapContext[[]string](path, data)
		assert.NotNil(res)
		assert.Contains([]string{"uuid1", "uuid2", "uuid3"}, res)
	})

	t.Run("wildcard-pattern-match", func(t *testing.T) {
		// Test wildcard * pattern that matches everything
		path := []string{"random_field"}
		abc := []string{"apple", "banana", "cherry"}
		data := map[string][]string{
			"*": abc,
		}
		res := replaceValueWithMapContext[[]string](path, data)
		assert.NotNil(res)
		// Should return a random value from the array
		assert.Contains(abc, res)

		// Should match any field name
		path = []string{"another_field"}
		res = replaceValueWithMapContext[[]string](path, data)
		assert.NotNil(res)
		assert.Contains(abc, res)
	})

	t.Run("direct-match-takes-precedence-over-wildcard", func(t *testing.T) {
		// Direct matches should take precedence over wildcard
		path := []string{"name"}
		data := map[string]string{
			"name": "specific-name",
			"*":    "wildcard-value",
		}
		// Direct match should win
		res := replaceValueWithMapContext[string](path, data)
		assert.Equal("specific-name", res)

		// Wildcard should match other fields
		path = []string{"other_field"}
		res = replaceValueWithMapContext[string](path, data)
		assert.Equal("wildcard-value", res)
	})

	t.Run("asterisk-wildcard-converted-to-regex", func(t *testing.T) {
		// Test that * is converted to .* regex pattern internally
		path := []string{"any_field_name"}
		data := map[string]string{
			"*": "wildcard-match",
		}
		res := replaceValueWithMapContext[string](path, data)
		assert.Equal("wildcard-match", res)

		// Should match various field names
		testCases := []string{
			"user_id",
			"email",
			"first_name",
			"status",
			"created_at",
		}
		for _, fieldName := range testCases {
			path = []string{fieldName}
			res = replaceValueWithMapContext[string](path, data)
			assert.Equal("wildcard-match", res, "* should match field: %s", fieldName)
		}
	})
}

func TestIsMapValue(t *testing.T) {
	assert := assert2.New(t)

	assert.True(isMapValue(map[string]any{"a": 1}))
	assert.True(isMapValue(map[string]string{"a": "b"}))
	assert.True(isMapValue(map[string]int{"a": 1}))
	assert.True(isMapValue(map[string]bool{"a": true}))
	assert.True(isMapValue(map[string]float64{"a": 1.0}))
	assert.False(isMapValue("string"))
	assert.False(isMapValue(123))
	assert.False(isMapValue(true))
	assert.False(isMapValue([]string{"a", "b"}))
}

func TestSuffixMatching(t *testing.T) {
	assert := assert2.New(t)

	t.Run("root-level-primitive-matches-any-depth", func(t *testing.T) {
		data := map[string]any{
			"foo": "global-foo",
		}
		path := []string{"response", "user", "data", "foo"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("global-foo", res)
	})

	t.Run("nested-context-suffix-match", func(t *testing.T) {
		data := map[string]any{
			"data": map[string]any{
				"foo": "nested-data-foo",
			},
		}
		path := []string{"user", "data", "foo"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("nested-data-foo", res)
	})

	t.Run("longer-suffix-wins-over-root-level", func(t *testing.T) {
		data := map[string]any{
			"foo": "global",
			"data": map[string]any{
				"foo": "specific",
			},
		}
		path := []string{"user", "data", "foo"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("specific", res)
	})

	t.Run("root-level-map-not-treated-as-value", func(t *testing.T) {
		data := map[string]any{
			"data": map[string]any{
				"inner": "value",
			},
		}
		path := []string{"data"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Nil(res)
	})

	t.Run("partial-suffix-match-deep-path", func(t *testing.T) {
		data := map[string]any{
			"address": map[string]any{
				"city": "Berlin",
			},
		}
		path := []string{"user", "home", "address", "city"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("Berlin", res)
	})

	t.Run("no-match-when-suffix-does-not-align", func(t *testing.T) {
		data := map[string]any{
			"billing": map[string]any{
				"city": "Munich",
			},
		}
		path := []string{"user", "shipping", "city"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Nil(res)
	})

	t.Run("regex-pattern-in-nested-context", func(t *testing.T) {
		data := map[string]any{
			"user": map[string]any{
				"_id$": "user-id-value",
			},
		}
		path := []string{"user", "account_id"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("user-id-value", res)
	})

	t.Run("regex-pattern-on-path-element-matches-nested", func(t *testing.T) {
		// Phase 1 regex: wildcard * on path element matches a parent
		data := map[string]any{
			"*": map[string]any{
				"city": "Berlin",
			},
		}
		path := []string{"user", "address", "city"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("Berlin", res)
	})

	t.Run("regex-pattern-on-path-element-with-suffix", func(t *testing.T) {
		// Phase 1 regex: pattern on path element
		data := map[string]any{
			"^addr": map[string]any{
				"city": "Munich",
			},
		}
		path := []string{"response", "address", "city"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("Munich", res)
	})

	t.Run("regex-pattern-root-level", func(t *testing.T) {
		data := map[string]any{
			"(_id|Id)$": "any-id-value",
		}
		path := []string{"response", "data", "userId"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("any-id-value", res)
	})

	t.Run("FakeFunc-at-root-matches-any-depth", func(t *testing.T) {
		fn := contexts.FakeFunc(func() contexts.MixedValue {
			return contexts.StringValue("fake-result")
		})
		data := map[string]any{
			"email": fn,
		}
		path := []string{"user", "contact", "email"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Equal("fake-result", res)
	})

	t.Run("array-at-root-matches-any-depth", func(t *testing.T) {
		data := map[string]any{
			"status": []string{"active", "inactive"},
		}
		path := []string{"order", "status"}
		res := replaceValueWithMapContext[any](path, data)
		assert.Contains([]string{"active", "inactive"}, res)
	})

	t.Run("nested-wins-over-root-in-replaceFromContext", func(t *testing.T) {
		fake := faker.New()
		s := &schema.Schema{Type: types.TypeString}
		state := NewReplaceStateWithName("order").WithOptions(WithName("status"))
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"status": []string{"global-a", "global-b"},
					"order": map[string]any{
						"status": []string{"pending", "shipped"},
					},
				},
			},
		})
		assert.Contains([]string{"pending", "shipped"}, res)
	})

	t.Run("exact-field-name-preserved-no-snake-case", func(t *testing.T) {
		fake := faker.New()
		s := &schema.Schema{Type: types.TypeString}
		state := NewReplaceStateWithName("dateOfBirth")
		res := replaceFromContext(&ReplaceContext{
			faker:  fake,
			schema: s,
			state:  state,
			data: []map[string]any{
				{
					"dateOfBirth": "1990-05-15",
				},
			},
		})
		assert.Equal("1990-05-15", res)
	})
}

func TestReplaceFromSchemaFormat(t *testing.T) {
	assert := assert2.New(t)

	t.Run("not-a-schema", func(t *testing.T) {
		res := replaceFromSchemaFormat(newTestReplaceContext("not-a-schema"))
		assert.Nil(res)
	})

	t.Run("unknown-format", func(t *testing.T) {
		s := &schema.Schema{
			Format: "my-format",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.Nil(res)
	})

	t.Run("date", func(t *testing.T) {
		s := &schema.Schema{
			Format: "date",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Equal(len(value), 10)
	})

	t.Run("date-time", func(t *testing.T) {
		s := &schema.Schema{
			Format: "date-time",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Equal(len(value), 24)
	})

	t.Run("email", func(t *testing.T) {
		s := &schema.Schema{
			Format: "email",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Greater(len(value), 6)
		assert.Contains(value, "@")
	})

	t.Run("uuid", func(t *testing.T) {
		s := &schema.Schema{Format: "uuid"}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Equal(len(value), 36)
	})

	t.Run("uuid-format-overrides-enum", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeString,
			Format: "uuid",
			Enum:   []any{"pending", "failed", "success"},
		}
		result := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(result)
		resultStr, ok := result.(string)
		assert.True(ok)
		assert.NotContains([]string{"pending", "failed", "success"}, resultStr)
		assert.Len(strings.ReplaceAll(resultStr, "-", ""), 32)
	})

	t.Run("password", func(t *testing.T) {
		s := &schema.Schema{
			Format: "password",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.GreaterOrEqual(len(value), 6)
	})

	t.Run("hostname", func(t *testing.T) {
		s := &schema.Schema{
			Format: "hostname",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Greater(len(value), 6)
		assert.Contains(value, ".")
	})

	t.Run("url", func(t *testing.T) {
		s := &schema.Schema{
			Format: "url",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Greater(len(value), 6)
		assert.Contains(value, ".")
		assert.True(strings.HasPrefix(value, "http"))
	})

	// Table-driven tests for integer formats
	intFormats := []string{"int32", "int64", "uint8", "uint16", "uint32", "uint64"}
	for _, format := range intFormats {
		t.Run(format, func(t *testing.T) {
			s := &schema.Schema{Format: format}
			res := replaceFromSchemaFormat(newTestReplaceContext(s))
			assert.NotNil(res)
		})
	}

	// Table-driven tests for string type with integer formats
	// This is a common pattern for large integers in JSON (e.g., timestamps, IDs)
	for _, format := range intFormats {
		t.Run("string-"+format, func(t *testing.T) {
			s := &schema.Schema{Type: types.TypeString, Format: format}
			res := replaceFromSchemaFormat(newTestReplaceContext(s))
			assert.NotNil(res)
			// Should be a string representation of a number
			value, ok := res.(string)
			assert.True(ok, "should return a string for type:string with format:%s", format)
			assert.NotEmpty(value)
			// Verify it's a valid number string
			// Use ParseUint for unsigned formats since they can exceed int64 max
			if strings.HasPrefix(format, "uint") {
				_, err := strconv.ParseUint(value, 10, 64)
				assert.NoError(err, "should be a valid unsigned integer string")
			} else {
				_, err := strconv.ParseInt(value, 10, 64)
				assert.NoError(err, "should be a valid integer string")
			}
		})
	}

	t.Run("ipv4", func(t *testing.T) {
		s := &schema.Schema{
			Format: "ipv4",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Greater(len(value), 6)
		addr := net.ParseIP(value)
		assert.NotNil(addr)
	})

	t.Run("ipv6", func(t *testing.T) {
		s := &schema.Schema{
			Format: "ipv6",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		assert.Greater(len(value), 6)
		addr := net.ParseIP(value)
		assert.NotNil(addr)
	})

	t.Run("byte", func(t *testing.T) {
		s := &schema.Schema{
			Format: "byte",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		// Should be a valid base64 string
		assert.Greater(len(value), 0)
		// Try to decode it to verify it's valid base64
		decoded, err := base64.StdEncoding.DecodeString(value)
		assert.NoError(err)
		assert.Greater(len(decoded), 0)
	})

	t.Run("byte format with constraints - should not modify base64", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "byte",
			MinLength: ptr(int64(100)), // This should be ignored for byte format
		}
		ctx := newTestReplaceContext(s)
		res := replaceFromSchemaFormat(ctx)
		assert.NotNil(res)

		// Apply constraints - should not modify the base64 string
		constrained := applySchemaStringConstraints(s, res.(string))
		assert.Equal(res, constrained, "base64 string should not be modified by constraints")

		// Verify it's still valid base64
		value, _ := constrained.(string)
		decoded, err := base64.StdEncoding.DecodeString(value)
		assert.NoError(err)
		assert.Greater(len(decoded), 0)
	})

	t.Run("byte format with plain text from context - should encode", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeString,
			Format: "byte",
		}

		// Simulate a plain text value coming from context
		plainText := "hello world"
		constrained := applySchemaStringConstraints(s, plainText)

		// Should be base64 encoded
		value, ok := constrained.(string)
		assert.True(ok)
		assert.NotEqual(plainText, value, "should be encoded")

		// Verify it's valid base64
		decoded, err := base64.StdEncoding.DecodeString(value)
		assert.NoError(err)
		assert.Equal("hello world", string(decoded))
	})

	t.Run("byte format with already base64 value - should not double encode", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeString,
			Format: "byte",
		}

		// Already base64 encoded value
		alreadyEncoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
		constrained := applySchemaStringConstraints(s, alreadyEncoded)

		// Should remain the same (not double encoded)
		value, ok := constrained.(string)
		assert.True(ok)
		assert.Equal(alreadyEncoded, value, "should not double encode")

		// Verify it decodes correctly
		decoded, err := base64.StdEncoding.DecodeString(value)
		assert.NoError(err)
		assert.Equal("hello world", string(decoded))
	})

	t.Run("binary format - should generate base64", func(t *testing.T) {
		s := &schema.Schema{
			Format: "binary",
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, _ := res.(string)
		// Should be a valid base64 string
		assert.Greater(len(value), 0)
		// Try to decode it to verify it's valid base64
		decoded, err := base64.StdEncoding.DecodeString(value)
		assert.NoError(err)
		assert.Greater(len(decoded), 0)
	})

	t.Run("binary format with plain text from context - should encode", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeString,
			Format: "binary",
		}

		// Simulate a plain text value coming from context
		plainText := "binary data here"
		constrained := applySchemaStringConstraints(s, plainText)

		// Should be base64 encoded
		value, ok := constrained.(string)
		assert.True(ok)
		assert.NotEqual(plainText, value, "should be encoded")

		// Verify it's valid base64
		decoded, err := base64.StdEncoding.DecodeString(value)
		assert.NoError(err)
		assert.Equal("binary data here", string(decoded))
	})
}

func TestReplaceFromSchemaPrimitive(t *testing.T) {
	assert := assert2.New(t)

	t.Run("not-a-schema", func(t *testing.T) {
		res := replaceFromSchemaPrimitive(newTestReplaceContext("not-a-schema"))
		assert.Nil(res)
	})

	t.Run("string", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString}
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		value, _ := res.(string)
		assert.Greater(len(value), 0)
	})

	t.Run("integer", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeInteger}
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		_, ok := res.(int32)
		assert.True(ok)
	})

	// Table-driven tests for integer formats
	intFormats := []string{"int32", "int64", "uint8", "uint16", "uint32", "uint64"}
	for _, format := range intFormats {
		t.Run("integer-"+format+"-format", func(t *testing.T) {
			s := &schema.Schema{Type: types.TypeInteger, Format: format}
			res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
			assert.NotNil(res)
		})
	}

	t.Run("number", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber}
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		_, ok := res.(int32)
		assert.True(ok)
	})

	t.Run("boolean", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeBoolean}
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		_, ok := res.(bool)
		assert.True(ok)
	})

	t.Run("other", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeObject}
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		assert.Nil(res)
	})

	t.Run("enum with zero and non-zero values - returns any enum value", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeInteger,
			Enum: []any{0, 1, 2, 3},
		}
		// Primitive replacer returns any value from enum (including 0)
		// The "prefer non-zero" logic is applied in the constraint step
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		assert.Contains(s.Enum, res, "should return a value from the enum")
	})

	t.Run("enum with all zeros - still returns zero", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeInteger,
			Enum: []any{0},
		}
		res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
		assert.Equal(0, res)
	})

	t.Run("integer without enum - returns non-zero", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeInteger}
		// Run multiple times to ensure we never get 0
		for i := 0; i < 20; i++ {
			res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
			value, ok := res.(int32)
			assert.True(ok)
			assert.NotEqual(int32(0), value, "should not return zero for integer type")
		}
	})

	t.Run("enum with null values - filters out null", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeString,
			Enum: []any{"approved", "changes_requested", nil, "null"},
		}
		for i := 0; i < 20; i++ {
			res := replaceFromSchemaPrimitive(newTestReplaceContext(s))
			assert.Contains([]any{"approved", "changes_requested"}, res)
		}
	})

}

func TestReplaceFromSchemaExample(t *testing.T) {
	assert := assert2.New(t)

	t.Run("not-a-schema", func(t *testing.T) {
		res := replaceFromSchemaExample(newTestReplaceContext("not-a-schema"))
		assert.Nil(res)
	})

	t.Run("with-a-schema", func(t *testing.T) {
		s := &schema.Schema{Example: "hallo, welt!"}
		res := replaceFromSchemaExample(newTestReplaceContext(s))
		assert.Equal("hallo, welt!", res)
	})
}

func TestApplySchemaConstraints(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil-schema", func(t *testing.T) {
		res := applySchemaConstraints(nil, "some-value")
		assert.Equal("some-value", res)
	})

	t.Run("not-a-schema", func(t *testing.T) {
		res := applySchemaConstraints("not-a-schema", "some-value")
		assert.Equal("some-value", res)
	})

	t.Run("case-not-applied", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeBoolean}
		res := applySchemaConstraints(s, true)
		assert.Equal(true, res)
	})

	t.Run("number-conv-fails", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber}
		res := applySchemaConstraints(s, "abc")
		assert.Nil(res)
	})

	t.Run("int-conv-fails", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeInteger}
		res := applySchemaConstraints(s, "abc")
		assert.Nil(res)
	})

	t.Run("string-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, MinLength: ptr(int64(5))}
		res := applySchemaConstraints(s, "hallo, welt!")
		assert.Equal("hallo, welt!", res)
	})

	t.Run("number-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Minimum: ptr(100.0)}
		res := applySchemaConstraints(s, 133)
		assert.Equal(133.0, res)
	})

	t.Run("int-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeInteger, Maximum: ptr(10.0)}
		res := applySchemaConstraints(s, 6)
		assert.Equal(int64(6), res)
	})

	t.Run("bool-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeBoolean}
		res := applySchemaConstraints(s, true)
		assert.True(res.(bool))
	})

	t.Run("bool-ok-with-enum", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeBoolean, Enum: []any{true}}
		res := applySchemaConstraints(s, false)
		assert.True(res.(bool))
	})
}

func TestApplySchemaStringConstraints(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil-schema", func(t *testing.T) {
		res := applySchemaStringConstraints(nil, "some-value")
		assert.Equal("some-value", res)
	})

	t.Run("no-constraints", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString}
		res := applySchemaStringConstraints(s, "hallo welt!")
		assert.Equal("hallo welt!", res)
	})

	t.Run("pattern-ok", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeString,
			Pattern: "^[0-9]{2}[a-z]+$",
		}

		res := applySchemaStringConstraints(s, "12go")
		assert.Equal("12go", res)
	})

	t.Run("pattern-fails", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeString,
			Pattern: "^[0-9]{2}$",
		}

		res := applySchemaStringConstraints(s, "12go")
		assert.NotNil(res)
	})

	t.Run("enum-ok", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeString,
			Enum: []any{
				"nice",
				"rice",
				"dice",
			},
		}

		res := applySchemaStringConstraints(s, "dice")
		assert.Equal("dice", res)
	})

	t.Run("enum-applied", func(t *testing.T) {
		enum := []any{
			"nice",
			"rice",
			"dice",
		}
		s := &schema.Schema{
			Type: types.TypeString,
			Enum: enum,
		}

		res := applySchemaStringConstraints(s, "mice")
		assert.Contains(enum, res)
	})

	t.Run("min-length-ok", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			MinLength: ptr(int64(5)),
		}

		res := applySchemaStringConstraints(s, "hallo")
		assert.Equal("hallo", res)
	})

	t.Run("min-length-applied", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			MinLength: ptr(int64(5)),
		}

		res := applySchemaStringConstraints(s, "ha")
		assert.Equal("ha---", res)
	})

	t.Run("max-length-ok", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			MaxLength: ptr(int64(5)),
		}

		res := applySchemaStringConstraints(s, "hallo")
		assert.Equal("hallo", res)
	})

	t.Run("max-length-applied", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			MaxLength: ptr(int64(5)),
		}

		res := applySchemaStringConstraints(s, "hallo welt!")
		assert.Equal("hallo", res)
	})

	t.Run("pattern-is-ignored", func(t *testing.T) {
		// Pattern validation/generation is intentionally skipped because
		// oapi-codegen doesn't generate validation for regex patterns
		s := &schema.Schema{
			Type:    types.TypeString,
			Pattern: "[0-9]+",
		}

		res := applySchemaStringConstraints(s, "hallo welt!")
		// Value is returned as-is since pattern is ignored
		assert.Equal("hallo welt!", res)
	})

	t.Run("date-time-ignores-minLength", func(t *testing.T) {
		// date-time format should not be modified by minLength constraints
		// This prevents corruption of datetime strings like "2006-01-02T15:04:05.000Z"
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "date-time",
			MinLength: ptr(int64(25)), // 24-char datetime would get padded without fix
		}

		datetime := "2006-01-02T15:04:05.000Z"
		res := applySchemaStringConstraints(s, datetime)
		assert.Equal(datetime, res)
	})

	t.Run("datetime-ignores-minLength", func(t *testing.T) {
		// datetime format (alternative spelling) should also be protected
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "datetime",
			MinLength: ptr(int64(30)),
		}

		datetime := "2006-01-02T15:04:05.000Z"
		res := applySchemaStringConstraints(s, datetime)
		assert.Equal(datetime, res)
	})

	t.Run("date-ignores-minLength", func(t *testing.T) {
		// date format should not be modified by minLength constraints
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "date",
			MinLength: ptr(int64(15)),
		}

		date := "2006-01-02"
		res := applySchemaStringConstraints(s, date)
		assert.Equal(date, res)
	})

	t.Run("date-time-ignores-maxLength", func(t *testing.T) {
		// date-time format should not be truncated by maxLength constraints
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "date-time",
			MaxLength: ptr(int64(10)),
		}

		datetime := "2006-01-02T15:04:05.000Z"
		res := applySchemaStringConstraints(s, datetime)
		assert.Equal(datetime, res)
	})

	t.Run("date-ignores-maxLength", func(t *testing.T) {
		// date format should not be truncated by maxLength constraints
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "date",
			MaxLength: ptr(int64(5)),
		}

		date := "2006-01-02"
		res := applySchemaStringConstraints(s, date)
		assert.Equal(date, res)
	})

	t.Run("nullable-enum-filters-null-string", func(t *testing.T) {
		// Nullable enums with "null" as a string value should filter it out
		// oapi-codegen doesn't generate constants for null, so we should never return "null"
		s := &schema.Schema{
			Type:     types.TypeString,
			Nullable: true,
			Enum: []any{
				"ACTIVE",
				"INACTIVE",
				"null", // This is the string "null", not nil
			},
		}

		// When the input value is not in the enum, it should pick a random valid value
		// The result should never be "null"
		for i := 0; i < 100; i++ {
			res := applySchemaStringConstraints(s, "UNKNOWN")
			assert.NotEqual("null", res, "Should never return 'null' string")
			assert.Contains([]any{"ACTIVE", "INACTIVE"}, res)
		}
	})

	t.Run("nullable-enum-with-nil-value", func(t *testing.T) {
		// Nullable enums with actual nil value should also filter it out
		s := &schema.Schema{
			Type:     types.TypeString,
			Nullable: true,
			Enum: []any{
				"ACTIVE",
				"INACTIVE",
				nil, // actual nil
			},
		}

		for i := 0; i < 100; i++ {
			res := applySchemaStringConstraints(s, "UNKNOWN")
			assert.NotNil(res, "Should never return nil")
			assert.Contains([]any{"ACTIVE", "INACTIVE"}, res)
		}
	})
}

func TestApplySchemaNumberConstraints(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil-schema", func(t *testing.T) {
		res := applySchemaNumberConstraints(nil, 123)
		assert.Equal(123.0, res)
	})

	t.Run("no-constraints", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber}
		res := applySchemaNumberConstraints(s, 123)
		assert.Equal(123.0, res)
	})

	t.Run("min-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Minimum: ptr(100.0)}
		res := applySchemaNumberConstraints(s, 100)
		assert.Equal(100.0, res)
	})

	t.Run("min-applied", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Minimum: ptr(100.0)}
		res := applySchemaNumberConstraints(s, 99)
		// Should be randomized between 100 and int32 max
		assert.GreaterOrEqual(res, 100.0)
		assert.LessOrEqual(res, 2147483647.0)
	})

	t.Run("max-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Maximum: ptr(100.0)}
		res := applySchemaNumberConstraints(s, 100)
		assert.Equal(100.0, res)
	})

	t.Run("max-applied", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, Maximum: ptr(100.0)}
		res := applySchemaNumberConstraints(s, 123)
		// Should be randomized between 1 and 100
		assert.GreaterOrEqual(res, 1.0)
		assert.LessOrEqual(res, 100.0)
	})

	t.Run("mult-of-ok", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, MultipleOf: ptr(5.0)}
		res := applySchemaNumberConstraints(s, 15)
		assert.Equal(15.0, res)
	})

	t.Run("mult-of-applied", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, MultipleOf: ptr(3.0)}
		res := applySchemaNumberConstraints(s, 100)
		assert.Equal(99.0, res)
	})

	t.Run("mult-of-produces-zero", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeNumber, MultipleOf: ptr(10.0)}
		// 5 / 10 = 0.5, int(0.5) = 0, 0 * 10 = 0, should return multipleOf value
		res := applySchemaNumberConstraints(s, 5)
		assert.Equal(10.0, res)
	})

	t.Run("min-max-mult-of-applied", func(t *testing.T) {
		s := &schema.Schema{
			Type:       types.TypeNumber,
			MultipleOf: ptr(3.0),
			Minimum:    ptr(12.0),
			Maximum:    ptr(21.0),
		}

		res := applySchemaNumberConstraints(s, 100)
		// Value should be randomized within range [12, 21]
		assert.GreaterOrEqual(res, 12.0)
		assert.LessOrEqual(res, 21.0)
	})

	t.Run("enum-ints", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeNumber,
			Enum: []any{10, 20, 30},
		}

		res := applySchemaNumberConstraints(s, 100)
		assert.Contains([]float64{10, 20, 30}, res)
	})

	t.Run("enum-floats", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeNumber,
			Enum: []any{10.1, 20.2, 30.3},
		}

		res := applySchemaNumberConstraints(s, 100)
		assert.Contains([]float64{10.1, 20.2, 30.3}, res)
	})

	t.Run("enum with zero and non-zero - returns any valid enum value", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeNumber,
			Enum: []any{0, 1, 2, 3},
		}

		// When input is not in enum, should return a random valid enum value (including 0)
		for i := 0; i < 20; i++ {
			res := applySchemaNumberConstraints(s, 100)
			assert.Contains([]float64{0, 1, 2, 3}, res, "should return a valid enum value")
		}
	})

	t.Run("enum with only zero - still returns zero", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeNumber,
			Enum: []any{0, 0.0},
		}

		res := applySchemaNumberConstraints(s, 100)
		assert.Equal(0.0, res)
	})

	t.Run("minimum_zero_is_respected", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeNumber,
			Minimum: ptr(0.0),
		}

		// Value below minimum should be randomized between 0 and int32 max
		res := applySchemaNumberConstraints(s, -5)
		assert.GreaterOrEqual(res, 0.0)
		assert.LessOrEqual(res, 2147483647.0)
	})

	t.Run("maximum_zero_is_respected", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeNumber,
			Maximum: ptr(0.0),
		}

		// Value above maximum should be randomized in valid range (negative to 0)
		res := applySchemaNumberConstraints(s, 5)
		assert.LessOrEqual(res, 0.0)
	})

	t.Run("maximum_zero_integer_generates_negative", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Maximum: ptr(0.0),
		}

		// Value above maximum should be randomized in valid range (negative to 0)
		for i := 0; i < 100; i++ {
			res := applySchemaNumberConstraints(s, 5)
			assert.LessOrEqual(res, 0.0, "iteration %d: should be <= 0", i)
		}
	})

	t.Run("minimum_and_maximum_zero", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeNumber,
			Minimum: ptr(0.0),
			Maximum: ptr(0.0),
		}

		// Value should be clamped to 0
		res := applySchemaNumberConstraints(s, 5)
		assert.Equal(0.0, res)

		res = applySchemaNumberConstraints(s, -5)
		assert.Equal(0.0, res)
	})

	t.Run("zero_value_only_valid_when_min_max_both_zero", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeNumber,
			Minimum: ptr(0.0),
			Maximum: ptr(0.0),
		}
		// When min=0 and max=0, zero is the only valid value
		res := applySchemaNumberConstraints(s, 0)
		assert.Equal(0.0, res)
	})

	t.Run("zero_value_for_non_integer_returns_small_positive", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeNumber,
		}
		// Zero value for non-integer should return 0.01
		res := applySchemaNumberConstraints(s, 0)
		assert.Equal(0.01, res)
	})

	t.Run("min_max_always_generates_within_range", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Minimum: ptr(0.0),
			Maximum: ptr(14.0),
		}

		// When min/max constraints exist, always generate within range
		// This ensures realistic values instead of huge random numbers
		for i := 0; i < 100; i++ {
			res := applySchemaNumberConstraints(s, 1234567890)
			assert.GreaterOrEqual(res, 0.0)
			assert.LessOrEqual(res, 14.0)
		}
	})

	t.Run("enum_starting_with_zero_returns_zero_when_input_is_zero", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeInteger,
			Enum: []any{0, 1, 2, 3, 4, 5},
		}

		// When input is 0 and 0 is a valid enum value, should return 0
		res := applySchemaNumberConstraints(s, 0)
		assert.Equal(0.0, res, "should return 0 when it's a valid enum value")
	})

	// Exclusive bounds tests - table-driven
	exclusiveBoundsTests := []struct {
		name      string
		schema    *schema.Schema
		input     float64
		checkFunc func(t *testing.T, res float64)
	}{
		{
			name:   "exclusive_min_below_bound",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMinimum: ptr(10.0)},
			input:  5,
			checkFunc: func(t *testing.T, res float64) {
				assert.Greater(res, 10.0)
				assert.LessOrEqual(res, 2147483647.0)
			},
		},
		{
			name:   "exclusive_min_integer_type",
			schema: &schema.Schema{Type: types.TypeInteger, ExclusiveMinimum: ptr(10.0)},
			input:  5,
			checkFunc: func(t *testing.T, res float64) {
				assert.GreaterOrEqual(res, 11.0) // exclusive min + 1 for integers
			},
		},
		{
			name:   "exclusive_max_integer_type",
			schema: &schema.Schema{Type: types.TypeInteger, ExclusiveMaximum: ptr(100.0)},
			input:  150,
			checkFunc: func(t *testing.T, res float64) {
				assert.LessOrEqual(res, 99.0) // exclusive max - 1 for integers
			},
		},
		{
			name:   "exclusive_min_equal_to_bound",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMinimum: ptr(10.0)},
			input:  10,
			checkFunc: func(t *testing.T, res float64) {
				assert.Greater(res, 10.0)
			},
		},
		{
			name:   "exclusive_min_above_bound",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMinimum: ptr(10.0)},
			input:  15,
			checkFunc: func(t *testing.T, res float64) {
				assert.Equal(15.0, res)
			},
		},
		{
			name:   "exclusive_max_above_bound",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMaximum: ptr(100.0)},
			input:  150,
			checkFunc: func(t *testing.T, res float64) {
				assert.Less(res, 100.0)
				assert.GreaterOrEqual(res, 1.0)
			},
		},
		{
			name:   "exclusive_max_below_bound",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMaximum: ptr(100.0)},
			input:  50,
			checkFunc: func(t *testing.T, res float64) {
				assert.Equal(50.0, res)
			},
		},
		{
			name:   "exclusive_min_and_max_combined",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMinimum: ptr(10.0), ExclusiveMaximum: ptr(20.0)},
			input:  5,
			checkFunc: func(t *testing.T, res float64) {
				assert.Greater(res, 10.0)
				assert.Less(res, 20.0)
			},
		},
		{
			name:   "exclusive_min_precedence",
			schema: &schema.Schema{Type: types.TypeNumber, Minimum: ptr(10.0), ExclusiveMinimum: ptr(15.0)},
			input:  12,
			checkFunc: func(t *testing.T, res float64) {
				assert.Greater(res, 15.0)
			},
		},
		{
			name:   "exclusive_max_precedence",
			schema: &schema.Schema{Type: types.TypeNumber, Maximum: ptr(100.0), ExclusiveMaximum: ptr(90.0)},
			input:  95,
			checkFunc: func(t *testing.T, res float64) {
				assert.Less(res, 90.0)
			},
		},
		{
			name:   "exclusive_bounds_with_multipleOf",
			schema: &schema.Schema{Type: types.TypeNumber, ExclusiveMinimum: ptr(10.0), ExclusiveMaximum: ptr(30.0), MultipleOf: ptr(5.0)},
			input:  33,
			checkFunc: func(t *testing.T, res float64) {
				assert.Greater(res, 10.0)
				assert.Less(res, 30.0)
			},
		},
	}

	for _, tc := range exclusiveBoundsTests {
		t.Run(tc.name, func(t *testing.T) {
			res := applySchemaNumberConstraints(tc.schema, tc.input)
			tc.checkFunc(t, res)
		})
	}

	t.Run("zero_value_with_min_zero_max_one_returns_one", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Minimum: ptr(0.0),
			Maximum: ptr(1.0),
		}

		// Value 0 is within bounds but should be avoided - return 1
		res := applySchemaNumberConstraints(s, 0)
		assert.Equal(1.0, res, "should return 1 to avoid zero value")
	})

	t.Run("zero_value_without_constraints_returns_one", func(t *testing.T) {
		s := &schema.Schema{
			Type: types.TypeInteger,
		}

		// No constraints, but 0 should still be avoided
		res := applySchemaNumberConstraints(s, 0)
		assert.Equal(1.0, res, "should return 1 to avoid zero value")
	})

	t.Run("zero_value_number_with_int_format_returns_one", func(t *testing.T) {
		// Some specs use type: number with format: int32
		// These should be treated as integers
		s := &schema.Schema{
			Type:   types.TypeNumber,
			Format: "int32",
		}
		res := applySchemaNumberConstraints(s, 0)
		assert.Equal(1.0, res, "type:number with format:int32 should return 1, not 0.01")
	})

	t.Run("out_of_bounds_with_min_zero_max_one_never_returns_zero", func(t *testing.T) {
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Minimum: ptr(0.0),
			Maximum: ptr(1.0),
		}

		// When regenerating due to out of bounds, should never return 0
		for i := 0; i < 100; i++ {
			res := applySchemaNumberConstraints(s, 1000) // Out of bounds
			assert.Equal(1.0, res, "iteration %d: should always return 1, not 0", i)
		}
	})

	t.Run("large_minimum_without_maximum_generates_valid_value", func(t *testing.T) {
		// This tests the case where minimum is larger than int32 max (e.g., Unix timestamps in milliseconds)
		// Previously this would panic with "invalid argument to Int63n" because
		// the default max (2147483647) was smaller than the minimum
		largeMin := float64(1356998400070) // Unix timestamp in milliseconds
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Minimum: &largeMin,
		}

		// Should not panic and should generate a value >= minimum
		for i := 0; i < 10; i++ {
			res := applySchemaNumberConstraints(s, 0) // Out of bounds, triggers regeneration
			assert.GreaterOrEqual(res, largeMin, "iteration %d: should be >= minimum", i)
		}
	})
}

func TestReplaceFromSchemaFallback(t *testing.T) {
	assert := assert2.New(t)

	t.Run("not-a-schema", func(t *testing.T) {
		res := replaceFromSchemaFallback(newTestReplaceContext("not-a-schema"))
		assert.Nil(res)
	})

	t.Run("with-a-schema", func(t *testing.T) {
		s := &schema.Schema{Default: "hallo, welt!"}
		res := replaceFromSchemaFallback(newTestReplaceContext(s))
		assert.Equal("hallo, welt!", res)
	})
}

func TestEnsureNonZeroInt(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"int32-zero", int32(0), int32(1)},
		{"int32-positive", int32(42), int32(42)},
		{"int32-negative", int32(-42), int32(42)},
		{"int64-max", int64(9223372036854775807), int64(9223372036854775807)},
		{"int64-large-negative", int64(-9223372036854775807), int64(9223372036854775807)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch v := tc.input.(type) {
			case int32:
				assert.Equal(tc.expected, ensureNonZeroInt(v))
			case int64:
				assert.Equal(tc.expected, ensureNonZeroInt(v))
			}
		})
	}
}

func TestEnsureNonZeroUint(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"uint8-zero", uint8(0), uint8(1)},
		{"uint8-positive", uint8(42), uint8(42)},
		{"uint8-max", uint8(255), uint8(255)},
		{"uint64-zero", uint64(0), uint64(1)},
		{"uint64-max", uint64(18446744073709551615), uint64(18446744073709551615)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			switch v := tc.input.(type) {
			case uint8:
				assert.Equal(tc.expected, ensureNonZeroUint(v))
			case uint64:
				assert.Equal(tc.expected, ensureNonZeroUint(v))
			}
		})
	}
}

func TestGetExpectedUUIDLength(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil schema returns 0", func(t *testing.T) {
		assert.Equal(0, getExpectedUUIDLength(nil))
	})

	t.Run("no constraints returns 0", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, Format: "uuid"}
		assert.Equal(0, getExpectedUUIDLength(s))
	})

	t.Run("minLength only returns minLength", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, Format: "uuid", MinLength: ptr(int64(32))}
		assert.Equal(32, getExpectedUUIDLength(s))
	})

	t.Run("maxLength only returns maxLength", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, Format: "uuid", MaxLength: ptr(int64(36))}
		assert.Equal(36, getExpectedUUIDLength(s))
	})

	t.Run("equal minLength and maxLength returns that value", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, Format: "uuid", MinLength: ptr(int64(64)), MaxLength: ptr(int64(64))}
		assert.Equal(64, getExpectedUUIDLength(s))
	})

	t.Run("different minLength and maxLength returns maxLength", func(t *testing.T) {
		s := &schema.Schema{Type: types.TypeString, Format: "uuid", MinLength: ptr(int64(32)), MaxLength: ptr(int64(36))}
		assert.Equal(36, getExpectedUUIDLength(s))
	})
}

func TestIsHexString(t *testing.T) {
	assert := assert2.New(t)

	t.Run("valid hex lowercase", func(t *testing.T) {
		assert.True(isHexString("0123456789abcdef"))
	})

	t.Run("valid hex uppercase", func(t *testing.T) {
		assert.True(isHexString("0123456789ABCDEF"))
	})

	t.Run("valid hex mixed case", func(t *testing.T) {
		assert.True(isHexString("0123456789AbCdEf"))
	})

	t.Run("empty string is valid", func(t *testing.T) {
		assert.True(isHexString(""))
	})

	t.Run("invalid with dash", func(t *testing.T) {
		assert.False(isHexString("550e8400-e29b-41d4"))
	})

	t.Run("invalid with non-hex char", func(t *testing.T) {
		assert.False(isHexString("0123456789abcdefg"))
	})
}

func TestGenerateHexString(t *testing.T) {
	assert := assert2.New(t)

	t.Run("generates correct length", func(t *testing.T) {
		for _, length := range []int{16, 32, 64, 128} {
			result := generateHexString(length)
			assert.Equal(length, len(result))
			assert.True(isHexString(result))
		}
	})

	t.Run("generates valid hex", func(t *testing.T) {
		result := generateHexString(64)
		assert.True(isHexString(result))
	})
}

func TestUUIDWithNonStandardLength(t *testing.T) {
	assert := assert2.New(t)

	t.Run("uuid-32-chars-no-dashes", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "uuid",
			MinLength: ptr(int64(32)),
			MaxLength: ptr(int64(32)),
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, ok := res.(string)
		assert.True(ok)
		assert.Equal(32, len(value))
		assert.True(isHexString(value))
	})

	t.Run("uuid-64-chars-hex-string", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "uuid",
			MinLength: ptr(int64(64)),
			MaxLength: ptr(int64(64)),
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, ok := res.(string)
		assert.True(ok)
		assert.Equal(64, len(value))
		assert.True(isHexString(value))
	})

	t.Run("uuid-standard-36-chars", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "uuid",
			MinLength: ptr(int64(36)),
			MaxLength: ptr(int64(36)),
		}
		res := replaceFromSchemaFormat(newTestReplaceContext(s))
		assert.NotNil(res)
		value, ok := res.(string)
		assert.True(ok)
		assert.Equal(36, len(value))
		// Standard UUID has dashes
		assert.Contains(value, "-")
	})
}

func TestHasCorrectSchemaValueUUIDWithNonStandardLength(t *testing.T) {
	assert := assert2.New(t)

	t.Run("valid-64-char-hex-string", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "uuid",
			MinLength: ptr(int64(64)),
			MaxLength: ptr(int64(64)),
		}
		// Generate a 64-char hex string
		hexStr := generateHexString(64)
		res := hasCorrectSchemaValue(newTestReplaceContext(s), hexStr)
		assert.True(res)
	})

	t.Run("invalid-64-char-with-wrong-length", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "uuid",
			MinLength: ptr(int64(64)),
			MaxLength: ptr(int64(64)),
		}
		// Standard UUID is 36 chars, should fail for 64-char constraint
		res := hasCorrectSchemaValue(newTestReplaceContext(s), "550e8400-e29b-41d4-a716-446655440000")
		assert.False(res)
	})

	t.Run("valid-32-char-uuid-without-dashes", func(t *testing.T) {
		s := &schema.Schema{
			Type:      types.TypeString,
			Format:    "uuid",
			MinLength: ptr(int64(32)),
			MaxLength: ptr(int64(32)),
		}
		// UUID without dashes
		res := hasCorrectSchemaValue(newTestReplaceContext(s), "550e8400e29b41d4a716446655440000")
		assert.True(res)
	})

	t.Run("standard-uuid-with-no-constraints", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeString,
			Format: "uuid",
		}
		res := hasCorrectSchemaValue(newTestReplaceContext(s), "550e8400-e29b-41d4-a716-446655440000")
		assert.True(res)
	})
}

func TestIsIntegerSchema(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name     string
		schema   *schema.Schema
		expected bool
	}{
		{
			name:     "nil schema",
			schema:   nil,
			expected: false,
		},
		{
			name:     "integer type",
			schema:   &schema.Schema{Type: types.TypeInteger},
			expected: true,
		},
		{
			name:     "number type without format",
			schema:   &schema.Schema{Type: types.TypeNumber},
			expected: false,
		},
		{
			name:     "number type with int32 format",
			schema:   &schema.Schema{Type: types.TypeNumber, Format: "int32"},
			expected: true,
		},
		{
			name:     "number type with int64 format",
			schema:   &schema.Schema{Type: types.TypeNumber, Format: "int64"},
			expected: true,
		},
		{
			name:     "string type with int32 format",
			schema:   &schema.Schema{Type: types.TypeString, Format: "int32"},
			expected: true,
		},
		{
			name:     "string type",
			schema:   &schema.Schema{Type: types.TypeString},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIntegerSchema(tt.schema)
			assert.Equal(tt.expected, result)
		})
	}
}
