package portable

import (
	"fmt"
	"os"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"go.yaml.in/yaml/v4"
)

// portableConfig holds the unified configuration for portable mode.
// The "app" section configures the application, while "services" provides
// per-service overrides (latency, errors, upstream, etc.).
type portableConfig struct {
	App      *config.AppConfig                `yaml:"app"`
	Services map[string]*config.ServiceConfig `yaml:"services"`
}

// loadPortableConfig reads and parses the unified config file.
// If path is empty, returns a config with defaults (nil App and nil Services).
func loadPortableConfig(path, baseDir string) (*portableConfig, error) {
	if path == "" {
		return &portableConfig{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Parse into a raw structure so we can handle app and services separately.
	var raw struct {
		App      yaml.Node                        `yaml:"app"`
		Services map[string]*config.ServiceConfig `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &portableConfig{}

	// Parse app section: start with defaults, then overlay YAML values.
	if raw.App.Kind != 0 {
		appCfg := config.NewDefaultAppConfig(baseDir)
		if err := raw.App.Decode(appCfg); err != nil {
			return nil, fmt.Errorf("parsing app config: %w", err)
		}
		cfg.App = appCfg
	}

	// Apply WithDefaults on each service config.
	if len(raw.Services) > 0 {
		cfg.Services = raw.Services
		for _, svc := range cfg.Services {
			svc.WithDefaults()
		}
	}

	return cfg, nil
}

// loadContexts reads a per-service context file and returns each service's
// context as YAML bytes suitable for factory.WithServiceContext.
// If path is empty, returns nil.
func loadContexts(path string) (map[string][]byte, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading context file %s: %w", path, err)
	}

	var raw map[string]map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing context file: %w", err)
	}

	if len(raw) == 0 {
		return nil, nil
	}

	result := make(map[string][]byte, len(raw))
	for name, values := range raw {
		bts, err := yaml.Marshal(values)
		if err != nil {
			return nil, fmt.Errorf("marshalling context for %s: %w", name, err)
		}
		result[name] = bts
	}
	return result, nil
}
