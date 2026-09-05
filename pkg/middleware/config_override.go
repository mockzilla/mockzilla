// Package middleware provides HTTP middleware for mockzilla services.
package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
)

// Header prefix and names for per-request config overrides.
// Headers are case-insensitive (Go's http.Header canonicalizes them).
const (
	// headerPrefix is the prefix for all config override headers.
	headerPrefix = "X-Mockzilla-"

	// Supported header names (without prefix, canonicalized form)
	headerCacheRequests    = "Cache-Requests"
	headerLatency          = "Latency"
	headerUpstreamURL      = "Upstream-Url"
	headerUpstreamHeaders  = "Upstream-Headers"
	headerSource           = "Source"
	headerValidateRequest  = "Validate-Request"
	headerValidateResponse = "Validate-Response"
	headerValidateVerbose  = "Validate-Verbose"
	headerValidateTimeout  = "Validate-Timeout"
)

const sourceUI = "ui"

// browserHeaders are headers a browser adds on its own. They say nothing about
// the call, so they are not forwarded upstream. They are not removed from the
// request: a service whose spec declares one - Accept-Language is a parameter
// of several published APIs - has to be able to read what the caller sent.
//
// History no longer consults this: historyHeaderAllowList decides what it
// keeps, and none of these are on it.
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

var historyHeaderAllowList = map[string]bool{
	"Content-Type": true,
	"User-Agent":   true,
}

// historyHeaderAllowPrefix keeps the caller's own control headers.
const historyHeaderAllowPrefix = "X-Mockzilla-"

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

			if req.Header.Get(headerPrefix+headerSource) == sourceUI {
				req.Header.Del("Authorization")
			}
			next.ServeHTTP(w, req)
		})
	}
}

// historyHeaders is what a request contributes to history: what the caller sent,
// less the headers a browser added for them.
func historyHeaders(h http.Header) []string {
	kept := make(http.Header, len(h))
	for name, values := range h {
		if !keepInHistory(name) {
			continue
		}
		kept[name] = values
	}
	return db.FlattenHeaders(kept)
}

// keepInHistory reports whether a header is the caller's rather than the
// platform's. Anything unrecognised is dropped: see historyHeaderAllowList.
func keepInHistory(name string) bool {
	canonical := http.CanonicalHeaderKey(name)
	return historyHeaderAllowList[canonical] ||
		strings.HasPrefix(canonical, historyHeaderAllowPrefix)
}

// dropBrowserHeaders removes what a browser added from a request about to be
// forwarded upstream. The caller's own headers are left alone.
func dropBrowserHeaders(req *http.Request) {
	for name := range req.Header {
		if browserHeaders[http.CanonicalHeaderKey(name)] {
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

	// Always deep-copy Validate even if nil so a per-request override
	// can write into a fresh struct without mutating the service-level
	// config or leaving the override invisible.
	if original.Validate != nil {
		validateCopy := *original.Validate
		cfg.Validate = &validateCopy
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

	case headerValidateRequest:
		if b, err := strconv.ParseBool(o.value); err == nil {
			if cfg.Validate == nil {
				cfg.Validate = &config.ValidateConfig{}
			}
			cfg.Validate.Request = &b
		}

	case headerValidateResponse:
		if b, err := strconv.ParseBool(o.value); err == nil {
			if cfg.Validate == nil {
				cfg.Validate = &config.ValidateConfig{}
			}
			cfg.Validate.Response = &b
		}

	case headerValidateVerbose:
		if b, err := strconv.ParseBool(o.value); err == nil {
			if cfg.Validate == nil {
				cfg.Validate = &config.ValidateConfig{}
			}
			cfg.Validate.Verbose = &b
		}

	case headerValidateTimeout:
		if d, err := time.ParseDuration(o.value); err == nil && d > 0 {
			if cfg.Validate == nil {
				cfg.Validate = &config.ValidateConfig{}
			}
			cfg.Validate.Timeout = &d
		}
	}
}
