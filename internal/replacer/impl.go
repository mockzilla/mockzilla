package replacer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/runtime"
	"github.com/google/uuid"
	"github.com/mockzilla/mockzilla/v2/internal/contexts"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// Replacer is a function that returns a value to replace the original value with.
// Replacer functions are predefined and set in the correct order to be executed.
type Replacer func(ctx *ReplaceContext) any

// NULL is used to force resolve to nil
const (
	NULL = "__null__"
)

// IsMatchSchemaReadWriteToState checks if the given schema is read-write match.
// ReadOnly - A property that is only available in a response.
// WriteOnly - A property that is only available in a request.
func IsMatchSchemaReadWriteToState(schema *schema.Schema, state *ReplaceState) bool {
	// unable to determine
	if schema == nil || state == nil {
		return true
	}

	// Path parameters are URL segments, not body content.
	// readOnly/writeOnly semantics don't apply to them.
	if state.IsPathParam {
		return true
	}

	if schema.ReadOnly && !state.IsContentReadOnly {
		return false
	}

	if schema.WriteOnly && !state.IsContentWriteOnly {
		return false
	}

	return true
}

// hasCorrectSchemaValue checks if the value is of the correct type and format.
func hasCorrectSchemaValue(ctx *ReplaceContext, value any) bool {
	// TODO: check how to handle other content schemas
	if ctx.schema == nil {
		return true
	}
	s, ok := ctx.schema.(*schema.Schema)
	if !ok || s == nil {
		return true
	}

	if !IsCorrectlyReplacedType(value, s.Type) {
		return false
	}

	reqFormat := s.Format
	if reqFormat == "" {
		return true
	}

	switch reqFormat {
	case "int32":
		// If type is string with int format, the value should be a string representation
		if s.Type == types.TypeString {
			str, ok := value.(string)
			if !ok {
				return false
			}
			_, err := strconv.ParseInt(str, 10, 32)
			return err == nil
		}
		_, ok = types.ToInt32(value)
		return ok
	case "int64":
		// If type is string with int format, the value should be a string representation
		if s.Type == types.TypeString {
			str, ok := value.(string)
			if !ok {
				return false
			}
			_, err := strconv.ParseInt(str, 10, 64)
			return err == nil
		}
		_, ok = types.ToInt64(value)
		return ok
	case "date":
		str, ok := value.(string)
		if !ok {
			// Could be a Unix timestamp (integer) - accept it
			_, ok = types.ToInt64(value)
			return ok
		}
		v, err := time.Parse("2006-01-02", str)
		return err == nil && !v.IsZero()
	case "date-time", "datetime":
		str, ok := value.(string)
		if !ok {
			// Could be a Unix timestamp (integer) - accept it
			_, ok = types.ToInt64(value)
			return ok
		}
		v, err := time.Parse("2006-01-02T15:04:05.000Z", str)
		return err == nil && !v.IsZero()
	case "email":
		str, ok := value.(string)
		if !ok {
			return false
		}
		email := runtime.Email(str)
		_, err := json.Marshal(email)
		return err == nil
	case "uuid":
		str, ok := value.(string)
		if !ok {
			return false
		}

		// Check if the schema has non-standard UUID length constraints
		// Standard UUID is 36 chars (with dashes) or 32 chars (without dashes)
		expectedLen := getExpectedUUIDLength(s)
		if expectedLen != 36 && expectedLen != 32 && expectedLen != 0 {
			// Non-standard UUID length - validate as hex string of expected length
			if len(str) != expectedLen {
				return false
			}
			return isHexString(str)
		}
		_, err := uuid.Parse(str)
		return err == nil
	default:
		return true
	}
}

// replaceInRequest is a replacer that replaces values only during request (write-only) generation.
// It checks compound areas: in-request-header, then in-request.
func replaceInRequest(ctx *ReplaceContext) any {
	if !ctx.state.IsContentWriteOnly {
		return nil
	}

	if ctx.state.IsHeader {
		if v := replaceInArea(ctx, "request-header"); v != nil {
			return v
		}
	}

	return replaceInArea(ctx, "request")
}

// replaceInResponse is a replacer that replaces values only during response (read-only) generation.
// It checks compound areas: in-response-header, then in-response.
func replaceInResponse(ctx *ReplaceContext) any {
	if !ctx.state.IsContentReadOnly {
		return nil
	}

	if ctx.state.IsHeader {
		if v := replaceInArea(ctx, "response-header"); v != nil {
			return v
		}
	}

	return replaceInArea(ctx, "response")
}

// replaceInHeaders is a replacer that replaces values only in headers.
func replaceInHeaders(ctx *ReplaceContext) any {
	if !ctx.state.IsHeader {
		return nil
	}
	v := replaceInArea(ctx, "header")

	s, ok := ctx.schema.(*schema.Schema)
	if !ok {
		return v
	}

	name := ctx.state.NamePath[0]
	format := s.Format

	if name == "authorization" {
		switch format {
		case "basic":
			if v == nil {
				v = fmt.Sprintf("%s:%s", ctx.faker.Internet().User(), ctx.faker.Internet().Password())
			}
			return "Basic " + types.Base64Encode(v.(string))
		case "bearer":
			if v == nil {
				v = ctx.faker.Internet().Password()
			}
			return "Bearer " + v.(string)
		}
	}

	return v
}

// replaceInPath is a replacer that replaces values only in path parameters.
func replaceInPath(ctx *ReplaceContext) any {
	if !ctx.state.IsPathParam {
		return nil
	}
	return replaceInArea(ctx, "path")
}

func replaceInArea(ctx *ReplaceContext, area string) any {
	ctxAreaPrefix := ctx.areaPrefix
	if ctxAreaPrefix == "" {
		return nil
	}

	if len(ctx.state.NamePath) == 0 {
		return nil
	}

	namePath := []string{ctx.state.NamePath[0]}

	for _, data := range ctx.data {
		replacements, ok := data[fmt.Sprintf("%s%s", ctxAreaPrefix, area)]
		if !ok {
			continue
		}

		if res := replaceValueWithContext(namePath, replacements); res != nil {
			return res
		}
	}

	return nil
}

// replaceFromContext is a replacer that replaces values from the context.
func replaceFromContext(ctx *ReplaceContext) any {
	for _, data := range ctx.data {
		if res := replaceValueWithContext(ctx.state.NamePath, data); res != nil {
			v := castToSchemaFormat(ctx, res)

			// If context returned empty string, return nil to let other replacers handle it
			// This avoids validation errors for required string fields
			if str, ok := v.(string); ok && str == "" {
				return nil
			}

			return v
		}
	}

	return nil
}

// castToSchemaFormat casts the value to the schema format if possible.
// If the schema format is not specified, the value is returned as-is.
// Returns nil for formats that should be handled by replaceFromSchemaFormat.
func castToSchemaFormat(ctx *ReplaceContext, value any) any {
	s, ok := ctx.schema.(*schema.Schema)
	if !ok || s == nil {
		return value
	}

	switch s.Format {
	case "uuid":
		return nil
	case "int32":
		if v, ok := types.ToInt32(value); ok {
			return v
		}
		return value
	case "int64":
		if v, ok := types.ToInt64(value); ok {
			return v
		}
		return value
	case "uint8":
		if v, ok := types.ToUint8(value); ok {
			return v
		}
		return value
	case "uint16":
		if v, ok := types.ToUint16(value); ok {
			return v
		}
		return value
	case "uint32":
		if v, ok := types.ToUint32(value); ok {
			return v
		}
		return value
	case "uint64":
		if v, ok := types.ToUint64(value); ok {
			return v
		}
		return value
	default:
		return value
	}
}

// replaceValueWithContext is a replacer that replaces values from the context.
func replaceValueWithContext(path []string, contextData any) interface{} {
	switch valueType := contextData.(type) {
	case map[string]string:
		return replaceValueWithMapContext[string](path, valueType)
	case map[string]int:
		return replaceValueWithMapContext[int](path, valueType)
	case map[string]bool:
		return replaceValueWithMapContext[bool](path, valueType)
	case map[string]float64:
		return replaceValueWithMapContext[float64](path, valueType)
	case map[string]any:
		return replaceValueWithMapContext[any](path, valueType)

	// base cases below:
	case contexts.FakeFunc:
		return valueType().Get()

	case string, int, bool, float64:
		return valueType
	case []string:
		return types.GetRandomSliceValue(valueType)
	case []int:
		return types.GetRandomSliceValue(valueType)
	case []bool:
		return types.GetRandomSliceValue(valueType)
	case []float64:
		return types.GetRandomSliceValue(valueType)
	case []any:
		return types.GetRandomSliceValue[any](valueType)
	default:
		return nil // unmapped type
	}
}

func replaceValueWithMapContext[T any](path []string, contextData map[string]T) any {
	if len(path) == 0 {
		return nil
	}

	fieldName := path[len(path)-1]

	// Phase 1: Try nested/suffix matches (most specific first).
	// Scan path elements from left to right (indices 0 to len-2).
	// Left-to-right means we try the longest suffix first.
	for i := 0; i < len(path)-1; i++ {
		pathElem := path[i]

		// Direct key match
		if value, exists := contextData[pathElem]; exists {
			if isMapValue(value) {
				if result := replaceValueWithContext(path[i+1:], value); result != nil {
					return result
				}
			}
		}

		// Regex pattern match on path element
		for key, keyValue := range contextData {
			if types.MaybeRegexPattern(key) && isMapValue(keyValue) {
				pattern := key
				if pattern == "*" {
					pattern = ".*"
				}
				if types.ValidateStringWithPattern(pathElem, pattern) {
					if result := replaceValueWithContext(path[i+1:], keyValue); result != nil {
						return result
					}
				}
			}
		}
	}

	// Phase 2: Root-level field name match (least specific).
	// Skip map values - those are namespaces for nested matching, not leaf values.
	if value, exists := contextData[fieldName]; exists {
		if !isMapValue(value) {
			return replaceValueWithContext(nil, value)
		}
	}

	// Phase 3: Regex pattern match on field name, also skipping map values.
	for key, keyValue := range contextData {
		if types.MaybeRegexPattern(key) && !isMapValue(keyValue) {
			pattern := key
			if pattern == "*" {
				pattern = ".*"
			}
			if types.ValidateStringWithPattern(fieldName, pattern) {
				return replaceValueWithContext(nil, keyValue)
			}
		}
	}

	return nil
}

// isMapValue checks if the given value is a map type.
func isMapValue[T any](value T) bool {
	switch any(value).(type) {
	case map[string]string, map[string]int, map[string]bool, map[string]float64, map[string]any:
		return true
	default:
		return false
	}
}

// replaceFromSchemaFormat is a replacer that replaces values from the schema format.
func replaceFromSchemaFormat(ctx *ReplaceContext) any {
	s, ok := ctx.schema.(*schema.Schema)
	if !ok || s == nil {
		return nil
	}

	switch s.Format {
	case "byte", "binary":
		// Both byte and binary formats should be base64-encoded in JSON
		// byte = base64-encoded characters
		// binary = arbitrary binary data (also base64-encoded when in JSON)
		randomBytes := ctx.stringExpression()
		return types.Base64Encode(randomBytes)
	case "date":
		return ctx.faker.Time().Time(time.Now()).Format("2006-01-02")
	case "date-time", "datetime":
		return ctx.faker.Time().Time(time.Now()).Format("2006-01-02T15:04:05.000Z")
	case "email":
		return ctx.faker.Internet().Email()
	case "uuid":
		// Check if the schema has non-standard UUID length constraints
		expectedLen := getExpectedUUIDLength(s)
		switch expectedLen {
		case 0, 36:
			// Standard UUID with dashes (36 chars) or no constraint
			return ctx.faker.UUID().V4()
		case 32:
			// UUID without dashes (32 hex chars)
			u := uuid.New()
			return strings.ReplaceAll(u.String(), "-", "")
		default:
			// Non-standard length - generate hex string of expected length
			return generateHexString(expectedLen)
		}
	case "password":
		return ctx.faker.Internet().Password()
	case "hostname":
		return ctx.faker.Internet().Domain()
	case "uri", "url":
		return ctx.faker.Internet().URL()
	case "int32":
		val := ensureNonZeroInt(ctx.faker.Int32())
		if s.Type == types.TypeString {
			return fmt.Sprintf("%d", val)
		}
		return val
	case "int64":
		val := ensureNonZeroInt(ctx.faker.Int64())
		if s.Type == types.TypeString {
			return fmt.Sprintf("%d", val)
		}
		return val
	case "uint8":
		val := ensureNonZeroUint(ctx.faker.UInt8())
		if s.Type == types.TypeString {
			return fmt.Sprintf("%d", val)
		}
		return val
	case "uint16":
		val := ensureNonZeroUint(ctx.faker.UInt16())
		if s.Type == types.TypeString {
			return fmt.Sprintf("%d", val)
		}
		return val
	case "uint32":
		val := ensureNonZeroUint(ctx.faker.UInt32())
		if s.Type == types.TypeString {
			return fmt.Sprintf("%d", val)
		}
		return val
	case "uint64":
		val := ensureNonZeroUint(ctx.faker.UInt64())
		if s.Type == types.TypeString {
			return fmt.Sprintf("%d", val)
		}
		return val
	case "ipv4":
		return ctx.faker.Internet().Ipv4()
	case "ipv6":
		return ctx.faker.Internet().Ipv6()
	}
	return nil
}

// replaceFromSchemaPrimitive is a replacer that replaces values from the schema primitive.
func replaceFromSchemaPrimitive(ctx *ReplaceContext) any {
	s, ok := ctx.schema.(*schema.Schema)
	if !ok || s == nil {
		return nil
	}
	faker := ctx.faker

	// Check for enum values first
	// If enum is explicitly defined, all values (including 0) are valid
	// Filter out nil and "null" values since oapi-codegen doesn't generate constants for null
	if len(s.Enum) > 0 {
		nonNilEnums := make([]any, 0, len(s.Enum))
		for _, v := range s.Enum {
			if v != nil && v != "null" {
				nonNilEnums = append(nonNilEnums, v)
			}
		}
		if len(nonNilEnums) > 0 {
			return types.GetRandomSliceValue(nonNilEnums)
		}
	}

	switch s.Type {
	case types.TypeString:
		val := ctx.stringExpression()
		return val
	case types.TypeInteger, types.TypeNumber:
		// Respect format constraints for integers
		switch s.Format {
		case "int32":
			return ensureNonZeroInt(faker.Int32())
		case "int64":
			return ensureNonZeroInt(faker.Int64())
		case "uint8":
			return ensureNonZeroUint(faker.UInt8())
		case "uint16":
			return ensureNonZeroUint(faker.UInt16())
		case "uint32":
			return ensureNonZeroUint(faker.UInt32())
		case "uint64":
			return ensureNonZeroUint(faker.UInt64())
		default:
			// Default: generate int32 for unspecified format
			return ensureNonZeroInt(faker.Int32())
		}
	case types.TypeBoolean:
		// Return random boolean value.
		// This works correctly with validation because we use the generated Validate() methods
		// which only validate nested fields, not struct-level required tags.
		// See pkg/api/validation.go ValidateResponse() for details.
		return ctx.faker.Bool()
	}
	return nil
}

// replaceFromSchemaExample is a replacer that replaces values from the schema example.
func replaceFromSchemaExample(ctx *ReplaceContext) any {
	s, ok := ctx.schema.(*schema.Schema)
	if !ok || s == nil {
		return nil
	}
	return s.Example
}

// applySchemaConstraints applies schema constraints to the value.
// It converts the input value to match the corresponding OpenAPI type specified in the schema.
func applySchemaConstraints(openAPISchema any, res any) any {
	if openAPISchema == nil {
		return res
	}

	s, ok := openAPISchema.(*schema.Schema)
	if !ok || s == nil {
		return res
	}

	switch s.Type {
	case types.TypeBoolean:
		if len(s.Enum) > 0 {
			return types.GetRandomSliceValue(s.Enum)
		}
	case types.TypeString:
		return applySchemaStringConstraints(s, res.(string))
	case types.TypeInteger:
		floatValue, err := types.ToFloat64(res)
		if err != nil {
			slog.Error("Failed to convert value to float64", "value", res, "error", err)
			return nil
		}
		return int64(applySchemaNumberConstraints(s, floatValue))
	case types.TypeNumber:
		floatValue, err := types.ToFloat64(res)
		if err != nil {
			slog.Error("Failed to convert value to float64", "value", res, "error", err)
			return nil
		}
		return applySchemaNumberConstraints(s, floatValue)
	}
	return res
}

// applySchemaStringConstraints applies string constraints to the value.
// in case of invalid value, the function tries to correct it.
func applySchemaStringConstraints(schema *schema.Schema, value string) any {
	if schema == nil {
		return value
	}

	// For byte and binary formats, ensure the value is base64 encoded
	// This handles cases where the value comes from context or other sources
	if schema.Format == "byte" || schema.Format == "binary" {
		// Check if it's already valid base64
		if _, err := base64.StdEncoding.DecodeString(value); err != nil {
			// Not valid base64, encode it
			return types.Base64Encode(value)
		}
		// Already valid base64, return as-is
		return value
	}

	// Skip minLength/maxLength constraints for date-time, date, and uuid formats
	// These have fixed formats that shouldn't be modified by length constraints
	// For uuid, the correct length is already generated in replaceFromSchemaFormat
	skipLengthConstraints := schema.Format == "date-time" || schema.Format == "datetime" || schema.Format == "date" || schema.Format == "uuid"

	expectedEnums := make(map[string]bool)
	// remove random nulls from enum values
	// Filter out both nil and the string "null" since oapi-codegen doesn't generate constants for null
	for _, v := range schema.Enum {
		if v != nil && v != "null" {
			// values can be numbers in the schema too, make sure we get strings here
			// otherwise we'll get a panic if validation is on.
			expectedEnums[fmt.Sprintf("%v", v)] = true
		}
	}

	if len(expectedEnums) > 0 && !expectedEnums[value] {
		return types.GetRandomKeyFromMap(expectedEnums)
	}

	// Note: We intentionally skip pattern validation/generation.
	// oapi-codegen doesn't generate validation for regex patterns, so there's no need
	// to generate pattern-matching values. Complex patterns can also cause hangs.

	if !skipLengthConstraints {
		if schema.MinLength != nil && len(value) < int(*schema.MinLength) {
			value += strings.Repeat("-", int(*schema.MinLength)-len(value))
		}

		if schema.MaxLength != nil && int64(len(value)) > *schema.MaxLength {
			value = value[:*schema.MaxLength]
		}
	}

	return value
}

// isIntegerSchema returns true if the schema represents an integer value.
// This includes schemas with type "integer" or with integer formats (int32, int64, etc.)
// even when the type is "number".
func isIntegerSchema(s *schema.Schema) bool {
	if s == nil {
		return false
	}
	if s.Type == types.TypeInteger {
		return true
	}

	// Check for integer formats - some specs use type: number with format: int32
	switch s.Format {
	case "int32", "int64", "uint8", "uint16", "uint32", "uint64":
		return true
	}
	return false
}

// applySchemaNumberConstraints applies number constraints to the value.
// If the value is out of bounds, generates a random value within the valid range.
func applySchemaNumberConstraints(schema *schema.Schema, value float64) float64 {
	if schema == nil {
		return value
	}

	expectedEnums := make(map[string]bool)
	// remove random nulls from enum values
	for _, v := range schema.Enum {
		if v != nil {
			// we can't have floats as keys in the map, so we convert them to strings
			enumStr := fmt.Sprintf("%v", v)
			expectedEnums[enumStr] = true
		}
	}

	vStr := fmt.Sprintf("%v", value)
	if len(expectedEnums) > 0 {
		// If current value is not in enum, pick a random valid enum value
		if !expectedEnums[vStr] {
			enumed := types.GetRandomKeyFromMap(expectedEnums)
			f, _ := strconv.ParseFloat(enumed, 64)
			return f
		}
		// Value is in enum, return it as-is (including zero if it's a valid enum value)
		return value
	}

	if schema.MultipleOf != nil && *schema.MultipleOf != 0 {
		value = float64(int(value/(*schema.MultipleOf))) * (*schema.MultipleOf)
		// Ensure multipleOf doesn't produce zero for required fields
		if value == 0 {
			value = *schema.MultipleOf
		}
	}

	// Determine the valid range
	var minVal, maxVal float64
	var hasMin, hasMax bool

	// Get minimum bound
	if schema.ExclusiveMinimum != nil {
		minVal = *schema.ExclusiveMinimum
		hasMin = true
		// For exclusive minimum, the actual minimum is slightly above the bound
		if schema.Type == "integer" {
			minVal += 1
		} else {
			minVal += 0.01
		}
	} else if schema.Minimum != nil {
		minVal = *schema.Minimum
		hasMin = true
	}

	// Get maximum bound
	if schema.ExclusiveMaximum != nil {
		maxVal = *schema.ExclusiveMaximum
		hasMax = true
		// For exclusive maximum, the actual maximum is slightly below the bound
		if schema.Type == "integer" {
			maxVal -= 1
		} else {
			maxVal -= 0.01
		}
	} else if schema.Maximum != nil {
		maxVal = *schema.Maximum
		hasMax = true
	}

	// Check if value is out of bounds
	outOfBounds := (hasMin && value < minVal) || (hasMax && value > maxVal)

	// Only regenerate if value is out of bounds
	if outOfBounds {
		// Use defaults for missing bounds to enable randomization
		if !hasMin {
			// If max is set and <= 0, use a negative min to allow valid negative values
			if hasMax && maxVal <= 0 {
				minVal = -2147483648
			} else if hasMax && maxVal < 1 {
				minVal = 0
			} else {
				minVal = 1
			}
		}
		if !hasMax {
			// Use a default max that's at least as large as minVal
			// This handles cases like timestamps where min can be very large (e.g., 1356998400070)
			defaultMax := float64(2147483647)
			if minVal > defaultMax {
				// Add a reasonable range above minVal for large values
				maxVal = minVal + 1000000
			} else {
				maxVal = defaultMax
			}
		}

		// Generate random value between minVal and maxVal
		if isIntegerSchema(schema) {
			// For integers, generate a random integer in [minVal, maxVal] inclusive
			// Avoid 0 unless it's the only valid value
			minInt := int64(minVal)
			maxInt := int64(maxVal)
			if minInt == 0 && maxInt > 0 {
				minInt = 1 // Avoid generating 0
			}
			rangeSize := maxInt - minInt + 1 // +1 to make maxVal inclusive

			// Guard against invalid range (can happen with conflicting constraints)
			if rangeSize <= 0 {
				return float64(minInt)
			}
			randomValue := minInt + rand.Int63n(rangeSize)
			return float64(randomValue)
		}

		// For floats, generate in [minVal, maxVal)
		rangeSize := maxVal - minVal
		randomValue := minVal + (rand.Float64() * rangeSize)
		return randomValue
	}

	// Avoid returning 0 - validators treat 0 as zero-value and fail required checks
	// Only return 0 if it's the only valid value (min=0, max=0)
	if value == 0 {
		if hasMin && hasMax && minVal == 0 && maxVal == 0 {
			return 0 // Only valid value
		}
		if isIntegerSchema(schema) {
			return 1.0
		}
		return 0.01
	}

	return value
}

// replaceFromSchemaFallback is the last resort to get a value from the schema.
func replaceFromSchemaFallback(ctx *ReplaceContext) any {
	s, ok := ctx.schema.(*schema.Schema)
	if !ok || s == nil {
		return nil
	}

	return s.Default
}

// ensureNonZeroInt ensures the value is non-zero and positive.
// Returns 1 if the value is 0, otherwise returns the absolute value.
// This avoids validation errors for required integer fields.
func ensureNonZeroInt[T types.SignedInt](val T) T {
	if val == 0 {
		return 1
	}
	if val < 0 {
		return -val
	}
	return val
}

// UnsignedInt is a constraint for unsigned integer types.
type UnsignedInt interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// ensureNonZeroUint ensures the value is non-zero.
// Returns 1 if the value is 0, otherwise returns the value as-is.
// This avoids validation errors for required integer fields.
func ensureNonZeroUint[T UnsignedInt](val T) T {
	if val == 0 {
		return 1
	}
	return val
}

// getExpectedUUIDLength returns the expected UUID length from schema constraints.
// Returns 0 if no length constraints are specified.
// Standard UUID is 36 chars (with dashes) or 32 chars (without dashes).
func getExpectedUUIDLength(s *schema.Schema) int {
	if s == nil {
		return 0
	}
	// If both min and max are set and equal, use that length
	if s.MinLength != nil && s.MaxLength != nil && *s.MinLength == *s.MaxLength {
		return int(*s.MinLength)
	}
	// If only maxLength is set, use that
	if s.MaxLength != nil {
		return int(*s.MaxLength)
	}
	// If only minLength is set, use that
	if s.MinLength != nil {
		return int(*s.MinLength)
	}
	return 0
}

// isHexString checks if a string contains only hexadecimal characters.
func isHexString(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// generateHexString generates a random hex string of the specified length.
func generateHexString(length int) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = hexChars[i%len(hexChars)]
	}

	// Use a simple UUID as seed for randomness
	uuidBytes := uuid.New()
	for i := 0; i < length && i < len(uuidBytes); i++ {
		result[i] = hexChars[int(uuidBytes[i%len(uuidBytes)])%len(hexChars)]
	}

	// Fill remaining with pattern from UUID bytes
	for i := len(uuidBytes); i < length; i++ {
		result[i] = hexChars[int(uuidBytes[i%len(uuidBytes)])%len(hexChars)]
	}

	return string(result)
}
