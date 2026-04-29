package generator

import (
	"net/url"
	"strings"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestGeneratePath(t *testing.T) {
	valueReplacer := replacer.CreateValueReplacer(replacer.Replacers, nil)

	tests := []struct {
		name          string
		op            *schema.Operation
		expectedPath  string
		checkQuery    bool
		expectedQuery map[string]string
		contextData   []map[string]any
	}{
		{
			name: "Simple path without parameters",
			op: &schema.Operation{
				Path: "/users",
			},
			expectedPath: "/users",
		},
		{
			name: "Path with string path parameters",
			op: &schema.Operation{
				Path: "/users/{id}/posts/{postId}",
				PathParams: &schema.Schema{
					Type: "object",
					Properties: map[string]*schema.Schema{
						"id":     {Type: "string", Enum: []any{"user123"}},
						"postId": {Type: "string", Enum: []any{"post456"}},
					},
				},
			},
			expectedPath: "/users/user123/posts/post456",
		},
		{
			name: "Path with integer path parameter",
			op: &schema.Operation{
				Path: "/items/{id}",
				PathParams: &schema.Schema{
					Type: "object",
					Properties: map[string]*schema.Schema{
						"id": {Type: "integer", Enum: []any{42}},
					},
				},
			},
			expectedPath: "/items/42",
		},
		{
			name: "Multiple path parameters",
			op: &schema.Operation{
				Path: "/api/{version}/users/{userId}/posts/{postId}",
				PathParams: &schema.Schema{
					Type: "object",
					Properties: map[string]*schema.Schema{
						"version": {Type: "string", Enum: []any{"v1"}},
						"userId":  {Type: "integer", Enum: []any{123}},
						"postId":  {Type: "integer", Enum: []any{456}},
					},
				},
			},
			expectedPath: "/api/v1/users/123/posts/456",
		},
		{
			name: "Nil path params generates values for missing params",
			op: &schema.Operation{
				Path:       "/users/{id}",
				PathParams: nil,
			},
			// Missing path params are auto-added as string type and get generated values
			expectedPath: "/users/123",
			contextData: []map[string]any{
				{"in-path": map[string]any{"id": "123"}},
			},
		},
		{
			name: "Empty query parameters",
			op: &schema.Operation{
				Path:  "/users",
				Query: schema.QueryParameters{},
			},
			expectedPath: "/users",
		},
		{
			name: "Nil query params",
			op: &schema.Operation{
				Path:  "/users",
				Query: nil,
			},
			expectedPath: "/users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testReplacer := valueReplacer
			if tt.contextData != nil {
				testReplacer = replacer.CreateValueReplacer(replacer.Replacers, tt.contextData)
			}
			result := generatePath(tt.op, testReplacer)

			if tt.checkQuery {
				assert.True(t, strings.Contains(result, "?"), "Expected query string in path")
				parts := strings.SplitN(result, "?", 2)
				assert.True(t, strings.HasPrefix(result, tt.expectedPath))

				values, err := url.ParseQuery(parts[1])
				assert.NoError(t, err)
				for key, expected := range tt.expectedQuery {
					assert.Equal(t, expected, values.Get(key))
				}
			} else {
				assert.Equal(t, tt.expectedPath, result)
			}
		})
	}

	// Query parameter tests with validation
	queryTests := []struct {
		name          string
		op            *schema.Operation
		expectedPath  string
		expectedQuery map[string]string
	}{
		{
			name: "Simple query parameters",
			op: &schema.Operation{
				Path: "/search",
				Query: schema.QueryParameters{
					"q":     {Schema: &schema.Schema{Type: "string", Enum: []any{"test"}}, Required: true},
					"limit": {Schema: &schema.Schema{Type: "integer", Enum: []any{10}}, Required: false},
				},
			},
			expectedPath: "/search",
			expectedQuery: map[string]string{
				"q":     "test",
				"limit": "10",
			},
		},
		{
			name: "Path and query parameters combined",
			op: &schema.Operation{
				Path: "/users/{userId}/posts",
				PathParams: &schema.Schema{
					Type: "object",
					Properties: map[string]*schema.Schema{
						"userId": {Type: "string", Enum: []any{"user123"}},
					},
				},
				Query: schema.QueryParameters{
					"status": {Schema: &schema.Schema{Type: "string", Enum: []any{"published"}}, Required: true},
				},
			},
			expectedPath: "/users/user123/posts",
			expectedQuery: map[string]string{
				"status": "published",
			},
		},
		{
			name: "Query with boolean parameter",
			op: &schema.Operation{
				Path: "/items",
				Query: schema.QueryParameters{
					"active": {Schema: &schema.Schema{Type: "boolean", Enum: []any{true}}},
				},
			},
			expectedPath: "/items",
			expectedQuery: map[string]string{
				"active": "true",
			},
		},
		{
			name: "Query with nested object parameter",
			op: &schema.Operation{
				Path: "/checkout",
				Query: schema.QueryParameters{
					"customer": {
						Schema: &schema.Schema{
							Type: "object",
							Properties: map[string]*schema.Schema{
								"name":  {Type: "string", Enum: []any{"John"}},
								"email": {Type: "string", Enum: []any{"john@example.com"}},
							},
						},
					},
				},
			},
			expectedPath: "/checkout",
			expectedQuery: map[string]string{
				"customer[name]":  "John",
				"customer[email]": "john@example.com",
			},
		},
	}

	for _, tt := range queryTests {
		t.Run(tt.name, func(t *testing.T) {
			result := generatePath(tt.op, valueReplacer)
			assert.True(t, strings.HasPrefix(result, tt.expectedPath))
			assert.Contains(t, result, "?")

			parts := strings.SplitN(result, "?", 2)
			values, err := url.ParseQuery(parts[1])
			assert.NoError(t, err)

			for key, expected := range tt.expectedQuery {
				assert.Equal(t, expected, values.Get(key), "Query param %s mismatch", key)
			}
		})
	}
}

func TestEnsurePathParams(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		pathParams     *schema.Schema
		expectedProps  []string
		expectModified bool
	}{
		{
			name:           "no placeholders",
			path:           "/users",
			pathParams:     nil,
			expectedProps:  nil,
			expectModified: false,
		},
		{
			name: "all params defined",
			path: "/users/{id}",
			pathParams: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"id": {Type: "integer"},
				},
			},
			expectedProps:  []string{"id"},
			expectModified: false,
		},
		{
			name:           "missing param added",
			path:           "/users/{id}",
			pathParams:     nil,
			expectedProps:  []string{"id"},
			expectModified: true,
		},
		{
			name: "partial params - missing one",
			path: "/users/{userId}/posts/{postId}",
			pathParams: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"userId": {Type: "integer"},
				},
			},
			expectedProps:  []string{"userId", "postId"},
			expectModified: true,
		},
		{
			name:           "multiple missing params",
			path:           "/apps/{appId}/items/{id}",
			pathParams:     nil,
			expectedProps:  []string{"appId", "id"},
			expectModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ensurePathParams(tt.path, tt.pathParams)

			if tt.expectedProps == nil {
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result)
			assert.Equal(t, "object", result.Type)
			assert.Len(t, result.Properties, len(tt.expectedProps))

			for _, prop := range tt.expectedProps {
				_, exists := result.Properties[prop]
				assert.True(t, exists, "Expected property %s to exist", prop)
			}

			if tt.expectModified && tt.pathParams != nil {
				// Verify original wasn't modified
				assert.NotEqual(t, result, tt.pathParams)
			}
		})
	}
}

func TestGeneratePath_PlaceholdersFullyReplaced(t *testing.T) {
	valueReplacer := replacer.CreateValueReplacer(replacer.Replacers, nil)

	tests := []struct {
		name       string
		op         *schema.Operation
		checkNoMap bool // check that path doesn't contain "map[]"
	}{
		{
			name: "any type path param should not produce map[]",
			op: &schema.Operation{
				Path: "/v1/accounts/{account}",
				PathParams: &schema.Schema{
					Type: "object",
					Properties: map[string]*schema.Schema{
						"account": {Type: "any"},
					},
				},
			},
			checkNoMap: true,
		},
		{
			name: "empty schema path param should not produce map[]",
			op: &schema.Operation{
				Path: "/v1/items/{id}",
				PathParams: &schema.Schema{
					Type: "object",
					Properties: map[string]*schema.Schema{
						"id": {},
					},
				},
			},
			checkNoMap: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generatePath(tt.op, valueReplacer)

			// Path should not contain any unreplaced placeholders
			assert.NotContains(t, result, "{", "Path should not contain unreplaced placeholders")
			assert.NotContains(t, result, "}", "Path should not contain unreplaced placeholders")

			if tt.checkNoMap {
				assert.NotContains(t, result, "map[]", "Path should not contain map[] from unresolved any type")
			}
		})
	}
}
