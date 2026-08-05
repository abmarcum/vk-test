// Package main - handlers.go
//
// HTTP handler layer implementing the video streaming API surface:
// upload, streaming with HTTP Range support, listing, metadata retrieval,
// and health/readiness probes.
//
// This file assumes the following contracts are satisfied elsewhere in the
// package:
//
//   models.go:
//     type Video struct {
//         ID          string    `json:"id"`
//         Filename    string    `json:"filename"`
//         ContentType string    `json:"content_type"`
//         SizeBytes   int64     `json:"size_bytes"`
//         GCSObject   string    `json:"gcs_object"`
//         CreatedAt   time.Time `json:"created_at"`
//     }
//     type VideoPage struct {
//         Videos        []*Video `json:"videos"`
//         NextPageToken string   `json:"next_page_token,omitempty"`
//     }
//     var ErrNotFound = errors.New("not found")
//     var ErrInvalidID = errors.New("invalid id")
//
//   storage.go:
//     A concrete type (e.g. *Storage) implementing the VideoStore interface
//     declared below (accept-interfaces, return-structs idiom).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// uploadFormField is the multipart form field name expected to carry
	// the video file bytes.
	uploadFormField = "file"

	defaultPageSize = 20
	maxPageSize     = 100

	readyzTimeout = 3 * time.Second
)

// allowedContentTypes is an explicit allowlist of accepted video MIME
// types. Anything outside this set is rejected to reduce the risk of
// storing/serving arbitrary or malicious content.
var allowedContentTypes = map[string]bool{
	"video/mp4":        true,
	"video/webm":       true,
	"video/ogg":        true,
	"video/quicktime":  true,
	"video/x-matroska": true,
}

// videoIDPattern restricts video identifiers to UUID form, preventing
// path traversal or injection when identifiers are used to build GCS
// object paths.
var videoIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// VideoStore is the data-access contract required by the handler layer.
// It is implemented by storage.go and wraps GCS (object bytes) and
// Firestore (metadata).
type VideoStore interface {
	SaveVideoObject(ctx context.Context, objectName, contentType string, r io.Reader) (int64, error)
	StreamVideoObject(ctx context.Context, objectName string, start, length int64) (io.ReadCloser, error)
	SaveMetadata(ctx context.Context, v *Video) error
	GetMetadata(ctx context.Context, id string) (*Video, error)
	ListMetadata(ctx context.Context, pageSize int, pageToken string) (*VideoPage, error)
	Ready(ctx context.Context) error
}

// HandlerSet groups the dependencies required by the HTTP handlers and
// exposes them as methods, keeping the handler layer stateless and
// horizontally scalable.
type HandlerSet struct {
	Store          VideoStore
	Logger         *slog.Logger
	MaxUploadBytes int64
}

// NewHandlerSet constructs a HandlerSet with sane defaults.
func NewHandlerSet(store VideoStore, logger *slog.Logger, maxUploadBytes int64) *HandlerSet {
	if logger == nil {
		logger = slog.Default()
	}
	return &HandlerSet{Store: store, Logger: logger, MaxUploadBytes: maxUploadBytes}
}

// ---------------------------------------------------------------------------
// JSON response helpers
// ---------------------------------------------------------------------------

type errorResponse struct {
	Error string `json:"error"`
}

func (h *HandlerSet) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.Logger.Error("failed to encode json response", "error", err)
	}
}

// writeError logs the internal error (if any) for observability while
// returning a masked, client-safe message to avoid leaking implementation
// details.
func (h *HandlerSet) writeError(w http.ResponseWriter, status int, clientMsg string, internalErr error) {
	if internalErr != nil {
		h.Logger.Error(clientMsg, "error", internalErr, "status", status)
	}
	h.writeJSON(w, status, errorResponse{Error: clientMsg})
}

func mapStoreError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "video not found"
	case errors.Is(err, ErrInvalidID):
		return http.StatusBadRequest, "invalid video id"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

// ---------------------------------------------------------------------------
// UploadVideoHandler
// ---------------------------------------------------------------------------

// UploadVideoHandler handles POST /videos. It streams a multipart file
// directly into GCS without buffering the whole file in memory, validates
// content type, generates a UUID, and persists metadata to Firestore.
func (h *HandlerSet) UploadVideoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	ctx := r.Context()

	// Hard cap request body size at the transport level to defend against
	// resource-exhaustion attacks, regardless of Content-Length header
	// correctness.
	r.Body = http.MaxBytesReader(w, r.Body, h.MaxUploadBytes)

	mr, err := r.MultipartReader()
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid multipart request", err)
		return
	}

	var video *Video

	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(perr, &maxErr) {
				h.writeError(w, http.StatusRequestEntityTooLarge, "file exceeds maximum allowed size", perr)
				return
			}
			h.writeError(w, http.StatusBadRequest, "malformed multipart body", perr)
			return
		}

		if part.FormName() != uploadFormField {
			_ = part.Close()
			continue
		}

		contentType := normalizeContentType(part.Header.Get("Content-Type"))
		if !allowedContentTypes[contentType] {
			_ = part.Close()
			h.writeError(w, http.StatusUnsupportedMediaType,
				fmt.Sprintf("unsupported content type: %q", contentType), nil)
			return
		}

		id := uuid.New().String()
		objectName := fmt.Sprintf("videos/%s%s", id, sanitizeExt(part.FileName()))

		size, serr := h.Store.SaveVideoObject(ctx, objectName, contentType, part)
		_ = part.Close()
		if serr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(serr, &maxErr) {
				h.writeError(w, http.StatusRequestEntityTooLarge, "file exceeds maximum allowed size", serr)
				return
			}
			h.writeError(w, http.StatusInternalServerError, "failed to store video", serr)
			return
		}
		if size == 0 {
			h.writeError(w, http.StatusBadRequest, "uploaded file is empty", nil)
			return
		}

		video = &Video{
			ID:          id,
			Filename:    sanitizeFilename(part.FileName()),
			ContentType: contentType,
			SizeBytes:   size,
			GCSObject:   objectName,
			CreatedAt:   time.Now().UTC(),
		}
		break // Only a single video file is accepted per upload.
	}

	if video == nil {
		h.writeError(w, http.StatusBadRequest,
			fmt.Sprintf("missing required form field %q", uploadFormField), nil)
		return
	}

	if err := h.Store.SaveMetadata(ctx, video); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to save video metadata", err)
		return
	}

	h.writeJSON(w, http.StatusCreated, video)
}

// ---------------------------------------------------------------------------
// StreamVideoHandler
// ---------------------------------------------------------------------------

// StreamVideoHandler handles GET /videos/{id}/stream. It parses the HTTP
// Range header (if present) and proxies only the requested byte range from
// GCS, avoiding full-file buffering.
func (h *HandlerSet) StreamVideoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	id, err := extractVideoID(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid video id", err)
		return
	}

	ctx := r.Context()
	video, err := h.Store.GetMetadata(ctx, id)
	if err != nil {
		status, msg := mapStoreError(err)
		h.writeError(w, status, msg, err)
		return
	}

	size := video.SizeBytes
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", video.ContentType)

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		h.proxyBytes(w, ctx, video.GCSObject, 0, size, id)
		return
	}

	start, end, rerr := parseRangeHeader(rangeHeader, size)
	if rerr != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		h.writeError(w, http.StatusRequestedRangeNotSatisfiable, "invalid range request", rerr)
		return
	}

	length := end - start + 1
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	h.proxyBytes(w, ctx, video.GCSObject, start, length, id)
}

// proxyBytes streams `length` bytes starting at `start` from the object
// store to the response writer. Headers must already be written by the
// caller before invoking this function.
func (h *HandlerSet) proxyBytes(w http.ResponseWriter, ctx context.Context, objectName string, start, length int64, id string) {
	rc, err := h.Store.StreamVideoObject(ctx, objectName, start, length)
	if err != nil {
		// Headers/status are already flushed; we can only log server-side.
		h.Logger.Error("failed to open video object stream", "error", err, "video_id", id)
		return
	}
	defer rc.Close()

	if _, err := io.Copy(w, rc); err != nil {
		// Typically caused by client disconnect/seek during playback; not
		// an application error, but recorded for observability.
		h.Logger.Warn("stream copy interrupted", "error", err, "video_id", id)
	}
}

// ---------------------------------------------------------------------------
// ListVideosHandler / GetVideoHandler
// ---------------------------------------------------------------------------

// ListVideosHandler handles GET /videos, returning a paginated list of
// video metadata records.
func (h *HandlerSet) ListVideosHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	q := r.URL.Query()
	pageSize := defaultPageSize
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			h.writeError(w, http.StatusBadRequest, "invalid page_size parameter", err)
			return
		}
		pageSize = n
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	pageToken := q.Get("page_token")

	page, err := h.Store.ListMetadata(r.Context(), pageSize, pageToken)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list videos", err)
		return
	}
	h.writeJSON(w, http.StatusOK, page)
}

// GetVideoHandler handles GET /videos/{id}, returning a single video's
// metadata.
func (h *HandlerSet) GetVideoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}

	id, err := extractVideoID(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid video id", err)
		return
	}

	video, err := h.Store.GetMetadata(r.Context(), id)
	if err != nil {
		status, msg := mapStoreError(err)
		h.writeError(w, status, msg, err)
		return
	}

	h.writeJSON(w, http.StatusOK, video)
}

// ---------------------------------------------------------------------------
// Health / readiness
// ---------------------------------------------------------------------------

// HealthzHandler is a liveness probe. It performs no downstream calls so
// that Cloud Run can distinguish "process alive" from "dependencies up".
func (h *HandlerSet) HealthzHandler(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadyzHandler is a readiness probe verifying downstream dependencies
// (GCS/Firestore connectivity) are reachable.
func (h *HandlerSet) ReadyzHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
	defer cancel()

	if err := h.Store.Ready(ctx); err != nil {
		h.writeError(w, http.StatusServiceUnavailable, "service not ready", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// ---------------------------------------------------------------------------
// Range-header parsing utilities
// ---------------------------------------------------------------------------

// parseRangeHeader parses a single-range HTTP `Range` header value of the
// form "bytes=start-end", "bytes=start-", or "bytes=-suffixLength" against
// a resource of the given total size. It returns an inclusive byte range
// [start, end]. Multi-range requests are collapsed to their first range,
// which is sufficient for typical HTML5 video seeking behavior.
func parseRangeHeader(header string, size int64) (start, end int64, err error) {
	if size <= 0 {
		return 0, 0, errors.New("resource has zero size")
	}

	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, fmt.Errorf("unsupported range unit in header %q", header)
	}

	spec := strings.TrimPrefix(header, prefix)
	if i := strings.Index(spec, ","); i != -1 {
		spec = spec[:i]
	}
	spec = strings.TrimSpace(spec)

	parts := strings.SplitN(spec, "-", 2)
	if len
