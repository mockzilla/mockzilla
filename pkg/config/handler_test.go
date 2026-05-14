package config

import (
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestNewHandlerConfig(t *testing.T) {
	assert := assert2.New(t)

	t.Run("creates handler config from service config", func(t *testing.T) {
		serviceCfg := &ServiceConfig{
			Mount: "/api",
		}

		handlerCfg := NewHandlerConfig(serviceCfg)

		assert.NotNil(handlerCfg)
		assert.Equal("/api", handlerCfg.SelfPrefix)
	})

	t.Run("handles empty resources prefix", func(t *testing.T) {
		serviceCfg := &ServiceConfig{}

		handlerCfg := NewHandlerConfig(serviceCfg)

		assert.NotNil(handlerCfg)
		assert.Empty(handlerCfg.SelfPrefix)
	})
}
