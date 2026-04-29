package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/mockzilla/mockzilla/v2/pkg/db"
)

// CreateCacheWriteMiddleware is a method on the Router to create a middleware
func CreateCacheWriteMiddleware(params *Params) func(http.Handler) http.Handler {
	recordHistory := params.serviceConfig.HistoryEnabled()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			requestID := GetRequestID(req)
			// Capture request body before downstream handlers consume it.
			var requestBody []byte
			if recordHistory && req.Body != nil && req.Body != http.NoBody {
				requestBody, _ = io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewBuffer(requestBody))
			}

			// Create a responseWriter to capture the response.
			// default to 200 status code
			rw := &responseWriter{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(rw, req)

			respContent := rw.body.Bytes()
			respStatusCode := rw.statusCode
			respContentType := rw.Header().Get("Content-Type")

			// Tag the source before snapshotting headers for history.
			if rw.Header().Get(ResponseHeaderSource) == "" {
				rw.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			}

			// Record request + response asynchronously - no need to block the response.
			if recordHistory {
				histReq := &db.HistoryRequest{
					Method:     req.Method,
					URL:        req.URL.String(),
					Body:       requestBody,
					Headers:    db.FlattenHeaders(req.Header),
					RemoteAddr: req.RemoteAddr,
					RequestID:  requestID,
				}
				histResp := &db.HistoryResponse{
					Body:          respContent,
					StatusCode:    respStatusCode,
					ContentType:   respContentType,
					Headers:       db.FlattenHeaders(rw.Header()),
					Duration:      GetDuration(req),
					UpstreamError: GetUpstreamError(req),
				}
				params.transformHistory(params.serviceConfig, histReq, histResp)
				resourcePath := GetResourcePath(req)
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), asyncWriteTimeout)
					defer cancel()
					params.DB().History().Set(ctx, resourcePath, histReq, histResp)
				}()
			}

			// Set our custom headers before writing
			SetRequestIDHeader(w, req)
			SetDurationHeader(w, req)
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			if respContentType != "" {
				w.Header().Set("Content-Type", respContentType)
			}
			w.WriteHeader(respStatusCode)
			_, _ = w.Write(respContent)
		})
	}
}
