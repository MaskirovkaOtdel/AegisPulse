package demo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaygroundHandler(t *testing.T) {
	h := PlaygroundHandler()
	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected Content-Type text/html, got %s", contentType)
	}

	body := rr.Body.String()
	// Must contain interactive controls and visualizer elements
	expectedPhrases := []string{
		"Dual-Threshold Token Bucket",
		"Circuit Breaker State Machine",
		"softLimitInput",
		"hardLimitInput",
		"refillRateInput",
		"tankFill",
		"btnTripTrip",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(body, phrase) {
			t.Errorf("expected playground HTML to contain '%s'", phrase)
		}
	}
}

func TestSwaggerHandler_UI(t *testing.T) {
	h := SwaggerHandler()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Fatalf("expected Content-Type text/html, got %s", contentType)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "swagger-ui") || !strings.Contains(body, "/docs/openapi.json") {
		t.Fatalf("expected Swagger UI loader script in HTML, got %s", body[:200])
	}
}

func TestSwaggerHandler_OpenAPISpecJSON(t *testing.T) {
	h := SwaggerHandler()
	req := httptest.NewRequest(http.MethodGet, "/docs/openapi.json", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected Content-Type application/json, got %s", contentType)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse OpenAPI JSON spec: %v", err)
	}

	if spec["openapi"] != "3.0.3" {
		t.Fatalf("expected openapi 3.0.3, got %v", spec["openapi"])
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		t.Fatalf("expected paths in OpenAPI spec, got %v", spec["paths"])
	}

	// Verify key endpoints exist in the spec
	requiredPaths := []string{"/healthz", "/metrics", "/demo", "/api/v1/panic/kill", "/api/{proxyPath}"}
	for _, p := range requiredPaths {
		if _, exists := paths[p]; !exists {
			t.Errorf("expected OpenAPI spec to contain path '%s'", p)
		}
	}
}
