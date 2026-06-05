package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
	"github.com/mockzilla/mockzilla/v2/pkg/middleware"
)

// CreateReplayRoutes adds replay explorer routes to the router.
func CreateReplayRoutes(router *Router) error {
	if router.Config().DisableUI || router.Config().Replay == nil || router.Config().Replay.URL == "" {
		return nil
	}

	handler := &ReplayHandler{
		router: router,
	}

	url := "/" + strings.Trim(router.Config().Replay.URL, "/")

	router.Route(url, func(r chi.Router) {
		r.Use(uiAuth(router))
		r.Get("/", handler.list)
		r.Delete("/", handler.clear)
	})

	return nil
}

// ReplayHandler handles replay explorer routes.
type ReplayHandler struct {
	router *Router
}

// ReplaySummary is the body-less list projection of a stored replay recording.
// Key is the storage key (SHA-256 of the matched request fields); the UI uses it
// to fetch the full record and to delete a single recording.
type ReplaySummary struct {
	Key            string         `json:"key"`
	Method         string         `json:"method"`
	Path           string         `json:"path"`
	Resource       string         `json:"resource,omitempty"`
	StatusCode     int            `json:"statusCode"`
	ContentType    string         `json:"contentType,omitempty"`
	IsFromUpstream bool           `json:"isFromUpstream,omitempty"`
	MatchValues    map[string]any `json:"matchValues,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// ReplaySummaryListResponse is the response for the replay list endpoint.
type ReplaySummaryListResponse struct {
	Items     []*ReplaySummary `json:"items"`
	Truncated bool             `json:"truncated,omitempty"`
}

// getService resolves the service from the ?service= query param and returns its DB.
// Writes a 404 and returns nil when the service is unknown.
func (h *ReplayHandler) getService(w http.ResponseWriter, r *http.Request) db.DB {
	serviceName := r.URL.Query().Get("service")
	if serviceName == RootServiceName {
		serviceName = ""
	}

	svc := h.router.GetServices()[serviceName]
	if svc == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return nil
	}

	if svc.Config != nil && !svc.Config.ReplayEnabled() {
		http.Error(w, "Replay disabled for this service", http.StatusNotFound)
		return nil
	}

	database := h.router.GetDB(serviceName)
	if database == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return nil
	}

	return database
}

func (h *ReplayHandler) list(w http.ResponseWriter, r *http.Request) {
	database := h.getService(w, r)
	if database == nil {
		return
	}

	table := database.Table("replay")

	// Single record by key
	if key := r.URL.Query().Get("key"); key != "" {
		val, ok := table.Get(r.Context(), key)
		if !ok {
			http.Error(w, "Recording not found", http.StatusNotFound)
			return
		}
		rec := middleware.DeserializeReplayRecord(val)
		if rec == nil {
			http.Error(w, "Recording not found", http.StatusNotFound)
			return
		}
		NewJSONResponse(w).Send(rec)
		return
	}

	data := table.Data(r.Context())
	summaries := make([]*ReplaySummary, 0, len(data))
	for k, val := range data {
		rec := middleware.DeserializeReplayRecord(val)
		if rec == nil {
			continue
		}
		summaries = append(summaries, &ReplaySummary{
			Key:            k,
			Method:         rec.Method,
			Path:           rec.Path,
			Resource:       rec.Resource,
			StatusCode:     rec.StatusCode,
			ContentType:    rec.ContentType,
			IsFromUpstream: rec.IsFromUpstream,
			MatchValues:    rec.MatchValues,
			CreatedAt:      rec.CreatedAt,
		})
	}

	// Map iteration is unordered; sort newest-first so the UI renders as-is.
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
	})

	truncated := false
	if len(summaries) > maxSummaryItems {
		summaries = summaries[:maxSummaryItems]
		truncated = true
	}

	NewJSONResponse(w).Send(&ReplaySummaryListResponse{Items: summaries, Truncated: truncated})
}

func (h *ReplayHandler) clear(w http.ResponseWriter, r *http.Request) {
	database := h.getService(w, r)
	if database == nil {
		return
	}

	table := database.Table("replay")
	if key := r.URL.Query().Get("key"); key != "" {
		table.Delete(r.Context(), key)
	} else {
		table.Clear(r.Context())
	}

	NewJSONResponse(w).Send(&ReplaySummaryListResponse{Items: make([]*ReplaySummary, 0)})
}
