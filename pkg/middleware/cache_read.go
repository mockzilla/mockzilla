package middleware

import (
	"net/http"
)

// CreateCacheReadMiddleware returns a middleware that checks if GET request is cached in History.
func CreateCacheReadMiddleware(params *Params) func(http.Handler) http.Handler {
	log := params.Logger("cache-read")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			cfg := params.GetServiceConfig(req)
			if cfg == nil || cfg.Cache == nil {
				next.ServeHTTP(w, req)
				return
			}

			// The cache is the history table, so recording off means nothing can
			// populate it and the lookup is a guaranteed miss on the request path.
			if req.Method != http.MethodGet || !cfg.Cache.Requests || !cfg.HistoryEnabled() {
				next.ServeHTTP(w, req)
				return
			}

			res, exists := params.DB().History().Get(req.Context(), req)
			if !exists {
				next.ServeHTTP(w, req)
				return
			}

			// A record the storage backend clipped to fit its body cap is still
			// good enough for the history UI, which badges it, but replaying one
			// as a response hands the client truncated JSON under a 200. Serve
			// the miss and regenerate instead.
			if res.Response != nil && res.Response.IsBodyTruncated {
				RequestLog(log, req).Debug("Cache hit ignored: stored body was truncated",
					"path", req.URL.Path)
				next.ServeHTTP(w, req)
				return
			}

			RequestLog(log, req).Info("Cache hit", "path", req.URL.Path)

			response := res.Response
			SetRequestIDHeader(w, req)
			SetDurationHeader(w, req)
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceCache)
			if response.ContentType != "" {
				w.Header().Set("Content-Type", response.ContentType)
			}
			w.WriteHeader(response.StatusCode)
			_, _ = w.Write(response.Body)
		})
	}
}
