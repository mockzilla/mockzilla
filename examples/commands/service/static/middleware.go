package static

import (
	"net/http"

	"github.com/mockzilla/mockzilla/v2/pkg/middleware"
)

// getMiddleware returns the middleware of this service, applied before the standard chain.
// This file is written once; edit it freely.
func getMiddleware() []func(*middleware.Params) func(http.Handler) http.Handler {
	return []func(*middleware.Params) func(http.Handler) http.Handler{}
}
