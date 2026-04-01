package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/db"
)

// asyncWriteTimeout is the maximum time allowed for background DB writes.
const asyncWriteTimeout = 5 * time.Second

// ResponseHeaderSource is the response header indicating where the response came from.
const ResponseHeaderSource = "X-Cxs-Source"

// ResponseHeaderSource values.
const (
	ResponseHeaderSourceUpstream  = "upstream"
	ResponseHeaderSourceCache     = "cache"
	ResponseHeaderSourceGenerated = "generated"
	ResponseHeaderSourceReplay    = "replay"
)

const serviceConfigKey ctxKey = "serviceConfig"

// HistoryTransformFunc is a callback invoked before saving a history entry.
// It receives the request and response about to be stored and may modify them
// in place (e.g. masking sensitive headers or redacting body fields).
type HistoryTransformFunc func(req *db.HistoryRequest, resp *db.HistoryResponse)

// Params provides access to service configuration and database for middleware.
type Params struct {
	serviceConfig    *config.ServiceConfig
	database         db.DB
	log              *slog.Logger
	router           chi.Routes
	historyTransform HistoryTransformFunc
}

// NewParams creates a new Params instance with the given configuration and database.
func NewParams(serviceConfig *config.ServiceConfig, database db.DB) *Params {
	return &Params{
		serviceConfig: serviceConfig,
		database:      database,
		log:           slog.With("service", serviceConfig.Name),
	}
}

// SetHistoryTransform registers a callback that is invoked before each history
// entry is saved. The callback may modify the request and response in place.
// It runs before the mask-headers config is applied.
// Must be called during setup, before the server starts serving requests.
func (p *Params) SetHistoryTransform(fn HistoryTransformFunc) {
	p.historyTransform = fn
}

// transformHistory applies the user callback (if set) and then masks headers
// listed in the service config's MaskHeaders field.
func (p *Params) transformHistory(svcCfg *config.ServiceConfig, req *db.HistoryRequest, resp *db.HistoryResponse) {
	if p.historyTransform != nil {
		p.historyTransform(req, resp)
	}
	if svcCfg.History != nil && len(svcCfg.History.MaskHeaders) > 0 {
		db.MaskHeaderValues(req.Headers, svcCfg.History.MaskHeaders)
		if resp != nil {
			db.MaskHeaderValues(resp.Headers, svcCfg.History.MaskHeaders)
		}
	}
}

// GetServiceConfig returns the per-request service config from the context if set
// by the config override middleware, otherwise falls back to the shared config.
func (p *Params) GetServiceConfig(req *http.Request) *config.ServiceConfig {
	if cfg, ok := req.Context().Value(serviceConfigKey).(*config.ServiceConfig); ok {
		return cfg
	}
	return p.serviceConfig
}

// SetRouter stores the router for resource path resolution at request time.
func (p *Params) SetRouter(r chi.Routes) {
	p.router = r
}

// DB returns the per-service database instance.
func (p *Params) DB() db.DB {
	return p.database
}

// Logger returns a logger with the given middleware name added to the service context.
func (p *Params) Logger(middlewareName string) *slog.Logger {
	if p.log != nil {
		return p.log.With("middleware", middlewareName)
	}
	return slog.With("middleware", middlewareName)
}

// responseWriter is a custom response writer that captures the response body
type responseWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	// Don't call underlying WriteHeader - we'll do it after setting our headers
}

// Write intercepts the response and writes to a buffer
func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.body.Write(b)
}
