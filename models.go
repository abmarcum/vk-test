package main

import (
	"errors"
	"time"
)

// -----------------------------------------------------------------------------
// Sentinel Errors
// -----------------------------------------------------------------------------
// These errors are returned by the storage layer (storage.go) and inspected
// by the HTTP handlers (handlers.go) using errors.Is to map internal failures
// to the correct HTTP status codes without leaking implementation details.
var (
	// ErrNotFound indicates the requested video metadata does not exist.
	ErrNotFound = errors.New("video not found")

	// ErrInvalidContentType indicates the uploaded file's content type is not
	// in the allow-list of supported video formats.
	ErrInvalidContentType = errors.New("unsupported content type")

	// ErrFileTooLarge indicates the uploaded file exceeds the configured
	// maximum upload size.
	ErrFileTooLarge = errors.New("file exceeds maximum allowed size")

	// ErrEmptyFile indicates the uploaded file contained zero bytes.
	ErrEmptyFile = errors.New("uploaded file is empty")

	// ErrInvalidID indicates a malformed or empty video ID was supplied.
	ErrInvalidID = errors.New("invalid video id")

	// ErrInvalidRange indicates a malformed HTTP Range header was supplied.
	ErrInvalidRange = errors.New("invalid range header")
)

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------
const (
	// DefaultMaxUploadSizeMB is the fallback maximum upload size (in
	// megabytes) used when MAX_UPLOAD_SIZE_MB is not set via environment
	// variable. Overridable via Config.
	DefaultMaxUploadSizeMB = 512

	// DefaultPageSize is the default number of items returned by
	// GET /videos when the caller does not specify a page size.
	DefaultPageSize = 20

	// MaxPageSize caps the number of items that can be requested in a single
	// page, protecting against unbounded/expensive Firestore reads.
	MaxPageSize = 100

	// FirestoreCollection is the name of the Firestore collection storing
	// video metadata documents.
	FirestoreCollection = "videos"

	// StreamBufferSize is the chunk size (bytes) used when proxying bytes
	// from GCS to the HTTP response writer during streaming.
	StreamBufferSize = 32 * 1024

	// APIKeyHeader is the HTTP header name used for optional API key auth.
	APIKeyHeader = "X-API-Key"
)

// AllowedContentTypes is the allow-list of MIME types accepted for video
// uploads. Using an allow-list (rather than a deny-list) mitigates the risk
// of accepting arbitrary/malicious file types (OWASP: unrestricted file
// upload).
var AllowedContentTypes = map[string]bool{
	"video/mp4":       true,
	"video/webm":      true,
	"video/quicktime": true,
	"video/x-matroska": true,
	"video/ogg":       true,
}

// IsAllowedContentType reports whether the given content type is permitted
// for upload. Comparison is exact (case-sensitive per MIME convention); the
// caller is responsible for normalizing/trimming input beforehand.
func IsAllowedContentType(contentType string) bool {
	return AllowedContentTypes[contentType]
}

// -----------------------------------------------------------------------------
// Domain Model
// -----------------------------------------------------------------------------

// Video represents the persisted metadata document for an uploaded video, as
// stored in Firestore. Field names use snake_case via firestore tags to match
// the documented data model.
type Video struct {
	ID          string    `firestore:"id" json:"id"`
	Filename    string    `firestore:"filename" json:"filename"`
	ContentType string    `firestore:"content_type" json:"content_type"`
	SizeBytes   int64     `firestore:"size_bytes" json:"size_bytes"`
	GCSObject   string    `firestore:"gcs_object" json:"-"` // internal storage path, never exposed via API
	CreatedAt   time.Time `firestore:"created_at" json:"created_at"`
}

// ToResponse converts the internal Video model into its public-facing DTO,
// deliberately omitting internal fields such as the raw GCS object path.
func (v Video) ToResponse() VideoResponse {
	return VideoResponse{
		ID:          v.ID,
		Filename:    v.Filename,
		ContentType: v.ContentType,
		SizeBytes:   v.SizeBytes,
		CreatedAt:   v.CreatedAt,
		StreamURL:   "/videos/" + v.ID + "/stream",
	}
}

// -----------------------------------------------------------------------------
// DTOs (API Wire Types)
// -----------------------------------------------------------------------------

// VideoResponse is the public representation of a video's metadata returned
// by the API. It intentionally excludes internal storage details (e.g. the
// GCS object path) to avoid leaking infrastructure information to clients.
type VideoResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
	StreamURL   string    `json:"stream_url"`
}

// ListVideosResponse is the paginated response body for GET /videos.
type ListVideosResponse struct {
	Videos     []VideoResponse `json:"videos"`
	NextCursor string          `json:"next_cursor,omitempty"`
	PageSize   int             `json:"page_size"`
}

// ErrorResponse is the standard JSON error envelope returned by the API on
// failure. Messages are deliberately generic for 5xx-class failures to avoid
// leaking internal implementation details (error masking).
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// NewErrorResponse constructs an ErrorResponse with the given short error
// code/category and an optional human-readable message.
func NewErrorResponse(errCode, message string) ErrorResponse {
	return ErrorResponse{Error: errCode, Message: message}
}
