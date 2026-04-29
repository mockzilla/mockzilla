//go:build !integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/files"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestBufferedWriter(t *testing.T) {
	t.Run("NewBufferedResponseWriter creates writer with buffer", func(t *testing.T) {
		bw := NewBufferedResponseWriter()
		assert2.NotNil(t, bw)
		assert2.Empty(t, bw.buf)
	})

	t.Run("Write appends to buffer", func(t *testing.T) {
		bw := NewBufferedResponseWriter()
		n, err := bw.Write([]byte("hello"))
		assert2.NoError(t, err)
		assert2.Equal(t, 5, n)
		assert2.Equal(t, []byte("hello"), bw.buf)

		n, err = bw.Write([]byte(" world"))
		assert2.NoError(t, err)
		assert2.Equal(t, 6, n)
		assert2.Equal(t, []byte("hello world"), bw.buf)
	})

	t.Run("Header returns empty header", func(t *testing.T) {
		bw := NewBufferedResponseWriter()
		h := bw.Header()
		assert2.NotNil(t, h)
		assert2.Empty(t, h)
	})

	t.Run("WriteHeader sets status code", func(t *testing.T) {
		bw := NewBufferedResponseWriter()
		assert2.Equal(t, 0, bw.statusCode)

		bw.WriteHeader(http.StatusCreated)
		assert2.Equal(t, http.StatusCreated, bw.statusCode)

		bw.WriteHeader(http.StatusNotFound)
		assert2.Equal(t, http.StatusNotFound, bw.statusCode)
	})
}

func TestCreateHomeRoutes(t *testing.T) {
	assert := assert2.New(t)

	cfg := config.NewDefaultAppConfig(t.TempDir())

	// Copy UI files before creating routes so filesystem check passes
	err := files.CopyDirectory(filepath.Join("..", "..", "resources", "ui"), cfg.Paths.UI)
	assert.Nil(err)

	router := NewRouter(WithConfigOption(cfg))
	err = CreateHomeRoutes(router)
	assert.Nil(err)

	t.Run("home", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusOK, w.Code)
		assert.Equal("text/html; charset=utf-8", w.Header().Get("Content-Type"))
		// template parsed
		assert.Contains(w.Body.String(), `serviceUrl: "\/.services"`)
	})

	t.Run("static", func(t *testing.T) {
		err = os.WriteFile(filepath.Join(router.Config().Paths.UI, "test.yml"), []byte("app:"), 0644)
		assert.Nil(err)
		req := httptest.NewRequest("GET", "/test.yml", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusOK, w.Code)
		assert.Equal("app:", w.Body.String())
	})

	t.Run("docs", func(t *testing.T) {
		// site is the source dir for the docs
		siteDir := filepath.Join(router.Config().Paths.Base, "site")
		_ = os.Mkdir(siteDir, 0755)
		_ = os.WriteFile(filepath.Join(siteDir, "hi.md"), []byte("Hallo!"), 0644)

		req := httptest.NewRequest("GET", "/docs/hi.md", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusOK, w.Code)
		assert.Equal("Hallo!", w.Body.String())
	})
}

func TestCreateHomeRoutes_CustomURL(t *testing.T) {
	assert := assert2.New(t)

	cfg := config.NewDefaultAppConfig(t.TempDir())
	cfg.HomeURL = "/.ui"

	err := files.CopyDirectory(filepath.Join("..", "..", "resources", "ui"), cfg.Paths.UI)
	assert.Nil(err)

	router := NewRouter(WithConfigOption(cfg))
	err = CreateHomeRoutes(router)
	assert.Nil(err)

	t.Run("home", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/.ui/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusOK, w.Code)
		assert.Equal("text/html; charset=utf-8", w.Header().Get("Content-Type"))
		assert.Contains(w.Body.String(), `serviceUrl: "\/.services"`)
	})

	t.Run("redirect without trailing slash", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/.ui", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusMovedPermanently, w.Code)
	})
}

func TestCreateHomeRoutes_EmbeddedFallback(t *testing.T) {
	assert := assert2.New(t)

	// Use empty temp dir - no UI files, should fall back to embedded
	cfg := config.NewDefaultAppConfig(t.TempDir())
	router := NewRouter(WithConfigOption(cfg))

	err := CreateHomeRoutes(router)
	assert.Nil(err)

	t.Run("home uses embedded", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusOK, w.Code)
		assert.Equal("text/html; charset=utf-8", w.Header().Get("Content-Type"))

		// template parsed from embedded resources
		assert.Contains(w.Body.String(), `serviceUrl: "\/.services"`)
	})

	t.Run("static files from embedded", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/css/console.css", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(http.StatusOK, w.Code)
		assert.Equal("text/css; charset=utf-8", w.Header().Get("Content-Type"))
	})
}
