package portable

import (
	"archive/tar"
	"compress/gzip"
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

func TestIsPackageFile(t *testing.T) {
	assert.True(t, isPackageFile("petstore.mock"))
	assert.True(t, isPackageFile("specs.tar.gz"))
	assert.True(t, isPackageFile("/path/to/my-api.mock"))
	assert.False(t, isPackageFile("petstore.yaml"))
	assert.False(t, isPackageFile("petstore.gz"))
	assert.False(t, isPackageFile("petstore.tar"))
	assert.False(t, isPackageFile("petstore"))
}

// buildTestPackage creates a .mock (tar.gz) archive in dir with the given files.
func buildTestPackage(t *testing.T, dir string, name string, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
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

func TestExtractPackage(t *testing.T) {
	t.Run("extracts spec and config files", func(t *testing.T) {
		pkg := buildTestPackage(t, t.TempDir(), "test.mock", map[string][]byte{
			"openapi/petstore.yml": []byte("openapi: 3.0.0"),
			"app.yml":              []byte("app:\n  port: 3000"),
			"context.yml":          []byte("petstore:\n  key: val"),
		})

		dir, err := extractPackage(pkg)
		require.NoError(t, err)

		data, err := os.ReadFile(filepath.Join(dir, "openapi", "petstore.yml"))
		require.NoError(t, err)
		assert.Equal(t, "openapi: 3.0.0", string(data))

		assert.True(t, fileExists(filepath.Join(dir, "app.yml")))
		assert.True(t, fileExists(filepath.Join(dir, "context.yml")))
	})

	t.Run("works with tar.gz extension", func(t *testing.T) {
		pkg := buildTestPackage(t, t.TempDir(), "specs.tar.gz", map[string][]byte{
			"openapi/api.yaml": []byte("openapi: 3.1.0"),
		})

		dir, err := extractPackage(pkg)
		require.NoError(t, err)

		assert.True(t, fileExists(filepath.Join(dir, "openapi", "api.yaml")))
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := extractPackage("/nonexistent/test.mock")
		assert.Error(t, err)
	})

	t.Run("returns error for invalid gzip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.mock")
		require.NoError(t, os.WriteFile(path, []byte("not gzip data"), 0o644))

		_, err := extractPackage(path)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "gzip")
	})

	t.Run("returns error for corrupt tar inside valid gzip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "corrupt.mock")
		f, err := os.Create(path)
		require.NoError(t, err)

		gw := gzip.NewWriter(f)
		_, _ = gw.Write([]byte("this is not tar data"))
		require.NoError(t, gw.Close())
		require.NoError(t, f.Close())

		_, err = extractPackage(path)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reading tar")
	})

	t.Run("skips path traversal entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "traversal.mock")
		f, err := os.Create(path)
		require.NoError(t, err)

		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)
		// Safe entry
		safeData := []byte("openapi: 3.0.0")
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: "safe.yml",
			Mode: 0o644,
			Size: int64(len(safeData)),
		}))
		_, err = tw.Write(safeData)
		require.NoError(t, err)
		// Path traversal entry -- should be skipped
		evilData := []byte("evil")
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: "../../../etc/evil.txt",
			Mode: 0o644,
			Size: int64(len(evilData)),
		}))
		_, err = tw.Write(evilData)
		require.NoError(t, err)
		require.NoError(t, tw.Close())
		require.NoError(t, gw.Close())
		require.NoError(t, f.Close())

		extracted, err := extractPackage(path)
		require.NoError(t, err)
		assert.True(t, fileExists(filepath.Join(extracted, "safe.yml")))
		assert.False(t, fileExists("/etc/evil.txt"))
	})

	t.Run("handles tar with directory entries", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "dirs.mock")
		f, err := os.Create(path)
		require.NoError(t, err)

		gw := gzip.NewWriter(f)
		tw := tar.NewWriter(gw)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     "openapi/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		}))
		data := []byte("openapi: 3.0.0")
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: "openapi/spec.yml",
			Mode: 0o644,
			Size: int64(len(data)),
		}))
		_, err = tw.Write(data)
		require.NoError(t, err)
		require.NoError(t, tw.Close())
		require.NoError(t, gw.Close())
		require.NoError(t, f.Close())

		extracted, err := extractPackage(path)
		require.NoError(t, err)
		assert.True(t, fileExists(filepath.Join(extracted, "openapi", "spec.yml")))
	})
}

func TestResolvePackageArgs(t *testing.T) {
	t.Run("extracts package and sets config/context flags", func(t *testing.T) {
		pkg := buildTestPackage(t, t.TempDir(), "test.mock", map[string][]byte{
			"openapi/petstore.yml": []byte("openapi: 3.0.0"),
			"app.yml":              []byte("app:\n  port: 3000"),
			"context.yml":          []byte("petstore:\n  key: val"),
		})

		fl := flags{}
		result := resolvePackageArgs([]string{pkg}, &fl)

		// Returns [openapi/ dir, parent dir]
		require.Len(t, result, 2)
		assert.True(t, fileExists(filepath.Join(result[0], "petstore.yml")))
		assert.NotEmpty(t, fl.config)
		assert.NotEmpty(t, fl.context)
	})

	t.Run("does not override existing config flag", func(t *testing.T) {
		pkg := buildTestPackage(t, t.TempDir(), "test.mock", map[string][]byte{
			"openapi/petstore.yml": []byte("openapi: 3.0.0"),
			"app.yml":              []byte("app:\n  port: 3000"),
		})

		fl := flags{config: "/my/config.yml"}
		resolvePackageArgs([]string{pkg}, &fl)

		assert.Equal(t, "/my/config.yml", fl.config)
	})

	t.Run("passes through non-package args unchanged", func(t *testing.T) {
		fl := flags{}
		args := []string{"petstore.yml", "/some/dir"}
		result := resolvePackageArgs(args, &fl)

		assert.Equal(t, args, result)
		assert.Empty(t, fl.config)
	})

	t.Run("downloads and extracts URL package", func(t *testing.T) {
		pkgPath := buildTestPackage(t, t.TempDir(), "test.mock", map[string][]byte{
			"openapi/api.yml": []byte("openapi: 3.0.0"),
		})
		pkgData, err := os.ReadFile(pkgPath)
		require.NoError(t, err)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(pkgData)
		}))
		defer srv.Close()

		fl := flags{}
		result := resolvePackageArgs([]string{srv.URL + "/api.mock"}, &fl)

		require.Len(t, result, 2)
		assert.True(t, fileExists(filepath.Join(result[0], "api.yml")))
	})

	t.Run("skips URL without package extension", func(t *testing.T) {
		fl := flags{}
		args := []string{"https://example.com/petstore.yml"}
		result := resolvePackageArgs(args, &fl)
		assert.Equal(t, args, result)
	})

	t.Run("skips URL that fails to download", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		fl := flags{}
		result := resolvePackageArgs([]string{srv.URL + "/fail.mock"}, &fl)
		assert.Equal(t, []string{srv.URL + "/fail.mock"}, result)
	})

	t.Run("skips invalid local package", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.mock")
		require.NoError(t, os.WriteFile(bad, []byte("not a tarball"), 0o644))

		fl := flags{}
		result := resolvePackageArgs([]string{bad}, &fl)
		assert.Equal(t, []string{bad}, result)
	})

	t.Run("package without openapi dir uses root only", func(t *testing.T) {
		pkg := buildTestPackage(t, t.TempDir(), "flat.mock", map[string][]byte{
			"petstore.yml": []byte("openapi: 3.0.0"),
		})

		fl := flags{}
		result := resolvePackageArgs([]string{pkg}, &fl)

		// No openapi/ subdir, so only the parent dir is returned
		require.Len(t, result, 1)
	})

	t.Run("does not override existing context flag", func(t *testing.T) {
		pkg := buildTestPackage(t, t.TempDir(), "test.mock", map[string][]byte{
			"openapi/petstore.yml": []byte("openapi: 3.0.0"),
			"context.yml":          []byte("petstore:\n  key: val"),
		})

		fl := flags{context: "/my/context.yml"}
		resolvePackageArgs([]string{pkg}, &fl)

		assert.Equal(t, "/my/context.yml", fl.context)
	})
}

func TestDownloadFile(t *testing.T) {
	t.Run("downloads file successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("file content"))
		}))
		defer srv.Close()

		path, err := downloadFile(srv.URL + "/test.mock")
		require.NoError(t, err)
		assert.Contains(t, path, "test.mock")

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "file content", string(data))
	})

	t.Run("returns error on HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := downloadFile(srv.URL + "/missing.mock")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("returns error on connection failure", func(t *testing.T) {
		_, err := downloadFile("http://127.0.0.1:1/test.mock")
		assert.Error(t, err)
	})

	t.Run("uses fallback name for root URL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("data"))
		}))
		defer srv.Close()

		path, err := downloadFile(srv.URL + "/")
		require.NoError(t, err)
		assert.Contains(t, path, "package.mock")
	})
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

	t.Run("detects .mock file arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"petstore.mock"}))
	})

	t.Run("detects .tar.gz file arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"specs.tar.gz"}))
	})

	t.Run("detects .mock URL arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"https://example.com/petstore.mock"}))
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
