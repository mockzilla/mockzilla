package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/db"
)

const upstreamErrorKey ctxKey = "upstreamError"

// GetUpstreamError returns the upstream error string stored in the request context, if any.
func GetUpstreamError(req *http.Request) string {
	if v, ok := req.Context().Value(upstreamErrorKey).(string); ok {
		return v
	}
	return ""
}

// upstreamHTTPError is returned when upstream responds with an error status code.
// It carries the status code and body/content-type so fail-on can forward the response to the client.
type upstreamHTTPError struct {
	StatusCode  int
	Body        string
	ContentType string
}

func (e *upstreamHTTPError) Error() string {
	return fmt.Sprintf("upstream response failed with status code %d, body: %s", e.StatusCode, e.Body)
}

// upstreamResponse holds the response data from an upstream service.
type upstreamResponse struct {
	Body        []byte
	ContentType string
	StatusCode  int
}

// CreateUpstreamRequestMiddleware returns a middleware that fetches data from an upstream service.
func CreateUpstreamRequestMiddleware(params *Params) func(http.Handler) http.Handler {
	log := params.Logger("upstream")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			svcCfg := params.GetServiceConfig(req)
			cfg := svcCfg.Upstream
			if cfg == nil || cfg.URL == "" {
				next.ServeHTTP(w, req)
				return
			}

			reqLog := RequestLog(log, req)
			reqLog.Debug("Service has upstream service defined")

			resp, err := getUpstreamResponse(log, svcCfg, params, req)

			// If an upstream service returns a successful response, write it and return immediately
			if err == nil && resp != nil {
				SetRequestIDHeader(w, req)
				SetDurationHeader(w, req)
				w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceUpstream)
				if resp.ContentType != "" {
					w.Header().Set("Content-Type", resp.ContentType)
				}
				w.WriteHeader(resp.StatusCode)
				_, _ = w.Write(resp.Body)
				return
			}

			if err != nil {
				reqLog.Error("Error fetching upstream service", "url", cfg.URL, "error", err)

				// Check fail-on: return upstream error directly without generator fallback.
				// nil (omitted) = default (400); pointer to empty list = disabled.
				failOn := cfg.FailOn
				if failOn == nil {
					failOn = &config.DefaultFailOnStatus
				}
				var httpErr *upstreamHTTPError
				if len(*failOn) > 0 && errors.As(err, &httpErr) && failOn.Is(httpErr.StatusCode) {
					reqLog.Info("Upstream error matches fail-on, returning directly",
						"status", httpErr.StatusCode,
					)

					requestID := GetRequestID(req)
					duration := GetDuration(req)
					if svcCfg.HistoryEnabled() {
						histReq := &db.HistoryRequest{
							Method:     req.Method,
							URL:        req.URL.String(),
							Headers:    db.FlattenHeaders(req.Header),
							RemoteAddr: req.RemoteAddr,
							RequestID:  requestID,
						}
						histResp := &db.HistoryResponse{
							Body:           []byte(httpErr.Body),
							StatusCode:     httpErr.StatusCode,
							ContentType:    httpErr.ContentType,
							IsFromUpstream: true,
							Duration:       duration,
						}
						params.transformHistory(svcCfg, histReq, histResp)
						resourcePath := GetResourcePath(req)

						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), asyncWriteTimeout)
							defer cancel()
							params.DB().History().Set(ctx, resourcePath, histReq, histResp)
						}()
					}

					SetRequestIDHeader(w, req)
					SetDurationHeader(w, req)
					w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceUpstream)
					if httpErr.ContentType != "" {
						w.Header().Set("Content-Type", httpErr.ContentType)
					}
					w.WriteHeader(httpErr.StatusCode)
					_, _ = w.Write([]byte(httpErr.Body))
					return
				}
			}

			// Proceed to the next handler if no upstream service matched
			if err != nil {
				ctx := context.WithValue(req.Context(), upstreamErrorKey, err.Error())
				req = req.WithContext(ctx)
			}
			next.ServeHTTP(w, req)
		})
	}
}

func getUpstreamResponse(log *slog.Logger, svcCfg *config.ServiceConfig, params *Params, req *http.Request) (*upstreamResponse, error) {
	log = RequestLog(log, req)
	cfg := svcCfg.Upstream

	timeout := config.DefaultUpstreamTimeout
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	client := http.Client{
		Timeout: timeout,
	}

	history := params.DB().History()
	resourcePrefix := "/" + svcCfg.Name
	recordHistory := svcCfg.HistoryEnabled()

	bodyBytes := readAndRestoreBody(req)

	outURL := fmt.Sprintf("%s/%s",
		strings.TrimSuffix(cfg.URL, "/"),
		strings.TrimPrefix(req.URL.Path[len(resourcePrefix):], "/"))

	if req.URL.RawQuery != "" {
		outURL += "?" + req.URL.RawQuery
	}

	log.Debug("Upstream request", "method", req.Method, "url", outURL)

	upReq, err := http.NewRequest(req.Method, outURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Error("Failed to create request", "error", err)
		return nil, err
	}

	cleanUpstreamHeaders(req)

	for name, values := range req.Header {
		for _, value := range values {
			upReq.Header.Add(name, value)
		}
	}

	// Remove Accept-Encoding so Go's http.Transport handles decompression
	// transparently. When set explicitly, Transport skips auto-decompression
	// and io.ReadAll returns raw compressed bytes (e.g. gzip).
	upReq.Header.Del("Accept-Encoding")
	upReq.Header.Set("User-Agent", "Connexions/2.0")
	for name, value := range cfg.Headers {
		upReq.Header.Set(name, value)
	}

	log.Info("Upstream request", "method", upReq.Method, "url", upReq.URL.String())

	resp, err := client.Do(upReq)
	if err != nil {
		return nil, fmt.Errorf("error calling upstream service %s: %s", upReq.URL.String(), err)
	}

	statusCode := resp.StatusCode

	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response from upstream service %s: %s", upReq.URL, err)
	}

	if statusCode >= http.StatusBadRequest {
		return nil, &upstreamHTTPError{
			StatusCode:  statusCode,
			Body:        string(body),
			ContentType: resp.Header.Get("Content-Type"),
		}
	}

	log.Info("Received successful upstream response", "body", string(body))

	contentType := resp.Header.Get("Content-Type")

	if recordHistory {
		histReq := &db.HistoryRequest{
			Method:     req.Method,
			URL:        req.URL.String(),
			Body:       bodyBytes,
			Headers:    db.FlattenHeaders(req.Header),
			RemoteAddr: req.RemoteAddr,
			RequestID:  GetRequestID(req),
		}
		histResp := &db.HistoryResponse{
			Body:           body,
			StatusCode:     statusCode,
			ContentType:    contentType,
			IsFromUpstream: true,
			UpstreamURL:    outURL,
			Headers:        db.FlattenHeaders(resp.Header),
			Duration:       GetDuration(req),
		}
		params.transformHistory(svcCfg, histReq, histResp)
		resourcePath := GetResourcePath(req)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), asyncWriteTimeout)
			defer cancel()
			history.Set(ctx, resourcePath, histReq, histResp)
		}()
	}

	return &upstreamResponse{
		Body:        body,
		ContentType: contentType,
		StatusCode:  statusCode,
	}, nil
}

// cleanUpstreamHeaders removes internal X-Cxs-* headers from the request
// before forwarding to upstream. When X-Cxs-Upstream-Headers is present,
// only the listed headers are kept (all others are removed).
func cleanUpstreamHeaders(req *http.Request) {
	allowList := req.Header.Get(headerPrefix + headerUpstreamHeaders)

	if allowList != "" {
		allowed := parseUpstreamHeadersList(allowList)
		for name := range req.Header {
			if _, ok := allowed[http.CanonicalHeaderKey(name)]; !ok {
				req.Header.Del(name)
			}
		}
		return
	}

	for name := range req.Header {
		if strings.HasPrefix(name, headerPrefix) {
			req.Header.Del(name)
		}
	}
}

// parseUpstreamHeadersList parses a comma-separated list of header names
// into a set of canonical header keys.
func parseUpstreamHeadersList(value string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, name := range strings.Split(value, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[http.CanonicalHeaderKey(name)] = struct{}{}
		}
	}
	return allowed
}
