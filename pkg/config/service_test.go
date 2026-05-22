package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

func TestNewServiceConfig(t *testing.T) {
	t.Run("Creates config with default values", func(t *testing.T) {
		cfg := NewServiceConfig()

		assert.NotNil(t, cfg)
		assert.NotNil(t, cfg.Errors)
		assert.NotNil(t, cfg.Latencies)
		assert.NotNil(t, cfg.Cache)
		assert.Empty(t, cfg.Errors)
		assert.Empty(t, cfg.Latencies)
	})
}

func TestNewServiceConfigFromBytes(t *testing.T) {
	t.Run("Parses valid YAML config", func(t *testing.T) {
		yamlData := []byte(`
latency: 100ms
latencies:
  p50: 50ms
  p90: 100ms
  p99: 200ms
errors:
  p10: 400
  p20: 500
cache:
  requests: true
`)

		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, 100*time.Millisecond, cfg.Latency)
		assert.Len(t, cfg.Latencies, 3)
		assert.Len(t, cfg.Errors, 2)
		assert.True(t, cfg.Cache.Requests)
	})

	t.Run("Returns error for invalid YAML", func(t *testing.T) {
		yamlData := []byte(`invalid: yaml: data: [`)

		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.Error(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("Parses empty config", func(t *testing.T) {
		yamlData := []byte(``)

		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.NotNil(t, cfg)
	})

	t.Run("Fills defaults for missing fields", func(t *testing.T) {
		yamlData := []byte(`
name: test-service
latency: 100ms
`)

		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, "test-service", cfg.Name)
		assert.Equal(t, 100*time.Millisecond, cfg.Latency)

		// These should be filled with defaults even though not in YAML
		assert.NotNil(t, cfg.Cache)
		assert.True(t, cfg.Cache.Requests)

		assert.NotNil(t, cfg.Errors)
		assert.NotNil(t, cfg.Latencies)
	})

	t.Run("Parses latencies and errors automatically", func(t *testing.T) {
		yamlData := []byte(`
latencies:
  p50: 50ms
  p90: 100ms
errors:
  p10: 400
  p20: 500
`)

		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.NotNil(t, cfg)

		// Internal parsed slices should be populated
		assert.NotNil(t, cfg.latencies)
		assert.Len(t, cfg.latencies, 2)
		assert.NotNil(t, cfg.errors)
		assert.Len(t, cfg.errors, 2)
	})
}

func TestServiceConfig_GetLatency(t *testing.T) {
	t.Run("Returns default latency when no percentiles defined", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latency:   100 * time.Millisecond,
			latencies: []*KeyValue[int, time.Duration]{},
		}

		latency := cfg.GetLatency()
		assert.Equal(t, 100*time.Millisecond, latency)
	})

	t.Run("Returns latency based on percentiles", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latency: 100 * time.Millisecond,
			latencies: []*KeyValue[int, time.Duration]{
				{Key: 50, Value: 50 * time.Millisecond},
				{Key: 90, Value: 100 * time.Millisecond},
				{Key: 99, Value: 200 * time.Millisecond},
			},
		}

		// Run multiple times to test randomness
		latencies := make(map[time.Duration]bool)
		for i := 0; i < 100; i++ {
			latency := cfg.GetLatency()
			latencies[latency] = true
			// Should be one of the defined latencies or 0
			assert.Contains(t, []time.Duration{
				0,
				50 * time.Millisecond,
				100 * time.Millisecond,
				200 * time.Millisecond,
			}, latency)
		}
	})

	t.Run("Returns 0 when random exceeds all percentiles", func(t *testing.T) {
		// Use a percentile that can never match (Key: 0 means 0% chance)
		cfg := &ServiceConfig{
			latencies: []*KeyValue[int, time.Duration]{
				{Key: 0, Value: 50 * time.Millisecond},
			},
		}

		// rnd is always 1-100, so it will always exceed Key: 0
		latency := cfg.GetLatency()
		assert.Equal(t, time.Duration(0), latency)
	})
}

func TestServiceConfig_GetError(t *testing.T) {
	t.Run("Returns 0 when no errors defined", func(t *testing.T) {
		cfg := &ServiceConfig{
			errors: []*KeyValue[int, int]{},
		}

		errorCode := cfg.GetError()
		assert.Equal(t, 0, errorCode)
	})

	t.Run("Returns error based on percentiles", func(t *testing.T) {
		cfg := &ServiceConfig{
			errors: []*KeyValue[int, int]{
				{Key: 10, Value: 400},
				{Key: 20, Value: 500},
				{Key: 30, Value: 503},
			},
		}

		// Run multiple times to test randomness
		errors := make(map[int]bool)
		for i := 0; i < 100; i++ {
			errorCode := cfg.GetError()
			errors[errorCode] = true
			// Should be one of the defined errors or 0
			assert.Contains(t, []int{0, 400, 500, 503}, errorCode)
		}
	})
}

func TestServiceConfig_parseLatencies(t *testing.T) {
	t.Run("Parses percentile latencies", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50": 50 * time.Millisecond,
				"p90": 100 * time.Millisecond,
				"p99": 200 * time.Millisecond,
			},
		}

		latencies := cfg.parseLatencies()
		expected := []*KeyValue[int, time.Duration]{
			{Key: 50, Value: 50 * time.Millisecond},
			{Key: 90, Value: 100 * time.Millisecond},
			{Key: 99, Value: 200 * time.Millisecond},
		}
		assert.Equal(t, expected, latencies)
	})

	t.Run("Sorts latencies by percentile", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p99": 200 * time.Millisecond,
				"p50": 50 * time.Millisecond,
				"p90": 100 * time.Millisecond,
			},
		}

		latencies := cfg.parseLatencies()
		expected := []*KeyValue[int, time.Duration]{
			{Key: 50, Value: 50 * time.Millisecond},
			{Key: 90, Value: 100 * time.Millisecond},
			{Key: 99, Value: 200 * time.Millisecond},
		}
		assert.Equal(t, expected, latencies)
	})

	t.Run("Ignores non-percentile keys", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50":     50 * time.Millisecond,
				"default": 100 * time.Millisecond,
				"max":     200 * time.Millisecond,
			},
		}

		latencies := cfg.parseLatencies()
		expected := []*KeyValue[int, time.Duration]{
			{Key: 50, Value: 50 * time.Millisecond},
		}
		assert.Equal(t, expected, latencies)
	})

	t.Run("Ignores invalid percentile values", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50":  50 * time.Millisecond,
				"pabc": 100 * time.Millisecond,
				"p":    200 * time.Millisecond,
			},
		}

		latencies := cfg.parseLatencies()
		expected := []*KeyValue[int, time.Duration]{
			{Key: 50, Value: 50 * time.Millisecond},
		}
		assert.Equal(t, expected, latencies)
	})

	t.Run("Returns empty slice for empty latencies", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{},
		}

		latencies := cfg.parseLatencies()
		assert.Empty(t, latencies)
	})
}

func TestServiceConfig_parseErrors(t *testing.T) {
	t.Run("Parses percentile errors", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p10": 400,
				"p20": 500,
				"p30": 503,
			},
		}

		errors := cfg.parseErrors()
		expected := []*KeyValue[int, int]{
			{Key: 10, Value: 400},
			{Key: 20, Value: 500},
			{Key: 30, Value: 503},
		}
		assert.Equal(t, expected, errors)
	})

	t.Run("Sorts errors by percentile", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p30": 503,
				"p10": 400,
				"p20": 500,
			},
		}

		errors := cfg.parseErrors()
		expected := []*KeyValue[int, int]{
			{Key: 10, Value: 400},
			{Key: 20, Value: 500},
			{Key: 30, Value: 503},
		}
		assert.Equal(t, expected, errors)
	})

	t.Run("Ignores non-percentile keys", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p10":     400,
				"default": 500,
				"max":     503,
			},
		}

		errors := cfg.parseErrors()
		expected := []*KeyValue[int, int]{
			{Key: 10, Value: 400},
		}
		assert.Equal(t, expected, errors)
	})

	t.Run("Ignores invalid percentile values", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p10":  400,
				"pabc": 500,
				"p":    503,
			},
		}

		errors := cfg.parseErrors()
		expected := []*KeyValue[int, int]{
			{Key: 10, Value: 400},
		}
		assert.Equal(t, expected, errors)
	})

	t.Run("Returns empty slice for empty errors", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{},
		}

		errors := cfg.parseErrors()
		assert.Empty(t, errors)
	})
}

func TestNewCacheConfig(t *testing.T) {
	t.Run("Creates cache config with default values", func(t *testing.T) {
		cfg := NewCacheConfig()

		assert.NotNil(t, cfg)
		assert.True(t, cfg.Requests)
	})
}

func TestServiceConfig_WithDefaults(t *testing.T) {
	t.Run("Fills nil Cache with defaults", func(t *testing.T) {
		cfg := &ServiceConfig{
			Cache: nil,
		}

		result := cfg.WithDefaults()

		assert.NotNil(t, result.Cache)
		assert.True(t, result.Cache.Requests)
	})

	t.Run("Fills nil Errors map with defaults", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: nil,
		}

		result := cfg.WithDefaults()

		assert.NotNil(t, result.Errors)
		assert.Empty(t, result.Errors)
	})

	t.Run("Fills nil Latencies map with defaults", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: nil,
		}

		result := cfg.WithDefaults()

		assert.NotNil(t, result.Latencies)
		assert.Empty(t, result.Latencies)
	})

	t.Run("Does not override existing Cache", func(t *testing.T) {
		customCache := &CacheConfig{
			Requests: false,
		}
		cfg := &ServiceConfig{
			Cache: customCache,
		}

		result := cfg.WithDefaults()

		assert.Equal(t, customCache, result.Cache)
		assert.False(t, result.Cache.Requests)
	})

	t.Run("Does not override existing Errors", func(t *testing.T) {
		customErrors := map[string]int{
			"p10": 400,
			"p20": 500,
		}
		cfg := &ServiceConfig{
			Errors: customErrors,
		}

		result := cfg.WithDefaults()

		assert.Equal(t, customErrors, result.Errors)
		assert.Len(t, result.Errors, 2)
	})

	t.Run("Does not override existing Latencies", func(t *testing.T) {
		customLatencies := map[string]time.Duration{
			"p50": 50 * time.Millisecond,
			"p90": 100 * time.Millisecond,
		}
		cfg := &ServiceConfig{
			Latencies: customLatencies,
		}

		result := cfg.WithDefaults()

		assert.Equal(t, customLatencies, result.Latencies)
		assert.Len(t, result.Latencies, 2)
	})

	t.Run("Does not override existing Mount", func(t *testing.T) {
		customPrefix := "/custom-prefix"
		cfg := &ServiceConfig{
			Mount: customPrefix,
		}

		result := cfg.WithDefaults()

		assert.Equal(t, customPrefix, result.Mount)
	})

	t.Run("Parses latencies when map is provided", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50": 50 * time.Millisecond,
				"p90": 100 * time.Millisecond,
			},
		}

		result := cfg.WithDefaults()

		assert.NotNil(t, result.latencies)
		assert.Len(t, result.latencies, 2)
	})

	t.Run("Parses errors when map is provided", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p10": 400,
				"p20": 500,
			},
		}

		result := cfg.WithDefaults()

		assert.NotNil(t, result.errors)
		assert.Len(t, result.errors, 2)
	})

	t.Run("Fills all nil fields at once", func(t *testing.T) {
		cfg := &ServiceConfig{
			Name:      "test-service",
			Cache:     nil,
			Errors:    nil,
			Latencies: nil,
			Mount:     "",
		}

		result := cfg.WithDefaults()

		assert.Equal(t, "test-service", result.Name)
		assert.NotNil(t, result.Cache)
		assert.NotNil(t, result.Errors)
		assert.NotNil(t, result.Latencies)
	})

	t.Run("Returns same instance (modifies in place)", func(t *testing.T) {
		cfg := &ServiceConfig{
			Cache: nil,
		}

		result := cfg.WithDefaults()

		assert.Equal(t, cfg, result)
		assert.NotNil(t, cfg.Cache)
	})

	t.Run("Does not override Upstream (not set in defaults)", func(t *testing.T) {
		customUpstream := &UpstreamConfig{
			URL: "http://example.com",
		}
		cfg := &ServiceConfig{
			Upstream: customUpstream,
		}

		result := cfg.WithDefaults()

		assert.Equal(t, customUpstream, result.Upstream)
	})

	t.Run("Leaves Upstream nil if not set", func(t *testing.T) {
		cfg := &ServiceConfig{
			Upstream: nil,
		}

		result := cfg.WithDefaults()

		assert.Nil(t, result.Upstream)
	})
}

func TestServiceConfig_OverwriteWith(t *testing.T) {
	t.Run("Overwrites Name when other has non-empty Name", func(t *testing.T) {
		cfg := &ServiceConfig{
			Name: "original",
		}
		other := &ServiceConfig{
			Name: "overwritten",
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, "overwritten", result.Name)
	})

	t.Run("Does not overwrite Name when other has empty Name", func(t *testing.T) {
		cfg := &ServiceConfig{
			Name: "original",
		}
		other := &ServiceConfig{
			Name: "",
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, "original", result.Name)
	})

	t.Run("Overwrites Mount when other has non-empty value", func(t *testing.T) {
		cfg := &ServiceConfig{
			Mount: "/original",
		}
		other := &ServiceConfig{
			Mount: "/overwritten",
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, "/overwritten", result.Mount)
	})

	t.Run("Overwrites Upstream when other has non-nil Upstream", func(t *testing.T) {
		cfg := &ServiceConfig{
			Upstream: &UpstreamConfig{
				URL: "http://original.com",
			},
		}
		other := &ServiceConfig{
			Upstream: &UpstreamConfig{
				URL: "http://overwritten.com",
			},
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, "http://overwritten.com", result.Upstream.URL)
	})

	t.Run("Does not overwrite Upstream when other has nil Upstream", func(t *testing.T) {
		originalUpstream := &UpstreamConfig{
			URL: "http://original.com",
		}
		cfg := &ServiceConfig{
			Upstream: originalUpstream,
		}
		other := &ServiceConfig{
			Upstream: nil,
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, originalUpstream, result.Upstream)
	})

	t.Run("Overwrites Cache when other has non-nil Cache", func(t *testing.T) {
		cfg := &ServiceConfig{
			Cache: &CacheConfig{
				Requests: true,
			},
		}
		other := &ServiceConfig{
			Cache: &CacheConfig{
				Requests: false,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.False(t, result.Cache.Requests)
	})

	t.Run("Overwrites Latency when other has non-zero Latency", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latency: 100 * time.Millisecond,
		}
		other := &ServiceConfig{
			Latency: 200 * time.Millisecond,
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, 200*time.Millisecond, result.Latency)
	})

	t.Run("Does not overwrite Latency when other has zero Latency", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latency: 100 * time.Millisecond,
		}
		other := &ServiceConfig{
			Latency: 0,
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, 100*time.Millisecond, result.Latency)
	})

	t.Run("Merges Latencies maps", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50": 50 * time.Millisecond,
				"p90": 100 * time.Millisecond,
			},
		}
		other := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p90": 150 * time.Millisecond, // Override existing
				"p99": 200 * time.Millisecond, // Add new
			},
		}

		result := cfg.OverwriteWith(other)

		assert.Len(t, result.Latencies, 3)
		assert.Equal(t, 50*time.Millisecond, result.Latencies["p50"])
		assert.Equal(t, 150*time.Millisecond, result.Latencies["p90"]) // Overwritten
		assert.Equal(t, 200*time.Millisecond, result.Latencies["p99"]) // Added
	})

	t.Run("Merges Errors maps", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p10": 400,
				"p20": 500,
			},
		}
		other := &ServiceConfig{
			Errors: map[string]int{
				"p20": 503, // Override existing
				"p30": 502, // Add new
			},
		}

		result := cfg.OverwriteWith(other)

		assert.Len(t, result.Errors, 3)
		assert.Equal(t, 400, result.Errors["p10"])
		assert.Equal(t, 503, result.Errors["p20"]) // Overwritten
		assert.Equal(t, 502, result.Errors["p30"]) // Added
	})

	t.Run("Creates Latencies map if nil before merge", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: nil,
		}
		other := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50": 50 * time.Millisecond,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.NotNil(t, result.Latencies)
		assert.Len(t, result.Latencies, 1)
		assert.Equal(t, 50*time.Millisecond, result.Latencies["p50"])
	})

	t.Run("Creates Errors map if nil before merge", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: nil,
		}
		other := &ServiceConfig{
			Errors: map[string]int{
				"p10": 400,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.NotNil(t, result.Errors)
		assert.Len(t, result.Errors, 1)
		assert.Equal(t, 400, result.Errors["p10"])
	})

	t.Run("Re-parses latencies after merge", func(t *testing.T) {
		cfg := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p50": 50 * time.Millisecond,
			},
			latencies: []*KeyValue[int, time.Duration]{
				{Key: 50, Value: 50 * time.Millisecond},
			},
		}
		other := &ServiceConfig{
			Latencies: map[string]time.Duration{
				"p90": 100 * time.Millisecond,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.NotNil(t, result.latencies)
		assert.Len(t, result.latencies, 2) // Should have both p50 and p90
	})

	t.Run("Re-parses errors after merge", func(t *testing.T) {
		cfg := &ServiceConfig{
			Errors: map[string]int{
				"p10": 400,
			},
			errors: []*KeyValue[int, int]{
				{Key: 10, Value: 400},
			},
		}
		other := &ServiceConfig{
			Errors: map[string]int{
				"p20": 500,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.NotNil(t, result.errors)
		assert.Len(t, result.errors, 2) // Should have both p10 and p20
	})

	t.Run("Returns same instance (modifies in place)", func(t *testing.T) {
		cfg := &ServiceConfig{
			Name: "original",
		}
		other := &ServiceConfig{
			Name: "overwritten",
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, cfg, result)
		assert.Equal(t, "overwritten", cfg.Name)
	})

	t.Run("Handles nil other gracefully", func(t *testing.T) {
		cfg := &ServiceConfig{
			Name: "original",
		}

		result := cfg.OverwriteWith(nil)

		assert.Equal(t, cfg, result)
		assert.Equal(t, "original", result.Name)
	})

	t.Run("Overwrites multiple fields at once", func(t *testing.T) {
		cfg := &ServiceConfig{
			Name:    "original",
			Latency: 100 * time.Millisecond,
			Mount:   "/original",
		}
		other := &ServiceConfig{
			Name:    "overwritten",
			Latency: 200 * time.Millisecond,
			Mount:   "/overwritten",
			Cache: &CacheConfig{
				Requests: false,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, "overwritten", result.Name)
		assert.Equal(t, 200*time.Millisecond, result.Latency)
		assert.Equal(t, "/overwritten", result.Mount)
		assert.NotNil(t, result.Cache)
		assert.False(t, result.Cache.Requests)
	})

	t.Run("Overwrites History when other has non-nil History", func(t *testing.T) {
		cfg := &ServiceConfig{
			History: &HistoryConfig{
				MaskHeaders: []string{"Authorization"},
			},
		}
		other := &ServiceConfig{
			History: &HistoryConfig{
				MaskHeaders: []string{"X-Api-Key"},
			},
		}

		result := cfg.OverwriteWith(other)

		assert.Equal(t, []string{"X-Api-Key"}, result.History.MaskHeaders)
	})

	t.Run("Overwrites SpecOptions when other has non-nil SpecOptions", func(t *testing.T) {
		cfg := &ServiceConfig{
			SpecOptions: nil,
		}
		other := &ServiceConfig{
			SpecOptions: &SpecOptions{
				LazyLoad: true,
				Simplify: true,
			},
		}

		result := cfg.OverwriteWith(other)

		assert.NotNil(t, result.SpecOptions)
		assert.True(t, result.SpecOptions.LazyLoad)
		assert.True(t, result.SpecOptions.Simplify)
	})
}

func TestEndpointConfig_GetLatency(t *testing.T) {
	t.Run("Returns fixed latency when no percentiles defined", func(t *testing.T) {
		ep := &EndpointConfig{
			Latency: 500 * time.Millisecond,
		}
		assert.Equal(t, 500*time.Millisecond, ep.GetLatency())
	})

	t.Run("Returns 0 when nothing configured", func(t *testing.T) {
		ep := &EndpointConfig{}
		assert.Equal(t, time.Duration(0), ep.GetLatency())
	})

	t.Run("Returns latency based on percentiles", func(t *testing.T) {
		ep := &EndpointConfig{
			latencies: []*KeyValue[int, time.Duration]{
				{Key: 50, Value: 50 * time.Millisecond},
				{Key: 100, Value: 200 * time.Millisecond},
			},
		}

		seen := make(map[time.Duration]bool)
		for i := 0; i < 100; i++ {
			seen[ep.GetLatency()] = true
		}
		assert.True(t, seen[50*time.Millisecond])
		assert.True(t, seen[200*time.Millisecond])
	})

	t.Run("Returns 0 when percentiles don't reach 100", func(t *testing.T) {
		// rand is 1..100; with the largest declared key at 50, half the
		// rolls fall through and the function returns 0 (the implicit
		// "no extra latency" remainder).
		ep := &EndpointConfig{
			latencies: []*KeyValue[int, time.Duration]{
				{Key: 50, Value: 50 * time.Millisecond},
			},
		}

		seen := make(map[time.Duration]bool)
		for i := 0; i < 200; i++ {
			seen[ep.GetLatency()] = true
		}
		assert.True(t, seen[50*time.Millisecond])
		assert.True(t, seen[0], "expected at least one roll past the 50%% bucket")
	})
}

func TestEndpointConfig_GetError(t *testing.T) {
	t.Run("Returns 0 when no errors defined", func(t *testing.T) {
		ep := &EndpointConfig{}
		assert.Equal(t, 0, ep.GetError())
	})

	t.Run("Returns error based on percentiles", func(t *testing.T) {
		ep := &EndpointConfig{
			errors: []*KeyValue[int, int]{
				{Key: 50, Value: 500},
				{Key: 100, Value: 503},
			},
		}

		seen := make(map[int]bool)
		for i := 0; i < 100; i++ {
			seen[ep.GetError()] = true
		}
		assert.True(t, seen[500])
		assert.True(t, seen[503])
	})
}

func TestServiceConfig_GetEndpointConfig(t *testing.T) {
	t.Run("Returns nil when no endpoints defined", func(t *testing.T) {
		cfg := &ServiceConfig{}
		assert.Nil(t, cfg.GetEndpointConfig("/foo", "GET"))
	})

	t.Run("Returns nil for non-matching path", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 100 * time.Millisecond}},
			},
		}
		assert.Nil(t, cfg.GetEndpointConfig("/bar", "GET"))
	})

	t.Run("Returns nil for non-matching method", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 100 * time.Millisecond}},
			},
		}
		assert.Nil(t, cfg.GetEndpointConfig("/foo", "POST"))
	})

	t.Run("Matches exact path and method", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 100 * time.Millisecond}},
			},
		}
		ep := cfg.GetEndpointConfig("/foo", "GET")
		assert.NotNil(t, ep)
		assert.Equal(t, 100*time.Millisecond, ep.Latency)
	})

	t.Run("Matches parameterized path", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/pets/{id}": {"GET": {Latency: 200 * time.Millisecond}},
			},
		}
		ep := cfg.GetEndpointConfig("/pets/123", "GET")
		assert.NotNil(t, ep)
		assert.Equal(t, 200*time.Millisecond, ep.Latency)
	})

	t.Run("Different methods on same path", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {
					"GET":  {Latency: 100 * time.Millisecond},
					"POST": {Latency: 500 * time.Millisecond},
				},
			},
		}
		assert.Equal(t, 100*time.Millisecond, cfg.GetEndpointConfig("/foo", "GET").Latency)
		assert.Equal(t, 500*time.Millisecond, cfg.GetEndpointConfig("/foo", "POST").Latency)
	})

	t.Run("Nil methods map returns empty endpoint config", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": nil,
			},
		}
		ep := cfg.GetEndpointConfig("/foo", "GET")
		assert.NotNil(t, ep)
		assert.Equal(t, time.Duration(0), ep.Latency)
	})

	t.Run("Nil endpoint config returns empty endpoint config", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": nil},
			},
		}
		ep := cfg.GetEndpointConfig("/foo", "GET")
		assert.NotNil(t, ep)
		assert.Equal(t, time.Duration(0), ep.Latency)
	})

	t.Run("Method keys are case-insensitive", func(t *testing.T) {
		yamlData := []byte(`
endpoints:
  /foo:
    get:
      latency: 100ms
    Post:
      latency: 200ms
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)

		ep := cfg.GetEndpointConfig("/foo", "GET")
		assert.NotNil(t, ep)
		assert.Equal(t, 100*time.Millisecond, ep.Latency)

		ep = cfg.GetEndpointConfig("/foo", "POST")
		assert.NotNil(t, ep)
		assert.Equal(t, 200*time.Millisecond, ep.Latency)
	})
}

func TestServiceConfig_EndpointsYAML(t *testing.T) {
	t.Run("Parses endpoints from YAML", func(t *testing.T) {
		yamlData := []byte(`
latency: 50ms
endpoints:
  /foo/bar:
    GET:
      latency: 500ms
    POST:
      latencies:
        p50: 100ms
        p90: 500ms
      errors:
        p10: 500
  /health:
    GET:
      latency: 0ms
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.Equal(t, 50*time.Millisecond, cfg.Latency)

		assert.Len(t, cfg.Endpoints, 2)

		fooGet := cfg.Endpoints["/foo/bar"]["GET"]
		assert.Equal(t, 500*time.Millisecond, fooGet.Latency)

		fooPost := cfg.Endpoints["/foo/bar"]["POST"]
		assert.Len(t, fooPost.Latencies, 2)
		assert.Len(t, fooPost.Errors, 1)
		assert.NotNil(t, fooPost.latencies)
		assert.NotNil(t, fooPost.errors)

		healthGet := cfg.Endpoints["/health"]["GET"]
		assert.Equal(t, time.Duration(0), healthGet.Latency)
	})

	t.Run("Parses endpoints with parameterized paths", func(t *testing.T) {
		yamlData := []byte(`
endpoints:
  /pets/{id}:
    GET:
      latency: 200ms
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.Len(t, cfg.Endpoints, 1)

		ep := cfg.GetEndpointConfig("/pets/42", "GET")
		assert.NotNil(t, ep)
		assert.Equal(t, 200*time.Millisecond, ep.Latency)
	})
}

func TestServiceConfig_WithDefaults_Endpoints(t *testing.T) {
	t.Run("Parses endpoint latencies and errors", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {
					"GET": {
						Latencies: map[string]time.Duration{"p50": 50 * time.Millisecond},
						Errors:    map[string]int{"p10": 400},
					},
				},
			},
		}
		cfg.WithDefaults()

		ep := cfg.Endpoints["/foo"]["GET"]
		assert.Len(t, ep.latencies, 1)
		assert.Len(t, ep.errors, 1)
	})

	t.Run("Skips nil endpoint configs", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": nil},
			},
		}
		assert.NotPanics(t, func() { cfg.WithDefaults() })
	})
}

func TestServiceConfig_OverwriteWith_Endpoints(t *testing.T) {
	t.Run("Merges endpoints from other", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {
					"GET": {Latency: 100 * time.Millisecond},
				},
			},
		}
		other := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {
					"POST": {Latency: 200 * time.Millisecond},
				},
				"/bar": {
					"GET": {Latency: 300 * time.Millisecond},
				},
			},
		}

		result := cfg.OverwriteWith(other)

		assert.Len(t, result.Endpoints, 2)
		assert.Equal(t, 100*time.Millisecond, result.Endpoints["/foo"]["GET"].Latency)
		assert.Equal(t, 200*time.Millisecond, result.Endpoints["/foo"]["POST"].Latency)
		assert.Equal(t, 300*time.Millisecond, result.Endpoints["/bar"]["GET"].Latency)
	})

	t.Run("Other endpoint overrides same path+method", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 100 * time.Millisecond}},
			},
		}
		other := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 999 * time.Millisecond}},
			},
		}

		result := cfg.OverwriteWith(other)
		assert.Equal(t, 999*time.Millisecond, result.Endpoints["/foo"]["GET"].Latency)
	})

	t.Run("Creates endpoints map if nil before merge", func(t *testing.T) {
		cfg := &ServiceConfig{}
		other := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 100 * time.Millisecond}},
			},
		}

		result := cfg.OverwriteWith(other)
		assert.Len(t, result.Endpoints, 1)
	})

	t.Run("Does not touch endpoints when other has nil", func(t *testing.T) {
		cfg := &ServiceConfig{
			Endpoints: map[string]map[string]*EndpointConfig{
				"/foo": {"GET": {Latency: 100 * time.Millisecond}},
			},
		}
		other := &ServiceConfig{}

		result := cfg.OverwriteWith(other)
		assert.Len(t, result.Endpoints, 1)
	})
}

func TestOptionalProperties_UnmarshalYAML(t *testing.T) {
	t.Run("parses min and max", func(t *testing.T) {
		yamlData := []byte(`
min: 1
max: 6
`)
		var opts OptionalProperties
		err := yaml.Unmarshal(yamlData, &opts)

		assert.NoError(t, err)
		assert.Equal(t, 1, opts.Min)
		assert.Equal(t, 6, opts.Max)
	})

	t.Run("parses only min", func(t *testing.T) {
		yamlData := []byte(`
min: 5
`)
		var opts OptionalProperties
		err := yaml.Unmarshal(yamlData, &opts)

		assert.NoError(t, err)
		assert.Equal(t, 5, opts.Min)
		assert.Equal(t, 0, opts.Max)
	})

	t.Run("parses only max", func(t *testing.T) {
		yamlData := []byte(`
max: 10
`)
		var opts OptionalProperties
		err := yaml.Unmarshal(yamlData, &opts)

		assert.NoError(t, err)
		assert.Equal(t, 0, opts.Min)
		assert.Equal(t, 10, opts.Max)
	})
}

func TestExtractPathValues(t *testing.T) {
	tests := []struct {
		name     string
		request  string
		pattern  string
		expected map[string]string
	}{
		{"single variable", "/pay/credit-card", "/pay/{paymentMethodName}", map[string]string{"paymentMethodName": "credit-card"}},
		{"multiple variables", "/pay/credit-card/tx/123", "/pay/{paymentMethodName}/tx/{txId}", map[string]string{"paymentMethodName": "credit-card", "txId": "123"}},
		{"no variables", "/foo/bar", "/foo/bar", map[string]string{}},
		{"all variables", "/a/b/c", "/{x}/{y}/{z}", map[string]string{"x": "a", "y": "b", "z": "c"}},
		{"length mismatch", "/foo/bar/baz", "/foo/{id}", nil},
		{"segment mismatch", "/foo/bar", "/baz/{id}", nil},
		{"trailing slash", "/pay/credit-card/", "/pay/{paymentMethodName}", map[string]string{"paymentMethodName": "credit-card"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ExtractPathValues(tt.request, tt.pattern))
		})
	}
}

func TestReplayConfig_WithDefaults(t *testing.T) {
	t.Run("sets TTL to default when zero", func(t *testing.T) {
		rc := &ReplayConfig{}
		rc.WithDefaults()
		assert.Equal(t, DefaultReplayTTL, rc.TTL)
	})

	t.Run("does not override non-zero TTL", func(t *testing.T) {
		rc := &ReplayConfig{TTL: 1 * time.Hour}
		rc.WithDefaults()
		assert.Equal(t, 1*time.Hour, rc.TTL)
	})
}

func TestReplayMatch_AllFields(t *testing.T) {
	t.Run("nil receiver returns nil", func(t *testing.T) {
		var rm *ReplayMatch
		assert.Nil(t, rm.AllFields())
	})

	t.Run("empty match returns empty slice", func(t *testing.T) {
		rm := &ReplayMatch{}
		assert.Equal(t, []string{}, rm.AllFields())
	})

	t.Run("concatenates path, body, query in order", func(t *testing.T) {
		// Order matters because callers use the concatenation to build
		// stable replay keys (a swap in input position would create a
		// different bucket for what should be the same request).
		rm := &ReplayMatch{
			Path:  []string{"id"},
			Body:  []string{"data.name", "data.email"},
			Query: []string{"tenant", "region"},
		}
		assert.Equal(t, []string{"id", "data.name", "data.email", "tenant", "region"}, rm.AllFields())
	})
}

func TestReplayConfig_GetEndpoint(t *testing.T) {
	rc := &ReplayConfig{
		Endpoints: map[string]map[string]*ReplayEndpoint{
			"/foo/{f-id}/bar/{b-id}": {
				"POST": {Match: &ReplayMatch{Body: []string{"data.name"}}},
			},
			"/simple": {
				"GET": {Match: &ReplayMatch{Query: []string{"q"}}},
			},
		},
	}

	t.Run("matches parameterized path", func(t *testing.T) {
		pattern, ep := rc.GetEndpoint("/foo/123/bar/456", "POST")
		assert.Equal(t, "/foo/{f-id}/bar/{b-id}", pattern)
		assert.NotNil(t, ep)
		assert.Equal(t, []string{"data.name"}, ep.Match.Body)
	})

	t.Run("matches simple path", func(t *testing.T) {
		pattern, ep := rc.GetEndpoint("/simple", "GET")
		assert.Equal(t, "/simple", pattern)
		assert.NotNil(t, ep)
	})

	t.Run("returns nil for wrong method", func(t *testing.T) {
		pattern, ep := rc.GetEndpoint("/foo/123/bar/456", "GET")
		assert.Equal(t, "", pattern)
		assert.Nil(t, ep)
	})

	t.Run("returns nil for non-matching path", func(t *testing.T) {
		pattern, ep := rc.GetEndpoint("/unknown/path", "POST")
		assert.Equal(t, "", pattern)
		assert.Nil(t, ep)
	})

	t.Run("returns nil for nil config", func(t *testing.T) {
		var nilRC *ReplayConfig
		pattern, ep := nilRC.GetEndpoint("/foo", "GET")
		assert.Equal(t, "", pattern)
		assert.Nil(t, ep)
	})

	t.Run("returns nil for nil endpoints", func(t *testing.T) {
		rc := &ReplayConfig{}
		pattern, ep := rc.GetEndpoint("/foo", "GET")
		assert.Equal(t, "", pattern)
		assert.Nil(t, ep)
	})

	t.Run("path only matches any method with no match fields", func(t *testing.T) {
		rc := &ReplayConfig{
			Endpoints: map[string]map[string]*ReplayEndpoint{
				"/pay": nil,
			},
		}
		pattern, ep := rc.GetEndpoint("/pay", "POST")
		assert.Equal(t, "/pay", pattern)
		assert.NotNil(t, ep)
		assert.Nil(t, ep.Match)

		pattern, ep = rc.GetEndpoint("/pay", "GET")
		assert.Equal(t, "/pay", pattern)
		assert.NotNil(t, ep)
	})

	t.Run("path with method but no match fields", func(t *testing.T) {
		rc := &ReplayConfig{
			Endpoints: map[string]map[string]*ReplayEndpoint{
				"/pay": {"POST": nil},
			},
		}
		pattern, ep := rc.GetEndpoint("/pay", "POST")
		assert.Equal(t, "/pay", pattern)
		assert.NotNil(t, ep)
		assert.Nil(t, ep.Match)

		_, ep = rc.GetEndpoint("/pay", "GET")
		assert.Nil(t, ep)
	})

	t.Run("multiple methods require exact match", func(t *testing.T) {
		rc := &ReplayConfig{
			Endpoints: map[string]map[string]*ReplayEndpoint{
				"/pay": {
					"POST": {Match: &ReplayMatch{Body: []string{"reference", "amount"}}},
					"GET":  {Match: &ReplayMatch{Query: []string{"reference"}}},
				},
			},
		}
		_, ep := rc.GetEndpoint("/pay", "POST")
		assert.Equal(t, []string{"reference", "amount"}, ep.Match.Body)

		_, ep = rc.GetEndpoint("/pay", "GET")
		assert.Equal(t, []string{"reference"}, ep.Match.Query)

		_, ep = rc.GetEndpoint("/pay", "PUT")
		assert.Nil(t, ep)
	})

	t.Run("method keys are case-insensitive via WithDefaults", func(t *testing.T) {
		yamlData := []byte(`
cache:
  replay:
    endpoints:
      /pay:
        post:
          match:
            body:
              - reference
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)

		_, ep := cfg.Cache.Replay.GetEndpoint("/pay", "POST")
		assert.NotNil(t, ep)
		assert.Equal(t, []string{"reference"}, ep.Match.Body)
	})
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		pattern  string
		expected bool
	}{
		{"exact match", "/foo/bar", "/foo/bar", true},
		{"param match", "/foo/123/bar/456", "/foo/{id}/bar/{bid}", true},
		{"length mismatch", "/foo/bar/baz", "/foo/bar", false},
		{"segment mismatch", "/foo/baz", "/foo/bar", false},
		{"trailing slash ignored", "/foo/bar/", "/foo/bar", true},
		{"leading slash ignored", "foo/bar", "/foo/bar", true},
		{"all params", "/a/b/c", "/{x}/{y}/{z}", true},
		{"empty segments", "/", "/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, matchesPattern(tt.path, tt.pattern))
		})
	}
}

func TestCacheConfig_ReplayParsing(t *testing.T) {
	t.Run("parses replay config from YAML", func(t *testing.T) {
		yamlData := []byte(`
cache:
  requests: true
  replay:
    ttl: 12h
    upstream-only: true
    endpoints:
      /foo/{f-id}/bar/{b-id}:
        POST:
          match:
            body:
              - data.name
              - data.address.zip
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.NotNil(t, cfg.Cache)
		assert.NotNil(t, cfg.Cache.Replay)
		assert.Equal(t, 12*time.Hour, cfg.Cache.Replay.TTL)
		assert.True(t, cfg.Cache.Replay.UpstreamOnly)
		assert.Len(t, cfg.Cache.Replay.Endpoints, 1)

		methods := cfg.Cache.Replay.Endpoints["/foo/{f-id}/bar/{b-id}"]
		assert.NotNil(t, methods)
		assert.NotNil(t, methods["POST"])
		assert.Equal(t, []string{"data.name", "data.address.zip"}, methods["POST"].Match.Body)
	})

	t.Run("parses replay config with query match", func(t *testing.T) {
		yamlData := []byte(`
cache:
  replay:
    endpoints:
      /search:
        GET:
          match:
            query:
              - q
              - page
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		ep := cfg.Cache.Replay.Endpoints["/search"]["GET"]
		assert.NotNil(t, ep)
		assert.Equal(t, []string{"q", "page"}, ep.Match.Query)
		assert.Empty(t, ep.Match.Body)
	})

	t.Run("parses replay config without optional fields", func(t *testing.T) {
		yamlData := []byte(`
cache:
  requests: true
  replay:
    endpoints:
      /test:
        GET:
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.NotNil(t, cfg.Cache.Replay)
		assert.Equal(t, time.Duration(0), cfg.Cache.Replay.TTL)
		assert.False(t, cfg.Cache.Replay.UpstreamOnly)
	})

	t.Run("nil replay when not specified", func(t *testing.T) {
		yamlData := []byte(`
cache:
  requests: true
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.Nil(t, cfg.Cache.Replay)
	})

	t.Run("parses auto-replay", func(t *testing.T) {
		yamlData := []byte(`
cache:
  replay:
    auto-replay: true
    endpoints:
      /foo:
        POST:
          match:
            body:
              - name
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.True(t, cfg.Cache.Replay.AutoReplay)
	})

	t.Run("parses replay config with path match", func(t *testing.T) {
		yamlData := []byte(`
cache:
  replay:
    endpoints:
      /pay/{paymentMethodName}/tx/{txId}:
        POST:
          match:
            path:
              - paymentMethodName
            body:
              - reference
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		ep := cfg.Cache.Replay.Endpoints["/pay/{paymentMethodName}/tx/{txId}"]["POST"]
		assert.NotNil(t, ep)
		assert.Equal(t, []string{"paymentMethodName"}, ep.Match.Path)
		assert.Equal(t, []string{"reference"}, ep.Match.Body)
	})

	t.Run("auto-replay defaults to false", func(t *testing.T) {
		yamlData := []byte(`
cache:
  replay:
    endpoints:
      /foo:
        POST:
          match:
            body:
              - name
`)
		cfg, err := NewServiceConfigFromBytes(yamlData)
		assert.NoError(t, err)
		assert.False(t, cfg.Cache.Replay.AutoReplay)
	})
}

func TestHistoryEnabled(t *testing.T) {
	t.Run("true when History is nil", func(t *testing.T) {
		cfg := &ServiceConfig{}
		assert.True(t, cfg.HistoryEnabled())
	})

	t.Run("true when Enabled is nil", func(t *testing.T) {
		cfg := &ServiceConfig{History: &HistoryConfig{}}
		assert.True(t, cfg.HistoryEnabled())
	})

	t.Run("true when explicitly enabled", func(t *testing.T) {
		enabled := true
		cfg := &ServiceConfig{History: &HistoryConfig{Enabled: &enabled}}
		assert.True(t, cfg.HistoryEnabled())
	})

	t.Run("false when explicitly disabled", func(t *testing.T) {
		disabled := false
		cfg := &ServiceConfig{History: &HistoryConfig{Enabled: &disabled}}
		assert.False(t, cfg.HistoryEnabled())
	})
}

func TestHistoryConfig_UnmarshalYAML(t *testing.T) {
	t.Run("boolean shorthand false", func(t *testing.T) {
		yamlData := []byte(`history: false`)
		var cfg ServiceConfig
		err := yaml.Unmarshal(yamlData, &cfg)

		assert.NoError(t, err)
		assert.NotNil(t, cfg.History)
		assert.NotNil(t, cfg.History.Enabled)
		assert.False(t, *cfg.History.Enabled)
	})

	t.Run("boolean shorthand true", func(t *testing.T) {
		yamlData := []byte(`history: true`)
		var cfg ServiceConfig
		err := yaml.Unmarshal(yamlData, &cfg)

		assert.NoError(t, err)
		assert.NotNil(t, cfg.History)
		assert.NotNil(t, cfg.History.Enabled)
		assert.True(t, *cfg.History.Enabled)
	})

	t.Run("object form with mask-headers", func(t *testing.T) {
		yamlData := []byte(`
history:
  enabled: true
  mask-headers:
    - Authorization
    - X-Secret-*
`)
		var cfg ServiceConfig
		err := yaml.Unmarshal(yamlData, &cfg)
		assert.NoError(t, err)
		assert.NotNil(t, cfg.History)
		assert.True(t, *cfg.History.Enabled)
		assert.Equal(t, []string{"Authorization", "X-Secret-*"}, cfg.History.MaskHeaders)
	})

	t.Run("object form without enabled defaults to nil", func(t *testing.T) {
		yamlData := []byte(`
history:
  mask-headers:
    - Authorization
`)
		var cfg ServiceConfig
		err := yaml.Unmarshal(yamlData, &cfg)

		assert.NoError(t, err)
		assert.NotNil(t, cfg.History)
		assert.Nil(t, cfg.History.Enabled)
		assert.Equal(t, []string{"Authorization"}, cfg.History.MaskHeaders)
	})
}

func TestWithDefaults_History(t *testing.T) {
	t.Run("sets default history when nil", func(t *testing.T) {
		cfg := &ServiceConfig{}
		cfg.WithDefaults()

		assert.NotNil(t, cfg.History)
		assert.Equal(t, defaultMaskHeaders, cfg.History.MaskHeaders)
	})

	t.Run("fills default mask-headers when empty and enabled", func(t *testing.T) {
		cfg := &ServiceConfig{History: &HistoryConfig{}}
		cfg.WithDefaults()
		assert.Equal(t, defaultMaskHeaders, cfg.History.MaskHeaders)
	})

	t.Run("preserves custom mask-headers", func(t *testing.T) {
		cfg := &ServiceConfig{
			History: &HistoryConfig{
				MaskHeaders: []string{"X-Custom"},
			},
		}
		cfg.WithDefaults()
		assert.Equal(t, []string{"X-Custom"}, cfg.History.MaskHeaders)
	})

	t.Run("skips default mask-headers when disabled", func(t *testing.T) {
		disabled := false
		cfg := &ServiceConfig{
			History: &HistoryConfig{Enabled: &disabled},
		}
		cfg.WithDefaults()
		assert.Nil(t, cfg.History.MaskHeaders)
	})
}

func TestValidateConfig_Defaults(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	t.Run("nil config: both off", func(t *testing.T) {
		var v *ValidateConfig
		assert.False(t, v.RequestEnabled())
		assert.False(t, v.ResponseEnabled())
		assert.False(t, v.VerboseEnabled())
	})

	t.Run("empty config: same as nil", func(t *testing.T) {
		v := &ValidateConfig{}
		assert.False(t, v.RequestEnabled())
		assert.False(t, v.ResponseEnabled())
		assert.False(t, v.VerboseEnabled())
	})

	t.Run("verbose toggles independently", func(t *testing.T) {
		v := &ValidateConfig{Verbose: boolPtr(true)}
		assert.True(t, v.VerboseEnabled())
		v.Verbose = boolPtr(false)
		assert.False(t, v.VerboseEnabled())
	})

	t.Run("explicit request=true enables request", func(t *testing.T) {
		v := &ValidateConfig{Request: boolPtr(true)}
		assert.True(t, v.RequestEnabled())
		assert.False(t, v.ResponseEnabled())
	})

	t.Run("explicit response=true enables response, request stays off", func(t *testing.T) {
		v := &ValidateConfig{Response: boolPtr(true)}
		assert.False(t, v.RequestEnabled())
		assert.True(t, v.ResponseEnabled())
	})

	t.Run("both explicit", func(t *testing.T) {
		v := &ValidateConfig{Request: boolPtr(true), Response: boolPtr(true)}
		assert.True(t, v.RequestEnabled())
		assert.True(t, v.ResponseEnabled())
	})
}

func TestValidateConfig_TimeoutOrDefault(t *testing.T) {
	durPtr := func(d time.Duration) *time.Duration { return &d }

	t.Run("nil config falls back to default", func(t *testing.T) {
		var v *ValidateConfig
		assert.Equal(t, DefaultValidationTimeout, v.TimeoutOrDefault())
	})

	t.Run("nil timeout falls back to default", func(t *testing.T) {
		v := &ValidateConfig{}
		assert.Equal(t, DefaultValidationTimeout, v.TimeoutOrDefault())
	})

	t.Run("explicit positive timeout wins", func(t *testing.T) {
		v := &ValidateConfig{Timeout: durPtr(7 * time.Second)}
		assert.Equal(t, 7*time.Second, v.TimeoutOrDefault())
	})

	t.Run("zero timeout falls back to default", func(t *testing.T) {
		v := &ValidateConfig{Timeout: durPtr(0)}
		assert.Equal(t, DefaultValidationTimeout, v.TimeoutOrDefault())
	})

	t.Run("negative timeout falls back to default", func(t *testing.T) {
		v := &ValidateConfig{Timeout: durPtr(-1 * time.Second)}
		assert.Equal(t, DefaultValidationTimeout, v.TimeoutOrDefault())
	})
}

func TestServiceConfig_OverwriteWith_Validate(t *testing.T) {
	// OverwriteWith should adopt a non-nil Validate from `other`.
	// Per-folder configs need this to opt response validation on (or
	// request validation off) over the runtime defaults.
	boolPtr := func(b bool) *bool { return &b }

	t.Run("adopts non-nil validation from other", func(t *testing.T) {
		base := NewServiceConfig()
		other := &ServiceConfig{
			Validate: &ValidateConfig{Response: boolPtr(true)},
		}
		base.OverwriteWith(other)
		assert.NotNil(t, base.Validate)
		assert.True(t, base.Validate.ResponseEnabled())
	})

	t.Run("preserves base validation when other has nil", func(t *testing.T) {
		base := NewServiceConfig()
		base.Validate = &ValidateConfig{Request: boolPtr(false)}
		base.OverwriteWith(&ServiceConfig{})
		assert.NotNil(t, base.Validate)
		assert.False(t, base.Validate.RequestEnabled())
	})
}
