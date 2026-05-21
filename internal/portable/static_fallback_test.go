package portable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeOpenAPISpec(t *testing.T) {
	// Stripe-shaped: lots of `components:` noise before the `openapi:` line.
	deep := append(bytes.Repeat([]byte("# noise\n"), 8192), []byte("openapi: 3.0.0")...)

	cases := []struct {
		name string
		data []byte
		want bool
	}{
		{"yaml openapi 3", []byte("openapi: 3.0.0\ninfo:\n  title: x"), true},
		{"json openapi 3", []byte(`{"openapi":"3.0.0","info":{"title":"x"}}`), true},
		{"mixed case yaml key", []byte("OpenAPI: 3.0.0"), true},
		{"uppercase json key", []byte(`{"OPENAPI":"3.0.0"}`), true},
		{"openapi key after another field", []byte(`{"info":{"title":"x"},"openapi":"3.0.0"}`), true},
		{"leading whitespace and newlines", []byte("\n\n  openapi: 3.0.0"), true},
		{"yaml single-quoted version", []byte(`openapi: '3.0.0'`), true},
		{"no whitespace around colon", []byte(`openapi:3.0`), true},
		{"marker deep in the document", deep, true},

		{"non-spec json body", []byte(`{"hello":"world","items":[1,2,3]}`), false},
		{"html document", []byte(`<!DOCTYPE html><html></html>`), false},
		{"plain text body", []byte(`plain text body`), false},
		{"nil input", nil, false},
		{"empty input", []byte{}, false},
		{"openapi inside a string value", []byte(`{"description":"my openapi-ish API"}`), false},
		{"openapi key with bool value", []byte(`{"openapi":true}`), false},
		{"openapi key with non-version string", []byte(`{"openapi":"foo"}`), false},
		{"openapi key with numeric value", []byte(`{"openapi":42}`), false},
		{"openapi key with unquoted yaml word", []byte("openapi: maybe"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, looksLikeOpenAPISpec(c.data))
		})
	}
}

func TestStaticContentExt(t *testing.T) {
	cases := []struct {
		name, pathHint, contentType, want string
	}{
		{"path .json wins over content-type", "data.json", "text/html", ".json"},
		{"path .html wins over content-type", "page.html", "application/json", ".html"},
		{"path .htm preserved as-is", "page.htm", "", ".htm"},
		{"path .yaml preserved as-is", "spec.yaml", "", ".yaml"},
		{"path .yml preserved as-is", "spec.yml", "", ".yml"},
		{"path .xml preserved as-is", "doc.xml", "", ".xml"},
		{"path .txt preserved as-is", "note.txt", "", ".txt"},

		{"no path ext, content-type json", "/foo/bar", "application/json", ".json"},
		{"unknown path ext, content-type application/xml", "data.unknown", "application/xml", ".xml"},
		{"unknown path ext, content-type text/xml", "data.unknown", "text/xml", ".xml"},
		{"unknown path ext, content-type text/html", "data.unknown", "text/html", ".html"},
		{"unknown path ext, content-type text/plain", "data.unknown", "text/plain", ".txt"},
		{"unknown path ext, content-type application/yaml", "data.unknown", "application/yaml", ".yml"},
		{"unknown path ext, content-type text/yaml", "data.unknown", "text/yaml", ".yml"},

		{"content-type with charset param", "/api/v3", "application/json; charset=utf-8", ".json"},
		{"content-type uppercase", "/api/v3", "APPLICATION/JSON", ".json"},
		{"content-type surrounding whitespace", "/api/v3", "  application/json  ", ".json"},

		{"unknown ext and unknown content-type defaults to .json", "data.bin", "application/octet-stream", ".json"},
		{"empty path and empty content-type defaults to .json", "", "", ".json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, staticContentExt(c.pathHint, c.contentType))
		})
	}
}

func TestIsStaticContentFile(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"json", "data.json", true},
		{"html", "page.html", true},
		{"htm", "page.htm", true},
		{"txt", "note.txt", true},
		{"xml", "doc.xml", true},
		{"yaml", "spec.yaml", true},
		{"yml", "spec.yml", true},
		{"with path prefix", "/a/b/c/data.json", true},

		{"go source file", "main.go", false},
		{"binary image", "img.png", false},
		{"no extension", "data", false},
		{"empty", "", false},
		{"uppercase ext is case-sensitive", "data.JSON", false},
		{"compound .json.bak picks .bak", "data.json.bak", false},
		{"compound .tar.gz picks .gz", "data.tar.gz", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isStaticContentFile(c.in))
		})
	}
}

func TestResolveStaticFileDirect(t *testing.T) {
	t.Run("preserves path extension and writes body verbatim", func(t *testing.T) {
		svcs, err := resolveStaticFile("any/path/data.json", []byte(`{"k":"v"}`), "")
		require.NoError(t, err)
		require.Len(t, svcs, 1)

		s := svcs[0]
		assert.Empty(t, s.Name, "static fallback mounts at root")
		assert.NotEmpty(t, s.StaticDir, "synthesized static dir tracked on Service")
		assert.NotEmpty(t, s.SpecPath, "synthesized spec is materialised on disk")

		body, err := os.ReadFile(filepath.Join(s.StaticDir, "index.json"))
		require.NoError(t, err)
		assert.Equal(t, `{"k":"v"}`, string(body))
	})

	t.Run("uses content-type when path ext is unknown", func(t *testing.T) {
		svcs, err := resolveStaticFile(
			"/api/v3/foo",
			[]byte(`<h1>hi</h1>`),
			"text/html; charset=utf-8",
		)
		require.NoError(t, err)
		require.Len(t, svcs, 1)

		body, err := os.ReadFile(filepath.Join(svcs[0].StaticDir, "index.html"))
		require.NoError(t, err)
		assert.Equal(t, `<h1>hi</h1>`, string(body))
	})

	t.Run("path ext beats content-type", func(t *testing.T) {
		svcs, err := resolveStaticFile(
			"response.xml",
			[]byte(`<root/>`),
			"application/json",
		)
		require.NoError(t, err)
		require.Len(t, svcs, 1)

		body, err := os.ReadFile(filepath.Join(svcs[0].StaticDir, "index.xml"))
		require.NoError(t, err)
		assert.Equal(t, `<root/>`, string(body))
	})

	t.Run("defaults to .json when no signal", func(t *testing.T) {
		svcs, err := resolveStaticFile("", []byte(`{"k":"v"}`), "")
		require.NoError(t, err)
		require.Len(t, svcs, 1)
		assert.FileExists(t, filepath.Join(svcs[0].StaticDir, "index.json"))
	})

	t.Run("each call gets its own temp dir", func(t *testing.T) {
		// Concurrent invocations (or just repeated calls) must not
		// share state; they each synthesize their own spec into a
		// fresh dir.
		s1, err := resolveStaticFile("a.json", []byte(`{"a":1}`), "")
		require.NoError(t, err)
		s2, err := resolveStaticFile("a.json", []byte(`{"a":2}`), "")
		require.NoError(t, err)

		require.Len(t, s1, 1)
		require.Len(t, s2, 1)
		assert.NotEqual(t, s1[0].StaticDir, s2[0].StaticDir)

		b1, err := os.ReadFile(filepath.Join(s1[0].StaticDir, "index.json"))
		require.NoError(t, err)
		b2, err := os.ReadFile(filepath.Join(s2[0].StaticDir, "index.json"))
		require.NoError(t, err)
		assert.Equal(t, `{"a":1}`, string(b1))
		assert.Equal(t, `{"a":2}`, string(b2))
	})

	t.Run("empty body still produces a service", func(t *testing.T) {
		svcs, err := resolveStaticFile("blank.txt", []byte{}, "")
		require.NoError(t, err)
		require.Len(t, svcs, 1)

		body, err := os.ReadFile(filepath.Join(svcs[0].StaticDir, "index.txt"))
		require.NoError(t, err)
		assert.Empty(t, body)
	})
}
