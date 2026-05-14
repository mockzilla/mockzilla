package config

// HandlerConfig is a config for the handler.
// It is created from the service config.
// SelfPrefix is the prefix for helper routes outside OpenAPI spec:
//
//	for example, payload generation.
type HandlerConfig struct {
	SelfPrefix string `yaml:"self-prefix"`
}

// NewHandlerConfig creates a new handler config from the service config.
func NewHandlerConfig(service *ServiceConfig) *HandlerConfig {
	return &HandlerConfig{
		SelfPrefix: service.Mount,
	}
}
