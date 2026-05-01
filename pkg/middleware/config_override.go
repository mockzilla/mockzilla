// Package middleware provides HTTP middleware for mockzilla services.
package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

// Header prefix and names for per-request config overrides.
// Headers are case-insensitive (Go's http.Header canonicalizes them).
const (
	// headerPrefix is the prefix for all config override headers.
	headerPrefix = "X-Mockzilla-"

	// Supported header names (without prefix, canonicalized form)
	headerCacheRequests   = "Cache-Requests"
	headerLatency         = "Latency"
	headerUpstreamURL     = "Upstream-Url"
	headerUpstreamHeaders = "Upstream-Headers"
	headerSource          = "Source"
)

const sourceUI = "ui"

// browserHeaders are headers automatically added by browsers that add noise
// to history and should not be forwarded upstream.
var browserHeaders = map[string]bool{
	"Origin":                            true,
	"Referer":                           true,
	"Cookie":                            true,
	"Sec-Fetch-Mode":                    true,
	"Sec-Fetch-Site":                    true,
	"Sec-Fetch-Dest":                    true,
	"Sec-Ch-Ua":                         true,
	"Sec-Ch-Ua-Mobile":                  true,
	"Sec-Ch-Ua-Platform":                true,
	"Sec-Fetch-User":                    true,
	"Upgrade-Insecure-Requests":         true,
	"Dnt":                               true,
	"Cache-Control":                     true,
	"Pragma":                            true,
	"Priority":                          true,
	"Accept-Language":                   true,
	"Sec-Gpc":                           true,
	"Sec-Purpose":                       true,
	"Service-Worker-Navigation-Preload": true,
}

// CreateConfigOverrideMiddleware creates a middleware that reads X-Mockzilla-* headers
// and temporarily overrides ServiceConfig values for the current request.
// Headers are case-insensitive. The original config is restored after the request completes.
func CreateConfigOverrideMiddleware(params *Params) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			overrides := parseConfigOverrides(req.Header)

			if len(overrides) > 0 {
				cfg := applyOverrides(params.serviceConfig, overrides)
				ctx := context.WithValue(req.Context(), serviceConfigKey, cfg)
				req = req.WithContext(ctx)
			}

			fromUI := req.Header.Get(headerPrefix+headerSource) == sourceUI
			stripBrowserHeaders(req, fromUI)
			next.ServeHTTP(w, req)
		})
	}
}

// stripBrowserHeaders removes known browser-injected headers from the request.
// When the request originates from the UI (detected by the presence of X-Mockzilla-*
// headers), Authorization is also stripped since it belongs to the UI session,
// not to the target API.
func stripBrowserHeaders(req *http.Request, fromUI bool) {
	for name := range req.Header {
		canonical := http.CanonicalHeaderKey(name)
		if browserHeaders[canonical] {
			req.Header.Del(name)
			continue
		}
		if fromUI && canonical == "Authorization" {
			req.Header.Del(name)
		}
	}
}

// configOverride represents a single config override from a header.
type configOverride struct {
	key   string
	value string
}

// parseConfigOverrides extracts X-Mockzilla-* headers from the request.
func parseConfigOverrides(headers http.Header) []configOverride {
	var overrides []configOverride

	for name, values := range headers {
		if !strings.HasPrefix(name, headerPrefix) {
			continue
		}
		if len(values) == 0 {
			continue
		}

		key := strings.TrimPrefix(name, headerPrefix)
		overrides = append(overrides, configOverride{
			key: key,

			// Use first value if multiple
			value: values[0],
		})
	}

	return overrides
}

// applyOverrides creates a shallow copy of the config with overrides applied.
func applyOverrides(original *config.ServiceConfig, overrides []configOverride) *config.ServiceConfig {
	if original == nil {
		return nil
	}

	// Create a shallow copy
	cfg := *original

	// Deep copy nested structs that we might modify
	if original.Cache != nil {
		cacheCopy := *original.Cache
		cfg.Cache = &cacheCopy
	}

	if original.Upstream != nil {
		upstreamCopy := *original.Upstream
		cfg.Upstream = &upstreamCopy
	}

	for _, o := range overrides {
		applyOverride(&cfg, o)
	}

	return &cfg
}

// applyOverride applies a single override to the config.
func applyOverride(cfg *config.ServiceConfig, o configOverride) {
	switch o.key {
	case headerCacheRequests:
		if cfg.Cache == nil {
			cfg.Cache = config.NewCacheConfig()
		}
		if b, err := strconv.ParseBool(o.value); err == nil {
			cfg.Cache.Requests = b
		}

	case headerLatency:
		if d, err := time.ParseDuration(o.value); err == nil {
			cfg.Latency = d
		}

	case headerUpstreamURL:
		// Empty string means disable upstream
		if o.value == "" {
			cfg.Upstream = nil
		} else {
			if cfg.Upstream == nil {
				cfg.Upstream = &config.UpstreamConfig{}
			}
			cfg.Upstream.URL = o.value
		}
	}
}
