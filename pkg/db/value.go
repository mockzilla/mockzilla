package db

import "encoding/json"

// DecodeValue converts a value read back from a Table into T.
// The memory backend hands back whatever was stored, while backends that
// serialize return the JSON shape (map[string]any), so callers of Table.Get
// have to cope with both.
func DecodeValue[T any](val any) (*T, bool) {
	if val == nil {
		return nil, false
	}

	if v, ok := val.(*T); ok {
		return v, true
	}

	data, err := json.Marshal(val)
	if err != nil {
		return nil, false
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false
	}
	return &out, true
}
