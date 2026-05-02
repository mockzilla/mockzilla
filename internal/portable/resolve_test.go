package portable

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsURL(t *testing.T) {
	assert.True(t, isURL("https://example.com/petstore.yml"))
	assert.True(t, isURL("http://localhost:8080/spec.json"))
	assert.False(t, isURL("petstore.yml"))
	assert.False(t, isURL("/path/to/spec.yml"))
	assert.False(t, isURL("ftp://example.com/spec.yml"))
}

func TestDownloadSpec(t *testing.T) {
	t.Run("downloads and saves spec", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		path, err := downloadSpec(srv.URL + "/petstore.yml")
		require.NoError(t, err)
		assert.Contains(t, path, "petstore.yml")

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "openapi: 3.0.0", string(data))
	})

	t.Run("appends .yml if no spec extension", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		path, err := downloadSpec(srv.URL + "/v2/api-docs")
		require.NoError(t, err)
		assert.True(t, isSpecFile(path))
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := downloadSpec(srv.URL + "/spec.yml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("uses host as filename for root URL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		path, err := downloadSpec(srv.URL + "/")
		require.NoError(t, err)
		assert.True(t, isSpecFile(path))
	})

	t.Run("returns error on connection failure", func(t *testing.T) {
		_, err := downloadSpec("http://127.0.0.1:1/spec.yml")
		assert.Error(t, err)
	})
}

func TestIsSpecFile(t *testing.T) {
	assert.True(t, isSpecFile("petstore.yaml"))
	assert.True(t, isSpecFile("petstore.yml"))
	assert.True(t, isSpecFile("petstore.json"))
	assert.False(t, isSpecFile("petstore.go"))
	assert.False(t, isSpecFile("petstore.txt"))
	assert.False(t, isSpecFile("petstore"))
}

func TestIsPortableMode(t *testing.T) {
	// Create temp dir with spec files
	dir := t.TempDir()
	specPath := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(specPath, []byte("openapi: 3.0.0"), 0644))

	t.Run("detects spec file arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{specPath}))
	})

	t.Run("detects directory with specs", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{dir}))
	})

	t.Run("ignores flags", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{specPath, "--port", "3000"}))
	})

	t.Run("returns false for non-spec args", func(t *testing.T) {
		assert.False(t, IsPortableMode([]string{"/some/app/dir"}))
	})

	t.Run("returns false for empty args", func(t *testing.T) {
		assert.False(t, IsPortableMode(nil))
	})

	t.Run("returns false for directory without specs", func(t *testing.T) {
		emptyDir := t.TempDir()
		assert.False(t, IsPortableMode([]string{emptyDir}))
	})

	t.Run("detects URL arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"https://example.com/petstore.yml"}))
	})

	t.Run("detects URL mixed with files", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{specPath, "https://example.com/api.json"}))
	})

	t.Run("detects non-existent spec file arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"/nonexistent/petstore.json"}))
	})

	t.Run("detects directory with static subdir", func(t *testing.T) {
		staticDir := t.TempDir()
		svcDir := filepath.Join(staticDir, "static", "myapi", "users", "get")
		require.NoError(t, os.MkdirAll(svcDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(svcDir, "index.json"), []byte(`{"id":1}`), 0o644))
		assert.True(t, IsPortableMode([]string{staticDir}))
	})
}

func TestResolveSpecs(t *testing.T) {
	dir := t.TempDir()
	spec1 := filepath.Join(dir, "petstore.yml")
	spec2 := filepath.Join(dir, "stripe.yaml")
	nonSpec := filepath.Join(dir, "readme.md")

	require.NoError(t, os.WriteFile(spec1, []byte("openapi: 3.0.0"), 0644))
	require.NoError(t, os.WriteFile(spec2, []byte("openapi: 3.0.0"), 0644))
	require.NoError(t, os.WriteFile(nonSpec, []byte("# readme"), 0644))

	t.Run("resolves individual spec files", func(t *testing.T) {
		specs := resolveSpecs([]string{spec1, spec2})
		assert.Len(t, specs, 2)
		assert.Contains(t, specs, spec1)
		assert.Contains(t, specs, spec2)
	})

	t.Run("resolves specs from directory", func(t *testing.T) {
		specs := resolveSpecs([]string{dir})
		assert.Len(t, specs, 2)
	})

	t.Run("skips flags", func(t *testing.T) {
		specs := resolveSpecs([]string{"--port", "3000", spec1})
		assert.Len(t, specs, 1)
	})

	t.Run("skips non-spec files", func(t *testing.T) {
		specs := resolveSpecs([]string{nonSpec})
		assert.Empty(t, specs)
	})

	t.Run("returns nil for no matches", func(t *testing.T) {
		specs := resolveSpecs([]string{"/nonexistent/path"})
		assert.Nil(t, specs)
	})

	t.Run("downloads URL specs", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		specs := resolveSpecs([]string{srv.URL + "/petstore.yml"})
		require.Len(t, specs, 1)
		data, err := os.ReadFile(specs[0])
		require.NoError(t, err)
		assert.Equal(t, "openapi: 3.0.0", string(data))
	})

	t.Run("mixes files and URLs", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		specs := resolveSpecs([]string{spec1, srv.URL + "/stripe.yml"})
		assert.Len(t, specs, 2)
	})

	t.Run("skips failed URL downloads", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		specs := resolveSpecs([]string{srv.URL + "/missing.yml"})
		assert.Empty(t, specs)
	})

	t.Run("resolves static directory into specs", func(t *testing.T) {
		rootDir := t.TempDir()
		svcDir := filepath.Join(rootDir, "static", "myapi", "users", "get")
		require.NoError(t, os.MkdirAll(svcDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(svcDir, "index.json"), []byte(`{"id":1,"name":"John"}`), 0o644))

		specs := resolveSpecs([]string{rootDir})
		require.Len(t, specs, 1)
		assert.Contains(t, specs[0], "myapi.yml")

		data, err := os.ReadFile(specs[0])
		require.NoError(t, err)
		assert.Contains(t, string(data), "openapi")
	})

	t.Run("mixes spec files and static dir", func(t *testing.T) {
		rootDir := t.TempDir()

		// Add a regular spec
		require.NoError(t, os.WriteFile(filepath.Join(rootDir, "petstore.yml"), []byte("openapi: 3.0.0\ninfo:\n  title: test\n  version: '1'\npaths: {}"), 0o644))

		// Add a static service
		svcDir := filepath.Join(rootDir, "static", "myapi", "users", "get")
		require.NoError(t, os.MkdirAll(svcDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(svcDir, "index.json"), []byte(`{"id":1}`), 0o644))

		specs := resolveSpecs([]string{rootDir})
		assert.Len(t, specs, 2)
	})
}

func TestHasStaticDir(t *testing.T) {
	t.Run("returns true for dir with static subdir containing service dirs", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "static", "myapi"), 0o755))
		assert.True(t, hasStaticDir(dir))
	})

	t.Run("returns false for dir without static subdir", func(t *testing.T) {
		assert.False(t, hasStaticDir(t.TempDir()))
	})

	t.Run("returns false for nonexistent dir", func(t *testing.T) {
		assert.False(t, hasStaticDir("/nonexistent"))
	})

	t.Run("returns false for static dir with only files", func(t *testing.T) {
		dir := t.TempDir()
		staticDir := filepath.Join(dir, "static")
		require.NoError(t, os.MkdirAll(staticDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(staticDir, "readme.txt"), []byte("hi"), 0o644))
		assert.False(t, hasStaticDir(dir))
	})
}

func TestResolveStaticSpecs(t *testing.T) {
	t.Run("returns nil for dir without static subdir", func(t *testing.T) {
		assert.Nil(t, resolveStaticSpecs(t.TempDir()))
	})

	t.Run("skips non-directory entries in static dir", func(t *testing.T) {
		dir := t.TempDir()
		staticDir := filepath.Join(dir, "static")
		require.NoError(t, os.MkdirAll(staticDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(staticDir, "readme.txt"), []byte("hi"), 0o644))
		specs := resolveStaticSpecs(dir)
		assert.Empty(t, specs)
	})

	t.Run("skips service dirs with no static files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "static", "empty-svc"), 0o755))
		specs := resolveStaticSpecs(dir)
		assert.Empty(t, specs)
	})
}

func TestParseFlags(t *testing.T) {
	t.Run("parses all flags", func(t *testing.T) {
		fl, positional := parseFlags([]string{
			"petstore.yml",
			"--port", "3000",
			"--config", "config.yml",
			"--context", "ctx.yml",
		})
		assert.Equal(t, 3000, fl.port)
		assert.Equal(t, "config.yml", fl.config)
		assert.Equal(t, "ctx.yml", fl.context)
		assert.Equal(t, []string{"petstore.yml"}, positional)
	})

	t.Run("handles no flags", func(t *testing.T) {
		fl, positional := parseFlags([]string{"spec1.yml", "spec2.yml"})

		// -1 is the sentinel for "user didn't pass --port"; 0 is reserved
		// for "let the kernel pick" (standard Unix idiom).
		assert.Equal(t, -1, fl.port)
		assert.Equal(t, "", fl.config)
		assert.Equal(t, []string{"spec1.yml", "spec2.yml"}, positional)
	})

	t.Run("handles mixed order", func(t *testing.T) {
		fl, positional := parseFlags([]string{
			"--port", "8080",
			"petstore.yml",
			"stripe.yml",
		})
		assert.Equal(t, 8080, fl.port)
		assert.Equal(t, []string{"petstore.yml", "stripe.yml"}, positional)
	})
}
