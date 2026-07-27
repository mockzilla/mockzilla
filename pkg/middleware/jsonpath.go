package middleware

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/types"
)

// extractJSONPath extracts a value from JSON bytes using a dotted path.
// See types.GetValueByJSONPath for the supported path syntax.
func extractJSONPath(data []byte, path string) any {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}

	return types.GetValueByJSONPath(parsed, path)
}

// extractBodyValue extracts a field value from the request body.
// For form-encoded content type, parses as URL-encoded form data.
// Otherwise, parses as JSON using dotted path notation.
func extractBodyValue(body []byte, contentType string, field string) any {
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		params, err := url.ParseQuery(string(body))
		if err == nil {
			if v := params.Get(field); v != "" || params.Has(field) {
				return v
			}
		}
		return nil
	}
	return extractJSONPath(body, field)
}

// formatValue converts a value to a stable string representation for key building.
func formatValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// JSON numbers are float64; format integers without decimal
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}
