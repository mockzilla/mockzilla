package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
)

const cacheTable = "cache"

const maxCacheBodyBytes = 10 * 1024

// cachedResponse carries only what replaying the response needs, so a hit is
// one read of a small value rather than of a full history record.
type cachedResponse struct {
	Body        []byte `json:"body,omitempty"`
	StatusCode  int    `json:"statusCode"`
	ContentType string `json:"contentType,omitempty"`
}

func isRequestCacheEnabled(cfg *config.ServiceConfig, req *http.Request) bool {
	// Only GET is ever read back, so nothing else is worth writing.
	return cfg != nil &&
		cfg.Cache != nil &&
		cfg.Cache.Requests &&
		req.Method == http.MethodGet
}

func cacheKey(method, url string) string {
	// Hashed so key length stays bounded regardless of query string size.
	sum := sha256.Sum256([]byte(method + " " + url))
	return hex.EncodeToString(sum[:])
}

func readCache(ctx context.Context, database db.DB, req *http.Request) (*cachedResponse, bool) {
	val, ok := database.Table(cacheTable).Get(ctx, cacheKey(req.Method, req.URL.String()))
	if !ok {
		return nil, false
	}
	return db.DecodeValue[cachedResponse](val)
}

func writeCache(ctx context.Context, database db.DB, key string, resp *cachedResponse, ttl time.Duration) {
	if len(resp.Body) > maxCacheBodyBytes {
		return
	}
	database.Table(cacheTable).Set(ctx, key, resp, ttl)
}

// cacheUpstreamResponse stores an upstream response in the request cache.
// The upstream middleware answers the client directly, so its responses never
// reach the cache-write middleware.
func cacheUpstreamResponse(params *Params, cfg *config.ServiceConfig, req *http.Request, status int, contentType string, body []byte) {
	if !isRequestCacheEnabled(cfg, req) {
		return
	}

	key := cacheKey(req.Method, req.URL.String())
	ttl := cacheTTL(cfg)
	database := params.DB()
	safeWrite(params.Logger("cache"), func(ctx context.Context) {
		writeCache(ctx, database, key, &cachedResponse{
			Body:        body,
			StatusCode:  status,
			ContentType: contentType,
		}, ttl)
	})
}

// cacheTTL returns how long a cached response stays valid. The request cache
// has no duration of its own, so it follows the service's history window.
func cacheTTL(cfg *config.ServiceConfig) time.Duration {
	if cfg == nil || cfg.History == nil {
		return 0
	}
	return cfg.History.Duration
}
