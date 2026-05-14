package portable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"go.yaml.in/yaml/v4"
)

// loadAppConfig reads app.yml at root if present, else returns defaults.
// app.yml is global app configuration only: port, history, storage,
// branding. It does NOT contain a services: map; service settings live
// in each service's config.yml.
func loadAppConfig(root, baseDir string) (*config.AppConfig, error) {
	if root == "" {
		return config.NewDefaultAppConfig(baseDir), nil
	}

	path := filepath.Join(root, appFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.NewDefaultAppConfig(baseDir), nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return config.NewAppConfigFromBytes(data, baseDir)
}

// loadServiceConfig reads a service's config.yml into a ServiceConfig.
// Missing config.yml is not an error; the caller falls back to defaults.
// The service Name and Mount are stamped from the discovered Service so
// the file can't override the folder identity.
func loadServiceConfig(svc Service) (*config.ServiceConfig, error) {
	cfg := config.NewServiceConfig()
	cfg.Name = svc.Name

	if svc.ConfigDir == "" {
		return cfg, nil
	}
	path := filepath.Join(svc.ConfigDir, configFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	parsed, err := config.NewServiceConfigFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	cfg.OverwriteWith(parsed)
	cfg.Name = svc.Name // folder name wins over any name: in the file
	return cfg, nil
}

// loadServiceContext reads a service's context.yml. The file body is the
// flat replacement map (no service-name wrapper). Values are returned
// as raw YAML bytes so factory.WithServiceContext can consume them
// directly.
func loadServiceContext(svc Service) ([]byte, error) {
	if svc.ConfigDir == "" {
		return nil, nil
	}

	path := filepath.Join(svc.ConfigDir, contextFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// Sanity-check that it parses as a mapping. Bad YAML at this stage
	// would crash deep in the factory with a confusing message.
	var probe map[string]any
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if len(probe) == 0 {
		return nil, nil
	}
	return data, nil
}

// rootFromArgs returns the most plausible "root" directory for app.yml
// lookup. Used by the runner to find a top-level app.yml across however
// many positional args the user passed.
func rootFromArgs(args []string) string {
	for _, a := range args {
		if isURL(a) || strings.HasPrefix(a, "-") {
			continue
		}
		info, err := os.Stat(a)
		if err != nil {
			continue
		}
		if info.IsDir() {
			return a
		}
		return filepath.Dir(a)
	}
	return ""
}
