package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestIsRequestCacheEnabled(t *testing.T) {
	assert := assert2.New(t)

	get := httptest.NewRequest(http.MethodGet, "/foo", nil)
	post := httptest.NewRequest(http.MethodPost, "/foo", nil)
	on := &config.ServiceConfig{Cache: &config.CacheConfig{Requests: true}}

	assert.False(isRequestCacheEnabled(nil, get))
	assert.False(isRequestCacheEnabled(&config.ServiceConfig{}, get))
	assert.False(isRequestCacheEnabled(&config.ServiceConfig{Cache: &config.CacheConfig{}}, get))
	assert.False(isRequestCacheEnabled(on, post))
	assert.True(isRequestCacheEnabled(on, get))
}

func TestCacheKey(t *testing.T) {
	assert := assert2.New(t)

	assert.Equal(cacheKey("GET", "/foo"), cacheKey("GET", "/foo"))
	assert.NotEqual(cacheKey("GET", "/foo"), cacheKey("POST", "/foo"))
	assert.NotEqual(cacheKey("GET", "/foo"), cacheKey("GET", "/foo?a=1"))
	assert.Len(cacheKey("GET", "/foo"), 64)
}

func TestCacheTTL(t *testing.T) {
	assert := assert2.New(t)

	assert.Zero(cacheTTL(nil))
	assert.Zero(cacheTTL(&config.ServiceConfig{}))
	assert.Equal(2*time.Minute, cacheTTL(&config.ServiceConfig{
		History: &config.HistoryConfig{Duration: 2 * time.Minute},
	}))
}
