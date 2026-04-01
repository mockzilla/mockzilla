package config

import (
	"strconv"
	"strings"
	"time"
)

type UpstreamConfig struct {
	URL     string            `yaml:"url"`
	Timeout time.Duration     `yaml:"timeout"`
	Headers map[string]string `yaml:"headers"`

	// FailOn defines which upstream HTTP status codes should be returned immediately
	// to the client without falling back to the generator.
	// nil (omitted): uses default (400-499 except 401, 403). Set to empty list (fail-on: []) to disable.
	FailOn *HTTPStatusMatchConfig `yaml:"fail-on"`
}

// DefaultFailOnStatus is the default fail-on config applied when FailOn is nil.
// Most 4xx errors indicate client-side problems that the generator cannot fix.
// 401/403 are excluded because they typically indicate missing/invalid credentials
// in the proxy setup, not a real client error.
var DefaultFailOnStatus = HTTPStatusMatchConfig{
	{Range: "400-499", Except: []int{401, 403}},
}

// DefaultUpstreamTimeout defaults.
const (
	DefaultUpstreamTimeout = 5 * time.Second
)

type HTTPStatusConfig struct {
	Exact  int    `yaml:"exact"`
	Range  string `yaml:"range"`
	Except []int  `yaml:"except"`
}

func (s *HTTPStatusConfig) Is(status int) bool {
	for _, ex := range s.Except {
		if ex == status {
			return false
		}
	}

	if s.Exact == status {
		return true
	}

	rangeParts := strings.Split(s.Range, "-")
	if len(rangeParts) != 2 {
		return false
	}

	lower, err1 := strconv.Atoi(rangeParts[0])
	upper, err2 := strconv.Atoi(rangeParts[1])
	if err1 == nil && err2 == nil && status >= lower && status <= upper {
		return true
	}

	return false
}

type HTTPStatusMatchConfig []HTTPStatusConfig

func (ss HTTPStatusMatchConfig) Is(status int) bool {
	for _, s := range ss {
		if s.Is(status) {
			return true
		}
	}

	return false
}
