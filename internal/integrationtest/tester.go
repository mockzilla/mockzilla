package integrationtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
)

var (
	// ServiceResourcesEndpoint is the endpoint to get service routes
	// Format: /.services/<service-name>
	ServiceResourcesEndpoint = "/.services/%s"
)

// IntegrationResult represents the result of testing a single endpoint
type IntegrationResult struct {
	Spec             string
	Path             string
	OperationID      string
	Method           string
	Ok               bool
	StatusCode       int
	GenerateErr      string
	Validated        bool
	ValidationMethod string
	BatchID          int // Batch number (0 = individual mode)
}

// ServiceTestResult represents the results of testing a single service
type ServiceTestResult struct {
	Spec            string
	Results         []IntegrationResult
	TotalEndpoints  int
	TestedEndpoints int
	TestDuration    float64 // Test execution time in seconds
}

// TestService tests a single generated service against the running server
// Returns: results, total endpoint count, tested endpoint count
func TestService(specFile, serverURL string) ([]IntegrationResult, int, int) {
	var results []IntegrationResult

	// Generate service name from spec file
	serviceName := api.NormalizeServiceName(specFile)

	// Get service routes from the API
	routesURL := serverURL + fmt.Sprintf(ServiceResourcesEndpoint, serviceName)
	debugLogger.Debug("HTTP GET request", "url", routesURL, "service", serviceName)
	resp, err := http.Get(routesURL)
	if err != nil {
		debugLogger.Debug("HTTP GET failed", "url", routesURL, "error", err)
		results = append(results, IntegrationResult{
			Spec:        specFile,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to get service routes: %v", err),
		})
		return results, 0, 0
	}
	defer func() { _ = resp.Body.Close() }()
	debugLogger.Debug("HTTP GET response", "url", routesURL, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		results = append(results, IntegrationResult{
			Spec:        specFile,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to get routes (status %d): %s", resp.StatusCode, string(bodyBytes)),
		})
		return results, 0, 0
	}

	var routesResponse api.ServiceResourcesResponse

	if err = json.NewDecoder(resp.Body).Decode(&routesResponse); err != nil {
		results = append(results, IntegrationResult{
			Spec:        specFile,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to decode routes response: %v", err),
		})
		return results, 0, 0
	}

	numRoutes := len(routesResponse.Endpoints)

	// No endpoints = nothing to test (not a failure)
	if numRoutes == 0 {
		return results, 0, 0
	}

	// Test actual endpoints concurrently.
	// Test all routes (or limit via env var).
	maxRoutesToTest := numRoutes
	if maxStr := os.Getenv("MAX_ROUTES_PER_SPEC"); maxStr != "" {
		if maxx, err := strconv.Atoi(maxStr); err == nil && maxx > 0 {
			maxRoutesToTest = maxx
			if numRoutes < maxRoutesToTest {
				maxRoutesToTest = numRoutes
			}
		}
	}

	var routeMu sync.Mutex
	var routeWg sync.WaitGroup

	// Limit concurrent route tests to avoid overwhelming the server
	// Default of 10 balances speed with stability for large specs
	maxRouteConcurrency := 10
	if maxStr := os.Getenv("MAX_ROUTE_CONCURRENCY"); maxStr != "" {
		if maxx, err := strconv.Atoi(maxStr); err == nil && maxx > 0 {
			maxRouteConcurrency = maxx
		}
	}
	routeSemaphore := make(chan struct{}, maxRouteConcurrency)

	// Test all routes concurrently with limited concurrency
	for i := 0; i < maxRoutesToTest; i++ {
		routeWg.Add(1)
		go func(route *api.RouteDescription) {
			defer routeWg.Done()
			routeSemaphore <- struct{}{}
			defer func() { <-routeSemaphore }()

			result := testRoute(serviceName, serverURL, route, specFile)

			routeMu.Lock()
			results = append(results, result)
			routeMu.Unlock()
		}(routesResponse.Endpoints[i])
	}
	routeWg.Wait()

	return results, numRoutes, maxRoutesToTest
}

// testRoute tests a single route endpoint
func testRoute(serviceName, serverURL string, route *api.RouteDescription, specFile string) IntegrationResult {
	// First, generate a payload for this endpoint using POST /<service>/.resources
	generateURL := serverURL + fmt.Sprintf(ServiceResourcesEndpoint, serviceName)
	generateReq := map[string]any{
		"path":   route.Path,
		"method": route.Method,
	}
	reqBody, _ := json.Marshal(generateReq)

	genResp, err := http.Post(generateURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to generate payload: %v", err),
		}
	}

	// Read response body first to check if it's empty
	genRespBody, err := io.ReadAll(genResp.Body)
	_ = genResp.Body.Close()
	if err != nil {
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to read generate response: %v", err),
		}
	}

	// Check for non-200 status or empty body
	if genResp.StatusCode != http.StatusOK || len(genRespBody) == 0 {
		errMsg := fmt.Sprintf("Generate returned status %d", genResp.StatusCode)
		if len(genRespBody) > 0 {
			errMsg += fmt.Sprintf(": %s", string(genRespBody))
		} else {
			errMsg += " with empty body"
		}
		debugLogger.Debug("Generate error",
			"method", route.Method,
			"path", route.Path,
			"operationId", route.ID,
			"request", string(reqBody),
			"status", genResp.StatusCode,
			"body", string(genRespBody))
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: errMsg,
		}
	}

	var generateResponse struct {
		Body        any    `json:"body"`
		ContentType string `json:"contentType"`
		Path        string `json:"path"`
	}

	if err := json.Unmarshal(genRespBody, &generateResponse); err != nil {
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to decode generate response: %v (body: %s)", err, string(genRespBody)),
		}
	}

	// Now call the actual endpoint with the generated payload
	// Use the generated path which has placeholders replaced with actual values
	actualPath := generateResponse.Path
	if actualPath == "" {
		actualPath = route.Path
	}

	// Check for unreplaced path placeholders (e.g., {paramName})
	if strings.HasPrefix(actualPath, "{") && strings.HasSuffix(actualPath, "}") && !strings.HasPrefix(actualPath, "{{") {
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Path contains unreplaced placeholders: %s", actualPath),
		}
	}

	endpointURL := serverURL + "/" + serviceName + actualPath

	var req *http.Request
	if generateResponse.Body != nil {
		var bodyBytes []byte
		if bodyMap, ok := generateResponse.Body.(map[string]any); ok {
			bodyBytes, _ = json.Marshal(bodyMap)
		} else if bodyStr, ok := generateResponse.Body.(string); ok {
			bodyBytes = []byte(bodyStr)
		}
		req, err = http.NewRequest(route.Method, endpointURL, bytes.NewReader(bodyBytes))
	} else {
		req, err = http.NewRequest(route.Method, endpointURL, nil)
	}

	if err != nil {
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Failed to create request: %v", err),
		}
	}

	if generateResponse.ContentType != "" {
		req.Header.Set("Content-Type", generateResponse.ContentType)
	}

	debugLogger.Debug("HTTP request", "method", route.Method, "url", endpointURL, "service", serviceName, "path", route.Path)
	endpointResp, err := HTTPClient.Do(req)
	if err != nil {
		debugLogger.Debug("HTTP request failed", "method", route.Method, "url", endpointURL, "error", err)
		return IntegrationResult{
			Spec:        specFile,
			Path:        route.Path,
			OperationID: route.ID,
			Method:      route.Method,
			Ok:          false,
			GenerateErr: fmt.Sprintf("Request failed: %v", err),
		}
	}
	defer func() { _ = endpointResp.Body.Close() }()
	debugLogger.Debug("HTTP response", "method", route.Method, "url", endpointURL, "status", endpointResp.StatusCode)

	// Check if response was validated
	validated := endpointResp.Header.Get("X-Validated") == "true"
	validationMethod := endpointResp.Header.Get("X-Validation-Method")

	// Read response body
	respBody, err := io.ReadAll(endpointResp.Body)
	if err != nil {
		return IntegrationResult{
			Spec:             specFile,
			Path:             route.Path,
			OperationID:      route.ID,
			Method:           route.Method,
			Ok:               false,
			StatusCode:       endpointResp.StatusCode,
			GenerateErr:      fmt.Sprintf("Failed to read response: %v", err),
			Validated:        validated,
			ValidationMethod: validationMethod,
		}
	}

	// Verify response is valid JSON if content-type is application/json
	contentType := endpointResp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") && len(respBody) > 0 {
		var jsonData any
		if err := json.Unmarshal(respBody, &jsonData); err != nil {
			bodyPreview := string(respBody)
			if len(bodyPreview) > 200 {
				bodyPreview = bodyPreview[:200] + "..."
			}
			return IntegrationResult{
				Spec:        specFile,
				Path:        route.Path,
				OperationID: route.ID,
				Method:      route.Method,
				Ok:          false,
				StatusCode:  endpointResp.StatusCode,
				GenerateErr: bodyPreview,
			}
		}
	}

	// Success!
	return IntegrationResult{
		Spec:             specFile,
		Path:             route.Path,
		OperationID:      route.ID,
		Method:           route.Method,
		Ok:               true,
		StatusCode:       endpointResp.StatusCode,
		Validated:        validated,
		ValidationMethod: validationMethod,
	}
}
