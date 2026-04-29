package api

import (
	"net/http"
	"strings"
	"time"

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

// HistorySummary is a slim version of HistoryEntry for list responses (no bodies).
type HistorySummary struct {
	ID        string              `json:"id"`
	Resource  string              `json:"resource"`
	Request   *db.HistoryRequest  `json:"request,omitempty"`
	Response  *db.HistoryResponse `json:"response,omitempty"`
	CreatedAt time.Time           `json:"createdAt"`
}

// HistorySummaryListResponse is the response for history list endpoint.
type HistorySummaryListResponse struct {
	Items []*HistorySummary `json:"items"`
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

	// List: return summaries without request/response bodies
	items := database.History().Data(r.Context())
	summaries := make([]*HistorySummary, 0, len(items))
	for _, e := range items {
		s := &HistorySummary{
			ID:        e.ID,
			Resource:  e.Resource,
			CreatedAt: e.CreatedAt,
		}
		if e.Request != nil {
			s.Request = &db.HistoryRequest{
				Method:    e.Request.Method,
				URL:       e.Request.URL,
				RequestID: e.Request.RequestID,
			}
		}
		if e.Response != nil {
			s.Response = &db.HistoryResponse{
				StatusCode:     e.Response.StatusCode,
				ContentType:    e.Response.ContentType,
				IsFromUpstream: e.Response.IsFromUpstream,
				Duration:       e.Response.Duration,
			}
		}
		summaries = append(summaries, s)
	}
	NewJSONResponse(w).Send(&HistorySummaryListResponse{Items: summaries})
}

func (h *HistoryHandler) clear(w http.ResponseWriter, r *http.Request) {
	_, database := h.getService(w, r)
	if database == nil {
		return
	}

	database.History().Clear(r.Context())
	NewJSONResponse(w).Send(&HistorySummaryListResponse{Items: make([]*HistorySummary, 0)})
}
