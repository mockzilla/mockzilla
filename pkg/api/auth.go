package api

import (
	"net/http"

	"github.com/mockzilla/mockzilla/v2/pkg/middleware"
)

// uiAuth is the HTTP Basic Auth middleware applied to the API Explorer UI route
// groups (home, .services, .history, .replay, .config). It gates only the
// explorer, never the mock API mounts, and is a passthrough when no credentials
// are configured.
func uiAuth(router *Router) func(http.Handler) http.Handler {
	return middleware.BasicAuth(router.Config().BasicAuth)
}
