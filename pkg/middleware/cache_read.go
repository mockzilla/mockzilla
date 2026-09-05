package middleware

import (
	"net/http"
)

// CreateCacheReadMiddleware returns a middleware that serves a GET request from
// the request cache when a previous response for the same method and URL is
// still stored.
func CreateCacheReadMiddleware(params *Params) func(http.Handler) http.Handler {
	log := params.Logger("cache-read")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !isRequestCacheEnabled(params.GetServiceConfig(req), req) {
				next.ServeHTTP(w, req)
				return
			}

			cached, ok := readCache(req.Context(), params.DB(), req)
			if !ok {
				next.ServeHTTP(w, req)
				return
			}

			RequestLog(log, req).Info("Cache hit", "path", req.URL.Path)

			SetRequestIDHeader(w, req)
			SetDurationHeader(w, req)
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceCache)
			if cached.ContentType != "" {
				w.Header().Set("Content-Type", cached.ContentType)
			}
			w.WriteHeader(cached.StatusCode)
			_, _ = w.Write(cached.Body)
		})
	}
}
