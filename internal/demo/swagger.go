package demo

import (
	"fmt"
	"net/http"
	"strings"
)

const openAPISpecJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "AegisPulse API Gateway & Zero-Trust Reverse Proxy",
    "version": "1.0.0",
    "description": "High-performance distributed API gateway featuring multi-tier token bucket rate limiting, zero-allocation anti-replay protection, automatic L1 cache fallback, circuit breakers, and administrative kill-switches."
  },
  "servers": [
    {
      "url": "http://localhost:8080",
      "description": "Local Gateway Server"
    }
  ],
  "paths": {
    "/healthz": {
      "get": {
        "summary": "Gateway Health Check",
        "description": "Liveness and readiness probe for container orchestrators.",
        "responses": {
          "200": {
            "description": "Gateway is online and operational",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": { "type": "string", "example": "UP" },
                    "gateway": { "type": "string", "example": "AegisPulse" }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/metrics": {
      "get": {
        "summary": "Prometheus Metrics Endpoint",
        "description": "Exposes standard Prometheus metrics for Grafana visualization.",
        "responses": {
          "200": {
            "description": "Prometheus metrics plain-text stream"
          }
        }
      }
    },
    "/demo": {
      "get": {
        "summary": "Interactive Demo Playground",
        "description": "Embedded visualizer for token bucket dynamics and circuit breaker state machines.",
        "responses": {
          "200": {
            "description": "HTML5 interactive dashboard visualizer"
          }
        }
      }
    },
    "/api/v1/panic/status": {
      "get": {
        "summary": "Resilience & Security Status",
        "description": "Returns the active status of circuit breakers, emergency cutoffs, and burst freeze switches.",
        "responses": {
          "200": {
            "description": "Current resilience configuration state",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "circuit_breaker": { "type": "string", "example": "CLOSED" },
                    "emergency_cutoff": { "type": "boolean", "example": false },
                    "burst_frozen": { "type": "boolean", "example": false },
                    "jitter_active": { "type": "boolean", "example": false }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v1/panic/kill": {
      "post": {
        "summary": "Emergency Key Kill-Switch",
        "description": "Immediately freezes an API key platform-wide across all gateway instances.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["api_key"],
                "properties": {
                  "api_key": { "type": "string", "example": "aegis_live_compromised_key_9999" }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "API Key successfully frozen"
          },
          "400": {
            "description": "Missing api_key parameter"
          }
        }
      }
    },
    "/api/v1/panic/burst-freeze": {
      "post": {
        "summary": "Global Burst Freeze Switch",
        "description": "Globally disables burst capacity and blocks any client attempting to exceed soft rate limits.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["frozen"],
                "properties": {
                  "frozen": { "type": "boolean", "example": true }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Burst freeze updated"
          }
        }
      }
    },
    "/api/v1/panic/emergency-cutoff": {
      "post": {
        "summary": "Emergency Upstream Cutoff",
        "description": "Forces all incoming gateway traffic to be served strictly from L1 smart cache fallbacks.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["cutoff"],
                "properties": {
                  "cutoff": { "type": "boolean", "example": true }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Emergency cutoff updated"
          }
        }
      }
    },
    "/api/v1/dlq": {
      "get": {
        "summary": "List Dead Letter Queue Items",
        "description": "Retrieves failed webhook payloads held in the Dead Letter Queue.",
        "responses": {
          "200": {
            "description": "Array of DLQ items"
          }
        }
      }
    },
    "/api/{proxyPath}": {
      "get": {
        "summary": "Upstream API Proxy",
        "description": "Authenticated reverse proxy endpoint forwarding to configured upstream targets.",
        "parameters": [
          {
            "name": "proxyPath",
            "in": "path",
            "required": true,
            "schema": { "type": "string" }
          },
          {
            "name": "X-API-Key",
            "in": "header",
            "required": true,
            "schema": { "type": "string", "example": "aegis_live_free_key_1001" }
          },
          {
            "name": "X-Aegis-Signature",
            "in": "header",
            "required": false,
            "description": "HMAC-SHA256 signature for zero-allocation replay defense (t=timestamp,v1=signature)",
            "schema": { "type": "string" }
          }
        ],
        "responses": {
          "200": {
            "description": "Successful upstream response",
            "headers": {
              "X-RateLimit-Limit": { "schema": { "type": "integer" } },
              "X-RateLimit-Remaining": { "schema": { "type": "integer" } },
              "X-RateLimit-Status": { "schema": { "type": "string", "example": "NORMAL" } },
              "X-Cache-Status": { "schema": { "type": "string", "example": "MISS" } }
            }
          },
          "401": { "description": "Invalid or missing API key" },
          "403": { "description": "API Key frozen by kill-switch" },
          "429": { "description": "Rate limit exceeded (soft and hard limits exhausted)" },
          "503": { "description": "Upstream circuit breaker OPEN with no cache fallback available" }
        }
      }
    }
  }
}`

func swaggerUIHTML(specURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>AegisPulse — Swagger API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css">
  <style>
    body { margin: 0; background: #0b0f19; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    .topbar { display: none !important; }
    .swagger-ui { filter: invert(88%%) hue-rotate(180deg); }
    .swagger-ui .info { margin: 20px 0; }
    .swagger-ui .scheme-container { background: transparent; box-shadow: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        url: "%s",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        layout: "BaseLayout"
      });
    };
  </script>
</body>
</html>`, specURL)
}

// SwaggerHandler serves the Swagger/OpenAPI documentation and raw OpenAPI 3.0 JSON specification.
func SwaggerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Raw OpenAPI JSON spec
		if path == "/docs/openapi.json" || path == "/docs/swagger.json" || strings.HasSuffix(path, "/openapi.json") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(openAPISpecJSON))
			return
		}

		// Swagger UI HTML Page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(swaggerUIHTML("/docs/openapi.json")))
	}
}
