package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"go.yaml.in/yaml/v4"
)

// AppConfig is the app configuration.
type AppConfig struct {
	Title       string `yaml:"title" env:"APP_TITLE"`
	Port        int    `yaml:"port" env:"APP_PORT"`
	BaseURL     string `yaml:"baseURL" env:"APP_BASE_URL"`
	InternalURL string `yaml:"internalURL" env:"APP_INTERNAL_URL"`
	HomeURL     string `yaml:"homeURL" env:"APP_HOME_URL"`
	ServiceURL  string `yaml:"serviceURL" env:"APP_SERVICE_URL"`

	// AssetsURL is the base URL for static UI assets (CSS, JS, images, icons).
	// Empty means assets are served with relative paths (default).
	AssetsURL string `yaml:"assetsURL" env:"APP_ASSETS_URL"`

	ContextAreaPrefix string            `yaml:"contextAreaPrefix"`
	DisableUI         bool              `yaml:"disableUI" env:"APP_DISABLE_UI"`
	DisableConfigUI   bool              `yaml:"disableConfigUI" env:"APP_DISABLE_CONFIG_UI"`
	Paths             Paths             `yaml:"-"`
	Editor            *EditorConfig     `yaml:"editor"`
	History           *AppHistoryConfig `yaml:"history"`
	Storage           *StorageConfig    `yaml:"storage"`
	Extra             map[string]any    `yaml:"extra"`
}

const (
	DefaultHistoryURL      = "/.history"
	DefaultHistoryDuration = 60 * time.Minute
)

// NewDefaultAppHistoryConfig creates the default history config.
func NewDefaultAppHistoryConfig() *AppHistoryConfig {
	return &AppHistoryConfig{
		URL:      DefaultHistoryURL,
		Duration: DefaultHistoryDuration,
	}
}

// AppHistoryConfig configures request/response history at the application level.
type AppHistoryConfig struct {
	Enabled  *bool         `yaml:"enabled" env:"ROUTER_HISTORY_ENABLED"`
	URL      string        `yaml:"url"`
	Duration time.Duration `yaml:"duration" env:"ROUTER_HISTORY_DURATION"`
}

// NewDefaultAppConfig creates a new default app config in case the config file is missing, not found or any other error.
func NewDefaultAppConfig(baseDir string) *AppConfig {
	return &AppConfig{
		Title:             "API Explorer",
		Port:              2200,
		HomeURL:           "/",
		ServiceURL:        "/.services",
		ContextAreaPrefix: "in-",
		Paths:             NewPaths(baseDir),
		Editor: &EditorConfig{
			Theme:     "chrome",
			DarkTheme: "cobalt",
			FontSize:  14,
		},
		History: NewDefaultAppHistoryConfig(),
		Storage: &StorageConfig{Type: StorageTypeMemory},
		Extra:   make(map[string]any),
	}
}

// NewAppConfigFromBytes creates an AppConfig from YAML bytes, filling missing values with defaults.
// Environment variables override YAML values when set (via `env` struct tags).
func NewAppConfigFromBytes(bts []byte, baseDir string) (*AppConfig, error) {
	cfg := NewDefaultAppConfig(baseDir)
	if err := yaml.Unmarshal(bts, cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling app config: %w", err)
	}
	cfg.Paths = NewPaths(baseDir)

	// Ensure nested structs exist so env.Parse can populate them.
	if cfg.History == nil {
		cfg.History = NewDefaultAppHistoryConfig()
	}
	// When explicitly disabled, clear the URL so downstream checks are simple.
	if cfg.History.Enabled != nil && !*cfg.History.Enabled {
		cfg.History.URL = ""
	}
	if cfg.Storage == nil {
		cfg.Storage = &StorageConfig{}
	}
	if cfg.Storage.Redis == nil {
		cfg.Storage.Redis = &RedisConfig{}
	}

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("applying env overrides: %w", err)
	}

	// Extract driver-specific config from the YAML section matching the storage type.
	// e.g. for type: dynamodb, capture the "dynamodb:" map.
	if typeName := string(cfg.Storage.Type); typeName != "" {
		var raw map[string]any
		if err := yaml.Unmarshal(bts, &raw); err == nil {
			if storageRaw, ok := raw["storage"].(map[string]any); ok {
				if driverOpts, ok := storageRaw[typeName].(map[string]any); ok {
					cfg.Storage.DriverConfig = driverOpts
				}
			}
		}
	}

	// Normalize BaseURL: strip trailing slash so template concatenation
	// (e.g. BaseURL + ServiceURL) never produces double slashes.
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	return cfg, nil
}

type EditorConfig struct {
	Theme     string `yaml:"theme"`
	DarkTheme string `yaml:"darkTheme"`
	FontSize  int    `yaml:"fontSize"`
}

// StorageType defines the type of storage backend.
type StorageType string

const (
	// StorageTypeMemory is the default in-memory storage (per-instance).
	StorageTypeMemory StorageType = "memory"

	// StorageTypeRedis uses Redis for distributed storage.
	StorageTypeRedis StorageType = "redis"
)

// StorageConfig configures shared storage for distributed features.
//
// Each driver's options live under a YAML key matching the type name:
//
//	storage:
//	  type: redis
//	  redis:
//	    address: localhost:6379
//
// DriverConfig is populated automatically from the matching section.
type StorageConfig struct {
	Type         StorageType    `yaml:"type" env:"STORAGE_TYPE"`
	DriverConfig map[string]any `yaml:"-"`

	// Redis is kept for backward compatibility with env tags (REDIS_HOST, etc.).
	Redis *RedisConfig `yaml:"redis"`
}

// DriverOptions returns the driver-specific config map.
// It prefers DriverConfig (extracted from the type-named YAML section).
// Falls back to converting the typed Redis config for backward compatibility.
func (c *StorageConfig) DriverOptions() map[string]any {
	if len(c.DriverConfig) > 0 {
		return c.DriverConfig
	}

	if c.Type == StorageTypeRedis && c.Redis != nil {
		data, err := yaml.Marshal(c.Redis)
		if err != nil {
			return nil
		}
		var opts map[string]any
		if err := yaml.Unmarshal(data, &opts); err != nil {
			return nil
		}
		return opts
	}

	return nil
}

// RedisConfig configures Redis connection.
type RedisConfig struct {
	// host:port address. When Host is set via env, Address is built from Host:Port.
	Address  string `yaml:"address"`
	Host     string `yaml:"host" env:"REDIS_HOST"`
	Port     string `yaml:"port" env:"REDIS_PORT" envDefault:"6379"`
	Username string `yaml:"username" env:"REDIS_USERNAME"`
	Password string `yaml:"password" env:"REDIS_PASSWORD"`
	DB       int    `yaml:"db" env:"REDIS_DB"`
	TLS      bool   `yaml:"tls" env:"REDIS_TLS"`
}

// GetAddress returns the Redis address. If Host is set, it takes precedence over Address.
func (r *RedisConfig) GetAddress() string {
	if r.Host != "" {
		return r.Host + ":" + r.Port
	}
	return r.Address
}
