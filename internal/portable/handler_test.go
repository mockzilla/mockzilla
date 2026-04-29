package portable

import (
	"bytes"
	"embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/**
var testDataFS embed.FS

func loadTestSpec(t *testing.T, name string) []byte {
	t.Helper()
	data, err := testDataFS.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return data
}

func TestNewHandler(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	t.Run("creates handler from valid spec", func(t *testing.T) {
		h, err := newHandler(specBytes)
		require.NoError(t, err)
		require.NotNil(t, h)

		routes := h.Routes()
		assert.NotEmpty(t, routes)

		// Verify expected routes exist
		routeMap := make(map[string]string)
		for _, r := range routes {
			routeMap[r.Method+" "+r.Path] = r.ID
		}
		assert.Contains(t, routeMap, "GET /pets")
		assert.Contains(t, routeMap, "POST /pets")
		assert.Contains(t, routeMap, "GET /pets/{petId}")
	})
}

func TestHandler_RegisterRoutes(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")
	h, err := newHandler(specBytes)
	require.NoError(t, err)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// Verify the catch-all route is registered
	req := httptest.NewRequest(http.MethodGet, "/pets", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should get a response (not 405 Method Not Allowed which means no route)
	assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandler_handleRequest(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")
	h, err := newHandler(specBytes)
	require.NoError(t, err)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	t.Run("returns mock response for matching route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
		assert.NotEmpty(t, w.Body.Bytes())

		// Should be valid JSON
		assert.True(t, json.Valid(w.Body.Bytes()), "response body should be valid JSON")
	})

	t.Run("returns 404 for non-matching route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns correct status code for POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/pets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("matches parameterized paths", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/pets/42", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, json.Valid(w.Body.Bytes()))
	})
}

func TestHandler_ContentTypeFromSpec(t *testing.T) {
	// Spec declares text/html success response; handler must propagate that
	// content-type rather than defaulting to application/json.
	specBytes := []byte(`openapi: 3.0.0
info:
  title: Static
  version: "1.0.0"
paths:
  /welcome:
    get:
      operationId: getWelcome
      responses:
        "200":
          description: OK
          content:
            text/html:
              schema:
                type: string
              example: "<html><body>hi</body></html>"
`)
	h, err := newHandler(specBytes)
	require.NoError(t, err)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/welcome", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html", w.Header().Get("Content-Type"))
	assert.NotEmpty(t, w.Body.Bytes())
}

func TestHandler_MountedUnderServicePrefix(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")
	h, err := newHandler(specBytes)
	require.NoError(t, err)

	// Simulate how RegisterService mounts the handler under /{service-name}
	r := chi.NewRouter()
	r.Route("/petstore", func(sub chi.Router) {
		h.RegisterRoutes(sub)
	})

	t.Run("routes correctly with service prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/petstore/pets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, json.Valid(w.Body.Bytes()))
	})

	t.Run("parameterized path with prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/petstore/pets/42", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("POST with prefix returns correct status code", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/petstore/pets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("404 for non-matching path under prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/petstore/nonexistent", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestSwappableHandler(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")
	h, err := newHandler(specBytes)
	require.NoError(t, err)

	sw := &swappableHandler{handler: h}

	t.Run("delegates Routes", func(t *testing.T) {
		routes := sw.Routes()
		assert.NotEmpty(t, routes)
		assert.Equal(t, h.Routes(), routes)
	})

	t.Run("delegates RegisterRoutes and handleRequest", func(t *testing.T) {
		r := chi.NewRouter()
		sw.RegisterRoutes(r)

		req := httptest.NewRequest(http.MethodGet, "/pets", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, json.Valid(w.Body.Bytes()))
	})

	t.Run("delegates Generate", func(t *testing.T) {
		body, _ := json.Marshal(api.GenerateRequest{
			Path:   "/pets",
			Method: "GET",
		})
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body))
		w := httptest.NewRecorder()

		sw.Generate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("swap replaces handler", func(t *testing.T) {
		h2, err := newHandler(specBytes)
		require.NoError(t, err)

		sw.swap(h2)
		assert.Equal(t, h2.Routes(), sw.Routes())
	})
}

func testRouter(t *testing.T) *api.Router {
	t.Helper()
	return api.NewRouter(api.WithConfigOption(config.NewDefaultAppConfig(t.TempDir())))
}

func TestRegisterService(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	// Write spec to temp file
	dir := t.TempDir()
	specPath := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(specPath, specBytes, 0644))

	router := testRouter(t)
	handlers := make(map[string]*swappableHandler)

	err := registerService(router, specPath, nil, nil, handlers)
	require.NoError(t, err)

	assert.Contains(t, handlers, "petstore")
	assert.NotNil(t, handlers["petstore"])

	// Verify the service is accessible through the router
	req := httptest.NewRequest(http.MethodGet, "/petstore/pets", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBuildHandler(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	dir := t.TempDir()
	specPath := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(specPath, specBytes, 0644))

	t.Run("builds from valid spec file", func(t *testing.T) {
		h, err := buildHandler(specPath, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, h.Routes())
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		_, err := buildHandler("/nonexistent/spec.yml", nil)
		assert.Error(t, err)
	})
}

func TestHandler_Generate(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")
	h, err := newHandler(specBytes)
	require.NoError(t, err)

	t.Run("returns generated request for valid operation", func(t *testing.T) {
		body, _ := json.Marshal(api.GenerateRequest{
			Path:   "/pets",
			Method: "GET",
		})
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.Generate(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var res schema.GeneratedRequest
		err := json.Unmarshal(w.Body.Bytes(), &res)
		assert.NoError(t, err)
		assert.NotEmpty(t, res.Path)
	})

	t.Run("returns 404 for non-matching operation", func(t *testing.T) {
		body, _ := json.Marshal(api.GenerateRequest{
			Path:   "/nonexistent",
			Method: "GET",
		})
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.Generate(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 400 for empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/generate", nil)
		w := httptest.NewRecorder()

		h.Generate(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestIntegration_EndToEnd exercises the full portable stack: register a spec
// via the public registerService path, hit the mock API, and verify the
// response body contains every required field from the schema.
func TestIntegration_EndToEnd(t *testing.T) {
	specBytes := loadTestSpec(t, "petstore.yml")

	dir := t.TempDir()
	specPath := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(specPath, specBytes, 0644))

	// Wire up the full router like Run() does
	router := testRouter(t)
	_ = api.CreateServiceRoutes(router)
	_ = api.CreateHistoryRoutes(router)
	handlers := make(map[string]*swappableHandler)

	err := registerService(router, specPath, nil, nil, handlers)
	require.NoError(t, err)

	ts := httptest.NewServer(router)
	defer ts.Close()

	t.Run("GET list returns array with required fields", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/petstore/pets")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

		var pets []map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&pets))
		require.NotEmpty(t, pets, "expected at least one pet in the array")

		for i, pet := range pets {
			assert.Contains(t, pet, "id", "pet[%d] missing required field 'id'", i)
			assert.Contains(t, pet, "name", "pet[%d] missing required field 'name'", i)
		}
	})

	t.Run("GET single pet returns object with required fields", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/petstore/pets/42")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var pet map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&pet))
		assert.Contains(t, pet, "id")
		assert.Contains(t, pet, "name")
	})

	t.Run("POST returns 201 with required fields", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/petstore/pets", "application/json", nil)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var pet map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&pet))
		assert.Contains(t, pet, "id")
		assert.Contains(t, pet, "name")
	})

	t.Run("UI services list includes petstore", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/.services/")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var list map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))

		items, ok := list["items"].([]any)
		require.True(t, ok, "expected items array")
		require.NotEmpty(t, items)

		found := false
		for _, item := range items {
			svc, _ := item.(map[string]any)
			if svc["name"] == "petstore" {
				found = true
				assert.Greater(t, svc["resourceNumber"], float64(0))
			}
		}
		assert.True(t, found, "petstore not found in services list")
	})

	t.Run("UI generate returns request with path", func(t *testing.T) {
		body, _ := json.Marshal(api.GenerateRequest{
			Path:   "/pets",
			Method: "GET",
		})
		resp, err := http.Post(ts.URL+"/.services/petstore/generate", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var gen schema.GeneratedRequest
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&gen))
		assert.Equal(t, "/pets", gen.Path)
	})
}
