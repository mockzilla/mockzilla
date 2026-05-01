package config

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

type KeyValue[K, V any] struct {
	Key   K
	Value V
}

// ServiceConfig defines the configuration for a service.
// Name is the optional name of the service.
// Upstream is the upstream configuration.
// Latency is the default latency for the service.
// Latencies is a map of percentiles to latencies.
// Errors is a map of percentiles to error codes.
// Validate is the validation configuration.
// Cache is the cache configuration.
// ResourcesPrefix is the prefix for helper routes outside OpenAPI spec.
// SpecOptions allows OpenAPI spec simplifications for code generation.
type ServiceConfig struct {
	Name            string                   `yaml:"name,omitempty"`
	Upstream        *UpstreamConfig          `yaml:"upstream,omitempty"`
	Latency         time.Duration            `yaml:"latency,omitempty"`
	Latencies       map[string]time.Duration `yaml:"latencies,omitempty"`
	Errors          map[string]int           `yaml:"errors,omitempty"`
	Cache           *CacheConfig             `yaml:"cache,omitempty"`
	History         *HistoryConfig           `yaml:"history,omitempty"`
	ResourcesPrefix string                   `yaml:"resources-prefix,omitempty"`
	SpecOptions     *SpecOptions             `yaml:"spec,omitempty"`
	Extra           map[string]any           `yaml:"extra,omitempty"`

	latencies []*KeyValue[int, time.Duration]
	errors    []*KeyValue[int, int]
}

// NewServiceConfig creates a new ServiceConfig with default values.
func NewServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		Errors:      make(map[string]int),
		Latencies:   make(map[string]time.Duration),
		Cache:       NewCacheConfig(),
		History:     NewHistoryConfig(),
		SpecOptions: NewSpecOptions(),
		Extra:       make(map[string]any),
	}
}

func NewServiceConfigFromBytes(bts []byte) (*ServiceConfig, error) {
	res := NewServiceConfig()
	err := yaml.Unmarshal(bts, res)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling service config: %w", err)
	}

	// Fill any nil fields with defaults (in case YAML didn't specify them)
	res.WithDefaults()

	return res, nil
}

// WithDefaults fills nil properties with default values from NewServiceConfig.
func (s *ServiceConfig) WithDefaults() *ServiceConfig {
	defaults := NewServiceConfig()

	// Fill nil pointer fields
	if s.Cache == nil {
		s.Cache = defaults.Cache
	}

	if s.History == nil {
		s.History = defaults.History
	} else if len(s.History.MaskHeaders) == 0 && (s.History.Enabled == nil || *s.History.Enabled) {
		s.History.MaskHeaders = defaults.History.MaskHeaders
	}

	if s.SpecOptions == nil {
		s.SpecOptions = defaults.SpecOptions
	}

	// Fill nil map fields
	if s.Errors == nil {
		s.Errors = defaults.Errors
	}

	if s.Latencies == nil {
		s.Latencies = defaults.Latencies
	}

	if s.Extra == nil {
		s.Extra = defaults.Extra
	}

	// Fill empty string fields with defaults
	if s.ResourcesPrefix == "" {
		s.ResourcesPrefix = defaults.ResourcesPrefix
	}

	// Parse latencies and errors if they haven't been parsed yet
	if s.latencies == nil && len(s.Latencies) > 0 {
		s.latencies = s.parseLatencies()
	}

	if s.errors == nil && len(s.Errors) > 0 {
		s.errors = s.parseErrors()
	}

	return s
}

// OverwriteWith overwrites fields in s with non-nil/non-empty values from other.
// This is useful for merging configurations where other takes precedence.
func (s *ServiceConfig) OverwriteWith(other *ServiceConfig) *ServiceConfig {
	if other == nil {
		return s
	}

	// Overwrite string fields if not empty
	if other.Name != "" {
		s.Name = other.Name
	}

	if other.ResourcesPrefix != "" {
		s.ResourcesPrefix = other.ResourcesPrefix
	}

	// Overwrite pointer fields if not nil
	if other.Upstream != nil {
		s.Upstream = other.Upstream
	}

	if other.Cache != nil {
		s.Cache = other.Cache
	}

	// Overwrite duration if set (non-zero)
	if other.Latency != 0 {
		s.Latency = other.Latency
	}

	// Overwrite map fields if not nil (merge maps)
	if other.Latencies != nil {
		if s.Latencies == nil {
			s.Latencies = make(map[string]time.Duration)
		}
		for k, v := range other.Latencies {
			s.Latencies[k] = v
		}
		// Re-parse latencies after merge
		s.latencies = s.parseLatencies()
	}

	if other.Errors != nil {
		if s.Errors == nil {
			s.Errors = make(map[string]int)
		}
		for k, v := range other.Errors {
			s.Errors[k] = v
		}
		// Re-parse errors after merge
		s.errors = s.parseErrors()
	}

	if other.History != nil {
		s.History = other.History
	}

	if other.SpecOptions != nil {
		s.SpecOptions = other.SpecOptions
	}

	if other.Extra != nil {
		if s.Extra == nil {
			s.Extra = make(map[string]any)
		}
		for k, v := range other.Extra {
			s.Extra[k] = v
		}
	}

	return s
}

// GetLatency returns the latency.
func (s *ServiceConfig) GetLatency() time.Duration {
	if len(s.latencies) == 0 {
		return s.Latency
	}

	rnd := rand.Intn(100) + 1
	for _, latencyKV := range s.latencies {
		if rnd <= latencyKV.Key {
			return latencyKV.Value
		}
	}

	return 0
}

// GetError returns the error based on the percentiles:
//
//	random number is generated between 1 and 100 to simulate the percentile.
//
// If no errors are defined, it returns 0.
func (s *ServiceConfig) GetError() int {
	if len(s.errors) == 0 {
		return 0
	}

	rnd := rand.Intn(100) + 1
	for _, errorKV := range s.errors {
		if rnd <= errorKV.Key {
			return errorKV.Value
		}
	}

	return 0
}

// HistoryEnabled returns whether request history recording is enabled.
// Defaults to true when not explicitly set.
func (s *ServiceConfig) HistoryEnabled() bool {
	return s.History == nil || s.History.Enabled == nil || *s.History.Enabled
}

func (s *ServiceConfig) parseLatencies() []*KeyValue[int, time.Duration] {
	latencies := make([]*KeyValue[int, time.Duration], 0)
	for k, v := range s.Latencies {
		if strings.HasPrefix(k, "p") {
			kNum, err := strconv.Atoi(strings.TrimPrefix(k, "p"))
			if err == nil {
				res := &KeyValue[int, time.Duration]{Key: kNum, Value: v}
				latencies = append(latencies, res)
			}
		}
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i].Key < latencies[j].Key
	})
	return latencies
}

func (s *ServiceConfig) parseErrors() []*KeyValue[int, int] {
	errors := make([]*KeyValue[int, int], 0)
	for k, v := range s.Errors {
		if strings.HasPrefix(k, "p") {
			kNum, err := strconv.Atoi(strings.TrimPrefix(k, "p"))
			if err == nil {
				res := &KeyValue[int, int]{Key: kNum, Value: v}
				errors = append(errors, res)
			}
		}
	}
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Key < errors[j].Key
	})
	return errors
}

// HistoryConfig controls request/response history recording for a service.
//
// Enabled toggles recording on or off (defaults to true when nil).
// MaskHeaders lists header names whose values should be masked before saving,
// showing only the last 4 characters (matched case-insensitively).
//
// Example YAML:
//
//	history:
//	  enabled: true
//	  mask-headers:
//	    - Authorization
//	    - X-Api-Key
//
// Shorthand to disable history entirely:
//
//	history:
//	  enabled: false
type HistoryConfig struct {
	Enabled     *bool    `yaml:"enabled,omitempty"`
	MaskHeaders []string `yaml:"mask-headers,omitempty"`
}

// defaultMaskHeaders is the default set of header names masked in history entries.
var defaultMaskHeaders = []string{
	"Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Api-Key",
}

// NewHistoryConfig creates a HistoryConfig with defaults: enabled, common
// sensitive headers masked.
func NewHistoryConfig() *HistoryConfig {
	return &HistoryConfig{
		MaskHeaders: defaultMaskHeaders,
	}
}

// UnmarshalYAML supports both boolean shorthand and object forms:
//
//	history: false        # shorthand to disable
//	history:              # full form
//	  enabled: true
//	  mask-headers: [Authorization]
func (h *HistoryConfig) UnmarshalYAML(unmarshal func(any) error) error {
	var enabled bool
	if err := unmarshal(&enabled); err == nil {
		h.Enabled = &enabled
		return nil
	}

	type plain HistoryConfig
	return unmarshal((*plain)(h))
}

// CacheConfig defines the cache configuration for a service.
// Requests is a flag whether to cache GET requests.
// Replay is the replay configuration for recording and replaying API responses.
type CacheConfig struct {
	Requests bool          `yaml:"requests"`
	Replay   *ReplayConfig `yaml:"replay,omitempty"`
}

// DefaultReplayTTL is the default time-to-live for replay recordings.
const DefaultReplayTTL = 24 * time.Hour

// ReplayConfig defines the replay (VCR-like) configuration for recording and replaying
// API responses based on request body content.
//
// Replay is activated either by the X-Mockzilla-Replay header or by setting AutoReplay to true.
// When a request comes in, specified fields are extracted from the request body,
// a content-addressed key is built, and a stored recording is returned if one exists.
// If no recording exists, the response from downstream is captured and stored.
//
// Example YAML:
//
//	cache:
//	  replay:
//	    ttl: 24h
//	    auto-replay: true
//	    upstream-only: false
//	    endpoints:
//	      /foo/{f-id}/bar/{b-id}:
//	        POST:
//	          match:
//	            body:
//	              - data.name
//	              - data.address.zip
type ReplayConfig struct {
	// TTL is how long recordings are kept. Default: 24h.
	TTL time.Duration `yaml:"ttl"`

	// UpstreamOnly when true only records responses from upstream services.
	UpstreamOnly bool `yaml:"upstream-only"`

	// AutoReplay when true activates replay for configured endpoints without requiring
	// the X-Mockzilla-Replay header. When false (default), the header must be present.
	AutoReplay bool `yaml:"auto-replay"`

	// Endpoints maps path patterns to their match configurations.
	// Two forms are supported:
	//
	//   With method (matches only that method):
	//     /pay:
	//       POST:
	//         match: [reference]
	//
	//   Without method (matches any request method):
	//     /pay:
	//       match: [reference]
	Endpoints map[string]map[string]*ReplayEndpoint `yaml:"endpoints"`
}

// ReplayEndpoint defines the match configuration for a specific endpoint and HTTP method.
type ReplayEndpoint struct {
	// Match specifies which request fields form the replay key.
	Match *ReplayMatch `yaml:"match"`
}

// ReplayMatch specifies where to extract match field values from.
type ReplayMatch struct {
	// Path fields are path variable names to include in the replay key.
	Path []string `yaml:"path"`

	// Body fields are extracted from the request body (JSON dotted paths or form-encoded flat keys).
	Body []string `yaml:"body"`

	// Query fields are extracted from the URL query string.
	Query []string `yaml:"query"`
}

// AllFields returns all match field names (path + body + query) for key building.
func (rm *ReplayMatch) AllFields() []string {
	if rm == nil {
		return nil
	}
	fields := make([]string, 0, len(rm.Path)+len(rm.Body)+len(rm.Query))
	fields = append(fields, rm.Path...)
	fields = append(fields, rm.Body...)
	fields = append(fields, rm.Query...)
	return fields
}

// WithDefaults fills zero-valued fields with defaults.
func (rc *ReplayConfig) WithDefaults() *ReplayConfig {
	if rc.TTL == 0 {
		rc.TTL = DefaultReplayTTL
	}
	return rc
}

// GetEndpoint finds the matching endpoint config for a request path and method.
// Returns the pattern path (for key building) and the endpoint config.
//
// Three config forms are supported:
//   - Path only ("/pay:") - matches any method, no match fields
//   - Path + method ("/pay: POST:") - matches that method, no match fields
//   - Path + method + match ("/pay: POST: match: [...]") - full config
//
// Returns "", nil if no match is found.
func (rc *ReplayConfig) GetEndpoint(requestPath, method string) (string, *ReplayEndpoint) {
	if rc == nil || rc.Endpoints == nil {
		return "", nil
	}

	for pattern, methods := range rc.Endpoints {
		if !matchesPattern(requestPath, pattern) {
			continue
		}

		// Form 1: path only, no methods map - matches any method
		if methods == nil {
			return pattern, &ReplayEndpoint{}
		}

		// Form 2 & 3: method must match
		ep, ok := methods[method]
		if !ok {
			continue
		}

		// Form 2: method present but endpoint is nil - no match fields
		if ep == nil {
			return pattern, &ReplayEndpoint{}
		}

		// Form 3: full config
		return pattern, ep
	}
	return "", nil
}

// matchesPattern checks if a request path matches a parameterized pattern.
// Pattern segments enclosed in {...} match any value.
func matchesPattern(requestPath, pattern string) bool {
	reqParts := strings.Split(strings.Trim(requestPath, "/"), "/")
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")

	if len(reqParts) != len(patParts) {
		return false
	}

	for i, pat := range patParts {
		if strings.HasPrefix(pat, "{") && strings.HasSuffix(pat, "}") {
			continue
		}
		if pat != reqParts[i] {
			return false
		}
	}
	return true
}

// ExtractPathValues aligns segments of a request path against a pattern and returns
// a map of path variable names to their actual values. Returns nil if paths don't align.
func ExtractPathValues(requestPath, pattern string) map[string]string {
	reqParts := strings.Split(strings.Trim(requestPath, "/"), "/")
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")

	if len(reqParts) != len(patParts) {
		return nil
	}

	values := make(map[string]string)
	for i, pat := range patParts {
		if strings.HasPrefix(pat, "{") && strings.HasSuffix(pat, "}") {
			name := pat[1 : len(pat)-1]
			values[name] = reqParts[i]
		} else if pat != reqParts[i] {
			return nil
		}
	}
	return values
}

// NewCacheConfig creates a new CacheConfig with default values.
func NewCacheConfig() *CacheConfig {
	return &CacheConfig{
		Requests: true,
	}
}

// SpecOptions allows OpenAPI spec simplifications for code generation.
// These simplifications are particularly helpful for enormous schemas that would
// otherwise generate unwieldy code.
//
// Simplifications include:
//   - Removal of extra union elements (anyOf/oneOf/allOf)
//   - Optionally limiting the number of optional properties kept
//
// LazyLoad enables on-demand parsing of operations. When true, operations are
// parsed only when first accessed and cached for subsequent requests. This
// significantly speeds up server startup for large specs (e.g., Stripe with 500+ endpoints).
//
// OptionalProperties is nil by default, meaning all optional properties are kept.
// Set it explicitly to limit the number of optional properties.
//
// Example usage in YAML:
//
//	spec:
//	  lazyLoad: true    # Parse operations on-demand instead of at startup
//	  simplify: true
//	  optional-properties:
//	    min: 5        # Keep exactly 5 optional properties (when min == max)
//	    max: 5
//	    # OR
//	    min: 2        # Keep random number between 2-8 optional properties
//	    max: 8
type SpecOptions struct {
	LazyLoad           bool                `yaml:"lazyLoad"`
	Simplify           bool                `yaml:"simplify"`
	OptionalProperties *OptionalProperties `yaml:"optional-properties"`
}

func NewSpecOptions() *SpecOptions {
	return &SpecOptions{
		LazyLoad: true,
		Simplify: false,
	}
}

// OptionalProperties controls how many optional properties to keep in generated types.
// This helps reduce the size of generated code for schemas with many optional fields.
//
// Min and Max specify the range of optional properties to keep:
//   - If Min == Max, keeps exactly that many optional properties
//   - If Min < Max, keeps a random number between Min and Max (inclusive)
//
// When nil (not set in config), all optional properties are kept.
type OptionalProperties struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}
