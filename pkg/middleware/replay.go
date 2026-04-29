package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

// headerReplayMatch is the HTTP header that activates replay.
// With an explicit value, replay activates for any method - match fields are taken from the header.
// With an empty value, replay activates only when the endpoint is configured for the request method -
// the config provides the pattern path (needed for placeholders) and match fields.
// Format: "body:field1,field2;query:field3,field4" - unqualified fields are treated as body.
//
// Examples:
//
//	X-Mz-Replay: data.name,data.address.zip
//	X-Mz-Replay: body:biller,reference;query:channel
const headerReplayMatch = "X-Mz-Replay"

// ReplayRecord holds a recorded response along with request metadata for debugging.
//
// Method is the HTTP method of the recorded request (e.g. GET, POST).
// Path is the actual request path (e.g. /pay/credit-card).
// Resource is the matched config endpoint pattern (e.g. /pay/{paymentMethod}).
// Empty when no config endpoint matched (header-only mode).
// Data is the response body bytes.
// Headers are the response headers (excluding internal X-Mz-* headers).
// StatusCode is the HTTP status code of the response.
// ContentType is the Content-Type header of the response.
// IsFromUpstream is true if the response came from an upstream service.
// RequestBody is the original request body (stored for debugging).
// MatchValues maps field paths to their extracted values (stored for debugging).
// CreatedAt is when the recording was created.
type ReplayRecord struct {
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Resource       string            `json:"resource,omitempty"`
	Data           []byte            `json:"data"`
	Headers        map[string]string `json:"headers"`
	StatusCode     int               `json:"statusCode"`
	ContentType    string            `json:"contentType"`
	IsFromUpstream bool              `json:"isFromUpstream"`
	RequestBody    []byte            `json:"requestBody"`
	MatchValues    map[string]any    `json:"matchValues"`
	CreatedAt      time.Time         `json:"createdAt"`
}

// parseReplayHeader parses the X-Mz-Replay header value into a ReplayMatch.
// Format: "body:field1,field2;query:field3,field4"
// Unqualified fields (no "body:" or "query:" prefix) are treated as body fields.
// Returns nil if the header value is empty.
func parseReplayHeader(headerValue string) *config.ReplayMatch {
	if headerValue == "" {
		return nil
	}

	match := &config.ReplayMatch{}
	sections := strings.Split(headerValue, ";")

	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		var source string
		var fieldsPart string

		if idx := strings.Index(section, ":"); idx != -1 {
			source = strings.TrimSpace(section[:idx])
			fieldsPart = section[idx+1:]
		} else {
			source = "body"
			fieldsPart = section
		}

		fields := splitFields(fieldsPart)
		switch source {
		case "path":
			match.Path = append(match.Path, fields...)
		case "query":
			match.Query = append(match.Query, fields...)
		default:
			match.Body = append(match.Body, fields...)
		}
	}

	if len(match.Path) == 0 && len(match.Body) == 0 && len(match.Query) == 0 {
		return nil
	}
	return match
}

// splitFields splits a comma-separated string into trimmed, non-empty fields.
func splitFields(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, f := range parts {
		f = strings.TrimSpace(f)
		if f != "" {
			result = append(result, f)
		}
	}
	return result
}

// resolveReplayParams resolves the match config, pattern path, and endpoint path for a replay request.
// Activation rules:
//   - Explicit header value (e.g. "body:field1;query:id") - activates for any method, even without
//     a configured endpoint. The caller provides match fields directly.
//   - Empty header value ("") - activates only when the endpoint is configured for this method.
//     Without config there is no pattern path for placeholders and no match fields to fall back to.
//   - Auto-replay (no header) - activates only for configured endpoints.
//   - Otherwise - skip.
//
// Returns the match config, the pattern path (for key building), and the actual endpoint path
// (for extracting path variable values).
func resolveReplayParams(req *http.Request, cfg *config.ServiceConfig) (match *config.ReplayMatch, patternPath, endpointPath string) {
	// Check header presence (not just value - empty header is valid)
	headerValues, headerPresent := req.Header[http.CanonicalHeaderKey(headerReplayMatch)]

	var headerValue string
	if headerPresent && len(headerValues) > 0 {
		headerValue = headerValues[0]
	}

	// Check if auto-replay is enabled
	autoReplay := cfg.Cache != nil && cfg.Cache.Replay != nil && cfg.Cache.Replay.AutoReplay

	// No header and no auto-replay → skip
	if !headerPresent && !autoReplay {
		return nil, "", ""
	}

	endpointPath = getEndpointPath(req, cfg.Name)

	// Try to get pattern path and config match from replay config
	var configMatch *config.ReplayMatch
	endpointConfigured := false
	if cfg.Cache != nil && cfg.Cache.Replay != nil {
		pattern, ep := cfg.Cache.Replay.GetEndpoint(endpointPath, req.Method)
		if ep != nil {
			patternPath = pattern
			configMatch = ep.Match
			endpointConfigured = true
		}
	}

	// auto-replay without header requires a configured endpoint
	if !headerPresent && !endpointConfigured {
		return nil, "", ""
	}

	// Empty header means "use config" - skip if no config for this method
	if headerPresent && headerValue == "" && !endpointConfigured {
		return nil, "", ""
	}

	// If no pattern matched, use the actual endpoint path
	if patternPath == "" {
		patternPath = endpointPath
	}

	// Header value overrides config match fields
	if headerMatch := parseReplayHeader(headerValue); headerMatch != nil {
		return headerMatch, patternPath, endpointPath
	}

	// Header present but empty value, or auto-replay → fall back to config
	return configMatch, patternPath, endpointPath
}

// buildReplayKey builds a deterministic key from the request, pattern path, endpoint path, match config, and body.
// Returns a SHA-256 hex digest, or empty string if any configured match field is missing from the request.
func buildReplayKey(req *http.Request, patternPath, endpointPath string, match *config.ReplayMatch, body []byte) string {
	contentType := req.Header.Get("Content-Type")
	query := req.URL.Query()

	// Collect all field=value pairs, prefixed with source for determinism
	var pairs []string
	if match != nil {
		if len(match.Path) > 0 {
			pathValues := config.ExtractPathValues(endpointPath, patternPath)
			for _, field := range match.Path {
				val, ok := pathValues[field]
				if !ok {
					return ""
				}
				pairs = append(pairs, "path:"+field+"="+val)
			}
		}
		for _, field := range match.Body {
			val := extractBodyValue(body, contentType, field)
			if val == nil {
				return ""
			}
			pairs = append(pairs, "body:"+field+"="+formatValue(val))
		}
		for _, field := range match.Query {
			if !query.Has(field) {
				return ""
			}
			pairs = append(pairs, "query:"+field+"="+query.Get(field))
		}
	}
	sort.Strings(pairs)

	var sb strings.Builder
	sb.WriteString(req.Method)
	sb.WriteString(":")
	sb.WriteString(patternPath)

	for _, pair := range pairs {
		sb.WriteString("|")
		sb.WriteString(pair)
	}

	hash := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("%x", hash)
}

// readAndRestoreBody reads the request body and restores it so subsequent handlers can read it.
func readAndRestoreBody(req *http.Request) []byte {
	if req.Body == nil {
		return nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

// getEndpointPath strips the service name prefix from the request URL path.
func getEndpointPath(req *http.Request, serviceName string) string {
	prefix := "/" + serviceName
	path := strings.TrimPrefix(req.URL.Path, prefix)
	if path == "" {
		path = "/"
	}
	return path
}

// replayTTL returns the configured replay TTL for a service, falling back to the default.
func replayTTL(cfg *config.ServiceConfig) time.Duration {
	if cfg.Cache != nil && cfg.Cache.Replay != nil && cfg.Cache.Replay.TTL > 0 {
		return cfg.Cache.Replay.TTL
	}
	return config.DefaultReplayTTL
}

// deserializeReplayRecord converts a value retrieved from the DB table into a ReplayRecord.
// Handles both direct *ReplayRecord (memory backend) and map[string]any (Redis backend).
func deserializeReplayRecord(val any) *ReplayRecord {
	if val == nil {
		return nil
	}

	// Direct type - memory backend stores it as-is
	if rec, ok := val.(*ReplayRecord); ok {
		return rec
	}

	// Redis backend returns map[string]any - re-serialize and parse
	data, err := json.Marshal(val)
	if err != nil {
		return nil
	}

	var rec ReplayRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil
	}
	return &rec
}
