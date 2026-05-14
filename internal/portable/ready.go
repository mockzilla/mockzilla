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
// `services` carries each registered service's name and the URL prefix
// it is actually mounted at (which may differ from `/` + name when a
// service overrides its mount). Names are sorted so the line is
// deterministic across runs.
func buildReadyStamp(port int, homeURL string, services []readyService) (string, error) {
	sorted := make([]readyService, len(services))
	copy(sorted, services)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	stamp := readyStamp{
		Status:   "ready",
		Port:     port,
		URL:      fmt.Sprintf("http://localhost:%d%s", port, homeURL),
		Services: sorted,
	}
	out, err := json.Marshal(stamp)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
