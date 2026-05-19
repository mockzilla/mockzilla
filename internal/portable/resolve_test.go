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

func TestIsSpecFile(t *testing.T) {
	assert.True(t, isSpecFile("petstore.yaml"))
	assert.True(t, isSpecFile("petstore.yml"))
	assert.True(t, isSpecFile("petstore.json"))
	assert.False(t, isSpecFile("petstore.go"))
	assert.False(t, isSpecFile("petstore.txt"))
	assert.False(t, isSpecFile("petstore"))
}

func TestIsPackageFile(t *testing.T) {
	assert.True(t, isPackageFile("petstore.mockz"))
	assert.True(t, isPackageFile("specs.tar.gz"))
	assert.True(t, isPackageFile("/path/to/my-api.mockz"))
	assert.False(t, isPackageFile("petstore.yaml"))
	assert.False(t, isPackageFile("petstore.gz"))
	assert.False(t, isPackageFile("petstore.tar"))
	assert.False(t, isPackageFile("petstore"))
}

func TestFindSpecInDir(t *testing.T) {
	t.Run("returns empty when no spec", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte("x: 1"), 0o644))
		assert.Empty(t, findSpecInDir(dir))
	})

	t.Run("picks canonical openapi.yml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"), []byte("openapi: 3.0.0"), 0o644))
		assert.Equal(t, filepath.Join(dir, "openapi.yml"), findSpecInDir(dir))
	})

	t.Run("picks single non-canonical spec", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.2026.yaml"), []byte("openapi: 3.0.0"), 0o644))
		assert.Equal(t, filepath.Join(dir, "stripe.2026.yaml"), findSpecInDir(dir))
	})

	t.Run("skips config/context/app files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte("x:1"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "context.yml"), []byte("y:2"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yml"), []byte("z:3"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yml"), []byte("openapi: 3.0.0"), 0o644))

		assert.Equal(t, filepath.Join(dir, "stripe.yml"), findSpecInDir(dir))
	})

	t.Run("picks alphabetically first of multiple candidates", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "z-other.yml"), []byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a-first.yml"), []byte("openapi: 3.0.0"), 0o644))

		assert.Equal(t, filepath.Join(dir, "a-first.yml"), findSpecInDir(dir))
	})

	t.Run("ignores subdirectories", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "static"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "static", "x.json"), []byte("{}"), 0o644))
		assert.Empty(t, findSpecInDir(dir))
	})
}

func TestResolveOne_File(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(spec, []byte("openapi: 3.0.0"), 0o644))

	services, err := resolveOne(spec)
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "petstore", services[0].Name)
	assert.Equal(t, spec, services[0].SpecPath)
	assert.Empty(t, services[0].ConfigDir)
}

func TestResolveOne_URL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("openapi: 3.0.0"))
	}))
	defer srv.Close()

	t.Run("specific filename wins", func(t *testing.T) {
		services, err := resolveOne(srv.URL + "/petstore.yml")
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Equal(t, "petstore", services[0].Name)
	})

	t.Run("generic basename falls back to host", func(t *testing.T) {
		// e.g. https://localhost:PORT/openapi.json → name from host.
		services, err := resolveOne(srv.URL + "/openapi.json")
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.NotEqual(t, "openapi", services[0].Name)
		assert.Contains(t, services[0].Name, "127.0.0.1")
	})
}

func TestResolveDir_SingleService(t *testing.T) {
	t.Run("dir with only canonical openapi.yml gets empty name (mounts at root)", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "pets")
		require.NoError(t, os.Mkdir(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"), []byte("openapi: 3.0.0"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 1)
		// `openapi.yml` is a generic filename and no config.yml is
		// present, so no inside-the-folder signal supplies a name.
		// The folder's own basename is intentionally NOT used.
		assert.Empty(t, services[0].Name)
		assert.Equal(t, dir, services[0].ConfigDir)
	})

	t.Run("dir with non-generic spec name uses spec basename", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "anything")
		require.NoError(t, os.Mkdir(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.2026.yaml"), []byte("openapi: 3.0.0"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Equal(t, "stripe.2026", services[0].Name)
	})

	t.Run("dir with config.yml `name:` field wins over spec basename", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "anything")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yaml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
			[]byte("name: payments\n"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Equal(t, "payments", services[0].Name)
	})

	t.Run("dir with only flat static gets empty name (mounts at root)", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "anything")
		usersGet := filepath.Join(dir, "users", "get")
		require.NoError(t, os.MkdirAll(usersGet, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(usersGet, "index.json"), []byte(`{"id":1}`), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Empty(t, services[0].Name)
		assert.NotEmpty(t, services[0].SpecPath)
		assert.Equal(t, dir, services[0].StaticDir)
	})

	t.Run("merge mode (generic spec + static) gets empty name", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "anything")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"),
			[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}"), 0o644))
		getDir := filepath.Join(dir, "v1", "get")
		require.NoError(t, os.MkdirAll(getDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(getDir, "index.json"),
			[]byte(`{"ok":true}`), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Empty(t, services[0].Name)
		assert.NotContains(t, services[0].SpecPath, dir)
		assert.Contains(t, services[0].SpecPath, "mockzilla-portable")
		assert.Equal(t, dir, services[0].StaticDir)
	})

	t.Run("dir with neither spec nor static returns error", func(t *testing.T) {
		_, err := resolveDir(t.TempDir())
		assert.Error(t, err)
	})

	t.Run("`.` and `./` don't leak cwd basename into service name", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "should-not-leak")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"),
			[]byte("openapi: 3.0.0"), 0o644))

		origWd, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(origWd) })
		require.NoError(t, os.Chdir(dir))

		for _, arg := range []string{".", "./"} {
			services, err := resolveDir(arg)
			require.NoError(t, err)
			require.Len(t, services, 1)
			assert.Empty(t, services[0].Name, "arg=%q should not produce a name from cwd basename", arg)
		}
	})
}

func TestResolveDir_FlatRoot(t *testing.T) {
	t.Run("multiple specs at root become multiple services", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "petstore.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yaml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "spoonacular.json"),
			[]byte("{\"openapi\": \"3.0.0\"}"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 3)

		names := make(map[string]bool)
		for _, s := range services {
			names[s.Name] = true
			assert.Equal(t, dir, s.ConfigDir, "flat-root services share the dir for shared context.yml")
		}
		assert.True(t, names["petstore"])
		assert.True(t, names["stripe"])
		assert.True(t, names["spoonacular"])
	})

	t.Run("shared context.yml at root applies to every flat-root service", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "context.yml"),
			[]byte("name: [\"Alice\", \"Bob\"]\n"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 2)

		for _, s := range services {
			ctxBytes, err := loadServiceContext(s)
			require.NoError(t, err)
			require.NotNil(t, ctxBytes, "service %q should pick up the shared context.yml", s.Name)
			assert.Contains(t, string(ctxBytes), "Alice")
		}
	})

	t.Run("app.yml is not picked up as a service", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "petstore.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yml"),
			[]byte("port: 9000\n"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 2)
		for _, s := range services {
			assert.NotEqual(t, "app", s.Name)
		}
	})

	t.Run("config.yml at root demotes flat root → single-service folder", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "petstore.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
			[]byte("latency: 10ms\n"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		// One service because config.yml flips us out of flat-root.
		// Name comes from inside signals: config.yml has no `name:`,
		// so the alphabetically-first non-generic spec wins.
		require.Len(t, services, 1)
		assert.Equal(t, "petstore", services[0].Name)
	})

	t.Run("config.yml `name:` wins over spec basenames", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "petstore.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yml"),
			[]byte("openapi: 3.0.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
			[]byte("name: combined\nlatency: 10ms\n"), 0o644))

		services, err := resolveDir(dir)
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Equal(t, "combined", services[0].Name)
	})
}

func TestResolveDir_ServicesRoot(t *testing.T) {
	root := t.TempDir()
	servicesPath := filepath.Join(root, "services")
	require.NoError(t, os.Mkdir(servicesPath, 0o755))

	// petstore: canonical spec
	petsDir := filepath.Join(servicesPath, "petstore")
	require.NoError(t, os.Mkdir(petsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(petsDir, "openapi.yml"), []byte("openapi: 3.0.0"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(petsDir, "config.yml"), []byte("latency: 100ms"), 0o644))

	// spoonacular: non-canonical spec name
	spoonDir := filepath.Join(servicesPath, "spoonacular")
	require.NoError(t, os.Mkdir(spoonDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(spoonDir, "spoonacular.com.yaml"), []byte("openapi: 3.0.0"), 0o644))

	// static-only service (no wrapper, just <method>/index.<ext>)
	staticOnlyDir := filepath.Join(servicesPath, "static-only")
	usersGet := filepath.Join(staticOnlyDir, "users", "get")
	require.NoError(t, os.MkdirAll(usersGet, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(usersGet, "index.json"), []byte(`{}`), 0o644))

	services, err := resolveDir(root)
	require.NoError(t, err)
	require.Len(t, services, 3)

	byName := map[string]Service{}
	for _, s := range services {
		byName[s.Name] = s
	}
	assert.Contains(t, byName, "petstore")
	assert.Contains(t, byName, "spoonacular")
	assert.Contains(t, byName, "static-only")
	assert.Equal(t, petsDir, byName["petstore"].ConfigDir)
}

func TestIsPortableMode(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(spec, []byte("openapi: 3.0.0"), 0o644))

	t.Run("detects spec file arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{spec}))
	})
	t.Run("detects directory with spec at root", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{dir}))
	})
	t.Run("detects services subdir", func(t *testing.T) {
		root := t.TempDir()
		svc := filepath.Join(root, "services", "pets")
		require.NoError(t, os.MkdirAll(svc, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(svc, "openapi.yml"), []byte("openapi: 3.0.0"), 0o644))
		assert.True(t, IsPortableMode([]string{root}))
	})
	t.Run("detects flat static endpoints", func(t *testing.T) {
		root := t.TempDir()
		s := filepath.Join(root, "x", "get")
		require.NoError(t, os.MkdirAll(s, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(s, "index.json"), []byte("{}"), 0o644))
		assert.True(t, IsPortableMode([]string{root}))
	})
	t.Run("ignores flags", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{spec, "--port", "3000"}))
	})
	t.Run("returns false for empty args", func(t *testing.T) {
		assert.False(t, IsPortableMode(nil))
	})
	t.Run("returns false for empty dir", func(t *testing.T) {
		assert.False(t, IsPortableMode([]string{t.TempDir()}))
	})
	t.Run("detects URL spec", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"https://example.com/petstore.yml"}))
	})
	t.Run("detects .mockz arg", func(t *testing.T) {
		assert.True(t, IsPortableMode([]string{"petstore.mockz"}))
	})
	t.Run("detects single static json file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "data.json")
		require.NoError(t, os.WriteFile(f, []byte(`{"hello":"world"}`), 0o644))
		assert.True(t, IsPortableMode([]string{f}))
	})
	t.Run("detects single static html file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "page.html")
		require.NoError(t, os.WriteFile(f, []byte(`<h1>hi</h1>`), 0o644))
		assert.True(t, IsPortableMode([]string{f}))
	})
}

func TestResolveStaticFile(t *testing.T) {
	t.Run("non-spec json file falls back to GET /", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "data.json")
		require.NoError(t, os.WriteFile(f, []byte(`{"hello":"world"}`), 0o644))
		svcs, err := resolveOne(f)
		require.NoError(t, err)
		require.Len(t, svcs, 1)
		assert.Empty(t, svcs[0].Name, "static fallback should mount at root")
		assert.NotEmpty(t, svcs[0].StaticDir, "static fallback should track its synthesized dir")
		assert.FileExists(t, filepath.Join(svcs[0].StaticDir, "index.json"))
	})
	t.Run("spec json file still routes to spec mode", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "petstore.json")
		require.NoError(t, os.WriteFile(f, []byte(`{"openapi":"3.0.0","info":{"title":"x","version":"1"},"paths":{}}`), 0o644))
		svcs, err := resolveOne(f)
		require.NoError(t, err)
		require.Len(t, svcs, 1)
		assert.Equal(t, "petstore", svcs[0].Name)
		assert.Equal(t, f, svcs[0].SpecPath, "real spec should be used as-is, not materialised")
		assert.Empty(t, svcs[0].StaticDir)
	})
	t.Run("html file is served as static at GET /", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "page.html")
		require.NoError(t, os.WriteFile(f, []byte(`<h1>hi</h1>`), 0o644))
		svcs, err := resolveOne(f)
		require.NoError(t, err)
		require.Len(t, svcs, 1)
		assert.Empty(t, svcs[0].Name)
		assert.FileExists(t, filepath.Join(svcs[0].StaticDir, "index.html"))
	})
}

// buildPackage creates a .mockz with the supplied in-archive contents.
func buildPackage(t *testing.T, dir, name string, files map[string][]byte) string {
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
	pkg := buildPackage(t, t.TempDir(), "test.mockz", map[string][]byte{
		"services/petstore/openapi.yml": []byte("openapi: 3.0.0"),
		"services/petstore/config.yml":  []byte("latency: 50ms"),
		"app.yml":                       []byte("port: 3000"),
	})

	dir, err := extractPackage(pkg)
	require.NoError(t, err)
	assert.True(t, fileExists(filepath.Join(dir, "services", "petstore", "openapi.yml")))
	assert.True(t, fileExists(filepath.Join(dir, "services", "petstore", "config.yml")))
	assert.True(t, fileExists(filepath.Join(dir, "app.yml")))
}

func TestResolveOne_Package(t *testing.T) {
	pkg := buildPackage(t, t.TempDir(), "test.mockz", map[string][]byte{
		"services/petstore/openapi.yml": []byte("openapi: 3.0.0"),
		"services/petstore/config.yml":  []byte("latency: 50ms"),
	})

	services, err := resolveOne(pkg)
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "petstore", services[0].Name)
}

func TestResolveOne_URLPackage(t *testing.T) {
	pkgPath := buildPackage(t, t.TempDir(), "test.mockz", map[string][]byte{
		"services/petstore/openapi.yml": []byte("openapi: 3.0.0"),
	})
	pkgData, err := os.ReadFile(pkgPath)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pkgData)
	}))
	defer srv.Close()

	services, err := resolveOne(srv.URL + "/test.mockz")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "petstore", services[0].Name)
}

// Extensionless URL whose response advertises application/gzip should be
// unpacked, not naively treated as an OpenAPI spec.
func TestResolveOne_URL_PackageByContentType(t *testing.T) {
	pkgPath := buildPackage(t, t.TempDir(), "test.mockz", map[string][]byte{
		"services/petstore/openapi.yml": []byte("openapi: 3.0.0"),
	})
	pkgData, err := os.ReadFile(pkgPath)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(pkgData)
	}))
	defer srv.Close()

	services, err := resolveOne(srv.URL + "/abc12345")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "petstore", services[0].Name)
}

// Same scenario but Content-Type is the generic application/octet-stream;
// the gzip magic-byte fallback should still recognise the body as a
// package.
func TestResolveOne_URL_PackageByMagicBytes(t *testing.T) {
	pkgPath := buildPackage(t, t.TempDir(), "test.mockz", map[string][]byte{
		"services/petstore/openapi.yml": []byte("openapi: 3.0.0"),
	})
	pkgData, err := os.ReadFile(pkgPath)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(pkgData)
	}))
	defer srv.Close()

	services, err := resolveOne(srv.URL + "/abc12345")
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "petstore", services[0].Name)
}

func TestIsPackageBytes(t *testing.T) {
	gzipMagic := []byte{0x1f, 0x8b, 0x00, 0x00}
	cases := []struct {
		name string
		ct   string
		body []byte
		want bool
	}{
		{"application/gzip", "application/gzip", nil, true},
		{"application/x-gzip", "application/x-gzip", nil, true},
		{"vnd.mockz", "application/vnd.mockz", nil, true},
		{"vnd.mockz+gzip", "application/vnd.mockz+gzip", nil, true},
		{"gzip with charset", "application/gzip; charset=binary", nil, true},
		{"uppercase content type", "APPLICATION/GZIP", nil, true},
		{"octet-stream + gzip magic", "application/octet-stream", gzipMagic, true},
		{"missing CT + gzip magic", "", gzipMagic, true},
		{"yaml content", "application/yaml", []byte("openapi: 3.0.0"), false},
		{"json content", "application/json", []byte("{}"), false},
		{"text plain", "text/plain", []byte("hi"), false},
		{"octet-stream without magic", "application/octet-stream", []byte("not gzip"), false},
		{"empty body no CT", "", nil, false},
		{"single byte body", "", []byte{0x1f}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isPackageBytes(c.ct, c.body))
		})
	}
}

func TestDownloadSpec(t *testing.T) {
	t.Run("downloads spec", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		path, err := downloadSpec(srv.URL + "/petstore.yml")
		require.NoError(t, err)
		assert.Contains(t, path, "petstore.yml")
	})

	t.Run("appends .yml when no spec extension", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("openapi: 3.0.0"))
		}))
		defer srv.Close()

		path, err := downloadSpec(srv.URL + "/v2/api-docs")
		require.NoError(t, err)
		assert.True(t, isSpecFile(path))
	})

	t.Run("errors on HTTP failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := downloadSpec(srv.URL + "/spec.yml")
		assert.Error(t, err)
	})
}

func TestResolveURLByContentFallback(t *testing.T) {
	t.Run("non-spec json URL falls back to static", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hello":"world"}`))
		}))
		defer srv.Close()

		svcs, err := resolveURLByContent(srv.URL + "/data.json")
		require.NoError(t, err)
		require.Len(t, svcs, 1)
		assert.Empty(t, svcs[0].Name, "static URL fallback should mount at root")
		assert.NotEmpty(t, svcs[0].StaticDir)
		assert.FileExists(t, filepath.Join(svcs[0].StaticDir, "index.json"))
	})

	t.Run("spec URL still routes to spec mode", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte("openapi: 3.0.0\ninfo:\n  title: x\n  version: 1\npaths: {}"))
		}))
		defer srv.Close()

		svcs, err := resolveURLByContent(srv.URL + "/petstore.yml")
		require.NoError(t, err)
		require.Len(t, svcs, 1)
		assert.NotEmpty(t, svcs[0].SpecPath)
		assert.Empty(t, svcs[0].StaticDir, "real spec should not be materialised as static")
	})
}

func TestParseFlags(t *testing.T) {
	t.Run("parses port and ready-stamp", func(t *testing.T) {
		fl, pos := parseFlags([]string{"petstore.yml", "--port", "3000", "--ready-stamp"})
		assert.Equal(t, 3000, fl.port)
		assert.True(t, fl.readyStamp)
		assert.Equal(t, []string{"petstore.yml"}, pos)
	})
	t.Run("defaults port to -1", func(t *testing.T) {
		fl, _ := parseFlags([]string{"petstore.yml"})
		assert.Equal(t, -1, fl.port)
	})
	t.Run("parses convenience flags", func(t *testing.T) {
		fl, _ := parseFlags([]string{
			"petstore.yml",
			"--latency", "100ms",
			"--mount", "pets/v2",
			"--errors", "p5=500",
			"--context", "ctx.yml",
		})
		assert.Equal(t, "100ms", fl.latency)
		assert.Equal(t, "pets/v2", fl.mount)
		assert.Equal(t, "p5=500", fl.errors)
		assert.Equal(t, "ctx.yml", fl.context)
	})
}

func TestParseErrorsFlag(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		got, err := parseErrorsFlag("p5=500")
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"p5": 500}, got)
	})
	t.Run("multiple", func(t *testing.T) {
		got, err := parseErrorsFlag("p5=500,p10=503")
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"p5": 500, "p10": 503}, got)
	})
	t.Run("rejects malformed pair", func(t *testing.T) {
		_, err := parseErrorsFlag("p5")
		assert.Error(t, err)
	})
	t.Run("rejects non-int status", func(t *testing.T) {
		_, err := parseErrorsFlag("p5=oops")
		assert.Error(t, err)
	})
}

func TestBuildOverrides(t *testing.T) {
	t.Run("nil when nothing set", func(t *testing.T) {
		o, err := buildOverrides(flags{})
		require.NoError(t, err)
		assert.Nil(t, o)
	})
	t.Run("parses latency, mount, errors", func(t *testing.T) {
		o, err := buildOverrides(flags{
			latency: "150ms",
			mount:   "pets/v2",
			errors:  "p5=503",
		})
		require.NoError(t, err)
		require.NotNil(t, o)
		assert.Equal(t, "150ms", o.latency.String())
		assert.Equal(t, "pets/v2", o.mount)
		assert.Equal(t, 503, o.errors["p5"])
	})
	t.Run("reads context file", func(t *testing.T) {
		dir := t.TempDir()
		ctx := filepath.Join(dir, "ctx.yml")
		require.NoError(t, os.WriteFile(ctx, []byte("status: [active]"), 0o644))
		o, err := buildOverrides(flags{context: ctx})
		require.NoError(t, err)
		assert.Contains(t, string(o.contextBytes), "status")
	})
	t.Run("errors on bad latency", func(t *testing.T) {
		_, err := buildOverrides(flags{latency: "fast"})
		assert.Error(t, err)
	})
}
