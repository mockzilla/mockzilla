package middleware

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

// CreateReplayWriteMiddleware returns middleware that records responses for replay.
// Activates when the X-Cxs-Replay header is present, or when auto-replay is enabled
// in config for the matching endpoint.
// It wraps downstream handlers, captures the response, and stores it indexed by match field values.
// Responses sourced from cache or replay are never recorded. When upstream-only is set,
// only upstream responses are recorded.
func CreateReplayWriteMiddleware(params *Params) func(http.Handler) http.Handler {
	log := params.Logger("replay-write")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			cfg := params.GetServiceConfig(req)
			if cfg == nil {
				next.ServeHTTP(w, req)
				return
			}

			match, patternPath, endpointPath := resolveReplayParams(req, cfg)
			if match == nil && patternPath == "" {
				next.ServeHTTP(w, req)
				return
			}

			body := readAndRestoreBody(req)
			key := buildReplayKey(req, patternPath, endpointPath, match, body)
			if key == "" {
				RequestLog(log, req).Info("Replay skipped: missing match fields", "method", req.Method, "path", req.URL.Path)
				next.ServeHTTP(w, req)
				return
			}

			table := params.DB().Table("replay")
			ctx := req.Context()

			// Skip if already recorded
			if _, exists := table.Get(ctx, key); exists {
				next.ServeHTTP(w, req)
				return
			}

			// Wrap response writer to capture output
			rw := &responseWriter{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rw, req)

			respContent := rw.body.Bytes()
			respStatusCode := rw.statusCode
			respContentType := rw.Header().Get("Content-Type")

			// Check source - don't record cache or replay responses
			source := rw.Header().Get(ResponseHeaderSource)
			if source == ResponseHeaderSourceCache || source == ResponseHeaderSourceReplay {
				writeThrough(w, rw)
				return
			}

			// Check upstream-only: if configured, only record upstream responses.
			// Return an error so the caller knows recording was skipped.
			isFromUpstream := source == ResponseHeaderSourceUpstream
			replayCfg := cfg.Cache != nil && cfg.Cache.Replay != nil
			if replayCfg && cfg.Cache.Replay.UpstreamOnly && !isFromUpstream {
				w.Header().Set(ResponseHeaderSource, source)
				http.Error(w, "replay: upstream-only is configured but response source is "+source, http.StatusBadGateway)
				return
			}

			ttl := replayTTL(cfg)

			// Extract match values for debugging
			matchValues := make(map[string]any)
			if match != nil {
				if len(match.Path) > 0 {
					pathValues := config.ExtractPathValues(endpointPath, patternPath)
					for _, field := range match.Path {
						matchValues["path:"+field] = pathValues[field]
					}
				}

				contentType := req.Header.Get("Content-Type")
				query := req.URL.Query()
				for _, field := range match.Body {
					matchValues["body:"+field] = extractBodyValue(body, contentType, field)
				}
				for _, field := range match.Query {
					matchValues["query:"+field] = query.Get(field)
				}
			}

			// Capture response headers
			headers := make(map[string]string)
			for k := range rw.Header() {
				// Skip our internal headers
				if k == ResponseHeaderSource || k == "X-Cxs-Duration" {
					continue
				}
				headers[k] = rw.Header().Get(k)
			}

			// Resolve the config endpoint pattern (if any)
			var resource string
			if replayCfg {
				resource, _ = cfg.Cache.Replay.GetEndpoint(endpointPath, req.Method)
			}

			rec := &ReplayRecord{
				Method:         req.Method,
				Path:           endpointPath,
				Resource:       resource,
				Data:           respContent,
				Headers:        headers,
				StatusCode:     respStatusCode,
				ContentType:    respContentType,
				IsFromUpstream: isFromUpstream,
				RequestBody:    body,
				MatchValues:    matchValues,
				CreatedAt:      time.Now(),
			}

			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), asyncWriteTimeout)
				defer cancel()
				table.Set(ctx, key, rec, ttl)
				RequestLog(log, req).Info("Replay recorded", "method", req.Method, "path", req.URL.Path)
			}()

			writeThrough(w, rw)
		})
	}
}

// writeThrough writes the captured response to the real response writer.
func writeThrough(w http.ResponseWriter, rw *responseWriter) {
	for k, vals := range rw.Header() {
		for _, v := range vals {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(rw.statusCode)
	_, _ = w.Write(rw.body.Bytes())
}
