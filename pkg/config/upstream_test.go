package config

import (
	"fmt"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestHTTPStatusConfig_Is(t *testing.T) {
	assert := assert2.New(t)
	type testcase struct {
		received int
		cfg      *HTTPStatusConfig
		expected bool
	}

	testcases := []testcase{
		{received: 400, cfg: &HTTPStatusConfig{Exact: 400}, expected: true},
		{received: 401, cfg: &HTTPStatusConfig{Exact: 400}, expected: false},
		{received: 400, cfg: &HTTPStatusConfig{Range: "400-404"}, expected: true},
		{received: 400, cfg: &HTTPStatusConfig{Range: "500-600"}, expected: false},
	}

	for i, tc := range testcases {
		t.Run(fmt.Sprintf("case %d", i+1), func(t *testing.T) {
			assert.Equal(tc.expected, tc.cfg.Is(tc.received))
		})
	}

	t.Run("invalid range", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Range: "400-"}
		assert.False(cfg.Is(401))
	})

	t.Run("except excludes from range", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Range: "400-499", Except: []int{401, 403}}
		testCases := []struct {
			status   int
			expected bool
		}{
			{400, true},
			{401, false},
			{402, true},
			{403, false},
			{404, true},
			{499, true},
			{500, false},
		}
		for _, tc := range testCases {
			assert.Equal(tc.expected, cfg.Is(tc.status))
		}
	})

	t.Run("except excludes from exact", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Exact: 400, Except: []int{400}}
		assert.False(cfg.Is(400))
	})

	t.Run("range boundaries", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Range: "400-404"}
		testCases := []struct {
			status   int
			expected bool
		}{
			{400, true},  // lower boundary
			{404, true},  // upper boundary
			{402, true},  // middle
			{399, false}, // below range
			{405, false}, // above range
		}
		for _, tc := range testCases {
			assert.Equal(tc.expected, cfg.Is(tc.status))
		}
	})

	t.Run("both exact and range can match", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Exact: 400, Range: "500-600"}
		testCases := []struct {
			status   int
			expected bool
		}{
			{400, true},  // matches exact
			{500, true},  // matches range
			{550, true},  // matches range
			{450, false}, // matches neither
		}
		for _, tc := range testCases {
			assert.Equal(tc.expected, cfg.Is(tc.status))
		}
	})

	t.Run("no match", func(t *testing.T) {
		cfg := &HTTPStatusConfig{}
		assert.False(cfg.Is(400))
	})

	t.Run("invalid range format - single number", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Range: "400"}
		assert.False(cfg.Is(400))
	})

	t.Run("invalid range format - non-numeric", func(t *testing.T) {
		cfg := &HTTPStatusConfig{Range: "abc-def"}
		assert.False(cfg.Is(400))
	})
}

func TestHTTPStatusMatchConfig_Is(t *testing.T) {
	assert := assert2.New(t)

	t.Run("single", func(t *testing.T) {
		cfg := HTTPStatusMatchConfig{
			{Exact: 400},
		}
		assert.True(cfg.Is(400))
		assert.False(cfg.Is(401))
	})

	t.Run("range", func(t *testing.T) {
		cfg := HTTPStatusMatchConfig{
			{Range: "400-404"},
		}
		assert.True(cfg.Is(400))
		assert.True(cfg.Is(404))
		assert.False(cfg.Is(405))
	})

	t.Run("multiple", func(t *testing.T) {
		cfg := HTTPStatusMatchConfig{
			{Exact: 400},
			{Range: "500-600"},
		}
		assert.True(cfg.Is(400))
		assert.True(cfg.Is(500))
		assert.True(cfg.Is(501))
		assert.True(cfg.Is(600))
		assert.False(cfg.Is(401))
	})

	t.Run("empty config", func(t *testing.T) {
		cfg := HTTPStatusMatchConfig{}
		assert.False(cfg.Is(400))
		assert.False(cfg.Is(500))
	})

	t.Run("overlapping ranges", func(t *testing.T) {
		cfg := HTTPStatusMatchConfig{
			{Range: "400-450"},
			{Range: "440-500"},
		}
		assert.True(cfg.Is(400))
		assert.True(cfg.Is(445)) // in both ranges
		assert.True(cfg.Is(500))
		assert.False(cfg.Is(350))
		assert.False(cfg.Is(550))
	})
}
