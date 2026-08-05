package main

// storage.go — Data access layer.
//
// StorageClient wraps Google Cloud Storage (binary video bytes) and
// Firestore (video metadata) behind a single, cohesive type. It is the
// only component in this application that talks directly to GCP client
// SDKs; handlers.go must never import "cloud.google.com/go/storage" or
// "cloud.google.com/go/firestore" directly.
//
// Contract with models.go:
//   - type Video struct {
//         ID          string    `firestore:"id"          json:"id"`
//         Filename    string    `firestore:"filename"    json:"filename"`
//         ContentType string    `firestore:"content_type" json:"content_type"`
//         SizeBytes   int64     `firestore:"size_bytes"  json:"size_bytes"`
//         GCSObject   string    `firestore:"gcs_object"  json:"gcs_object"`
//         CreatedAt   time.Time `firestore:"created_at"  json:"created_at"`
//     }
//
// Client lifecycle (creation of *storage.Client / *firestore.Client) is
// owned by main.go; StorageClient only wraps already-constructed clients
// so that credentials and connection pooling are configured in exactly
// one place.

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

// Sentinel errors returned by StorageClient methods. Handlers must use
// errors.Is against these to map to the correct HTTP status code, and
// must never leak underlying SDK error strings to clients (error
// masking / information disclosure prevention).
var (
	// ErrObjectNotFound indicates the requested GCS object does not exist.
	ErrObjectNotFound = errors.New("storage: object not found")
	// ErrMetadataNotFound indicates no Firestore document exists for the id.
	ErrMetadataNotFound = errors.New("storage: metadata not found")
	// ErrMetadataConflict indicates a document with the given id already exists.
	ErrMetadataConflict = errors.New("storage: metadata already exists")
	// ErrInvalidInput indicates a caller-supplied argument failed validation.
	ErrInvalidInput = errors.New("storage: invalid input")
	// ErrUnavailable indicates a dependency (GCS/Firestore) is unreachable.
	ErrUnavailable = errors.New("storage: backend unavailable")
)

const (
	defaultListPageSize = 20
	maxListPageSize     = 100
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

// NewStorageClient wires an already-initialized GCS client and Firestore
// client into a StorageClient. Both clients and their lifecycle (Close)
// remain owned by main.go... except that Close on StorageClient is
// provided as a convenience for a single, orderly shutdown path.
func NewStorageClient(gcsClient *storage.Client, fsClient *firestore.Client, bucketName, collection string, logger *slog.Logger) (*StorageClient, error) {
	if gcsClient == nil || fsClient == nil {
		return nil, fmt.Errorf("storage: nil client passed to NewStorageClient")
	}
	if strings.TrimSpace(bucketName) == "" {
		return nil, fmt.Errorf("storage: bucketName is required")
	}
	if strings.TrimSpace(collection) == "" {
		return nil, fmt.Errorf("storage: collection is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &StorageClient{
		gcs:        gcsClient,
		bucketName: bucketName,
		bucket:     gcsClient.Bucket(bucketName),
		fs:         fsClient,
		collection: collection,
		logger:     logger,
	}, nil
}

// validateObjectName defends against path traversal / control-character
// injection into GCS object names, since object names may be derived
// from user-supplied filenames.
func validateObjectName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: object name is empty", ErrInvalidInput)
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: object name contains illegal path segment", ErrInvalidInput)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: object name contains control characters", ErrInvalidInput)
		}
	}
	if len(name) > 1024 {
		return fmt.Errorf("%w: object name too long", ErrInvalidInput)
	}
	return nil
}

func validateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidInput)
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("%w: id contains illegal characters", ErrInvalidInput)
	}
	return nil
}

// ---------------------------------------------------------------------
// GCS: object storage
// ---------------------------------------------------------------------

// SaveVideoObject streams r into the GCS object identified by
// objectName, setting the given contentType. It never buffers the full
// file in memory. Returns the number of bytes written. On any failure,
// it best-effort deletes the partially written object to avoid orphans.
func (s *StorageClient) SaveVideoObject(ctx context.Context, objectName, contentType string, r io.Reader) (int64, error) {
	if err := validateObjectName(objectName); err != nil {
		return 0, err
	}
	if strings.TrimSpace(contentType) == "" {
		return 0, fmt.Errorf("%w: contentType is empty", ErrInvalidInput)
	}

	obj := s.bucket.Object(objectName)
	w := obj.NewWriter(ctx)
	w.ContentType = contentType
	// ChunkSize left at SDK default for resumable, memory-bounded uploads.

	written, copyErr := io.Copy(w, r)
	closeErr := w.Close()

	if copyErr != nil || closeErr != nil {
		// Best-effort cleanup of a partial object; ignore cleanup errors.
		_ = obj.Delete(context.WithoutCancel(ctx))
		if copyErr != nil {
			s.logger.Error("gcs upload copy failed", "object", objectName, "err", copyErr)
			return 0, fmt.Errorf("storage: upload failed: %w", ErrUnavailable)
		}
		s.logger.Error("gcs upload close failed", "object", objectName, "err", closeErr)
		return 0, fmt.Errorf("storage: upload finalize failed: %w", ErrUnavailable)
	}

	return written, nil
}

// ObjectSize returns the size in bytes of the stored GCS object.
func (s *StorageClient) ObjectSize(ctx context.Context, objectName string) (int64, error) {
	if err := validateObjectName(objectName); err != nil {
		return 0, err
	}
	attrs, err := s.bucket.Object(objectName).Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return 0, ErrObjectNotFound
		}
		s.logger.Error("gcs stat failed", "object", objectName, "err", err)
		return 0, fmt.Errorf("storage: stat failed: %w", ErrUnavailable)
	}
	return attrs.Size, nil
}

// StreamVideoObject returns a ReadCloser over the byte range
// [offset, offset+length) of the object (length <= 0 means "to EOF").
// Callers (handlers.go) are responsible for closing the returned reader.
func (s *StorageClient) StreamVideoObject(ctx context.Context, objectName string, offset, length int64) (io.ReadCloser, error) {
	if err := validateObjectName(objectName); err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, fmt.Errorf("%w: negative offset", ErrInvalidInput)
	}
	if length < 0 {
		length = -1 // GCS convention: negative length reads to EOF.
	}

	r, err := s.bucket.Object(objectName).NewRangeReader(ctx, offset, length)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, ErrObjectNotFound
		}
		s.logger.Error("gcs range read failed", "object", objectName, "offset", offset, "length", length, "err", err)
		return nil, fmt.Errorf("storage: stream open failed: %w", ErrUnavailable)
	}
	return r, nil
}

// DeleteVideoObject removes the underlying GCS object. Missing objects
// are treated as a successful no-op from the caller's perspective is a
// design choice left to handlers.go; here we surface ErrObjectNotFound
// so callers can decide.
func (s *StorageClient) DeleteVideoObject(ctx context.Context, objectName string) error {
	if err := validateObjectName(objectName); err != nil {
		return err
	}
	if err := s.bucket.Object(objectName).Delete(ctx); err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return ErrObjectNotFound
		}
		s.logger.Error("gcs delete failed", "object", objectName, "err", err)
		return fmt.Errorf("storage: delete failed: %w", ErrUnavailable)
	}
	return nil
}

// ---------------------------------------------------------------------
// Firestore: metadata persistence
// ---------------------------------------------------------------------

// SaveMetadata persists a new Video metadata document, keyed by
// video.ID. It uses Create semantics so an accidental duplicate UUID
// collision is surfaced rather than silently overwritten.
func (s *StorageClient) SaveMetadata(ctx context.Context, video *Video) error {
	if video == nil {
		return fmt.Errorf("%w: video is nil", ErrInvalidInput)
	}
	if err := validateID(video.ID); err != nil {
		return err
	}
	if strings.TrimSpace(video.Filename) == "" || strings.TrimSpace(video.ContentType) == "" {
		return fmt.Errorf("%w: filename/content_type required", ErrInvalidInput)
	}
	if video.CreatedAt.IsZero() {
		video.CreatedAt = time.Now().UTC()
	}

	doc := s.fs.Collection(s.collection).Doc(video.ID)
	_, err := doc.Create(ctx, video)
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return ErrMetadataConflict
		}
		s.logger.Error("firestore create failed", "id", video.ID, "err", err)
		return fmt.Errorf("storage: metadata save failed: %w", ErrUnavailable)
	}
	return nil
}

// GetMetadata fetches a single Video document by id.
func (s *StorageClient) GetMetadata(ctx context.Context, id string) (*Video, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}

	snap, err := s.fs.Collection(s.collection).Doc(id).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrMetadataNotFound
		}
		s.logger.Error("firestore get failed", "id", id, "err", err)
		return nil, fmt.Errorf("storage: metadata fetch failed: %w", ErrUnavailable)
	}

	var v Video
	if err := snap.DataTo(&v); err != nil {
		s.logger.Error("firestore decode failed", "id", id, "err", err)
		return nil, fmt.Errorf("storage: metadata decode failed: %w", ErrUnavailable)
	}
	return &v, nil
}

// listCursor is the internal shape of an opaque pagination token.
// Encoding it as base64(JSON) keeps ListMetadata stateless while
// avoiding leaking raw Firestore internals to API clients.
type listCursor struct {
	CreatedAtUnixNano int64  `json:"t"`
	ID                string `json:"id"`
}

func encodeCursor(v *Video) string {
	c := listCursor{CreatedAtUnixNano: v.CreatedAt.UnixNano(), ID: v.ID}
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(token string) (*listCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed page token", ErrInvalidInput)
	}
	var c listCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%w: malformed page token", ErrInvalidInput)
	}
	return &c, nil
}

// ListMetadata returns a page of Video metadata ordered newest-first,
// along with an opaque token for the next page (empty string when there
// are no more results). pageSize <= 0 falls back to a sane default and
// is clamped to maxListPageSize to bound resource usage.
func (s *StorageClient) ListMetadata(ctx context.Context, pageSize int, pageToken string) ([]*Video, string, error) {
	if pageSize <= 0 {
		pageSize = defaultListPageSize
	}
	if pageSize > maxListPageSize {
		pageSize = maxListPageSize
	}

	q := s.fs.Collection(s.collection).
		OrderBy("created_at", firestore.Desc).
		OrderBy("id", firestore.Desc).
		Limit(pageSize)

	if pageToken != "" {
		cur, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", err
		}
		q = q.StartAfter(time.Unix(0, cur.CreatedAtUnixNano).UTC(), cur.ID)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	videos := make([]*Video, 0, pageSize)
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			s.logger.Error("firestore list failed", "err", err)
			return nil, "", fmt.Errorf("storage: metadata list failed: %w", ErrUnavailable)
		}
		var v Video
		if err := snap.DataTo(&v); err != nil {
			s.logger.Error("firestore decode failed during list", "err", err)
			return nil, "", fmt.Errorf("storage: metadata decode failed: %w", ErrUnavailable)
		}
		videos = append(videos, &v)
	}

	var next string
	if len(videos) == pageSize {
		next = encodeCursor(videos[len(videos)-1])
	}
	return videos, next, nil
}

// ---------------------------------------------------------------------
// Health & lifecycle
// ---------------------------------------------------------------------

// Ping performs lightweight reachability checks against both GCS and
// Firestore, suitable for backing GET /readyz. It must not perform
// expensive operations (e.g., listing all objects).
func (s *StorageClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if _, err := s.bucket.Attrs(ctx); err != nil {
		s.logger.Error("readiness: gcs bucket check failed", "bucket", s.bucketName, "err", err)
		return fmt.Errorf("%w: gcs unreachable", ErrUnavailable)
	}

	iter := s.fs.Collection(s.collection).Limit(1).Documents(ctx)
	defer iter.Stop()
	if _, err := iter.Next(); err != nil && err != iterator.Done {
		s.logger.Error("readiness: firestore check failed", "collection", s.collection, "err", err)
		return fmt.Errorf("%w: firestore unreachable", ErrUnavailable)
	}

	return nil
}

// Close releases underlying GCP client resources. Safe to call once
// during graceful shutdown in main.go.
func (s *StorageClient) Close() error {
	var errs []error
	if err := s.gcs.Close(); err != nil {
		errs = append(errs, fmt.Errorf("gcs client close: %w", err))
	}
	if err := s.fs.Close(); err != nil {
		errs = append(errs, fmt.Errorf("firestore client close: %w", err))
	}
	return errors.Join(errs...)
}
