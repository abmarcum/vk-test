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
	"encoding/json"
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
