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
		ReadTimeout:       getEnvDuration("READ_TIMEOUT_SECONDS", 0) * time.Second,  // 0 == unbounded, needed for large uploads
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
func shutdown(srv
