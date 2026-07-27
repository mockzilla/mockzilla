package types

import (
	"strconv"
	"strings"
)

// PathSegment represents a single segment in a dotted path.
type PathSegment struct {
	Key   string
	Index int  // -1 means no index
	IsArr bool // true if this segment has an array index like [0]
}

// ParseDottedPath splits a dotted path into segments.
// "data.items[0].name" → [{Key:"data"}, {Key:"items", Index:0, IsArr:true}, {Key:"name"}]
// "[0].name"           → [{Key:"", Index:0, IsArr:true}, {Key:"name"}]
func ParseDottedPath(path string) []PathSegment {
	parts := strings.Split(path, ".")
	segments := make([]PathSegment, 0, len(parts))

	for _, part := range parts {
		if part == "" {
			continue
		}

		if idx := strings.Index(part, "["); idx != -1 {
			key := part[:idx]
			indexStr := strings.TrimSuffix(part[idx+1:], "]")
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				// Invalid index, treat as plain key
				segments = append(segments, PathSegment{Key: part, Index: -1})
				continue
			}
			segments = append(segments, PathSegment{Key: key, Index: index, IsArr: true})
		} else {
			segments = append(segments, PathSegment{Key: part, Index: -1})
		}
	}

	return segments
}

// GetValueByJSONPath returns the value at the given dotted path in already
// decoded JSON data. Returns nil when the path doesn't resolve.
// Supports:
//   - Simple: "data.name" - traverse nested objects
//   - Array index: "data.items[0].name" - specific element
//   - Array wildcard: "data.items.name" - when items is an array, search each element
//   - Top-level array: "[0].name" - index into a root-level array
func GetValueByJSONPath(data any, path string) any {
	return navigatePath(data, ParseDottedPath(path))
}

// navigatePath traverses the decoded JSON structure following the path segments.
func navigatePath(current any, segments []PathSegment) any {
	for i, seg := range segments {
		if current == nil {
			return nil
		}

		// Handle array at current level (top-level or bare index after traversal)
		if arr, ok := current.([]any); ok {
			if seg.IsArr && seg.Key == "" {
				// Bare index like [0] - index directly into the current array
				if seg.Index < 0 || seg.Index >= len(arr) {
					return nil
				}
				current = arr[seg.Index]
				continue
			}
			// Array wildcard - search each element with remaining segments
			remaining := segments[i:]
			for _, elem := range arr {
				result := navigatePath(elem, remaining)
				if result != nil {
					return result
				}
			}
			return nil
		}

		switch v := current.(type) {
		case map[string]any:
			val, ok := v[seg.Key]
			if !ok {
				return nil
			}

			if seg.IsArr {
				// Need to index into an array
				arr, ok := val.([]any)
				if !ok || seg.Index < 0 || seg.Index >= len(arr) {
					return nil
				}
				current = arr[seg.Index]
			} else {
				// Check if val is an array and we have more segments - wildcard search
				if arr, ok := val.([]any); ok && i+1 < len(segments) {
					remaining := segments[i+1:]
					for _, elem := range arr {
						result := navigatePath(elem, remaining)
						if result != nil {
							return result
						}
					}
					return nil
				}
				current = val
			}

		default:
			return nil
		}
	}

	return current
}
