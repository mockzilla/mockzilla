package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/connexions/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestCreateResourceResolverMiddleware(t *testing.T) {
	assert := assert2.New(t)

	t.Run("resolves parameterized route pattern", func(t *testing.T) {
		r := chi.NewRouter()
		r.Get("/pets/{id}", func(w http.ResponseWriter, r *http.Request) {})

		params := newTestParams(&config.ServiceConfig{Name: "petstore"})
		params.SetRouter(r)

		var captured string
		mw := CreateResourceResolverMiddleware(params)
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			captured = GetResourcePath(req)
		})

		req := httptest.NewRequest(http.MethodGet, "/petstore/pets/42", nil)
		mw(handler).ServeHTTP(httptest.NewRecorder(), req)

		assert.Equal("/pets/{id}", captured)
	})

	t.Run("resolves exact route", func(t *testing.T) {
		r := chi.NewRouter()
		r.Get("/pets", func(w http.ResponseWriter, r *http.Request) {})

		params := newTestParams(&config.ServiceConfig{Name: "petstore"})
		params.SetRouter(r)

		var captured string
		mw := CreateResourceResolverMiddleware(params)
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			captured = GetResourcePath(req)
		})

		req := httptest.NewRequest(http.MethodGet, "/petstore/pets", nil)
		mw(handler).ServeHTTP(httptest.NewRecorder(), req)

		assert.Equal("/pets", captured)
	})

	t.Run("falls back to endpoint path when no router set", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{Name: "petstore"})

		var captured string
		mw := CreateResourceResolverMiddleware(params)
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			captured = GetResourcePath(req)
		})

		req := httptest.NewRequest(http.MethodGet, "/petstore/pets/42", nil)
		mw(handler).ServeHTTP(httptest.NewRecorder(), req)

		assert.Equal("/pets/42", captured)
	})

	t.Run("falls back to endpoint path when route does not match", func(t *testing.T) {
		r := chi.NewRouter()
		r.Get("/other", func(w http.ResponseWriter, r *http.Request) {})

		params := newTestParams(&config.ServiceConfig{Name: "petstore"})
		params.SetRouter(r)

		var captured string
		mw := CreateResourceResolverMiddleware(params)
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			captured = GetResourcePath(req)
		})

		req := httptest.NewRequest(http.MethodGet, "/petstore/pets/42", nil)
		mw(handler).ServeHTTP(httptest.NewRecorder(), req)

		assert.Equal("/pets/42", captured)
	})
}

func TestGetResourcePath(t *testing.T) {
	assert := assert2.New(t)

	t.Run("returns URL path when context not set", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
		assert.Equal("/foo/bar", GetResourcePath(req))
	})
}
