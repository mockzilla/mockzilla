package portable

import (
	"encoding/json"
	"fmt"
	"sort"
)

// readyStamp is emitted on stdout once the HTTP listener is bound when the
// `--ready-stamp` flag is set. Programmatic supervisors (like the
// mockzilla MCP bridge) read it instead of polling /healthz, so they get
// the resolved port and the registered services in a single shot.
type readyStamp struct {
	Status   string         `json:"status"`
	Port     int            `json:"port"`
	URL      string         `json:"url"`
	Services []readyService `json:"services"`
}

type readyService struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// buildReadyStamp returns the JSON line a supervisor should consume.
// `serviceNames` is the set of registered service names (i.e. the keys of
// the handlers map). They are sorted so the line is deterministic across
// runs, which keeps tests and downstream consumers stable.
func buildReadyStamp(port int, homeURL string, serviceNames []string) (string, error) {
	names := make([]string, len(serviceNames))
	copy(names, serviceNames)
	sort.Strings(names)

	services := make([]readyService, 0, len(names))
	for _, name := range names {
		services = append(services, readyService{Name: name, Path: "/" + name})
	}

	stamp := readyStamp{
		Status:   "ready",
		Port:     port,
		URL:      fmt.Sprintf("http://localhost:%d%s", port, homeURL),
		Services: services,
	}
	out, err := json.Marshal(stamp)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
