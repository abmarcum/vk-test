package main

// storage.go — Data access layer.
//
// StorageClient wraps Google Cloud Storage (binary video bytes) and
// Firestore (video metadata) behind a single, cohesive type. It is the
// only component in this application that talks directly to GCP client
// SDKs; handlers.go must never import "cloud.google.com/go/storage" or
// "cloud.google.com/go/firestore" directly.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Storage-internal sentinel errors. Only ErrNotFound and ErrInvalidID
// (defined in models.go) are pattern-matched by handlers.go; everything
// else below results in a masked HTTP 500 response.
var (
	// ErrObjectNotFound indicates the requested GCS object does not exist.
	ErrObjectNotFound = errors.New("storage: object not found")
	// ErrMetadataConflict indicates a document with the given id already exists.
	ErrMetadataConflict = errors.New("storage: metadata already exists")
	// ErrUnavailable indicates a dependency (GCS/Firestore) is unreachable.
	ErrUnavailable = errors.New("storage: backend unavailable")
)

const (
	defaultListPageSize = DefaultPageSize
	maxListPageSize     = MaxPageSize
)

// StorageClient is the single data-access facade used by handlers.go.
type StorageClient struct {
	gcs        *storage.Client
	bucketName string
	bucket     *storage.BucketHandle

	fs         *firestore.Client
	collection string

	logger *slog.Logger
}

// NewStorageClient constructs GCS and Firestore clients using Application
// Default Credentials and wires them into a StorageClient. Client lifecycle
// (Close) is managed by the returned StorageClient's Close method, invoked
// by main.go during graceful shutdown.
func NewStorageClient(ctx context.Context, projectID, bucketName string) (*StorageClient, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("storage: projectID is required")
	}
	if strings.TrimSpace(bucketName) == "" {
		return nil, fmt.Errorf("storage: bucketName is required")
	}

	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create GCS client: %w", err)
	}

	fsClient, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		_ = gcsClient.Close()
		return nil, fmt.
