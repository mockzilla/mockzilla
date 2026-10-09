// Package formenc converts between Go values and application/x-www-form-urlencoded bodies.
package formenc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// maxIndex caps array positions read from keys, so `a[999999999]` cannot allocate a huge slice.
const maxIndex = 10000

// FieldEncoding is the OpenAPI encoding of one top-level form field.
type FieldEncoding struct {
	Style   string
	Explode *bool
}

// Encode serialises data, which must marshal to a JSON object, as a form body.
// A field uses the "form" style with explode unless its encoding says otherwise.
func Encode(data any, encoding map[string]FieldEncoding) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if err = dec.Decode(&root); err != nil {
		return "", err
	}

	values := url.Values{}
	for key, val := range root {
		style, explode := "form", true
		if enc, found := encoding[key]; found {
			if enc.Style != "" {
				style = enc.Style
			}
			if enc.Style == "form" && enc.Explode != nil {
				explode = *enc.Explode
			}
		}

		if style == "deepObject" {
			addDeepObject(values, key, val)
			continue
		}
		addForm(values, key, val, explode)
	}

	return values.Encode(), nil
}

// ToJSON reads a form body into JSON. Keys like `a[b][0][c]` become nested
// objects and arrays, repeated keys become arrays, and values that look like
// booleans, integers or decimals become those types.
func ToJSON(body []byte) ([]byte, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("parse form body: %w", err)
	}

	out := make(map[string]any, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		path := []string{key}
		if strings.Contains(key, "[") {
			path = splitKey(key)
		}
		if len(path) == 0 {
			continue
		}
		out[path[0]] = assign(out[path[0]], path[1:], fieldValue(values[key]))
	}

	return json.Marshal(out)
}

func addForm(values url.Values, key string, val any, explode bool) {
	switch v := val.(type) {
	case map[string]any:
		names := slices.Sorted(maps.Keys(v))
		if explode {
			for _, name := range names {
				addForm(values, key+"."+name, v[name], explode)
			}
			return
		}
		pairs := make([]string, 0, len(names))
		for _, name := range names {
			pairs = append(pairs, fmt.Sprintf("%s,%v", name, v[name]))
		}
		values.Set(key, strings.Join(pairs, ","))
	case []any:
		for _, item := range v {
			values.Add(key, fmt.Sprint(item))
		}
	default:
		values.Set(key, fmt.Sprint(v))
	}
}

func addDeepObject(values url.Values, key string, val any) {
	switch v := val.(type) {
	case map[string]any:
		for name, sub := range v {
			addDeepObject(values, key+"["+name+"]", sub)
		}
	case []any:
		for i, item := range v {
			addDeepObject(values, key+"["+strconv.Itoa(i)+"]", item)
		}
	default:
		values.Set(key, fmt.Sprint(v))
	}
}

func splitKey(key string) []string {
	return strings.FieldsFunc(key, func(r rune) bool {
		return r == '[' || r == ']'
	})
}

// assign stores val at path below node and returns the updated node.
func assign(node any, path []string, val any) any {
	if len(path) == 0 {
		return val
	}

	if idx, ok := arrayIndex(path[0]); ok {
		list, _ := node.([]any)
		for len(list) <= idx {
			list = append(list, nil)
		}
		list[idx] = assign(list[idx], path[1:], val)
		return list
	}

	obj, _ := node.(map[string]any)
	if obj == nil {
		obj = make(map[string]any)
	}
	obj[path[0]] = assign(obj[path[0]], path[1:], val)
	return obj
}

func arrayIndex(s string) (int, bool) {
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	idx, err := strconv.Atoi(s)
	return idx, err == nil && idx <= maxIndex
}

func fieldValue(vals []string) any {
	if len(vals) == 1 {
		return typedValue(vals[0])
	}
	list := make([]any, len(vals))
	for i, v := range vals {
		list[i] = typedValue(v)
	}
	return list
}

// typedValue is conservative, so phone numbers and zero-padded codes stay strings.
func typedValue(s string) any {
	switch {
	case s == "true":
		return true
	case s == "false":
		return false
	case strings.HasPrefix(s, "+"), strings.ContainsAny(s, " ()"):
		return s
	}

	if s != "" && (s[0] != '0' || s == "0") {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}

	if strings.Contains(s, ".") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}

	return s
}
