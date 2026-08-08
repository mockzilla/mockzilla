//go:build !integration

package contexts

import (
	"reflect"
	"testing"
	"time"

	"github.com/jaswdr/faker/v2"
	assert2 "github.com/stretchr/testify/assert"
)

func TestMixedValues(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	s := StringValue("hello")
	assert.Equal("hello", s.Get())

	i := IntValue(123)
	assert.Equal(int64(123), i.Get())

	f := Float64Value(123.456)
	assert.Equal(123.456, f.Get())

	b := BoolValue(true)
	assert.Equal(true, b.Get())
}

func TestFromReflectedStringValue(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	fn := reflect.ValueOf(func() string {
		return "hello"
	})
	f := fromReflectedStringValue(fn)
	assert.Equal("hello", f().Get())
}

func TestFromReflectedIntValue(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	fn := reflect.ValueOf(func() int {
		return 123
	})
	f := fromReflectedIntValue(fn)
	assert.Equal(int64(123), f().Get())
}

func TestFromReflectedUIntValue(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	fn := reflect.ValueOf(func() uint {
		return 123
	})
	f := fromReflectedUIntValue(fn)
	assert.Equal(int64(123), f().Get())
}

func TestFromReflectedBoolValue(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	fn := reflect.ValueOf(func() bool {
		return true
	})
	f := fromReflectedBoolValue(fn)
	assert.Equal(true, f().Get())
}

func TestFromReflectedFloat64Value(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	fn := reflect.ValueOf(func() float64 {
		return 123.456
	})
	f := fromReflectedFloat64Value(fn)
	assert.Equal(123.456, f().Get())
}

func TestGetFakeFuncFactoryWithString(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	funcs := getFakeFuncFactoryWithString()
	assert.NotNil(funcs)

	expectedKeys := []string{
		"botify",
		"echo",
	}
	var keys []string
	for key, fn := range funcs {
		assert.NotNil(fn)
		keys = append(keys, key)
		res := fn("hello")()
		assert.Greater(len(res.Get().(string)), 0)
	}

	assert.ElementsMatch(expectedKeys, keys)
}

func TestGetFakeFuncFactoryWith2Strings(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	funcs := getFakeFuncFactoryWith2Strings()
	assert.NotNil(funcs)

	expectedKeys := []string{
		"int_between",
		"date_between",
	}
	var keys []string
	for key := range funcs {
		keys = append(keys, key)
	}

	assert.ElementsMatch(expectedKeys, keys)

	t.Run("int_between valid range", func(t *testing.T) {
		fn := funcs["int_between"]("100", "50000")
		for i := 0; i < 100; i++ {
			val := fn().Get().(int64)
			assert.GreaterOrEqual(val, int64(100))
			assert.LessOrEqual(val, int64(50000))
		}
	})

	t.Run("int_between single value", func(t *testing.T) {
		fn := funcs["int_between"]("42", "42")
		val := fn().Get().(int64)
		assert.Equal(int64(42), val)
	})

	t.Run("date_between offset and now", func(t *testing.T) {
		fn := funcs["date_between"]("-30d", "now")
		for i := 0; i < 100; i++ {
			val, err := time.Parse("2006-01-02T15:04:05.000Z", fn().Get().(string))
			assert.NoError(err)
			assert.True(val.After(time.Now().AddDate(0, 0, -31)), "%s is older than the window", val)
			assert.True(val.Before(time.Now().Add(time.Minute)), "%s is in the future", val)
		}
	})

	t.Run("date_between absolute dates", func(t *testing.T) {
		fn := funcs["date_between"]("2024-01-01", "2024-12-31")
		val, err := time.Parse("2006-01-02T15:04:05.000Z", fn().Get().(string))
		assert.NoError(err)
		assert.Equal(2024, val.Year())
	})

	t.Run("date_between swapped bounds still land inside them", func(t *testing.T) {
		fn := funcs["date_between"]("now", "-24h")
		val, err := time.Parse("2006-01-02T15:04:05.000Z", fn().Get().(string))
		assert.NoError(err)
		assert.True(val.After(time.Now().Add(-25*time.Hour)), "%s is outside the day", val)
	})

	t.Run("date_between unreadable bounds mean now", func(t *testing.T) {
		fn := funcs["date_between"]("nonsense", "also nonsense")
		val, err := time.Parse("2006-01-02T15:04:05.000Z", fn().Get().(string))
		assert.NoError(err)
		assert.WithinDuration(time.Now(), val, time.Minute)
	})
}

func TestGetFakes(t *testing.T) {
	assert := assert2.New(t)

	fakes := getFakes()
	assert.Greater(len(fakes), 0)

	assert.Equal("bar", fakes["foo"]().Get())
}

func TestGetFakeFuncs(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	visited := make(map[reflect.Type]bool)
	fakes := getFakeFuncs(faker.New(), "", visited)
	assert.Greater(len(fakes), 0)
}
