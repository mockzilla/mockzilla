package factory

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	assert2 "github.com/stretchr/testify/assert"
)

func TestSplitPath(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		path     string
		expected []string
	}{
		{"/", nil},
		{"", nil},
		{"/users", []string{"users"}},
		{"/users/1", []string{"users", "1"}},
		{"/users/{id}", []string{"users", "{id}"}},
		{"/users/{id}/posts/{postId}", []string{"users", "{id}", "posts", "{postId}"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := splitPath(tt.path)
			assert.Equal(tt.expected, result)
		})
	}
}

func TestIsPlaceholder(t *testing.T) {
	assert := assert2.New(t)

	assert.True(isPlaceholder("{id}"))
	assert.True(isPlaceholder("{user-id}"))
	assert.True(isPlaceholder("{some_name_1}"))
	assert.False(isPlaceholder("users"))
	assert.False(isPlaceholder(""))
	assert.False(isPlaceholder("{}"))
	assert.False(isPlaceholder("{"))
	assert.False(isPlaceholder("}"))
}

func TestMatchSegments(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name     string
		concrete []string
		pattern  []string
		expected bool
	}{
		{"exact match", []string{"users"}, []string{"users"}, true},
		{"placeholder match", []string{"users", "42"}, []string{"users", "{id}"}, true},
		{"multiple placeholders", []string{"users", "42", "posts", "7"}, []string{"users", "{id}", "posts", "{postId}"}, true},
		{"mismatch", []string{"users", "42"}, []string{"pets", "{id}"}, false},
		{"different length", []string{"users"}, []string{"users", "{id}"}, false},
		{"both nil", nil, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(tt.expected, matchSegments(tt.concrete, tt.pattern))
		})
	}
}

func TestPathMatcher_Match(t *testing.T) {
	assert := assert2.New(t)

	routes := []typedef.RouteInfo{
		{ID: "listUsers", Method: "GET", Path: "/users"},
		{ID: "getUser", Method: "GET", Path: "/users/{id}"},
		{ID: "createUser", Method: "POST", Path: "/users"},
		{ID: "getUserPosts", Method: "GET", Path: "/users/{id}/posts"},
		{ID: "getPost", Method: "GET", Path: "/users/{id}/posts/{postId}"},
	}
	m := newPathMatcher(routes)

	tests := []struct {
		name         string
		path         string
		method       string
		expectedPath string
		expectedOK   bool
	}{
		{"exact path", "/users", "GET", "/users", true},
		{"single placeholder", "/users/42", "GET", "/users/{id}", true},
		{"nested path", "/users/42/posts", "GET", "/users/{id}/posts", true},
		{"nested placeholders", "/users/42/posts/7", "GET", "/users/{id}/posts/{postId}", true},
		{"method match POST", "/users", "POST", "/users", true},
		{"method mismatch", "/users", "DELETE", "", false},
		{"no match", "/pets", "GET", "", false},
		{"case insensitive method", "/users", "get", "/users", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := m.Match(tt.path, tt.method)
			assert.Equal(tt.expectedOK, ok)
			assert.Equal(tt.expectedPath, path)
		})
	}
}

func TestPathMatcher_PrefersSpecificPaths(t *testing.T) {
	assert := assert2.New(t)

	// When a concrete path matches both a wildcard and an exact pattern,
	// the more specific (exact) pattern should win.
	routes := []typedef.RouteInfo{
		{ID: "getCatchAll", Method: "GET", Path: "/{resource}"},
		{ID: "getUsers", Method: "GET", Path: "/users"},
	}
	m := newPathMatcher(routes)

	path, ok := m.Match("/users", "GET")
	assert.True(ok)
	assert.Equal("/users", path)
}

func TestPathMatcher_AmbiguousLastWins(t *testing.T) {
	assert := assert2.New(t)

	// When two patterns are equally specific and both match the URL
	// (same wildcard count, identical shape), the LAST in iteration
	// order wins. This aligns mockzilla's runtime routing with
	// libopenapi-validator's radix tree, which keeps only the most
	// recently inserted leaf per parameter slot. Without this, specs
	// like pubsub that declare two operations on identical URL shapes
	// (/v1/{project}/snapshots vs /v1/{topic}/snapshots) would have
	// mockzilla generate the response for one and the validator check
	// it against the other.
	routes := []typedef.RouteInfo{
		{ID: "listProjectSnapshots", Method: "GET", Path: "/v1/{project}/snapshots"},
		{ID: "listTopicSnapshots", Method: "GET", Path: "/v1/{topic}/snapshots"},
	}
	m := newPathMatcher(routes)

	path, ok := m.Match("/v1/abc/snapshots", "GET")
	assert.True(ok)
	assert.Equal("/v1/{topic}/snapshots", path)
}

func TestPathMatcher_SpecificBeatsLastWhenWildcardsDiffer(t *testing.T) {
	assert := assert2.New(t)

	// The last-wins tie-break in [TestPathMatcher_AmbiguousLastWins]
	// must not override the more-specific-wins rule when wildcard
	// counts differ. The exact path keeps winning even when it appears
	// before the wildcard.
	routes := []typedef.RouteInfo{
		{ID: "getUsersMe", Method: "GET", Path: "/users/me"},
		{ID: "getUser", Method: "GET", Path: "/users/{id}"},
	}
	m := newPathMatcher(routes)

	path, ok := m.Match("/users/me", "GET")
	assert.True(ok)
	assert.Equal("/users/me", path)
}

func TestPathMatcher_StripsHashDiscriminator(t *testing.T) {
	assert := assert2.New(t)

	// AWS-style specs use a `#qparam1&qparam2` suffix on path keys to
	// disambiguate operations that share a base path with different
	// required query parameters. The suffix is never part of an actual
	// URL, so the matcher strips it from pattern segments while keeping
	// the original spec path as the returned key (callers look the
	// operation up by that exact key).
	routes := []typedef.RouteInfo{
		{ID: "describeThumbnails", Method: "GET", Path: "/prod/channels/{id}/thumbnails#pipelineId&thumbnailType"},
	}
	m := newPathMatcher(routes)

	path, ok := m.Match("/prod/channels/abc/thumbnails", "GET")
	assert.True(ok)
	assert.Equal("/prod/channels/{id}/thumbnails#pipelineId&thumbnailType", path)
}
