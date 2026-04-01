package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"go.yaml.in/yaml/v4"
)

const DefaultServiceConfigURL = "/.config"

// CreateServiceConfigRoutes adds service config routes to the router.
func CreateServiceConfigRoutes(router *Router) error {
	if router.Config().DisableUI {
		return nil
	}

	handler := &ServiceConfigHandler{
		router: router,
	}

	url := DefaultServiceConfigURL
	url = "/" + strings.Trim(url, "/")

	router.Route(url, func(r chi.Router) {
		r.Get("/", handler.get)
	})

	return nil
}

// ServiceConfigHandler handles service configuration routes.
type ServiceConfigHandler struct {
	router *Router
}

func (h *ServiceConfigHandler) get(w http.ResponseWriter, r *http.Request) {
	serviceName := r.URL.Query().Get("service")
	if serviceName == RootServiceName {
		serviceName = ""
	}

	svc := h.router.GetServices()[serviceName]
	if svc == nil {
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	cfg := svc.Config
	if cfg == nil {
		cfg = config.NewServiceConfig()
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		http.Error(w, "Failed to marshal config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	_, _ = w.Write(data)
}
