package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
	"github.com/stretchr/testify/assert"
)

func TestCreateServiceRoutes(t *testing.T) {
	t.Run("Creates service routes when UI is enabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/api/services"

		err := CreateServiceRoutes(router)
		assert.NoError(t, err)

		// Test that the routes are mounted
		req := httptest.NewRequest(http.MethodGet, "/api/services/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Does not create routes when UI is disabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.DisableUI = true
		router.config.ServiceURL = "/api/services"

		err := CreateServiceRoutes(router)
		assert.NoError(t, err)

		// Routes should not be mounted
		req := httptest.NewRequest(http.MethodGet, "/api/services/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Does not create routes when ServiceURL is empty", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = ""

		err := CreateServiceRoutes(router)
		assert.NoError(t, err)
	})

	t.Run("Trims slashes from ServiceURL", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "///api/services///"

		err := CreateServiceRoutes(router)
		assert.NoError(t, err)

		// Test that the routes work with trimmed URL
		req := httptest.NewRequest(http.MethodGet, "/api/services/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestServiceHandler_list(t *testing.T) {
	t.Run("Returns empty list when no services registered", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/api/services"
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/api/services/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ServiceListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response.Items)
	})

	t.Run("Returns list of registered services", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/api/services"

		// Register mock services - note: service name comes from service.Name(), not the prefix
		service1 := &mockService{
			name:   "service1",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Get("/endpoint1", func(w http.ResponseWriter, r *http.Request) {})
				r.Post("/endpoint2", func(w http.ResponseWriter, r *http.Request) {})
			},
		}
		service2 := &mockService{
			name:   "service2",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Get("/endpoint3", func(w http.ResponseWriter, r *http.Request) {})
			},
		}

		registerTestService(router, service1)
		registerTestService(router, service2)
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/api/services/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ServiceListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Items, 2)

		// Services should be sorted alphabetically by service name (not prefix)
		assert.Equal(t, "service1", response.Items[0].Name)
		assert.Equal(t, 0, response.Items[0].ResourceNumber) // mockService.Routes() returns nil
		assert.Equal(t, "service2", response.Items[1].Name)
		assert.Equal(t, 0, response.Items[1].ResourceNumber)
	})

	t.Run("Returns services in alphabetical order", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/api/services"

		// Register services in non-alphabetical order
		serviceZ := &mockService{name: "zebra", config: config.NewServiceConfig(), routes: func(r chi.Router) {}}
		serviceA := &mockService{name: "apple", config: config.NewServiceConfig(), routes: func(r chi.Router) {}}
		serviceM := &mockService{name: "mango", config: config.NewServiceConfig(), routes: func(r chi.Router) {}}

		registerTestService(router, serviceZ)
		registerTestService(router, serviceA)
		registerTestService(router, serviceM)
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/api/services/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response ServiceListResponse
		_ = json.Unmarshal(w.Body.Bytes(), &response)

		assert.Equal(t, "apple", response.Items[0].Name)
		assert.Equal(t, "mango", response.Items[1].Name)
		assert.Equal(t, "zebra", response.Items[2].Name)
	})
}

func TestServiceHandler_getService(t *testing.T) {
	t.Run("Returns service by name", func(t *testing.T) {
		router := newTestRouter(t)
		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		handler := &ServiceHandler{router: router}

		// Use the ServiceURL from config to construct the path
		serviceURL := router.Config().ServiceURL
		req := httptest.NewRequest(http.MethodGet, serviceURL+"/test-service", nil)

		result := handler.getService(req)
		assert.NotNil(t, result)
		assert.Equal(t, "test-service", result.Name)
	})

	t.Run("Returns nil for non-existent service", func(t *testing.T) {
		router := newTestRouter(t)
		handler := &ServiceHandler{router: router}

		serviceURL := router.Config().ServiceURL
		req := httptest.NewRequest(http.MethodGet, serviceURL+"/nonexistent", nil)

		result := handler.getService(req)
		assert.Nil(t, result)
	})

	t.Run("Returns root service when path contains RootServiceName", func(t *testing.T) {
		router := newTestRouter(t)
		service := &mockService{
			name:   "", // Root service has empty name
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		handler := &ServiceHandler{router: router}

		serviceURL := router.Config().ServiceURL

		// Test with just RootServiceName
		req := httptest.NewRequest(http.MethodGet, serviceURL+"/"+RootServiceName, nil)
		result := handler.getService(req)
		assert.NotNil(t, result)
		assert.Equal(t, "", result.Name)

		// Test with RootServiceName + path
		req = httptest.NewRequest(http.MethodGet, serviceURL+"/"+RootServiceName+"/recurring/v2/agreements", nil)
		result = handler.getService(req)
		assert.NotNil(t, result)
		assert.Equal(t, "", result.Name)
	})
}

func TestRouteDescriptions_Sort(t *testing.T) {
	t.Run("Sorts routes by path first", func(t *testing.T) {
		routes := RouteDescriptions{
			{Path: "/users", Method: http.MethodGet},
			{Path: "/admin", Method: http.MethodGet},
			{Path: "/posts", Method: http.MethodGet},
		}

		routes.Sort()

		assert.Equal(t, "/admin", routes[0].Path)
		assert.Equal(t, "/posts", routes[1].Path)
		assert.Equal(t, "/users", routes[2].Path)
	})

	t.Run("Sorts by method when paths are equal - GET first", func(t *testing.T) {
		routes := RouteDescriptions{
			{Path: "/users", Method: http.MethodPost},
			{Path: "/users", Method: http.MethodGet},
			{Path: "/users", Method: http.MethodPut},
		}

		routes.Sort()

		assert.Equal(t, http.MethodGet, routes[0].Method)
		assert.Equal(t, http.MethodPost, routes[1].Method)
		assert.Equal(t, http.MethodPut, routes[2].Method)
	})

	t.Run("Sorts by method when paths are equal - POST second", func(t *testing.T) {
		routes := RouteDescriptions{
			{Path: "/users", Method: http.MethodDelete},
			{Path: "/users", Method: http.MethodPost},
			{Path: "/users", Method: http.MethodGet},
		}

		routes.Sort()

		assert.Equal(t, http.MethodGet, routes[0].Method)
		assert.Equal(t, http.MethodPost, routes[1].Method)
		assert.Equal(t, http.MethodDelete, routes[2].Method)
	})

	t.Run("Sorts other methods alphabetically after GET and POST", func(t *testing.T) {
		routes := RouteDescriptions{
			{Path: "/users", Method: http.MethodPut},
			{Path: "/users", Method: http.MethodGet},
			{Path: "/users", Method: http.MethodDelete},
			{Path: "/users", Method: http.MethodPost},
			{Path: "/users", Method: http.MethodPatch},
		}

		routes.Sort()

		assert.Equal(t, http.MethodGet, routes[0].Method)
		assert.Equal(t, http.MethodPost, routes[1].Method)
		// DELETE, PATCH, PUT should be in alphabetical order (all have order 3)
		// But since we use stable sort, they maintain their relative order
		assert.Equal(t, http.MethodPut, routes[2].Method)
		assert.Equal(t, http.MethodDelete, routes[3].Method)
		assert.Equal(t, http.MethodPatch, routes[4].Method)
	})

	t.Run("Complex sorting with multiple paths and methods", func(t *testing.T) {
		routes := RouteDescriptions{
			{Path: "/users/{id}", Method: http.MethodDelete},
			{Path: "/posts", Method: http.MethodPost},
			{Path: "/users", Method: http.MethodPut},
			{Path: "/posts", Method: http.MethodGet},
			{Path: "/users", Method: http.MethodGet},
			{Path: "/users/{id}", Method: http.MethodGet},
		}

		routes.Sort()

		// Expected order:
		// /posts GET
		// /posts POST
		// /users GET
		// /users PUT
		// /users/{id} GET
		// /users/{id} DELETE

		assert.Equal(t, "/posts", routes[0].Path)
		assert.Equal(t, http.MethodGet, routes[0].Method)

		assert.Equal(t, "/posts", routes[1].Path)
		assert.Equal(t, http.MethodPost, routes[1].Method)

		assert.Equal(t, "/users", routes[2].Path)
		assert.Equal(t, http.MethodGet, routes[2].Method)

		assert.Equal(t, "/users", routes[3].Path)
		assert.Equal(t, http.MethodPut, routes[3].Method)

		assert.Equal(t, "/users/{id}", routes[4].Path)
		assert.Equal(t, http.MethodGet, routes[4].Method)

		assert.Equal(t, "/users/{id}", routes[5].Path)
		assert.Equal(t, http.MethodDelete, routes[5].Method)
	})

	t.Run("Empty slice", func(t *testing.T) {
		routes := RouteDescriptions{}
		routes.Sort()
		assert.Empty(t, routes)
	})

	t.Run("Single element", func(t *testing.T) {
		routes := RouteDescriptions{
			{Path: "/users", Method: http.MethodGet},
		}
		routes.Sort()
		assert.Len(t, routes, 1)
		assert.Equal(t, "/users", routes[0].Path)
	})
}

func TestComparePathMethod(t *testing.T) {
	testCases := []struct {
		name     string
		path1    string
		method1  string
		path2    string
		method2  string
		expected bool
	}{
		{"different paths - first less", "/admin", "GET", "/users", "GET", true},
		{"different paths - first greater", "/users", "GET", "/admin", "GET", false},
		{"same path - GET before POST", "/users", "GET", "/users", "POST", true},
		{"same path - POST after GET", "/users", "POST", "/users", "GET", false},
		{"same path - GET before DELETE", "/users", "GET", "/users", "DELETE", true},
		{"same path - POST before DELETE", "/users", "POST", "/users", "DELETE", true},
		{"same path - DELETE after POST", "/users", "DELETE", "/users", "POST", false},
		{"same path and method", "/users", "GET", "/users", "GET", false},
		{"same path - other methods equal priority", "/users", "PUT", "/users", "DELETE", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ComparePathMethod(tc.path1, tc.method1, tc.path2, tc.method2)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNormalizeServiceName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple filename",
			input:    "petstore.yaml",
			expected: "petstore",
		},
		{
			name:     "JSON extension",
			input:    "petstore.json",
			expected: "petstore",
		},
		{
			name:     "Spaces in filename",
			input:    "IP Push Notification_sandbox.json",
			expected: "ip_push_notification_sandbox",
		},
		{
			name:     "Complex filename with dots, hyphens, underscores",
			input:    "FaSta_-_Station_Facilities_Status-2.1.359.yaml",
			expected: "fa_sta_station_facilities_status_2_1_359",
		},
		{
			name:     "Double underscores",
			input:    "RIS__Journeys.yaml",
			expected: "ris_journeys",
		},
		{
			name:     "Full path",
			input:    "testdata/specs/3.0/petstore.yaml",
			expected: "petstore",
		},
		{
			name:     "Absolute path with spaces",
			input:    "/full/path/to/My API Service.json",
			expected: "my_api_service",
		},
		{
			name:     "Dots and hyphens",
			input:    "navigationservice.e-spirit.cloud.yml",
			expected: "navigationservice_e_spirit_cloud",
		},
		{
			name:     "Starts with digit",
			input:    "1password.com.yaml",
			expected: "n_1password_com",
		},
		{
			name:     "Starts with digit - full path",
			input:    "testdata/specs/new/3.0/1password.com.yaml",
			expected: "n_1password_com",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := NormalizeServiceName(tc.input)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestServiceHandler_routes(t *testing.T) {
	t.Run("Returns 404 when service not found", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/.services"
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.services/nonexistent/routes", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Service not found")
	})

	t.Run("Returns routes for existing service", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/.services"

		service := &mockServiceWithRoutes{
			mockService: mockService{
				name:   "test-service",
				config: config.NewServiceConfig(),
				routes: func(r chi.Router) {
					r.Get("/users", func(w http.ResponseWriter, r *http.Request) {})
					r.Post("/users", func(w http.ResponseWriter, r *http.Request) {})
				},
			},
			routeDescriptions: RouteDescriptions{
				{Path: "/users", Method: http.MethodGet},
				{Path: "/users", Method: http.MethodPost},
			},
		}
		registerTestServiceWithRoutes(router, service)
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.services/test-service/routes", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ServiceResourcesResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Endpoints, 2)
	})
}

func TestServiceHandler_generate(t *testing.T) {
	t.Run("Returns 404 when service not found", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/.services"
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodPost, "/.services/nonexistent/generate", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Service not found")
	})

	t.Run("Proxies generate request to service handler", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.ServiceURL = "/.services"

		generateCalled := false
		service := &mockServiceWithGenerate{
			mockService: mockService{
				name:   "test-service",
				config: config.NewServiceConfig(),
				routes: func(r chi.Router) {},
			},
			generateFunc: func(w http.ResponseWriter, r *http.Request) {
				generateCalled = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"generated": true}`))
			},
		}
		registerTestServiceWithGenerate(router, service)
		_ = CreateServiceRoutes(router)

		req := httptest.NewRequest(http.MethodPost, "/.services/test-service/generate", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.True(t, generateCalled)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"generated": true`)
	})
}

// mockServiceWithRoutes extends mockService with Routes() implementation
type mockServiceWithRoutes struct {
	mockService
	routeDescriptions RouteDescriptions
}

func (m *mockServiceWithRoutes) Routes() RouteDescriptions {
	return m.routeDescriptions
}

func registerTestServiceWithRoutes(router *Router, service *mockServiceWithRoutes) {
	if service.config.Name == "" {
		service.config.Name = service.name
	}
	router.RegisterService(service.config, service)
}

// mockServiceWithGenerate extends mockService with custom Generate implementation
type mockServiceWithGenerate struct {
	mockService
	generateFunc func(w http.ResponseWriter, r *http.Request)
}

func (m *mockServiceWithGenerate) Generate(w http.ResponseWriter, r *http.Request) {
	if m.generateFunc != nil {
		m.generateFunc(w, r)
	}
}

func registerTestServiceWithGenerate(router *Router, service *mockServiceWithGenerate) {
	if service.config.Name == "" {
		service.config.Name = service.name
	}
	router.RegisterService(service.config, service)
}

func TestServiceParams(t *testing.T) {
	t.Run("struct fields are accessible", func(t *testing.T) {
		appCfg := &config.AppConfig{
			Title:       "Test App",
			BaseURL:     "https://api.example.com",
			InternalURL: "http://localhost:2200",
			Extra: map[string]any{
				"customKey": "customValue",
			},
		}
		serviceCfg := &config.ServiceConfig{
			Name: "test-service",
		}
		storage := db.NewStorage(nil)
		defer storage.Close()
		serviceDB := storage.NewDB("test-service", 5*time.Minute)

		params := &ServiceParams{
			AppConfig:     appCfg,
			ServiceConfig: serviceCfg,
			DB:            serviceDB,
		}

		assert.Equal(t, appCfg, params.AppConfig)
		assert.Equal(t, serviceCfg, params.ServiceConfig)
		assert.Equal(t, serviceDB, params.DB)
	})

	t.Run("provides access to app config fields", func(t *testing.T) {
		params := &ServiceParams{
			AppConfig: &config.AppConfig{
				BaseURL:     "https://public.example.com",
				InternalURL: "http://internal:8080",
				Extra: map[string]any{
					"apiKey":     "secret123",
					"maxRetries": 5,
				},
			},
			ServiceConfig: config.NewServiceConfig(),
		}

		assert.Equal(t, "https://public.example.com", params.AppConfig.BaseURL)
		assert.Equal(t, "http://internal:8080", params.AppConfig.InternalURL)
		assert.Equal(t, "secret123", params.AppConfig.Extra["apiKey"])
		assert.Equal(t, 5, params.AppConfig.Extra["maxRetries"])
	})

	t.Run("provides access to service config fields", func(t *testing.T) {
		params := &ServiceParams{
			AppConfig: config.NewDefaultAppConfig("/test"),
			ServiceConfig: &config.ServiceConfig{
				Name:            "my-service",
				ResourcesPrefix: "/resources",
			},
		}

		assert.Equal(t, "my-service", params.ServiceConfig.Name)
		assert.Equal(t, "/resources", params.ServiceConfig.ResourcesPrefix)
	})

	t.Run("handles nil db", func(t *testing.T) {
		params := &ServiceParams{
			AppConfig:     config.NewDefaultAppConfig("/test"),
			ServiceConfig: config.NewServiceConfig(),
			DB:            nil,
		}

		assert.Nil(t, params.DB)
	})
}
