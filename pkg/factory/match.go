package factory

import (
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
)

// pathPattern holds a pre-split spec path for efficient matching.
type pathPattern struct {
	specPath string
	method   string
	segments []string
	// wildcardCount is used to prefer more specific matches (fewer placeholders).
	wildcardCount int
}

// pathMatcher matches concrete request paths (e.g., /users/1) to
// OpenAPI spec path patterns (e.g., /users/{id}).
type pathMatcher struct {
	patterns []pathPattern
}

// newPathMatcher builds a matcher from route info.
// Patterns are sorted so that more specific paths (fewer placeholders) are tried first.
func newPathMatcher(routes []typedef.RouteInfo) *pathMatcher {
	patterns := make([]pathPattern, 0, len(routes))
	for _, r := range routes {
		segs := splitPath(r.Path)

		// Strip the AWS-style `#qparam` discriminator suffix from spec
		// path keys; it's never part of a real request URL.
		for i, s := range segs {
			if idx := strings.IndexByte(s, '#'); idx >= 0 {
				segs[i] = s[:idx]
			}
		}

		wc := 0
		for _, s := range segs {
			if isPlaceholder(s) {
				wc++
			}
		}

		patterns = append(patterns, pathPattern{
			specPath:      r.Path,
			method:        strings.ToUpper(r.Method),
			segments:      segs,
			wildcardCount: wc,
		})
	}

	// Stable sort: fewer wildcards first so exact segments win.
	for i := 1; i < len(patterns); i++ {
		for j := i; j > 0 && patterns[j].wildcardCount < patterns[j-1].wildcardCount; j-- {
			patterns[j], patterns[j-1] = patterns[j-1], patterns[j]
		}
	}

	return &pathMatcher{patterns: patterns}
}

// Match returns the spec path that matches the concrete request. Fewer
// wildcards win; ties go to the last-inserted pattern so routing aligns
// with libopenapi-validator's radix tree, which keeps only the most
// recent leaf at any parameter slot.
func (m *pathMatcher) Match(path, method string) (string, bool) {
	method = strings.ToUpper(method)
	segs := splitPath(path)

	var best *pathPattern
	for i := range m.patterns {
		p := &m.patterns[i]
		if p.method != method {
			continue
		}

		if !matchSegments(segs, p.segments) {
			continue
		}
		if best == nil || p.wildcardCount <= best.wildcardCount {
			best = p
		}
	}

	if best == nil {
		return "", false
	}
	return best.specPath, true
}

// splitPath splits a URL path into non-empty segments.
func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}

// matchSegments checks whether concrete segments match a pattern's segments.
// A pattern segment that starts with '{' is treated as a wildcard.
func matchSegments(concrete, pattern []string) bool {
	if len(concrete) != len(pattern) {
		return false
	}
	for i, ps := range pattern {
		if isPlaceholder(ps) {
			continue
		}
		if concrete[i] != ps {
			return false
		}
	}
	return true
}

// isPlaceholder returns true if the segment is an OpenAPI path parameter placeholder.
func isPlaceholder(segment string) bool {
	return len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}'
}
