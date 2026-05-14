package portable

import (
	"io"
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
		"services/petstore/openapi.yml": &fstest.MapFile{Data: []byte("openapi: 3.0.0")},
		"services/petstore/config.yml":  &fstest.MapFile{Data: []byte("latency: 100ms")},
		"app.yml":                       &fstest.MapFile{Data: []byte("port: 3000")},
	}

	dir := t.TempDir()
	err := extractFS(mapFS, dir)
	require.NoError(t, err)

	t.Run("extracts nested files", func(t *testing.T) {
		assert.True(t, fileExists(filepath.Join(dir, "services", "petstore", "openapi.yml")))
		assert.True(t, fileExists(filepath.Join(dir, "services", "petstore", "config.yml")))
	})

	t.Run("extracts root files", func(t *testing.T) {
		assert.True(t, fileExists(filepath.Join(dir, "app.yml")))
	})
}

func TestExtractFS_Empty(t *testing.T) {
	require.NoError(t, extractFS(fstest.MapFS{}, t.TempDir()))
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.txt")
	require.NoError(t, os.WriteFile(p, []byte("hi"), 0o644))
	assert.True(t, fileExists(p))
	assert.False(t, fileExists(filepath.Join(dir, "nope.txt")))
}

// TestIntegration_ServicesRoot exercises the full pipeline: a root dir
// with services/<name>/{spec,config,context}, no app.yml, served via
// the public Run-like flow (router, registerService, real HTTP).
func TestIntegration_ServicesRoot(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	root := t.TempDir()
	svcDir := filepath.Join(root, "services", "petstore")
	require.NoError(t, os.MkdirAll(svcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(svcDir, "openapi.yml"), specBytes, 0o644))

	services, err := resolveServices([]string{root})
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "petstore", services[0].Name)

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], nil, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/petstore/pets")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestIntegration_SingleServiceFolder exercises the single-folder
// shorthand. The service identity comes from inside the folder:
// config.yml `name:` or a non-generic spec basename. The folder's own
// basename is not used as a fallback. Here `mount:` in config.yml
// pins the URL prefix regardless.
func TestIntegration_SingleServiceFolder(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	dir := filepath.Join(t.TempDir(), "anything")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"), specBytes, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte("name: pets\nlatency: 0ms\nmount: pets/v2\n"), 0o644))

	services, err := resolveServices([]string{dir})
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "pets", services[0].Name)

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], nil, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pets/v2/pets")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestIntegration_PackageRoundtrip builds a .mockz with the new
// per-service shape, resolves it, registers, and hits the mock API.
func TestIntegration_PackageRoundtrip(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	pkg := buildPackage(t, t.TempDir(), "test.mockz", map[string][]byte{
		"services/petstore/openapi.yml": specBytes,
		"services/petstore/config.yml":  []byte("latency: 1ms"),
		"app.yml":                       []byte("port: 0"),
	})

	services, err := resolveServices([]string{pkg})
	require.NoError(t, err)
	require.Len(t, services, 1)

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], nil, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/petstore/pets")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestIntegration_MergeSpecAndStatic confirms the user-facing rule for
// "spec + static endpoints in same folder": the spec keeps driving its
// own endpoints, each static file either overrides the spec's response
// for that (path, method) or adds a new endpoint, and the spec file
// itself is also served at `GET /<filename>` as a literal asset so it
// stays fetchable for documentation. Folder has only a generic
// `openapi.yml`, so no inside-name signal: service mounts at /.
func TestIntegration_MergeSpecAndStatic(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	dir := filepath.Join(t.TempDir(), "any-cwd")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pets", "{petId}", "get"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "extra", "get"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"), specBytes, 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "pets", "{petId}", "get", "index.json"),
		[]byte(`{"id":"static","name":"fixture"}`), 0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "extra", "get", "index.json"),
		[]byte(`{"extra":true}`), 0o644,
	))

	services, err := resolveServices([]string{dir})
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Empty(t, services[0].Name)

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], nil, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()

	// 1. Spec endpoint /pets is untouched; comes from generator.
	resp, err := http.Get(ts.URL + "/pets")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Static override for /pets/{petId} returns the fixture body.
	respO, err := http.Get(ts.URL + "/pets/42")
	require.NoError(t, err)
	defer func() { _ = respO.Body.Close() }()
	assert.Equal(t, http.StatusOK, respO.StatusCode)
	body, _ := io.ReadAll(respO.Body)
	assert.Contains(t, string(body), `"id":"static"`)
	assert.Contains(t, string(body), `"name":"fixture"`)

	// 3. New endpoint /extra wasn't in the spec; comes from static.
	respE, err := http.Get(ts.URL + "/extra")
	require.NoError(t, err)
	defer func() { _ = respE.Body.Close() }()
	assert.Equal(t, http.StatusOK, respE.StatusCode)

	// 4. The spec file itself is fetchable at its literal path.
	respA, err := http.Get(ts.URL + "/openapi.yml")
	require.NoError(t, err)
	defer func() { _ = respA.Body.Close() }()
	assert.Equal(t, http.StatusOK, respA.StatusCode)
}

// TestIntegration_ImplicitGetStatic confirms the simpler convention
// where `<path>/index.<ext>` (no method dir) implies GET, plus
// `index.<ext>` at the service root serves the service's root URL.
// The folder has no inside-name signal, so the service has empty Name
// and mounts at /.
func TestIntegration_ImplicitGetStatic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "any-cwd")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"),
		[]byte(`{"service":"root"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "v1", "users"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "v1", "users", "index.json"),
		[]byte(`[{"id":1}]`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "v2", "me"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "v2", "me", "index.json"),
		[]byte(`{"id":"me"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "v1", "users", "post"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "v1", "users", "post", "index.json"),
		[]byte(`{"created":true}`), 0o644))

	services, err := resolveServices([]string{dir})
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Empty(t, services[0].Name, "no inside signal → empty name → mounts at /")

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], nil, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()

	for _, c := range []struct {
		method, path, wantSubstr string
	}{
		{"GET", "/", `"service":"root"`},
		{"GET", "/v1/users", `"id":1`},
		{"GET", "/v2/me", `"id":"me"`},
		{"POST", "/v1/users", `"created":true`},
	} {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, nil)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "%s %s", c.method, c.path)
		assert.Contains(t, string(body), c.wantSubstr, "%s %s body", c.method, c.path)
	}
}

// TestIntegration_ScannerSkipsNoisyDirs verifies that node_modules /
// .git / dotted dirs inside a service folder don't surface stray
// endpoints (and don't crash the scan). No inside name signal here,
// so the service mounts at /.
func TestIntegration_ScannerSkipsNoisyDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "any-cwd")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "v1", "get"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "v1", "get", "index.json"),
		[]byte(`{"ok":true}`), 0o644))
	for _, noise := range []string{"node_modules", ".git", "_vendor"} {
		bogus := filepath.Join(dir, noise, "bogus", "get")
		require.NoError(t, os.MkdirAll(bogus, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(bogus, "index.json"),
			[]byte(`{"hidden":true}`), 0o644))
	}

	services, err := resolveServices([]string{dir})
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Empty(t, services[0].Name)

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], nil, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()

	// Real endpoint is up at the root mount.
	resp, err := http.Get(ts.URL + "/v1")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Noise paths must NOT have been registered.
	for _, p := range []string{"/node_modules/bogus", "/.git/bogus", "/_vendor/bogus"} {
		r, err := http.Get(ts.URL + p)
		require.NoError(t, err)
		_ = r.Body.Close()
		assert.Equal(t, http.StatusNotFound, r.StatusCode, "expected 404 for %s", p)
	}
}

// TestIntegration_ConvenienceFlags verifies --latency/--mount/--errors
// take effect for a single-spec invocation.
func TestIntegration_ConvenienceFlags(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")
	dir := t.TempDir()
	spec := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(spec, specBytes, 0o644))

	services, err := resolveServices([]string{spec})
	require.NoError(t, err)
	require.Len(t, services, 1)

	overrides, err := buildOverrides(flags{
		latency: "1ms",
		mount:   "pets/v9",
	})
	require.NoError(t, err)
	require.NotNil(t, overrides)

	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	handlers := make(map[string]*swappableHandler)
	require.NoError(t, registerService(router, services[0], overrides, handlers))

	ts := httptest.NewServer(router)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/pets/v9/pets")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
