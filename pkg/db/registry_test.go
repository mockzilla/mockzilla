package db

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseOptions(t *testing.T) {
	type testConfig struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
		TLS  bool   `yaml:"tls"`
	}

	t.Run("parses basic options", func(t *testing.T) {
		opts := map[string]any{
			"host": "localhost",
			"port": 6379,
			"tls":  true,
		}
		cfg, err := ParseOptions[testConfig](opts)
		assert.NoError(t, err)
		assert.Equal(t, "localhost", cfg.Host)
		assert.Equal(t, 6379, cfg.Port)
		assert.True(t, cfg.TLS)
	})

	t.Run("nil options returns zero value", func(t *testing.T) {
		cfg, err := ParseOptions[testConfig](nil)
		assert.NoError(t, err)
		assert.Equal(t, "", cfg.Host)
		assert.Equal(t, 0, cfg.Port)
		assert.False(t, cfg.TLS)
	})

	t.Run("empty options returns zero value", func(t *testing.T) {
		cfg, err := ParseOptions[testConfig](map[string]any{})
		assert.NoError(t, err)
		assert.Equal(t, "", cfg.Host)
	})

	t.Run("ignores unknown keys", func(t *testing.T) {
		opts := map[string]any{
			"host":    "localhost",
			"unknown": "value",
		}
		cfg, err := ParseOptions[testConfig](opts)
		assert.NoError(t, err)
		assert.Equal(t, "localhost", cfg.Host)
	})
}

func TestRegister_PanicsOnNilFactory(t *testing.T) {
	assert.PanicsWithValue(t, "db: Register factory is nil", func() {
		Register("nil-factory", nil)
	})
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	name := "dup-test-" + fmt.Sprintf("%d", len(Drivers()))
	Register(name, func(options map[string]any) (Storage, error) {
		return nil, nil
	})

	assert.PanicsWithValue(t, "db: Register called twice for driver "+name, func() {
		Register(name, func(options map[string]any) (Storage, error) {
			return nil, nil
		})
	})
}

func TestDrivers(t *testing.T) {
	names := Drivers()
	assert.Contains(t, names, "redis")
}

func TestRegisteredCarriesCompat(t *testing.T) {
	byName := make(map[string]Driver)
	for _, driver := range Registered() {
		byName[driver.Name] = driver
	}

	t.Run("the built-ins are there", func(t *testing.T) {
		assert.Equal(t, KindEmbedded, byName["memory"].Compat.Kind)
		assert.Equal(t, KindServer, byName["redis"].Compat.Kind)
	})

	t.Run("a server driver says what it was tested on", func(t *testing.T) {
		compat := byName["redis"].Compat

		assert.NotEmpty(t, compat.Min)
		assert.NotEmpty(t, compat.Tested)
		assert.Contains(t, compat.Tested, compat.Min, "the minimum has to be one of the versions that ran")
	})

	t.Run("a password is marked so nothing displays it", func(t *testing.T) {
		for _, setting := range byName["redis"].Compat.Settings {
			if setting.Env == "REDIS_PASSWORD" {
				assert.True(t, setting.IsSensitive)
			}
		}
	})

	t.Run("Registered is sorted and Drivers agrees with it", func(t *testing.T) {
		var names []string
		for _, driver := range Registered() {
			names = append(names, driver.Name)
		}

		assert.True(t, slices.IsSorted(names))
		assert.Equal(t, Drivers(), names)
	})
}
