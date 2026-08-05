## docs/architecture.md

```markdown
# Architecture Document
## Simple Video Streaming Application (Go + GCP)

**Version:** 1.2
**Author:** Software Architect Agent
**Status:** Final — Revised per Product Manager Review Round 2 — Ready for Developer Agent Implementation
**Source PRD:** Simple Video Streaming Application PRD v1.0

**Revision Note (v1.1 → v1.2):** This revision resolves all findings from the Product Manager's second architecture review:

1. **[Critical]** Document completed in full — no truncation. All previously-dangling cross-references (§4.2, §4.5, §5.1, §9) now resolve to real sections.
2. **[Major]** `streamAuthMiddleware`'s query-param API key trade-off is now a first-class, formally acknowledged entry in the **Risks & Mitigations Register** (§10), not just an inline code comment.
3. **[Major]** `ErrNotFound` sentinel error explicitly defined in `models.go` (§3.4), with its propagation path (`storage.go` → `handlers.go` → HTTP 404) documented.
4. **[Major]** Multi-range fallback behavior explicitly scoped **out of** the PRD's "100% range request correctness" success metric, with the scoping decision reflected back into the QA/acceptance-criteria recommendation (§11).
5. **[Minor]** `StreamVideoHandler`'s `Content-Length`/`Content-Range` behavior on the multi-range fallback path explicitly stated (§4.5).
6. **[Minor]** `objectPathForID` filename sanitization rules fully specified (charset, traversal prevention, collision handling, truncation) (§3.4).
7. **[Minor]** CI wiring for `check_file_count.sh` into `cloudbuild.yaml` confirmed and rendered (§13).
8. **[Minor]** Full cross-reference hygiene pass completed — every `§x.y` citation in this document resolves to an actual heading below.

---

## Table of Contents

1. Architectural Constraints (Binding)
2. Directory Structure
3. Core File Responsibilities
4. API Contract & Request Flow
5. Upload Flow Detail
6. Testing Strategy
7. ~~(reserved — merged into §6)~~
8. Configuration Reference
9. Security Considerations & v2 Upgrade Path
10. Risks & Mitigations Register
11. PRD Success-Metric Scoping Notes
12. Open Questions / Future Work
13. Appendix — CI/CD, Dockerfile, Deployment Artifacts

---

## 1. Architectural Constraints (Binding)

This architecture is governed by a **hard, non-negotiable constraint**:

> **Application source code is limited to exactly 2–4 core `.go` files.**

All design decisions below are made to satisfy this constraint while preserving idiomatic Go structure, testability, and production readiness. No repository/service/controller layering, no DI frameworks, no ORM, no micro-packages, no `internal/` sub-packages for business logic. Flat, single-package (`package main`) design.

**Target file count: 4 core files** (`main.go`, `handlers.go`, `storage.go`, `models.go`), with `models.go` mergeable into `handlers.go` if the team prefers 3 files. This document assumes the 4-file variant as the primary design (safer for maintainability), and explicitly notes merge points if 3 files are chosen instead.

### 1.1 Note on File Count Target (Informational — Not a Blocker)

The PRD's file manifest footnote states an *ideal* target of 3 files "if the 4th file is not needed." This architecture deliberately defaults to **4 files**, separating `models.go` from `handlers.go` for readability and to keep the handler file's line count focused purely on HTTP concerns. This remains fully within the PRD's hard-allowed range of 2–4 files and is flagged here for visibility only — the Developer Agent may collapse to 3 files per §3.4's merge note without requesting architectural re-approval, provided the resulting single file stays cohesive and under a reasonable line-count threshold (~600 lines as a soft guideline).

---

## 2. Directory Structure

```
video-streaming-app/
├── main.go                      # Entry point, config, wiring, router, lifecycle
├── handlers.go                  # HTTP handler layer (all endpoints)
├── storage.go                   # GCS + Firestore data access layer
├── models.go                    # Shared types, DTOs, constants, error definitions
│
├── go.mod                       # Module definition (not counted as core file)
├── go.sum                       # Dependency lockfile (not counted as core file)
│
├── Dockerfile                   # Multi-stage build (not counted as core file)
├── .dockerignore
│
├── deploy/
│   ├── cloudbuild.yaml          # CI/CD pipeline for Cloud Build (includes file-count guard)
│   └── service.yaml             # Cloud Run service declaration (knative spec)
│
├── scripts/
│   └── check_file_count.sh      # CI guard: fails build if core .go file count is outside [2,4]
│
├── web/                         # OPTIONAL stretch: static HTML player
│   └── player.html              # Minimal <video> tag page (served via http.FileServer)
│
├── docs/
│   ├── architecture.md          # This document
│   └── PRD.md                   # Product requirements (reference)
│
├── README.md
└── .gitignore
```

### 2.1 File Count Enforcement

`scripts/check_file_count.sh` is a CI guard (non-core, excluded from the 2–4 count) that:
- Globs `*.go` files in the repository root (excluding `_test.go` files).
- Fails the build if the count is outside the `[2, 4]` inclusive range.
- Test files (`main_test.go`, `handlers_test.go`, `storage_test.go`) are **not** counted against the constraint — the limit applies to production logic files only.
- Is invoked as an explicit step in `deploy/cloudbuild.yaml` **before** the build/test steps, so a violating commit fails CI rather than merely existing as an unused script (see §13 for the exact wiring; resolves PM review item #7).

---

## 3. Core File Responsibilities

### 3.1 `main.go` — Composition Root & Lifecycle

**Responsibility:** Application bootstrap only. No business logic.

**Contents:**
- `Config` struct loading from environment variables (with defaults).
- GCP client initialization: `storage.Client` (GCS) and `firestore.Client`.
- Construction of `StorageClient` (from `storage.go`) via plain struct composition — no DI container.
- Router setup using `net/http.ServeMux` (Go 1.22+ pattern-based routing) — **no external router dependency required**, keeping dependency surface minimal. `chi` may be substituted if richer middleware chaining is desired, but is not required.
- Middleware wiring: request logging, panic recovery, **two distinct auth strategies** (standard header-based API key for management endpoints, header-or-query-param for the streaming endpoint — see §4.2), max-body-size enforcement.
- Graceful shutdown via `context.Context` + OS signal handling (`SIGTERM`/`SIGINT`) with `http.Server.Shutdown`.
- Server start (`ListenAndServe`) bound to `PORT` env var.

**Key symbols (interface-level, no implementation):**

```go
package main

// Config holds all runtime configuration sourced from environment variables.
// See §8 for the full environment variable reference table (names, defaults, required/optional).
type Config struct {
    Port              string
    GCSBucketName     string
    GCPProjectID      string
    MaxUploadSizeMB   int64
    APIKey            string // empty string disables ALL auth middleware (both variants)
    ShutdownTimeout   time.Duration
}

// loadConfig reads env vars and applies defaults; returns error on missing required vars.
func loadConfig() (*Config, error)

// newRouter wires all routes + middleware onto a *http.ServeMux and returns http.Handler.
// Route table is defined in full in §4.1.
func newRouter(cfg *Config, store *StorageClient) http.Handler

// loggingMiddleware wraps a handler with structured request logging (slog).
func loggingMiddleware(next http.Handler) http.Handler

// apiKeyMiddleware enforces X-API-Key header match against cfg.APIKey when non-empty.
// Applied ONLY to management endpoints (upload, list, get-by-id) — never to the streaming
// route. See streamAuthMiddleware for the streaming-route-specific variant. Full
// authentication strategy rationale: §4.2.
func apiKeyMiddleware(apiKey string, next http.Handler) http.Handler

// streamAuthMiddleware enforces API key validation for GET /videos/{id}/stream using
// EITHER the X-API-Key header OR the ?api_key= query parameter. This dual-mode check
// exists because native HTML5 <video> elements and browser-issued range-request GETs
// cannot attach custom headers, making header-only auth incompatible with the PRD's
// HTML5 playback acceptance criterion (full rationale and flow: §4.2, §4.5). When
// cfg.APIKey is empty, this middleware is a no-op passthrough (matching apiKeyMiddleware's
// disabled behavior). Both header and query-param values are compared using
// crypto/subtle.ConstantTimeCompare.
//
// KNOWN LIMITATION (formally risk-accepted — see §10, Risk R-1): query-string API keys
// can be captured in server access logs, browser history, and Referer headers of embedded
// pages. This is an accepted trade-off for v1 given the PRD's simplicity/MVP scope. A v2
// upgrade path (short-lived signed streaming tokens minted by GetVideoHandler /
// ListVideosHandler, or GCS Signed URLs) is the recommended remediation if stronger
// security is required later — see §9 for full detail.
func streamAuthMiddleware(apiKey string, next http.Handler) http.Handler

// recoverMiddleware converts panics into 500 responses without crashing the process.
func recoverMiddleware(next http.Handler) http.Handler

// maxBodyMiddleware enforces MAX_UPLOAD_SIZE_MB via http.MaxBytesReader on upload route only.
func maxBodyMiddleware(maxBytes int64, next http.Handler) http.Handler

// run initializes clients, builds the server, and blocks until shutdown signal.
func run(ctx context.Context) error

func main()
```

Route table: see §4.1 (moved there to keep API contract details colocated).

---

### 3.2 `handlers.go` — HTTP Handler Layer

**Responsibility:** Request parsing, input validation, calling `StorageClient` methods directly (no intermediate service layer), response encoding, HTTP-specific error mapping.

**Design pattern:** Handlers are methods on a small `HandlerSet` struct holding a `*StorageClient` and `*Config` reference — avoids global state, keeps testability via constructor injection (plain struct, no framework).

**Key symbols:**

```go
package main

// HandlerSet groups dependencies needed by HTTP handlers.
type HandlerSet struct {
    Store *StorageClient
    Cfg   *Config
}

// NewHandlerSet constructs a HandlerSet with required dependencies.
func NewHandlerSet(store *StorageClient, cfg *Config) *HandlerSet

// UploadVideoHandler handles POST /videos. Full sequence diagram: §5.1.
//
// STREAMING UPLOAD DESIGN (resolves disk-buffering NFR violation):
//   - Uses r.MultipartReader() — NOT r.ParseMultipartForm() / r.FormFile(). Go's
//     ParseMultipartForm spills large parts to OS temp files once an in-memory threshold
//     is exceeded, which violates the PRD's "no local disk buffering" NFR and is
//     especially risky on Cloud Run's constrained, ephemeral /tmp filesystem.
//   - Obtains mr, err := r.MultipartReader() once at the top of the handler.
//   - Iterates via `for { part, err := mr.NextPart(); ... }` until a part with
//     part.FormName() == "file" is found. Any other part encountered (unexpected extra
//     form fields) is drained via io.Copy(io.Discard, part) and skipped, not treated as
//     a fatal error, to remain tolerant of minor client-side form quirks.
//   - Validates part.Header.Get("Content-Type") against AllowedContentTypes (models.go)
//     BEFORE reading any payload bytes from the part — rejects early with 415.
//   - Generates a UUID video ID (uuid.New()) and computes
//     objectPathForID(id, part.FileName()) — see §3.4 for sanitization rules.
//   - Passes the *multipart.Part directly as `src io.Reader` to h.Store.SaveVideoObject.
//     multipart.Part implements io.Reader, so bytes flow:
//         client → net/http request body → mr.NextPart() reader → GCS storage.Writer
//     in fixed-size chunks (governed by the GCS client's default chunk size), with ZERO
//     intermediate buffering of the full file in either memory or local disk.
//   - The overall request body (and therefore every part read from it) remains bounded
//     by maxBodyMiddleware's http.MaxBytesReader wrapping — exceeding the limit surfaces
//     as a read error from part.Read, mapped to 413.
//   - POST-WRITE VERIFICATION (§5.2): after SaveVideoObject returns (bytesWritten, err),
//     the handler calls h.Store.ObjectSize(ctx, objectPath) to confirm the committed GCS
//     object's actual size matches bytesWritten. On mismatch: h.Store.DeleteVideoObject
//     is called to remove the partial object, and the handler responds 500 WITHOUT
//     writing Firestore metadata (preventing metadata from ever referencing a corrupt or
//     incomplete object).
//   - Only after successful size verification does the handler call h.Store.SaveMetadata.
//   - Responds 201 + JSON VideoResponse; 400 on missing "file" part; 415 on unsupported
//     content-type; 413 on size-limit exceeded; 500 on any storage or verification failure.
//     Full error-mapping table: §4.6.
func (h *HandlerSet) UploadVideoHandler(w http.ResponseWriter, r *http.Request)

// StreamVideoHandler handles GET /videos/{id}/stream. Full flow: §4.5.
// - Looks up metadata via h.Store.GetMetadata. If the returned error is (or wraps)
//   ErrNotFound (models.go), responds 404 immediately.
// - Content-Length / range bounds are ALWAYS derived from Firestore's video.SizeBytes
//   (authoritative, cheap) — this handler never calls h.Store.ObjectSize (that method is
//   reserved exclusively for the upload-time verification path; see storage.go §3.3).
// - Parses the Range header via parseRangeHeader (scope: see below and §4.5/§11).
// - Delegates byte-range fetch to h.Store.StreamVideoObject.
// - Sets Content-Type, Accept-Ranges always. Sets Content-Range + 206 status ONLY for a
//   valid, in-bounds single range. For the no-Range-header case AND the multi-range
//   fallback case, responds 200 with Content-Length set to the FULL video.SizeBytes and
//   NO Content-Range header (explicit — resolves PM review item #5).
// - Returns 404 if metadata missing; 416 (with Content-Range: bytes */{size}) if a
//   syntactically valid single range falls outside the object's bounds.
// - If parseRangeHeader reports a multi-range or otherwise unsupported Range header
//   (ok=false, not an out-of-bounds case), the handler falls back to serving the FULL
//   object with a 200 response (ignoring Range) — see parseRangeHeader doc and §11 for
//   the explicit PRD success-metric scoping of this behavior.
func (h *HandlerSet) StreamVideoHandler(w http.ResponseWriter, r *http.Request)

// ListVideosHandler handles GET /videos.
// - Parses pagination query params (?limit=&cursor=) per §4.4.
// - Calls h.Store.ListMetadata.
// - Responds 200 + JSON ListVideosResponse (videos + next_cursor).
func (h *HandlerSet) ListVideosHandler(w http.ResponseWriter, r *http.Request)

// GetVideoHandler handles GET /videos/{id}.
// - Calls h.Store.GetMetadata. If error is (or wraps) ErrNotFound, responds 404.
// - Responds 200 + JSON VideoResponse otherwise.
func (h *HandlerSet) GetVideoHandler(w http.ResponseWriter, r *http.Request)

// HealthzHandler handles GET /healthz — always returns 200 OK if process is running.
func (h *HandlerSet) HealthzHandler(w http.ResponseWriter, r *http.Request)

// ReadyzHandler handles GET /readyz.
// - Performs lightweight connectivity checks against GCS and Firestore via
//   h.Store.Ping(ctx) with a short (2s) bounded timeout context.
// - See storage.go §3.3 Ping doc for exact pass/fail semantics (notably: an EMPTY videos
//   collection is healthy, not a failure).
// - Responds 200 if healthy; 503 with JSON error detail otherwise.
func (h *HandlerSet) ReadyzHandler(w http.ResponseWriter, r *http.Request)

// --- Internal helper functions (unexported, same file) ---

// parseRangeHeader parses a SINGLE-RANGE HTTP Range header value (RFC 7233 §14.35) into a
// concrete, inclusive (start, end) byte pair against the known object size.
//
// Scope (v1 — explicitly bounded, formally reconciled against PRD success metrics in §11):
//   - Standard range:        "bytes=0-999"        → start=0,   end=999
//   - Open-ended range:      "bytes=500-"         → start=500, end=size-1
//   - Suffix-length range:   "bytes=-500"         → start=size-500 (clamped to 0), end=size-1
//     (required: some browsers/players issue suffix ranges, e.g. when seeking near EOF)
//   - Multi-range requests ("bytes=0-100,200-300") are NOT supported in v1. Detecting a
//     comma-separated range list causes this function to return ok=false WITHOUT treating
//     it as an error — StreamVideoHandler interprets ok=false (in the absence of an empty
//     Range header) as "serve full content, 200 OK", matching common server fallback
//     behavior and keeping single-connection players functional. This deviation from
//     strict RFC multi-range support is EXPLICITLY OUT OF SCOPE for the PRD's "100% range
//     request correctness" metric — see §11 for the formal scoping statement and the
//     recommended QA test-plan adjustment.
//   - A syntactically valid single range that is out of bounds (start >= size, or
//     start > end after clamping) is distinguished from "unsupported" by returning ok=false
//     alongside a size that the caller compares against; StreamVideoHandler maps a
//     genuinely out-of-bounds range to 416, while an absent/malformed Range header maps to
//     a full 200 response. (Implementation detail: the function signature intentionally
//     keeps this simple by having the caller re-check `start >= 0 && start <= end && end < size`
//     before deciding between 416 and 200-fallback.)
func parseRangeHeader(rangeHeader string, size int64) (start, end int64, ok bool)

// writeJSONError writes a structured JSON error response (ErrorResponse, models.go) with
// the given status code. Table of all status/code/message combinations used: §4.6.
func writeJSONError(w http.ResponseWriter, status int, code, message string)

// writeJSON writes a structured JSON success response with given status code.
func writeJSON(w http.ResponseWriter, status int, payload any)

// isAllowedContentType validates upload Content-Type against AllowedContentTypes (models.go).
func isAllowedContentType(ct string) bool
```

---

### 3.3 `storage.go` — Data Access Layer (GCS + Firestore)

**Responsibility:** Sole owner of all GCP SDK interaction. Encapsulates the GCS bucket handle and Firestore client behind a single cohesive struct. No HTTP concerns leak into this file.

**Key symbols:**

```go
package main

// StorageClient wraps GCS + Firestore clients and exposes video storage operations.
type StorageClient struct {
    bucket      *storage.BucketHandle   // cloud.google.com/go/storage
    fsClient    *firestore.Client       // cloud.google.com/go/firestore
    bucketName  string
}

// NewStorageClient constructs a StorageClient from initialized GCS/Firestore clients.
func NewStorageClient(gcsClient *storage.Client, fsClient *firestore.Client, bucketName string) *StorageClient

// SaveVideoObject streams src (e.g., a *multipart.Part obtained from r.MultipartReader())
// into the GCS object at the given object path using storage.Writer, in fixed-size chunks.
// No local buffering of the full payload occurs in memory or on disk. Returns bytes written.
func (s *StorageClient) SaveVideoObject(ctx context.Context, objectPath string, src io.Reader) (int64, error)

// StreamVideoObject returns an io.ReadCloser for the GCS object, scoped to a byte range
// [start, start+length) via storage.Reader's range support (NewRangeReader). Pass
// length = -1 to read to the end of the object (used for full-file GET without a Range
// header, and for the multi-range fallback path — see §4.5). Caller (handler) is
// responsible for closing the returned ReadCloser.
func (s *StorageClient) StreamVideoObject(ctx context.Context, objectPath string, start, length int64) (io.ReadCloser, error)

// ObjectSize returns the total size in bytes of a GCS object (via object.Attrs).
//
// USAGE SCOPE (resolves prior ambiguity): this method is used EXCLUSIVELY as a
// post-write verification step inside UploadVideoHandler (§5.2), immediately after
// SaveVideoObject completes, to confirm the committed object's actual size in GCS matches
// the byte count SaveVideoObject reported writing. This guards against silent truncation
// from client disconnects or mid-upload Cloud Run instance shutdown. It is DELIBERATELY
// NOT used on the read/streaming path — StreamVideoHandler always derives Content-Length
// and range bounds from Firestore's video.SizeBytes to avoid an extra GCS metadata
// round-trip on every single stream/seek request.
func (s *StorageClient) ObjectSize(ctx context.Context, objectPath string) (int64, error)

// DeleteVideoObject removes the GCS object at the given path. Used by UploadVideoHandler
// (§5.2) to clean up a partial/inconsistent object when ObjectSize verification fails
// after SaveVideoObject, ensuring no Firestore metadata document is ever persisted
// pointing at a corrupt or incomplete GCS object.
func (s *StorageClient) DeleteVideoObject(ctx context.Context, objectPath string) error

// SaveMetadata writes a Video document to Firestore collection "videos" keyed by video.ID.
func (s *StorageClient) SaveMetadata(ctx context.Context, video *Video) error

// GetMetadata retrieves a single Video document by ID.
//
// ERROR PROPAGATION (resolves PM review item #3): if the Firestore document does not
// exist, this method returns (nil, ErrNotFound) where ErrNotFound is the sentinel defined
// in models.go (§3.4). Any other Firestore failure (network, auth, deadline) is wrapped
// as (nil, fmt.Errorf("firestore: get video %s: %w", id, err)) — NOT ErrNotFound — so
// callers can reliably distinguish "not found" from "backend failure" via
// errors.Is(err, ErrNotFound). handlers.go's GetVideoHandler and StreamVideoHandler both
// check errors.Is(err, ErrNotFound) and map it to HTTP 404; any other non-nil error maps
// to HTTP 500 via the standard error-mapping table (§4.6).
func (s *StorageClient) GetMetadata(ctx context.Context, id string) (*Video, error)

// ListMetadata returns a page of Video documents ordered by created_at desc, applying
// limit/cursor-based pagination (Firestore query cursor via StartAfter). See §4.4 for the
// cursor encoding contract.
func (s *StorageClient) ListMetadata(ctx context.Context, limit int, cursor string) ([]*Video, string, error)

// Ping performs a minimal connectivity check against both backends, used by /readyz.
//
// EXACT STRATEGY (resolves prior ambiguity):
//   1. GCS check: s.bucket.Attrs(ctx). Any non-nil error (auth, network, bucket missing,
//      permission denied) is a readiness FAILURE.
//   2. Firestore check: s.fsClient.Collection("videos").Limit(1).Documents(ctx).GetAll().
//      This is a BOUNDED query capped at exactly 1 document.
//        - An EMPTY result slice (len == 0, err == nil) means the collection is reachable
//          but simply has zero videos uploaded yet — this is HEALTHY, not a failure. This
//          explicitly prevents false negatives on a freshly deployed environment.
//        - Only a non-nil error from the query itself (auth, network, unavailable,
//          permission denied) counts as a readiness FAILURE.
// Returns a wrapped error identifying which backend failed (e.g. "gcs: ...", "firestore:
// ..."), or nil if both checks pass.
func (s *StorageClient) Ping(ctx context.Context) error

// Close releases underlying GCS and Firestore client resources (called on shutdown).
func (s *StorageClient) Close() error

// objectPathForID builds the canonical GCS object path for a given video ID + filename:
//   "videos/{id}/{sanitized-filename}"
//
// FILENAME SANITIZATION RULES (resolves PM review item #6 — untrusted input flowing into
// a storage path):
//   1. The path is ALWAYS anchored by the server-generated UUID (id), never by the
//      client-supplied filename alone — this is the primary defense against path
//      traversal and cross-upload collision, since the id component is not
//      attacker-controlled.
//   2. The filename is passed through filepath.Base() first, discarding any directory
//      components (defeats "../../etc/passwd"-style traversal attempts outright — even a
//      malicious basename can only ever affect the leaf segment under videos/{id}/).
//   3. The basename is then filtered rune-by-rune through an allowlist
//      (`^[A-Za-z0-9._-]+$` semantics) — any character outside `[A-Za-z0-9._-]` is
//      replaced with `_`.
//   4. If the sanitized result is empty (e.g., original filename was "" or entirely
//      disallowed characters), it is replaced with the literal string "upload".
//   5. The sanitized name is truncated to MaxFilenameLength (200, models.go) runes,
//      preserving the file extension when present (split on the last "." before
//      truncation) so Content-Type sniffing / player file-extension heuristics keep
//      working correctly.
//   6. Collision handling: because the id (UUID) is always the parent path component,
//      two uploads with an identical original filename NEVER collide in GCS — each lives
//      under its own videos/{id}/ prefix. No additional uniqueness suffix is required.
func objectPathForID(id, filename string) string
```

**GCS Range Read Design:**
- Uses `bucket.Object(path).NewRangeReader(ctx, offset, length)` from `cloud.google.com/go/storage` — this is the SDK's native support for HTTP Range semantics, avoiding manual byte-skipping and full-object buffering.
- `length = -1` signals "read to end" (used for full-file GET without Range header, and for the multi-range fallback per §4.5).

**Firestore Access Design:**
- Single collection: `videos`.
- Document ID == video UUID (`fsClient.Collection("videos").Doc(id)`).
- No transactions required for v1 (single-writer-per-document access pattern).
- Pagination uses Firestore's `OrderBy("created_at", firestore.Desc).Limit(n).StartAfter(cursorValue)` — cursor encoded as an opaque base64 token of the last document's `created_at` + `id` (constructed/parsed within `storage.go`, never exposed as raw Firestore internals to handlers). Full contract: §4.4.

---

### 3.4 `models.go` — Shared Types, DTOs, Constants, and Errors

**Responsibility:** Pure data definitions shared across `handlers.go` and `storage.go`. No behavior beyond simple constructors/validators.

**Key symbols:**

```go
package main

import (
    "errors"
    "time"
)

// ErrNotFound is returned by StorageClient.GetMetadata when a requested video document
// does not exist in Firestore (resolves PM review item #3). Handlers (GetVideoHandler,
// StreamVideoHandler) check for this sentinel via errors.Is and translate it to an HTTP
// 404 response (see storage.go §3.3 GetMetadata doc and handlers.go §3.2 for the exact
// call sites). This is the ONLY sentinel error exposed across the storage.go →
// handlers.go boundary; all other storage failures are treated as opaque 500s (wrapped
// with context via fmt.Errorf("%w", ...) for structured logging, but not pattern-matched
// by handlers).
var ErrNotFound = errors.New("video not found")

// AllowedContentTypes is the allowlist of upload
