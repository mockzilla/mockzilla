package api

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
)

type RouteType string

const (
	RouteTypeOpenAPI RouteType = "openapi"
	RouteTypeStatic  RouteType = "static"
)

const (
	// RootServiceName is the name used in URLs for services with empty name.
	// Services with empty name are registered as "" but accessed via "/.root" in the UI.
	// The dot prefix makes it a reserved name to avoid conflicts with actual service names.
	RootServiceName = ".root"
)

// CreateServiceRoutes adds service routes to the router.
func CreateServiceRoutes(router *Router) error {
	if router.Config().DisableUI || router.Config().ServiceURL == "" {
		return nil
	}

	handler := &ServiceHandler{
		router: router,
	}

	url := router.Config().ServiceURL
	url = "/" + strings.Trim(url, "/")

	router.Route(url, func(r chi.Router) {
		r.Get("/", handler.list)
		r.Get("/*", handler.routes)
		r.Post("/*", handler.generate)
	})

	return nil
}

// ServiceHandler handles service routes.
type ServiceHandler struct {
	router *Router
	mu     sync.Mutex
}

func (h *ServiceHandler) list(w http.ResponseWriter, r *http.Request) {
	services := h.router.GetServices()
	var keys []string
	for key := range services {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]*ServiceItemResponse, 0)
	for _, key := range keys {
		svcItem := services[key]
		var resourceCount int
		if svcItem.Handler != nil {
			resourceCount = len(svcItem.Handler.Routes())
		}
		items = append(items, &ServiceItemResponse{
			Name:           key,
			ResourceNumber: resourceCount,
		})
	}
	res := &ServiceListResponse{
		Items: items,
	}

	NewJSONResponse(w).Send(res)
}

// routes returns the list of routes for the service.
func (h *ServiceHandler) routes(w http.ResponseWriter, r *http.Request) {
	svc := h.getService(r)
	if svc == nil || svc.Handler == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	routes := svc.Handler.Routes()
	routes.Sort()
	res := &ServiceResourcesResponse{
		Endpoints: routes,
	}
	NewJSONResponse(w).Send(res)
}

// generate proxies the generate request to the service handler.
func (h *ServiceHandler) generate(w http.ResponseWriter, r *http.Request) {
	svc := h.getService(r)
	if svc == nil || svc.Handler == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	svc.Handler.Generate(w, r)
}

// getService returns the service by name from the path.
func (h *ServiceHandler) getService(r *http.Request) *ServiceItem {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Extract service name from wildcard path
	// The path will be like /.services/stripe/v2, we need to extract "stripe/v2"
	// For root service (no name), frontend sends /.services/root/..., we need to map "root" to ""
	serviceURL := h.router.Config().ServiceURL
	prefix := "/" + strings.Trim(serviceURL, "/") + "/"
	name := strings.TrimPrefix(r.URL.Path, prefix)

	// Strip /routes or /generate suffix from the path
	// These are the endpoints that call getService
	name = strings.TrimSuffix(name, "/routes")
	name = strings.TrimSuffix(name, "/generate")

	// Handle root service: RootServiceName or "RootServiceName/..." -> ""
	if name == RootServiceName || strings.HasPrefix(name, RootServiceName+"/") {
		name = ""
	}

	return h.router.GetServices()[name]
}

// RouteDescription describes a route for the UI Application.
// Path is relative to the service prefix.
type RouteDescription struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	ContentType string `json:"contentType"`
}

// RouteDescriptions is a slice of RouteDescription.
// Allows to add custom methods.
type RouteDescriptions []*RouteDescription

// Sort sorts the routes by path and method.
// The order is: GET, POST, other methods (alphabetically)
func (rs RouteDescriptions) Sort() {
	sort.SliceStable(rs, func(i, j int) bool {
		return ComparePathMethod(rs[i].Path, rs[i].Method, rs[j].Path, rs[j].Method)
	})
}

// ServiceItem represents a service with its handler.
// Service can hold multiple OpenAPI specs.
type ServiceItem struct {
	Name    string                `json:"name"`
	Handler Handler               `json:"-"`
	Config  *config.ServiceConfig `json:"-"`
}

type ServiceItemResponse struct {
	Name           string `json:"name"`
	ResourceNumber int    `json:"resourceNumber"`
}

// ServicePayload is a struct that represents a new service payload.
type ServicePayload struct {
	IsOpenAPI   bool   `json:"isOpenApi"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Response    []byte `json:"response"`
	ContentType string `json:"contentType"`
	URL         string `json:"url"`
}

// ServiceDescription is a struct created from the service payload to facilitate file path composition.
type ServiceDescription struct {
	Method    string
	Path      string
	Ext       string
	IsOpenAPI bool
}

// GenerateRequest is a struct that represents a request to generate a resource.
type GenerateRequest struct {
	Path    string         `json:"path"`
	Method  string         `json:"method"`
	Context map[string]any `json:"context"`
}

type GenerateResponse struct {
	// Request  *GeneratedRequest  `json:"request"`
	// Response *GeneratedResponse `json:"response"`
}

type ServiceListResponse struct {
	Items []*ServiceItemResponse `json:"items"`
}

type ServiceEmbedded struct {
	Name string `json:"name"`
}

type ServiceResourcesResponse struct {
	Endpoints RouteDescriptions `json:"endpoints"`
}

type ResourceResponse struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Extension   string `json:"extension"`
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type SavedResourceResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ID      int    `json:"id"`
}

// NormalizeServiceName extracts a valid service name from a spec file path.
// It takes the base filename (without extension) and converts it to snake_case,
// ensuring it's a valid Go package/service identifier.
//
// Examples:
//   - "petstore.yaml" -> "petstore"
//   - "IP Push Notification_sandbox.json" -> "ip_push_notification_sandbox"
//   - "FaSta_-_Station_Facilities_Status-2.1.359.yaml" -> "fa_sta_station_facilities_status_2_1_359"
func NormalizeServiceName(specPath string) string {
	// Get base filename without extension
	baseName := strings.TrimSuffix(filepath.Base(specPath), filepath.Ext(specPath))
	// Convert to snake_case (handles spaces, dots, hyphens, etc.)
	return types.ToSnakeCase(baseName)
}

// ComparePathMethod compares two path/method pairs for sorting.
// Returns true if (path1, method1) should come before (path2, method2).
// Order: path (alphabetically), then method (GET, POST, others alphabetically).
func ComparePathMethod(path1, method1, path2, method2 string) bool {
	if path1 != path2 {
		return path1 < path2
	}

	methodOrder := map[string]int{
		http.MethodGet:  1,
		http.MethodPost: 2,
	}

	m1 := methodOrder[method1]
	if m1 == 0 {
		m1 = 3
	}

	m2 := methodOrder[method2]
	if m2 == 0 {
		m2 = 3
	}

	return m1 < m2
}

// ServiceParams provides access to application and service configuration
// along with the database connection. This struct is passed to user services
// to allow access to configuration without coupling to framework internals.
//
// Example usage in service.go:
//
//	func newService(params *api.ServiceParams) *service {
//	    baseURL := params.AppConfig.BaseURL
//	    extra := params.AppConfig.Extra["myKey"]
//	    return &service{params: params}
//	}
//
// For custom configuration, you can:
//  1. Use Extra map in app.yml for simple key-value config
//  2. Embed and load your own config files in newService
//
// Example app.yml with Extra:
//
//	baseURL: https://api.example.com
//	extra:
//	  myApiKey: "secret"
//	  maxRetries: 3
type ServiceParams struct {
	// AppConfig is the application-wide configuration.
	// Contains BaseURL, InternalURL, and Extra map for custom values.
	AppConfig *config.AppConfig

	// ServiceConfig is the service-specific configuration.
	ServiceConfig *config.ServiceConfig

	// DB is the database connection for this service.
	DB db.DB
}
