package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanStaticFiles_Meta(t *testing.T) {
	dir := t.TempDir()
	writeStatic(t, dir, "index.json", `{"root":true}`)
	writeStatic(t, dir, "meta.json", `{"status":203}`)
	writeStatic(t, dir, "plain/index.json", `{"plain":true}`)
	writeStatic(t, dir, "users/post/index.json", `{"id":42}`)
	writeStatic(t, dir, "users/post/meta.json", `{"status":201,"headers":{"Location":"/users/42","X-Request-Id":"abc"}}`)
	writeStatic(t, dir, "problem/index.json", `{"title":"missing"}`)
	writeStatic(t, dir, "problem/meta.json", `{"status":404,"headers":{"content-type":"application/problem+json"}}`)
	writeStatic(t, dir, "gone/delete/meta.json", `{"status":204,"headers":{"X-Deleted":"yes"}}`)
	writeStatic(t, dir, "empty/index.txt", "")
	writeStatic(t, dir, "empty/meta.json", `{"status":202}`)
	writeStatic(t, dir, "assets/meta.json", `{"status":204}`)
	writeStatic(t, dir, "assets/notes.json", `{"asset":true}`)

	routes, err := scanStaticFiles(dir)
	require.NoError(t, err)

	byKey := make(map[string]Route, len(routes))
	for _, r := range routes {
		assert.NotContains(t, r.Path, "meta.json", "meta.json must never become a route")
		byKey[r.Method+" "+r.Path] = r
	}
	assert.Len(t, routes, 8)

	for _, c := range []struct {
		key         string
		status      int
		headers     map[string]string
		contentType string
		content     string
	}{
		{"GET /", 203, nil, "application/json", `{"root":true}`},
		{"GET /plain", 0, nil, "application/json", `{"plain":true}`},
		{"POST /users", 201, map[string]string{"Location": "/users/42", "X-Request-Id": "abc"}, "application/json", `{"id":42}`},
		{"GET /problem", 404, map[string]string{}, "application/problem+json", `{"title":"missing"}`},
		{"DELETE /gone", 204, map[string]string{"X-Deleted": "yes"}, "", ""},
		{"GET /empty", 202, nil, "text/plain", ""},
		{"GET /assets", 204, nil, "", ""},
		{"GET /assets/notes.json", 0, nil, "application/json", `{"asset":true}`},
	} {
		r, ok := byKey[c.key]
		require.True(t, ok, c.key)
		assert.Equal(t, c.status, r.Status, c.key)
		assert.Equal(t, c.headers, r.Headers, c.key)
		assert.Equal(t, c.contentType, r.ContentType, c.key)
		assert.Equal(t, c.content, r.Content, c.key)
	}
}

func TestScanStaticFiles_MetaErrors(t *testing.T) {
	for _, c := range []struct {
		name, meta, want string
	}{
		{"invalid json", `{"status":`, "parsing"},
		{"unknown key", `{"statusCode":201}`, `unknown field "statusCode"`},
		{"status too high", `{"status":999}`, "out of range"},
		{"status too low", `{"status":100}`, "out of range"},
		{"non-string header", `{"headers":{"X-Count":1}}`, "cannot unmarshal number"},
		{"empty header", `{"headers":{"X-Empty":""}}`, "empty value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeStatic(t, dir, "users/index.json", `{}`)
			writeStatic(t, dir, "users/meta.json", c.meta)

			_, err := scanStaticFiles(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), filepath.Join(dir, "users", "meta.json"))
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestHasStaticEndpoints(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"index file", map[string]string{"users/index.json": `{}`}, true},
		{"bodiless meta only", map[string]string{"users/delete/meta.json": `{"status":204}`}, true},
		{"spec only", map[string]string{"openapi.yml": "openapi: 3.0.0"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range c.files {
				writeStatic(t, dir, rel, content)
			}

			assert.Equal(t, c.want, HasStaticEndpoints(dir))
		})
	}
}

func TestHasRootEndpoint(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"root index", map[string]string{"index.json": `{}`}, true},
		{"root meta only", map[string]string{"meta.json": `{"status":204}`}, true},
		{"nested only", map[string]string{"users/index.json": `{}`}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range c.files {
				writeStatic(t, dir, rel, content)
			}

			assert.Equal(t, c.want, HasRootEndpoint(dir))
		})
	}
}

func TestBuildStaticOperation_Meta(t *testing.T) {
	for _, c := range []struct {
		name        string
		route       Route
		wantCode    string
		wantDesc    string
		wantHeaders map[string]string
	}{
		{
			name:     "defaults to 200",
			route:    Route{Method: "GET", Path: "/", ContentType: "application/json", Content: `{}`},
			wantCode: "200", wantDesc: "OK",
		},
		{
			name: "status and headers",
			route: Route{Method: "POST", Path: "/users", ContentType: "application/json", Content: `{"id":42}`,
				Status: 201, Headers: map[string]string{"Location": "/users/42"}},
			wantCode: "201", wantDesc: "Created", wantHeaders: map[string]string{"Location": "/users/42"},
		},
		{
			name:     "error status",
			route:    Route{Method: "GET", Path: "/missing", ContentType: "application/json", Content: `{}`, Status: 404},
			wantCode: "404", wantDesc: "Not Found",
		},
		{
			name:     "bodiless",
			route:    Route{Method: "DELETE", Path: "/users/{id}", Status: 204, Headers: map[string]string{"X-Deleted": "yes"}},
			wantCode: "204", wantDesc: "No Content", wantHeaders: map[string]string{"X-Deleted": "yes"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			op, err := buildStaticOperation(c.route)
			require.NoError(t, err)

			responses := op["responses"].(map[string]any)
			require.Len(t, responses, 1)
			response, ok := responses[c.wantCode].(map[string]any)
			require.True(t, ok, "response key %s", c.wantCode)
			assert.Equal(t, c.wantDesc, response["description"])

			if c.route.Content == "" {
				assert.Equal(t, true, response["x-static-response"])
				assert.NotContains(t, response, "content")
			} else {
				assert.NotContains(t, response, "x-static-response")
				content := response["content"].(map[string]any)[c.route.ContentType].(map[string]any)
				assert.Equal(t, c.route.Content, content["x-static-response"])
			}

			headers, has := response["headers"].(map[string]any)
			assert.Equal(t, len(c.wantHeaders) > 0, has)
			for name, value := range c.wantHeaders {
				h := headers[name].(map[string]any)
				assert.Equal(t, map[string]any{"type": "string"}, h["schema"])
				assert.Equal(t, value, h["x-static-response"])
			}
		})
	}
}

func writeStatic(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
