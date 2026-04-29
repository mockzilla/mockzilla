package replacer

import (
	"reflect"

	"github.com/jaswdr/faker/v2"
	"github.com/mockzilla/mockzilla/v2/internal/contexts"
	"github.com/mockzilla/mockzilla/v2/internal/types"
)

// ValueReplacer is a function that replaces value in schema or content.
// This function should encapsulate all the logic, data, contexts etc. of replacing values.
type ValueReplacer func(schemaOrContent any, state *ReplaceState) any

// ReplaceContext is a context that is used to replace values in schemas and contents.
//
// schema is a schema that is used to replace values.
// Currently only OpenAPI schema is supported.
// It does not depend on schema provider as this is already converted to internal schema type.
//
// state is a state of the current replace operation.
// It is used to store information about the current element, including its name, index, content type etc.
//
// areaPrefix is a prefix that is used to identify the correct section
// in the context config for specific replacement area.
// e.g. in-
// then in the contexts we should have:
// in-header:
//
//	X-GeneratedRequest-ID: 123
//
// in-path:
//
//	user_id: 123
//
// data is a list of contexts that are used to replace values.
// faker is a faker instance that is used to generate fake data.
// functions is a map of all fake functions that are used to replace values.
type ReplaceContext struct {
	schema     any
	state      *ReplaceState
	areaPrefix string
	data       []map[string]any
	faker      faker.Faker
	functions  map[string]contexts.FakeFunc
}

// function is a helper function to get value from the given function.
func (r *ReplaceContext) function(name string) contexts.MixedValue {
	if fn, exists := r.functions[name]; exists {
		return fn()
	}
	return nil
}

// stringExpression is a helper function to get string value from the "expression" function.
// It's a shortcut for r.function("expression").Get().(string)
// Function contents is defined in the words.yml context file.
// Falls back to faker.Lorem().Word() if the expression function is not available.
func (r *ReplaceContext) stringExpression() string {
	if val := r.function("expression"); val != nil {
		return val.Get().(string)
	}
	return r.faker.Lorem().Word()
}

// Replacers is a list of replacers that are used to replace values in schemas and contents in the specified order.
var Replacers = []Replacer{
	replaceInRequest,
	replaceInResponse,
	replaceInHeaders,
	replaceInPath,
	replaceFromContext,
	replaceFromSchemaExample,
	replaceFromSchemaFormat,
	replaceFromSchemaPrimitive,
	replaceFromSchemaFallback,
}

// CreateValueReplacer is a factory that creates a new ValueReplacer instance from the given config and contexts.
func CreateValueReplacer(replacers []Replacer, contexts []map[string]any) ValueReplacer {
	fns := getContextFunctions(contexts)
	return func(content any, state *ReplaceState) any {
		if state == nil {
			state = NewReplaceState()
		}

		ctx := &ReplaceContext{
			schema:     content,
			state:      state,
			areaPrefix: "in-",
			data:       contexts,
			faker:      faker.New(),
			functions:  fns,
		}

		for _, fn := range replacers {
			res := fn(ctx)
			if res != nil && ctx.schema != nil {
				if !hasCorrectSchemaValue(ctx, res) {
					continue
				}
				res = applySchemaConstraints(ctx.schema, res)
			}

			if res == nil {
				continue
			}

			// return nil if function suggests
			if str, ok := res.(string); ok {
				if str == NULL {
					return nil
				}

				// Ensure we never return empty strings to avoid validation errors for required fields
				if str == "" {
					continue
				}
			}

			return res
		}

		return nil
	}
}

// IsCorrectlyReplacedType checks if the given value is of the correct schema type.
func IsCorrectlyReplacedType(value any, neededType string) bool {
	switch neededType {
	case "", "string":
		_, ok := value.(string)
		return ok
	case "integer":
		return types.IsInteger(value)
	case "number":
		return types.IsNumber(value)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		return reflect.TypeOf(value).Kind() == reflect.Map
	case "array":
		val := reflect.ValueOf(value)
		return val.Kind() == reflect.Slice || val.Kind() == reflect.Array
	case "any":
		return true
	default:
		return false
	}
}

// getContextFunctions returns a map of all fake functions from the given contexts.
func getContextFunctions(data []map[string]any) map[string]contexts.FakeFunc {
	res := make(map[string]contexts.FakeFunc)
	for _, ctxCollection := range data {
		for k, v := range ctxCollection {
			if fn, ok := v.(contexts.FakeFunc); ok {
				res[k] = fn
			}
		}
	}
	return res
}
