package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
)

// CreateHistoryRoutes adds history routes to the router.
func CreateHistoryRoutes(router *Router) error {
	if router.Config().DisableUI || router.Config().History.URL == "" {
		return nil
	}

	handler := &HistoryHandler{
		router: router,
	}

	url := router.Config().History.URL
	url = "/" + strings.Trim(url, "/")

	router.Route(url, func(r chi.Router) {
		r.Use(uiAuth(router))
		r.Get("/", handler.list)
		r.Delete("/", handler.clear)
	})

	return nil
}

// HistoryHandler handles history routes.
type HistoryHandler struct {
	router *Router
}

// HistoryListResponse is the response for history list endpoint.
type HistoryListResponse struct {
	Items []*db.HistoryEntry `json:"items"`
}

// HistorySummary aliases db.HistorySummary so existing callers and tests
// continue to compile while the projection logic lives in the storage layer.
type HistorySummary = db.HistorySummary

// maxSummaryItems caps how many newest summaries the history and replay list
// endpoints return, so a high-traffic service can't produce an unbounded
// response or DOM. Older items are dropped (newest-first) and Truncated is set.
const maxSummaryItems = 100

// HistorySummaryListResponse is the response for history list endpoint.
type HistorySummaryListResponse struct {
	Items     []*HistorySummary `json:"items"`
	Truncated bool              `json:"truncated,omitempty"`
}

// getService looks up the service by name and checks that history is enabled for it.
// Returns the DB or writes an error response and returns nil.
func (h *HistoryHandler) getService(w http.ResponseWriter, r *http.Request) (string, db.DB) {
	serviceName := r.URL.Query().Get("service")
	if serviceName == RootServiceName {
		serviceName = ""
	}

	svc := h.router.GetServices()[serviceName]
	if svc == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return serviceName, nil
	}

	if svc.Config != nil && !svc.Config.HistoryEnabled() {
		http.Error(w, "History disabled for this service", http.StatusNotFound)
		return serviceName, nil
	}

	database := h.router.GetDB(serviceName)
	if database == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return serviceName, nil
	}

	return serviceName, database
}

func (h *HistoryHandler) list(w http.ResponseWriter, r *http.Request) {
	_, database := h.getService(w, r)
	if database == nil {
		return
	}

	// Single entry by ID
	id := r.URL.Query().Get("id")
	if id != "" {
		entry, ok := database.History().GetByID(r.Context(), id)
		if !ok {
			http.Error(w, "Entry not found", http.StatusNotFound)
			return
		}
		NewJSONResponse(w).Send(entry)
		return
	}

	summaries := database.History().Recent(r.Context(), maxSummaryItems)
	if summaries == nil {
		summaries = make([]*HistorySummary, 0)
	}

	// Storage keeps at most db.MaxHistoryEntries, so a full page means older
	// entries have already been dropped.
	truncated := len(summaries) == maxSummaryItems

	// Recent returns newest-first; the wire format is oldest-first.
	slices.Reverse(summaries)

	NewJSONResponse(w).Send(&HistorySummaryListResponse{Items: summaries, Truncated: truncated})
}

func (h *HistoryHandler) clear(w http.ResponseWriter, r *http.Request) {
	_, database := h.getService(w, r)
	if database == nil {
		return
	}

	database.History().Clear(r.Context())
	NewJSONResponse(w).Send(&HistorySummaryListResponse{Items: make([]*HistorySummary, 0)})
}
