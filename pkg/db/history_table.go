package db

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

// HistoryTable provides typed access to request/response history.
type HistoryTable interface {
	// Get retrieves the latest request record matching the HTTP request's method and URL.
	Get(ctx context.Context, req *http.Request) (*HistoryEntry, bool)

	// Set stores a request record with a unique ID.
	Set(ctx context.Context, resource string, req *HistoryRequest, response *HistoryResponse) *HistoryEntry

	// SetResponse updates the response for the latest request record matching the request's method and URL.
	SetResponse(ctx context.Context, req *HistoryRequest, response *HistoryResponse)

	// GetByID retrieves a single history entry by its ID.
	GetByID(ctx context.Context, id string) (*HistoryEntry, bool)

	// Data returns all request records as an ordered log.
	Data(ctx context.Context) []*HistoryEntry

	// Summaries returns a body-less projection of the most recent entries.
	Summaries(ctx context.Context) []*HistorySummary

	// Len returns the number of history entries.
	Len(ctx context.Context) int

	// Clear removes all history records.
	Clear(ctx context.Context)
}

// HistorySummary is the body-less view returned by HistoryTable.Summaries.
// JSON shape mirrors HistoryEntry so the same UI renders list and detail.
type HistorySummary struct {
	ID        string                  `json:"id"`
	Resource  string                  `json:"resource"`
	Request   *HistorySummaryRequest  `json:"request,omitempty"`
	Response  *HistorySummaryResponse `json:"response,omitempty"`
	CreatedAt time.Time               `json:"createdAt"`
}

type HistorySummaryRequest struct {
	Method    string `json:"method"`
	URL       string `json:"url"`
	RequestID string `json:"requestId,omitempty"`
}

type HistorySummaryResponse struct {
	StatusCode     int           `json:"statusCode"`
	ContentType    string        `json:"contentType,omitempty"`
	IsFromUpstream bool          `json:"isFromUpstream,omitempty"`
	Duration       time.Duration `json:"duration,omitempty"`
	UpstreamError  string        `json:"upstreamError,omitempty"`
}

// SummaryOf projects a full HistoryEntry down to the list-view fields.
func SummaryOf(e *HistoryEntry) *HistorySummary {
	if e == nil {
		return nil
	}
	s := &HistorySummary{
		ID:        e.ID,
		Resource:  e.Resource,
		CreatedAt: e.CreatedAt,
	}
	if e.Request != nil {
		s.Request = &HistorySummaryRequest{
			Method:    e.Request.Method,
			URL:       e.Request.URL,
			RequestID: e.Request.RequestID,
		}
	}
	if e.Response != nil {
		s.Response = &HistorySummaryResponse{
			StatusCode:     e.Response.StatusCode,
			ContentType:    e.Response.ContentType,
			IsFromUpstream: e.Response.IsFromUpstream,
			Duration:       e.Response.Duration,
			UpstreamError:  e.Response.UpstreamError,
		}
	}
	return s
}

// HistoryRequest represents the HTTP request stored in a history entry.
// IsBodyTruncated indicates the storage backend clipped Body to fit a cap;
// the UI can render a "(truncated)" badge so users know the displayed
// payload is not the full original.
type HistoryRequest struct {
	Method          string   `json:"method"`
	URL             string   `json:"url"`
	Body            []byte   `json:"body,omitempty"`
	IsBodyTruncated bool     `json:"isBodyTruncated,omitempty"`
	Headers         []string `json:"headers,omitempty"`
	RemoteAddr      string   `json:"remoteAddr,omitempty"`
	RequestID       string   `json:"requestId,omitempty"`
}

// HistoryEntry represents a recorded request in the history.
// ID is a unique identifier for this entry
// Resource is the openapi resource path, i.e. /pets, /pets/{id}
// Response is the response if present
// Request is the method, URL, body, headers, and remote address of the original request
type HistoryEntry struct {
	ID        string           `json:"id"`
	Resource  string           `json:"resource"`
	Response  *HistoryResponse `json:"response,omitempty"`
	Request   *HistoryRequest  `json:"request,omitempty"`
	CreatedAt time.Time        `json:"createdAt"`
}

// HistoryResponse represents the response that was generated or received from the server.
// Body is the response body
// StatusCode is the HTTP status code returned
// ContentType is the Content-Type header of the response
// IsFromUpstream is true if the response was received from the upstream server
// UpstreamURL is the URL that was actually sent to the upstream service
// Duration is the time taken to produce the response
type HistoryResponse struct {
	Body            []byte        `json:"body"`
	IsBodyTruncated bool          `json:"isBodyTruncated,omitempty"`
	StatusCode      int           `json:"statusCode"`
	ContentType     string        `json:"contentType"`
	IsFromUpstream  bool          `json:"isFromUpstream"`
	UpstreamURL     string        `json:"upstreamURL"`
	Headers         []string      `json:"headers,omitempty"`
	Duration        time.Duration `json:"duration,omitempty"`
	UpstreamError   string        `json:"upstreamError,omitempty"`
}

// FlattenHeaders converts http.Header to a sorted slice of "Key: value" strings.
func FlattenHeaders(h http.Header) []string {
	if len(h) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(h))
	for _, k := range keys {
		result = append(result, k+": "+strings.Join(h.Values(k), ", "))
	}
	return result
}

// MaskHeaderValues masks values of headers matching the patterns in maskHeaders,
// replacing all but the last 4 characters with asterisks.
// Patterns are matched case-insensitively. A trailing "*" matches any header
// with that prefix (e.g. "X-Internal-*" matches "X-Internal-Token").
// The input slice is modified in place.
func MaskHeaderValues(headers []string, maskHeaders []string) {
	if len(headers) == 0 || len(maskHeaders) == 0 {
		return
	}

	exact := make(map[string]bool)
	var prefixes []string
	for _, pattern := range maskHeaders {
		lower := strings.ToLower(pattern)
		if strings.HasSuffix(lower, "*") {
			prefixes = append(prefixes, strings.TrimSuffix(lower, "*"))
		} else {
			exact[lower] = true
		}
	}

	for i, h := range headers {
		colonIdx := strings.Index(h, ": ")
		if colonIdx < 0 {
			continue
		}
		name := strings.ToLower(h[:colonIdx])
		if shouldMask(name, exact, prefixes) {
			headers[i] = h[:colonIdx] + ": " + maskValue(h[colonIdx+2:])
		}
	}
}

func shouldMask(name string, exact map[string]bool, prefixes []string) bool {
	if exact[name] {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// maskValue replaces all but the last 4 characters of v with asterisks.
// Values with fewer than 5 characters are fully masked.
func maskValue(v string) string {
	if len(v) < 5 {
		return strings.Repeat("*", len(v))
	}
	return strings.Repeat("*", len(v)-4) + v[len(v)-4:]
}
