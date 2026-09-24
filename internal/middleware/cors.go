package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSConfig defines the settings for Cross-Origin Resource Sharing.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	MaxAge           int
	AllowCredentials bool
	SecurityHeaders  bool
}

// DefaultCORSConfig returns standard permissive defaults for web clients and tools.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Accept",
			"Origin",
			"X-API-Key",
			"X-Aegis-Signature",
			"traceparent",
		},
		ExposedHeaders: []string{
			"X-RateLimit-Limit",
			"X-RateLimit-Remaining",
			"X-RateLimit-Burst-Limit",
			"X-RateLimit-Burst-Remaining",
			"X-RateLimit-Status",
			"X-Cache-Status",
			"X-Aegis-Trace-ID",
		},
		MaxAge:           86400,
		AllowCredentials: false, // Must be false if AllowedOrigins contains "*"
		SecurityHeaders:  true,
	}
}

// CORSMiddleware wraps an http.Handler with CORS and browser security headers.
type CORSMiddleware struct {
	cfg CORSConfig
}

// NewCORSMiddleware creates a new CORS and security middleware handler.
func NewCORSMiddleware(cfg CORSConfig) *CORSMiddleware {
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{"*"}
	}
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	return &CORSMiddleware{cfg: cfg}
}

// Wrap returns an http.Handler that executes the CORS & security policies before calling next.
func (cm *CORSMiddleware) Wrap(next http.Handler) http.Handler {
	allowedMethodsStr := strings.Join(cm.cfg.AllowedMethods, ", ")
	allowedHeadersStr := strings.Join(cm.cfg.AllowedHeaders, ", ")
	exposedHeadersStr := strings.Join(cm.cfg.ExposedHeaders, ", ")
	maxAgeStr := strconv.Itoa(cm.cfg.MaxAge)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// 1. Inject Standard Defensive Security Headers
		if cm.cfg.SecurityHeaders {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		}

		// 2. Resolve Allowed Origin
		if origin != "" {
			matched := false
			for _, o := range cm.cfg.AllowedOrigins {
				if o == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
					matched = true
					break
				}
				if strings.EqualFold(o, origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if cm.cfg.AllowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
					w.Header().Add("Vary", "Origin")
					matched = true
					break
				}
			}

			if matched {
				w.Header().Set("Access-Control-Expose-Headers", exposedHeadersStr)
			}
		}

		// 3. Handle OPTIONS Preflight
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", allowedMethodsStr)
			w.Header().Set("Access-Control-Allow-Headers", allowedHeadersStr)
			w.Header().Set("Access-Control-Max-Age", maxAgeStr)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
