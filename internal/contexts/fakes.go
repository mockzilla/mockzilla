package contexts

import (
	"reflect"
	"strconv"

	"github.com/jaswdr/faker/v2"
	"github.com/mockzilla/mockzilla/v2/internal/types"
)

// FakeFunc is a function that returns a MixedValue.
// This is u unified way to work with different return types from fake library.
type FakeFunc func() MixedValue

// FakeFuncFactoryWithString is a function that returns a FakeFunc.
type FakeFuncFactoryWithString func(value string) FakeFunc

// FakeFuncFactoryWith2Strings is a function that returns a FakeFunc with 2 string arguments.
type FakeFuncFactoryWith2Strings func(arg1, arg2 string) FakeFunc

var (
	ContextFunctions0Arg = getFakes()
	ContextFunctions1Arg = getFakeFuncFactoryWithString()
	ContextFunctions2Arg = getFakeFuncFactoryWith2Strings()
)

// MixedValue is a value that can represent string, int, float64, or bool type.
type MixedValue interface {
	Get() any
}

type StringValue string
type IntValue int
type Float64Value float64
type BoolValue bool

func (s StringValue) Get() any {
	return string(s)
}

func (i IntValue) Get() any {
	return int64(i)
}

func (f Float64Value) Get() any {
	return float64(f)
}

func (b BoolValue) Get() any {
	return bool(b)
}

// getFakeFuncFactoryWithString returns a map of utility fake functions.
func getFakeFuncFactoryWithString() map[string]FakeFuncFactoryWithString {
	fake := faker.New()
	return map[string]FakeFuncFactoryWithString{
		"botify": func(pattern string) FakeFunc {
			return func() MixedValue {
				return StringValue(fake.Bothify(pattern))
			}
		},
		"echo": func(pattern string) FakeFunc {
			return func() MixedValue {
				return StringValue(pattern)
			}
		},
	}
}

// getFakeFuncFactoryWith2Strings returns a map of utility fake functions that take 2 arguments.
func getFakeFuncFactoryWith2Strings() map[string]FakeFuncFactoryWith2Strings {
	fake := faker.New()
	return map[string]FakeFuncFactoryWith2Strings{
		"int_between": func(minStr, maxStr string) FakeFunc {
			return func() MixedValue {
				mn, _ := strconv.ParseInt(minStr, 10, 64)
				mx, _ := strconv.ParseInt(maxStr, 10, 64)
				return IntValue(fake.Int64Between(mn, mx))
			}
		},
	}
}

// getFakes returns a map of fake functions from underlying fake library by
// gathering all exported methods from the faker.Faker struct into map.
// The keys are the snake_cased dot-separated method names, which reflect the location of the function:
// For example: person.first_name will return a fake first name from the Person struct.
func getFakes() map[string]FakeFunc {
	visited := make(map[reflect.Type]bool)
	res := getFakeFuncs(faker.New(), "", visited)

	res["foo"] = func() MixedValue {
		return StringValue("bar")
	}

	return res
}

// getFakeFuncs returns a map of fake functions from a struct.
// The keys are the snake_cased struct field names, and the values are the fake functions.
// The fake functions can be called to get a MixedValue, which can be converted to a string, int, float64, or bool.
// visited tracks already-processed types to prevent infinite recursion.
func getFakeFuncs(obj any, prefix string, visited map[reflect.Type]bool) map[string]FakeFunc {
	res := make(map[string]FakeFunc)

	ref := reflect.ValueOf(obj)
	objType := ref.Type()

	// Check if we've already visited this type to prevent infinite recursion
	if visited[objType] {
		return res
	}
	visited[objType] = true

	for i := 0; i < ref.NumMethod(); i++ {
		mType := ref.Type().Method(i)
		name := mType.Name
		mappedName := types.ToSnakeCase(name)

		fn := ref.MethodByName(name)
		numIn := mType.Type.NumIn() - 1

		if numIn > 0 {
			continue
		}

		returnType := mType.Type.Out(0).Kind()

		switch returnType {
		case reflect.Struct:
			structInstance := fn.Call(nil)[0].Interface()
			fromStruct := getFakeFuncs(structInstance, prefix+mappedName+".", visited)
			for k, v := range fromStruct {
				res[k] = v
			}
		case reflect.Float32, reflect.Float64:
			res[prefix+mappedName] = fromReflectedFloat64Value(fn)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			res[prefix+mappedName] = fromReflectedIntValue(fn)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			res[prefix+mappedName] = fromReflectedUIntValue(fn)
		case reflect.Bool:
			res[prefix+mappedName] = fromReflectedBoolValue(fn)
		case reflect.String:
			res[prefix+mappedName] = fromReflectedStringValue(fn)
		default:
		}
	}

	return res
}

func fromReflectedStringValue(fn reflect.Value) FakeFunc {
	return func() MixedValue {
		return StringValue(fn.Call(nil)[0].String())
	}
}

func fromReflectedBoolValue(fn reflect.Value) FakeFunc {
	return func() MixedValue {
		return BoolValue(fn.Call(nil)[0].Bool())
	}
}

func fromReflectedIntValue(fn reflect.Value) FakeFunc {
	return func() MixedValue {
		return IntValue(fn.Call(nil)[0].Int())
	}
}

func fromReflectedUIntValue(fn reflect.Value) FakeFunc {
	return func() MixedValue {
		return IntValue(fn.Call(nil)[0].Uint())
	}
}

func fromReflectedFloat64Value(fn reflect.Value) FakeFunc {
	return func() MixedValue {
		return Float64Value(fn.Call(nil)[0].Float())
	}
}
