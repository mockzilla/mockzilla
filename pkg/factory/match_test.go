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
