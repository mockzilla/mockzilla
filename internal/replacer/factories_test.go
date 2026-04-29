//go:build !integration

package replacer

import (
	"testing"

	"github.com/jaswdr/faker/v2"
	"github.com/mockzilla/mockzilla/v2/internal/contexts"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	assert2 "github.com/stretchr/testify/assert"
)

func TestReplacers(t *testing.T) {
	assert := assert2.New(t)
	assert.Equal(9, len(Replacers))
}

func TestCreateValueReplacer(t *testing.T) {
	assert := assert2.New(t)
	fooReplacer := func(ctx *ReplaceContext) any { return "foo" }
	intReplacer := func(ctx *ReplaceContext) any { return 1 }
	nilReplacer := func(ctx *ReplaceContext) any { return nil }
	forceNullReplacer := func(ctx *ReplaceContext) any { return NULL }

	t.Run("with-nil-state", func(t *testing.T) {
		fn := CreateValueReplacer([]Replacer{fooReplacer}, nil)
		res := fn("", nil)
		assert.Equal("foo", res)
	})

	t.Run("with-incorrect-schema-type", func(t *testing.T) {
		fn := CreateValueReplacer([]Replacer{fooReplacer, intReplacer}, nil)
		s := &schema.Schema{Type: types.TypeInteger}
		res := fn(s, nil)
		assert.Equal(int64(1), res)
	})

	t.Run("with-force-null", func(t *testing.T) {
		fn := CreateValueReplacer([]Replacer{forceNullReplacer, fooReplacer}, nil)
		res := fn("", nil)
		assert.Nil(res)
	})

	t.Run("continues-on-nil", func(t *testing.T) {
		fn := CreateValueReplacer([]Replacer{nilReplacer, fooReplacer}, nil)
		res := fn("foo", nil)
		assert.Equal("foo", res)
	})

	t.Run("continues-on-empty-string", func(t *testing.T) {
		emptyStringReplacer := func(ctx *ReplaceContext) any { return "" }
		fn := CreateValueReplacer([]Replacer{emptyStringReplacer, fooReplacer}, nil)
		res := fn("", nil)
		assert.Equal("foo", res, "should skip empty string and continue to next replacer")
	})

	t.Run("continues-on-zero-integer", func(t *testing.T) {
		zeroReplacer := func(ctx *ReplaceContext) any { return int32(0) }
		fn := CreateValueReplacer([]Replacer{zeroReplacer, intReplacer}, nil)
		s := &schema.Schema{Type: types.TypeInteger}
		res := fn(s, nil)
		assert.Equal(int64(1), res, "should skip zero and continue to next replacer")
	})

	t.Run("finishes-with-nil", func(t *testing.T) {
		fn := CreateValueReplacer([]Replacer{nilReplacer, nilReplacer}, nil)
		res := fn("foo", nil)
		assert.Nil(res)
	})
}

func TestIsCorrectlyReplacedType(t *testing.T) {
	assert := assert2.New(t)
	type testCase struct {
		value    any
		needed   string
		expected bool
	}

	testCases := []testCase{
		{"foo", types.TypeString, true},
		{1, types.TypeString, false},
		{"foo", types.TypeInteger, false},
		{1, types.TypeInteger, true},
		{"1", types.TypeNumber, false},
		{1, types.TypeNumber, true},
		{1.12, types.TypeNumber, true},
		{"true", types.TypeBoolean, false},
		{true, types.TypeBoolean, true},
		{[]string{"foo", "bar"}, types.TypeArray, true},
		{[]int{1, 2}, types.TypeArray, true},
		{[]bool{true, false}, types.TypeArray, true},
		{map[string]string{"foo": "bar"}, types.TypeObject, true},
		{map[string]int{"foo": 1}, types.TypeObject, true},
		{map[string]bool{"foo": true}, types.TypeObject, true},
		{map[string]any{"foo": "bar"}, types.TypeObject, true},
		{"foo", types.TypeObject, false},
		{"foo", "bar", false},
		{"anything", "any", true},
		{123, "any", true},
		{true, "any", true},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			assert.Equal(tc.expected, IsCorrectlyReplacedType(tc.value, tc.needed))
		})
	}
}

func TestGetContextFunctions(t *testing.T) {
	assert := assert2.New(t)

	t.Run("empty contexts", func(t *testing.T) {
		res := getContextFunctions(nil)
		assert.NotNil(res)
		assert.Equal(0, len(res))
	})

	t.Run("contexts without functions", func(t *testing.T) {
		contexts := []map[string]any{
			{
				"name":  "John",
				"age":   30,
				"email": "john@example.com",
			},
			{
				"city":    "New York",
				"country": "USA",
			},
		}
		res := getContextFunctions(contexts)
		assert.NotNil(res)
		assert.Equal(0, len(res))
	})

	t.Run("contexts with functions", func(t *testing.T) {
		fn1 := func() contexts.MixedValue {
			return contexts.StringValue("hello")
		}
		fn2 := func() contexts.MixedValue {
			return contexts.IntValue(123)
		}
		fn3 := func() contexts.MixedValue {
			return contexts.BoolValue(true)
		}

		contextData := []map[string]any{
			{
				"greeting": contexts.FakeFunc(fn1),
				"name":     "John",
			},
			{
				"count":   contexts.FakeFunc(fn2),
				"enabled": contexts.FakeFunc(fn3),
				"city":    "New York",
			},
		}

		res := getContextFunctions(contextData)
		assert.NotNil(res)
		assert.Equal(3, len(res))
		assert.Contains(res, "greeting")
		assert.Contains(res, "count")
		assert.Contains(res, "enabled")

		// Verify the functions work correctly
		assert.Equal("hello", res["greeting"]().Get())
		assert.Equal(int64(123), res["count"]().Get())
		assert.Equal(true, res["enabled"]().Get())
	})

	t.Run("contexts with mixed types", func(t *testing.T) {
		fn := func() contexts.MixedValue {
			return contexts.StringValue("test")
		}

		contextData := []map[string]any{
			{
				"func":   contexts.FakeFunc(fn),
				"string": "value",
				"int":    42,
				"bool":   true,
				"slice":  []string{"a", "b"},
				"map":    map[string]string{"key": "value"},
			},
		}

		res := getContextFunctions(contextData)
		assert.NotNil(res)
		assert.Equal(1, len(res))
		assert.Contains(res, "func")
		assert.Equal("test", res["func"]().Get())
	})

	t.Run("multiple contexts with same function name - last one wins", func(t *testing.T) {
		fn1 := func() contexts.MixedValue {
			return contexts.StringValue("first")
		}
		fn2 := func() contexts.MixedValue {
			return contexts.StringValue("second")
		}

		contextData := []map[string]any{
			{
				"greeting": contexts.FakeFunc(fn1),
			},
			{
				"greeting": contexts.FakeFunc(fn2),
			},
		}

		res := getContextFunctions(contextData)
		assert.NotNil(res)
		assert.Equal(1, len(res))
		assert.Equal("second", res["greeting"]().Get())
	})
}

func TestReplaceContext_function(t *testing.T) {
	assert := assert2.New(t)

	t.Run("function exists", func(t *testing.T) {
		fn := func() contexts.MixedValue {
			return contexts.StringValue("test-value")
		}
		ctx := &ReplaceContext{
			functions: map[string]contexts.FakeFunc{
				"testFunc": fn,
			},
		}

		result := ctx.function("testFunc")
		assert.NotNil(result)
		assert.Equal("test-value", result.Get())
	})

	t.Run("function does not exist", func(t *testing.T) {
		ctx := &ReplaceContext{
			functions: map[string]contexts.FakeFunc{},
		}

		result := ctx.function("nonExistent")
		assert.Nil(result)
	})

	t.Run("function returns different types", func(t *testing.T) {
		ctx := &ReplaceContext{
			functions: map[string]contexts.FakeFunc{
				"stringFunc": func() contexts.MixedValue {
					return contexts.StringValue("hello")
				},
				"intFunc": func() contexts.MixedValue {
					return contexts.IntValue(42)
				},
				"floatFunc": func() contexts.MixedValue {
					return contexts.Float64Value(3.14)
				},
				"boolFunc": func() contexts.MixedValue {
					return contexts.BoolValue(true)
				},
			},
		}

		assert.Equal("hello", ctx.function("stringFunc").Get())
		assert.Equal(int64(42), ctx.function("intFunc").Get())
		assert.Equal(3.14, ctx.function("floatFunc").Get())
		assert.Equal(true, ctx.function("boolFunc").Get())
	})
}

func TestReplaceContext_stringExpression(t *testing.T) {
	assert := assert2.New(t)

	t.Run("returns string from expression function", func(t *testing.T) {
		ctx := &ReplaceContext{
			functions: map[string]contexts.FakeFunc{
				"expression": func() contexts.MixedValue {
					return contexts.StringValue("dynamic-expression")
				},
			},
		}

		result := ctx.stringExpression()
		assert.Equal("dynamic-expression", result)
	})

	t.Run("returns different values on each call", func(t *testing.T) {
		counter := 0
		ctx := &ReplaceContext{
			functions: map[string]contexts.FakeFunc{
				"expression": func() contexts.MixedValue {
					counter++
					return contexts.StringValue("value-" + string(rune('0'+counter)))
				},
			},
		}

		result1 := ctx.stringExpression()
		result2 := ctx.stringExpression()
		assert.NotEqual(result1, result2)
	})

	t.Run("uses faker fallback when expression function not available", func(t *testing.T) {
		ctx := &ReplaceContext{
			functions: map[string]contexts.FakeFunc{},
			faker:     faker.New(),
		}

		result := ctx.stringExpression()
		assert.NotEmpty(result, "should return a faker-generated word")
	})

	t.Run("uses faker fallback when functions map is nil", func(t *testing.T) {
		ctx := &ReplaceContext{
			functions: nil,
			faker:     faker.New(),
		}

		result := ctx.stringExpression()
		assert.NotEmpty(result, "should return a faker-generated word")
	})
}

func TestRequiredFieldsNeverGetZeroOrEmpty(t *testing.T) {
	assert := assert2.New(t)

	t.Run("required-string-with-empty-context", func(t *testing.T) {
		// Simulate context returning empty string (like when pattern doesn't match)
		s := &schema.Schema{
			Type: types.TypeString,
		}
		state := NewReplaceStateWithName("merchant_currency")
		cs := []map[string]any{
			{
				// Context has no matching pattern for merchant_currency
				"other_field": "value",
			},
		}

		// Run 100 times to ensure no flakiness
		for i := 0; i < 100; i++ {
			fn := CreateValueReplacer(Replacers, cs)
			res := fn(s, state)
			assert.NotNil(res, "iteration %d: should not return nil for required string", i)
			if str, ok := res.(string); ok {
				assert.NotEmpty(str, "iteration %d: should not return empty string for required field", i)
			}
		}
	})

	t.Run("required-integer-with-zero-context", func(t *testing.T) {
		// Simulate context returning 0 (like from fake.u_int8)
		s := &schema.Schema{
			Type: types.TypeInteger,
		}
		state := NewReplaceStateWithName("presentment_amount")
		cs := []map[string]any{
			{
				// Context might return 0 from faker
				"presentment_amount": 0,
			},
		}

		// Run 100 times to ensure no flakiness
		for i := 0; i < 100; i++ {
			fn := CreateValueReplacer(Replacers, cs)
			res := fn(s, state)
			assert.NotNil(res, "iteration %d: should not return nil for required integer", i)
			if intVal, ok := types.ToInt64(res); ok {
				assert.NotEqual(int64(0), intVal, "iteration %d: should not return 0 for required field", i)
			}
		}
	})

	t.Run("required-int32-with-zero-context", func(t *testing.T) {
		s := &schema.Schema{
			Type:   types.TypeInteger,
			Format: "int32",
		}
		state := NewReplaceStateWithName("count")
		cs := []map[string]any{
			{
				"count": int32(0),
			},
		}

		for i := 0; i < 100; i++ {
			fn := CreateValueReplacer(Replacers, cs)
			res := fn(s, state)
			assert.NotNil(res, "iteration %d: should not return nil", i)
			if intVal, ok := types.ToInt32(res); ok {
				assert.NotEqual(int32(0), intVal, "iteration %d: should not return 0", i)
			}
		}
	})

	t.Run("required-string-with-pattern-context-returning-empty", func(t *testing.T) {
		// Simulate a context pattern that might return empty
		s := &schema.Schema{
			Type: types.TypeString,
		}
		state := NewReplaceStateWithName("currency")
		cs := []map[string]any{
			{
				"currency": "",
			},
		}

		for i := 0; i < 100; i++ {
			fn := CreateValueReplacer(Replacers, cs)
			res := fn(s, state)
			assert.NotNil(res, "iteration %d: should not return nil", i)
			if str, ok := res.(string); ok {
				assert.NotEmpty(str, "iteration %d: should not return empty string", i)
			}
		}
	})

	t.Run("required-string-with-faker-function-returning-empty", func(t *testing.T) {
		// Simulate a FakeFunc that returns empty string (like Currency().Code() sometimes does)
		emptyFakeFunc := func() contexts.MixedValue {
			return contexts.StringValue("")
		}

		s := &schema.Schema{
			Type: types.TypeString,
		}
		state := NewReplaceStateWithName("presentment_currency")
		cs := []map[string]any{
			{
				"presentment_currency": contexts.FakeFunc(emptyFakeFunc),
			},
		}

		for i := 0; i < 100; i++ {
			fn := CreateValueReplacer(Replacers, cs)
			res := fn(s, state)
			assert.NotNil(res, "iteration %d: should not return nil", i)
			if str, ok := res.(string); ok {
				assert.NotEmpty(str, "iteration %d: should not return empty string from FakeFunc", i)
			}
		}
	})

	t.Run("integer-with-minimum-zero-avoids-zero", func(t *testing.T) {
		// Even when schema has minimum: 0, we avoid zero because validators treat it as unset
		minVal := float64(0)
		maxVal := float64(1)
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Minimum: &minVal,
			Maximum: &maxVal,
		}
		state := NewReplaceStateWithName("is_groundhog")
		cs := []map[string]any{
			{
				"is_groundhog": 0,
			},
		}

		fn := CreateValueReplacer(Replacers, cs)
		res := fn(s, state)
		assert.NotNil(res, "should not return nil")
		if intVal, ok := types.ToInt64(res); ok {
			assert.Equal(int64(1), intVal, "should return 1 to avoid zero")
		}
	})

	t.Run("integer-with-enum-including-zero-allows-zero", func(t *testing.T) {
		// When schema has enum that includes 0, zero should be allowed
		s := &schema.Schema{
			Type: types.TypeInteger,
			Enum: []any{0, 1},
		}
		state := NewReplaceStateWithName("active")
		cs := []map[string]any{
			{
				"active": 0,
			},
		}

		fn := CreateValueReplacer(Replacers, cs)
		res := fn(s, state)
		assert.NotNil(res, "should not return nil when enum includes zero")
		if intVal, ok := types.ToInt64(res); ok {
			assert.Equal(int64(0), intVal, "should return 0 when enum includes it")
		}
	})

	t.Run("integer-with-min-zero-max-one-generates-valid-value", func(t *testing.T) {
		// Simulate the isGroundhog schema from groundhog-day.com
		minVal := float64(0)
		maxVal := float64(1)
		s := &schema.Schema{
			Type:    types.TypeInteger,
			Minimum: &minVal,
			Maximum: &maxVal,
		}
		state := NewReplaceStateWithName("is_groundhog")

		// Run multiple times to ensure we always get a valid value (0 or 1)
		nilCount := 0
		for i := 0; i < 100; i++ {
			fn := CreateValueReplacer(Replacers, nil)
			res := fn(s, state)
			if res == nil {
				nilCount++
				continue
			}
			if intVal, ok := types.ToInt64(res); ok {
				assert.True(intVal >= 0 && intVal <= 1, "iteration %d: value %d should be between 0 and 1", i, intVal)
			}
		}
		// We should never get nil when schema has min=0, max=1
		assert.Equal(0, nilCount, "should never return nil for integer with min=0, max=1")
	})
}
