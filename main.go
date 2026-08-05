// Command video-streaming-app is the entry point for the Simple Video
// Streaming Application. It is responsible ONLY for:
//   - loading configuration from environment variables
//   - constructing the GCS/Firestore-backed StorageClient (storage.go)
//   - constructing the HandlerSet (handlers.go)
//   - wiring the HTTP router and middleware chain
//   - managing graceful startup/shutdown for Cloud Run
//
// No business logic lives in this file.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// -----------------------------------------------------------------------------
// Configuration
// -----------------------------------------------------------------------------

// Config holds all runtime configuration sourced from environment variables.
// Cloud Run injects PORT automatically; all other values are set via the
// service manifest / Secret Manager.
type Config struct {
	Port            string
	GCSBucket       string
	GCPProjectID    string
	MaxUploadSizeMB int64
	APIKey          string // empty disables API key auth
	AllowedOrigin   string // empty disables CORS headers entirely
	StaticDir       string // directory containing web/player.html assets

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// MaxUploadSizeBytes returns the configured upload ceiling in bytes.
func (c *Config) MaxUploadSizeBytes() int64 {
	return c.MaxUploadSizeMB * 1024 * 1024
}

// loadConfig reads configuration from the environment, applying sane
// defaults and validating required fields fail-fast at startup.
func loadConfig() (*Config, error) {
	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		GCSBucket:         getEnv("GCS_BUCKET_NAME", ""),
		GCPProjectID:      getEnv("GCP_PROJECT_ID", ""),
		MaxUploadSizeMB:   getEnvInt64("MAX_UPLOAD_SIZE_MB", 500),
		APIKey:            getEnv("API_KEY", ""),
		AllowedOrigin:     getEnv("ALLOWED_ORIGIN", ""),
		StaticDir:         getEnv("STATIC_DIR", "web"),
		ReadHeaderTimeout: getEnvDuration("READ_HEADER_TIMEOUT_SECONDS", 10) * time.Second,
		ReadTimeout:       getEnvDuration("READ_TIMEOUT_SECONDS", 0) * time.Second, // 0 == unbounded, needed for large uploads
		WriteTimeout:      getEnvDuration("WRITE_TIMEOUT_SECONDS", 0) * time.Second, // 0 == unbounded, needed for long streams
		IdleTimeout:       getEnvDuration("IDLE_TIMEOUT_SECONDS", 120) * time.Second,
		ShutdownTimeout:   getEnvDuration("SHUTDOWN_TIMEOUT_SECONDS", 25) * time.Second,
	}

	var errs []string
	if cfg.GCSBucket == "" {
		errs = append(errs, "GCS_BUCKET_NAME is required")
	}
	if cfg.GCPProjectID == "" {
		errs = append(errs, "GCP_PROJECT_ID is required")
	}
	if cfg.MaxUploadSizeMB <= 0 {
		errs = append(errs, "MAX_UPLOAD_SIZE_MB must be a positive integer")
	}
	if len(errs) > 0 {
		return nil, errors.New("invalid configuration: " + strings.Join(errs, "; "))
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getEnvInt64(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func getEnvDuration(key string, defSeconds int64) time.Duration {
	return time.Duration(getEnvInt64(key, defSeconds))
}

// -----------------------------------------------------------------------------
// Entry point
// -----------------------------------------------------------------------------

func main() {
	logger := newLogger()
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("configuration error", "error", err)
		os.Exit(1)
	}

	// Root context governs the lifetime of GCP clients. It is cancelled on
	// SIGTERM/SIGINT (the signals Cloud Run sends on scale-down/deploy).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// storage.go: NewStorageClient constructs and owns the GCS + Firestore
	// SDK clients, exposing the data-access methods used by handlers.go.
	store, err := NewStorageClient(ctx, cfg.GCPProjectID, cfg.GCSBucket)
	if err != nil {
		logger.Error("failed to initialize storage client", "error", err)
		os.Exit(1)
	}
	defer func() {
		if cerr := store.Close(); cerr != nil {
			logger.Error("error closing storage client", "error", cerr)
		}
	}()

	// handlers.go: NewHandlerSet wires the data-access layer + config into
	// the HTTP handler methods.
	h := NewHandlerSet(store, cfg, logger)

	router := buildRouter(h, cfg, logger)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	select {
	case err := <-serverErrCh:
		if err != nil {
			logger.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
		shutdown(srv, cfg.ShutdownTimeout, logger)
	}

	logger.Info("server stopped cleanly")
}

// shutdown performs a bounded graceful shutdown, allowing in-flight
// uploads/streams to complete (or be cut off) within the configured window.
func shutdown(srv *http.Server, timeout time.Duration, logger *slog.Logger) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed, forcing close", "error", err)
		if cerr := srv.Close(); cerr != nil {
			logger.Error("forced close failed", "error", cerr)
		}
	}
}

// newLogger configures structured (JSON) logging suitable for Cloud Logging
// ingestion. Cloud Logging parses the "severity"-style level automatically
// when using slog's JSON handler with default keys.
func newLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: false,
	})
	return slog.New(handler)
}

// -----------------------------------------------------------------------------
// Router & middleware chain
// -----------------------------------------------------------------------------

// buildRouter registers all routes (net/http 1.22+ method-aware ServeMux)
// and applies the middleware chain in the order:
//
//	recover -> logging -> security headers -> (route-specific) auth/body-limit
//
// handlers.go is expected to expose an *HandlerSet with the following
// methods, each satisfying http.HandlerFunc:
//
//	UploadVideo, ListVideos, GetVideo, StreamVideo, Healthz, Readyz
func buildRouter(h *HandlerSet, cfg *Config, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Health checks: unauthenticated, no body-size limit, cheap.
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)

	// Video API - protected by general API key auth.
	mux.Handle("POST /videos", apiKeyAuth(cfg)(maxBodySize(cfg)(http.HandlerFunc(h.UploadVideo))))
	mux.Handle("GET /videos", apiKeyAuth(cfg)(http.HandlerFunc(h.ListVideos)))
	mux.Handle("GET /videos/{id}", apiKeyAuth(cfg)(http.HandlerFunc(h.GetVideo)))

	// Streaming endpoint uses a dedicated "stream auth" middleware: the
	// HTML5 <video> element cannot attach custom headers, so in addition to
	// the X-API-Key header this route accepts ?api_key=<key> as a query
	// parameter. This is a deliberate, documented trade-off (see
	// docs/architecture.md tech-debt register) and is only enabled on this
	// single read-only endpoint.
	mux.Handle("GET /videos/{id}/stream", streamAuth(cfg)(http.HandlerFunc(h.StreamVideo)))

	// Optional static player UI (Tier 1 UI spec): serves web/player.html and
	// any co-located static assets (style.css, app.js). Not authenticated;
	// contains no sensitive data.
	if cfg.StaticDir != "" {
		fs := http.FileServer(http.Dir(cfg.StaticDir))
		mux.Handle("GET /", fs)
	}

	var handler http.Handler = mux
	handler = securityHeaders(cfg)(handler)
	handler = loggingMiddleware(logger)(handler)
	handler = recoverMiddleware(logger)(handler)
	return handler
}

// -----------------------------------------------------------------------------
// Middleware implementations
// -----------------------------------------------------------------------------

// statusRecorder captures the response status code for structured logging
// without altering the semantics of the underlying ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// loggingMiddleware logs one structured line per request: method, path,
// status, duration and remote address. It never logs request/response
// bodies (avoids leaking video bytes or credentials into logs).
func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", clientIP(r),
			)
		})
	}
}

// recoverMiddleware ensures a panic in any handler (e.g. a nil pointer while
// proxying GCS bytes) is converted into a 500 response instead of crashing
// the process, and masks internal error detail from the client.
func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						"error", rec,
						"path", r.URL.Path,
						"method", r.Method,
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// securityHeaders applies baseline OWASP-recommended response headers and,
// when configured, a restrictive CORS policy. CORS is opt-in via
// ALLOWED_ORIGIN; by default the app is same-origin only (no CORS headers).
func securityHeaders(cfg *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cache-Control", "no-store")

			if cfg.AllowedOrigin != "" {
				origin := r.Header.Get("Origin")
				if origin == cfg.AllowedOrigin {
					h.Set("Access-Control-Allow-Origin", cfg.AllowedOrigin)
					h.Set("Vary", "Origin")
					h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
				}
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// apiKeyAuth enforces a shared-secret API key via the X-API-Key header.
// When Config.APIKey is empty, auth is disabled (useful for local dev /
// environments fronted by another auth layer). Comparison is constant-time
// to avoid timing side channels.
func apiKeyAuth(cfg *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if cfg.APIKey == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			supplied := r.Header.Get("X-API-Key")
			if !constantTimeEquals(supplied, cfg.APIKey) {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// streamAuth is a variant of apiKeyAuth used exclusively for the
// GET /videos/{id}/stream route. Because the browser's native <video>
// element issues plain GET requests (with Range headers) and cannot attach
// custom headers, this middleware additionally accepts the key via the
// "api_key" query parameter. This is intentionally scoped to a read-only,
// non-mutating endpoint to limit exposure (keys may appear in access logs /
// browser history for this path only).
func streamAuth(cfg *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if cfg.APIKey == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			supplied := r.Header.Get("X-API-Key")
			if supplied == "" {
				supplied = r.URL.Query().Get("api_key")
			}
			if !constantTimeEquals(supplied, cfg.APIKey) {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// maxBodySize protects the server from unbounded/oversized uploads by
// wrapping the request body in http.MaxBytesReader. Enforced only on the
// upload route; streaming/list/get responses are unaffected.
func maxBodySize(cfg *Config) func(http.Handler) http.Handler {
	limit := cfg.MaxUploadSizeBytes()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func constantTimeEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

// clientIP extracts a best-effort client address for logging purposes only
// (not used for any security decision, since X-Forwarded-For is
// attacker-controllable unless verified by the trusted Cloud Run/GFE proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}
```

**Integration contract assumed from sibling files** (for the other files' implementers):

- `storage.go` exposes:
  `func NewStorageClient(ctx context.Context, projectID, bucket string) (*StorageClient, error)` and `func (s *StorageClient) Close() error`.
- `handlers.go` exposes:
  `func NewHandlerSet(store *StorageClient, cfg *Config, logger *slog.Logger) *HandlerSet` with methods `UploadVideo`, `ListVideos`, `GetVideo`, `StreamVideo`, `Healthz`, `Readyz` matching `http.HandlerFunc`.
- `models.go` may define shared error/DTO types used internally by `storage.go`/`handlers.go`; `main.go` has no direct dependency on it.
