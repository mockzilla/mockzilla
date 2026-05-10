package portable

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFS(t *testing.T) {
	mapFS := fstest.MapFS{
		"petstore.yml":              &fstest.MapFile{Data: []byte("openapi: 3.0.0")},
		"app.yml":                   &fstest.MapFile{Data: []byte("port: 3000")},
		"context.yml":               &fstest.MapFile{Data: []byte("key: value")},
		"static/svc/GET_hello.json": &fstest.MapFile{Data: []byte(`{"msg":"hi"}`)},
	}

	dir := t.TempDir()
	err := extractFS(mapFS, dir)
	require.NoError(t, err)

	t.Run("extracts files at root", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(dir, "petstore.yml"))
		require.NoError(t, err)
		assert.Equal(t, "openapi: 3.0.0", string(data))
	})

	t.Run("extracts nested files", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(dir, "static", "svc", "GET_hello.json"))
		require.NoError(t, err)
		assert.Equal(t, `{"msg":"hi"}`, string(data))
	})

	t.Run("creates directories", func(t *testing.T) {
		info, err := os.Stat(filepath.Join(dir, "static", "svc"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}

func TestExtractFS_empty(t *testing.T) {
	mapFS := fstest.MapFS{}
	dir := t.TempDir()
	err := extractFS(mapFS, dir)
	require.NoError(t, err)
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.txt")
	require.NoError(t, os.WriteFile(path, []byte("hi"), 0o644))

	assert.True(t, fileExists(path))
	assert.False(t, fileExists(filepath.Join(dir, "nope.txt")))
}

func TestExtractFS_withEmbeddedSpec(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	mapFS := fstest.MapFS{
		"petstore.yml": &fstest.MapFile{Data: specBytes},
	}

	dir := t.TempDir()
	require.NoError(t, extractFS(mapFS, dir))

	specPath := filepath.Join(dir, "petstore.yml")
	require.True(t, fileExists(specPath))

	// Verify the extracted spec can be used to create a handler
	data, err := os.ReadFile(specPath)
	require.NoError(t, err)

	h, err := newHandler(data)
	require.NoError(t, err)
	assert.NotEmpty(t, h.Routes())
}

// buildMockPackage creates a .mockz archive from a map of filenames to content.
func buildMockPackage(t *testing.T, name string, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	for fname, data := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: fname,
			Mode: 0o644,
			Size: int64(len(data)),
		}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return path
}

// TestIntegration_MockPackage exercises the full .mockz package flow:
// build a package, extract it, resolve specs, register services, hit the API.
func TestIntegration_MockPackage(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	pkg := buildMockPackage(t, "petstore.mockz", map[string][]byte{
		"openapi/petstore.yml": specBytes,
	})

	// Simulate the Run() pipeline: parseFlags -> resolvePackageArgs -> resolveSpecs
	fl := flags{}
	positional := resolvePackageArgs([]string{pkg}, &fl)
	specs := resolveSpecs(positional)
	require.Len(t, specs, 1, "expected one spec from .mockz package")

	// Wire up the router and register the spec
	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)

	err := registerService(router, specs[0], nil, nil, handlers)
	require.NoError(t, err)
	assert.Contains(t, handlers, "petstore")

	ts := httptest.NewServer(router)
	defer ts.Close()

	t.Run("GET /petstore/pets returns mock data", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/petstore/pets")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var pets []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&pets))
		require.NotEmpty(t, pets)
		assert.Contains(t, pets[0], "id")
		assert.Contains(t, pets[0], "name")
	})

	t.Run("POST /petstore/pets returns 201", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/petstore/pets", "application/json", nil)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})
}

// TestIntegration_MockPackageWithConfig verifies that app.yml and context.yml
// inside a .mockz package are picked up by resolvePackageArgs.
func TestIntegration_MockPackageWithConfig(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	pkg := buildMockPackage(t, "full.mockz", map[string][]byte{
		"openapi/petstore.yml": specBytes,
		"app.yml":              []byte("app:\n  port: 3000\nservices:\n  petstore:\n    latency: 10ms"),
		"context.yml":          []byte("petstore:\n  base_url: http://localhost:3000"),
	})

	fl := flags{}
	positional := resolvePackageArgs([]string{pkg}, &fl)
	specs := resolveSpecs(positional)
	require.Len(t, specs, 1)

	assert.NotEmpty(t, fl.config, "app.yml should be set from package")
	assert.NotEmpty(t, fl.context, "context.yml should be set from package")
	assert.True(t, fileExists(fl.config))
	assert.True(t, fileExists(fl.context))

	// Config and context should be loadable
	cfg, err := loadPortableConfig(fl.config, t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, cfg.App)

	contexts, err := loadContexts(fl.context)
	require.NoError(t, err)
	assert.Contains(t, contexts, "petstore")
}

// TestIntegration_MockPackageFromURL verifies the full flow when
// the .mockz package is served over HTTP.
func TestIntegration_MockPackageFromURL(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	pkg := buildMockPackage(t, "remote.mockz", map[string][]byte{
		"openapi/petstore.yml": specBytes,
	})
	pkgData, err := os.ReadFile(pkg)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pkgData)
	}))
	defer srv.Close()

	fl := flags{}
	positional := resolvePackageArgs([]string{srv.URL + "/petstore.mockz"}, &fl)
	specs := resolveSpecs(positional)
	require.Len(t, specs, 1)

	router := testRouter(t)
	handlers := make(map[string]*swappableHandler)
	err = registerService(router, specs[0], nil, nil, handlers)
	require.NoError(t, err)
	assert.Contains(t, handlers, "petstore")
}
