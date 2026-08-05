# API Reference — Simple Video Streaming Application

Base URL (local): `http://localhost:8080`
Base URL (Cloud Run): `https://<service-name>-<hash>-<region>.a.run.app`

All request/response bodies are JSON unless otherwise noted (video upload
uses `multipart/form-data`; streaming responses are raw binary video bytes).

## Table of Contents

- [Authentication](#authentication)
- [Common Response Conventions](#common-response-conventions)
- [Errors](#errors)
- [Endpoints](#endpoints)
  - [GET /healthz](#get-healthz)
  - [GET /readyz](#get-readyz)
  - [POST /videos](#post-videos)
  - [GET /videos](#get-videos)
  - [GET /videos/{id}](#get-videosid)
  - [GET /videos/{id}/stream](#get-videosidstream)
- [Content-Type Allow-List](#content-type-allow-list)
- [Pagination](#pagination)
- [HTTP Range Requests](#http-range-requests)

---

## Authentication

Authentication is a shared-secret API key, configured via the `API_KEY`
environment variable on the server. **If `API_KEY` is unset, authentication
is disabled** on all routes (useful for local development or when fronted
by another auth layer).

Two ways to supply the key:

| Method                | Applies to                                         |
|-----------------------|-----------------------------------------------------|
| `X-API-Key: <key>` header | All authenticated routes                        |
| `?api_key=<key>` query parameter | **Only** `GET /videos/{id}/stream` (see note below) |

> **Note:** The streaming endpoint additionally accepts the API key as a
> query parameter because the browser's native `<video>` element cannot
> attach custom HTTP headers. This is a deliberate, narrowly-scoped
> trade-off; the key may appear in access logs or browser history for this
> path only. All other routes require the header.

Requests without a valid key receive:

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"error":"unauthorized"}
```

## Common Response Conventions

- All JSON responses set `Content-Type: application/json; charset=utf-8`.
- All responses include baseline security headers:
  `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer`, `Cache-Control: no-store`.
- If `ALLOWED_ORIGIN` is configured server-side, matching cross-origin
  requests receive CORS headers (`Access-Control-Allow-Origin`,
  `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`), and
  `OPTIONS` preflight requests receive `204 No Content`.

## Errors

All error responses share a single JSON envelope:

```json
{ "error": "<human-readable, non-sensitive message>" }
```

| HTTP Status | Meaning                                                        |
|-------------|-----------------------------------------------------------------|
| 400         | Malformed request (bad multipart body, invalid ID, bad query param) |
| 401         | Missing/invalid API key                                        |
| 404         | Video not found                                                |
| 405         | HTTP method not allowed on this route                          |
| 413         | Uploaded file exceeds `MAX_UPLOAD_SIZE_MB`                      |
| 415         | Unsupported video content type                                 |
| 416         | Range Not Satisfiable — invalid `Range` header                 |
| 500         | Internal server error (masked; details logged server-side)     |
| 503         | Service not ready (readiness probe failure)                    |

Internal errors never leak SDK error strings, stack traces, or GCS object
paths to the client.

---

## Endpoints

### GET /healthz

Liveness probe. Performs no downstream calls — reports whether the process
itself is alive, distinct from readiness of its dependencies.

**Auth:** none

**Request:**
```
GET /healthz
```

**Response — 200 OK:**
```json
{ "status": "ok" }
```

---

### GET /readyz

Readiness probe. Verifies connectivity to GCS and Firestore. Use this for
Cloud Run/Kubernetes readiness checks and load-balancer health checks that
should exclude instances with broken downstream dependencies.

**Auth:** none

**Request:**
```
GET /readyz
```

**Response — 200 OK:**
```json
{ "status": "ready" }
```

**Response — 503 Service Unavailable:**
```json
{ "error": "service not ready" }
```

---

### POST /videos

Uploads a new video. The file is streamed directly to Google Cloud Storage
without buffering the full payload in memory; metadata is then persisted to
Firestore.

**Auth:** `X-API-Key` header (if `API_KEY` is configured)

**Content-Type:** `multipart/form-data`

**Form Fields:**

| Field  | Type | Required | Description                                   |
|--------|------|----------|-------------------------------------------------|
| `file` | file | yes      | The video binary. Must match an allow-listed content type. |

**Constraints:**
- Maximum size: `MAX_UPLOAD_SIZE_MB` (default 500 MB), enforced at the
  transport layer via `http.MaxBytesReader`.
- Content type must be one of the [allow-listed types](#content-type-allow-list).
- Exactly one file is accepted per request; only the field named `file` is
  processed.
- Empty files (0 bytes) are rejected.

**Example Request:**

```bash
curl -X POST https://api.example.com/videos \
  -H "X-API-Key: $API_KEY" \
  -F "file=@sample.mp4;type=video/mp4"
```

**Response — 201 Created:**

```json
{
  "id": "3f29b1d4-8c1e-4a2b-9c34-7e2f6a1d5b90",
  "filename": "sample.mp4",
  "content_type": "video/mp4",
  "size_bytes": 15728640,
  "created_at": "2024-05-01T12:34:56.789Z"
}
```

> Note: the internal GCS object path (`gcs_object`) is never included in
> API responses.

**Error Responses:**

| Status | Condition                                             |
|--------|--------------------------------------------------------|
| 400    | Missing `file` field, malformed multipart body, empty file |
| 413    | File exceeds `MAX_UPLOAD_SIZE_MB`                        |
| 415    | Content type not in the allow-list                       |
| 500    | Storage write or metadata persistence failure            |

---

### GET /videos

Returns a paginated list of video metadata, ordered newest-first.

**Auth:** `X-API-Key` header (if `API_KEY` is configured)

**Query Parameters:**

| Param       | Type   | Required | Default | Notes                                   |
|-------------|--------|----------|---------|-------------------------------------------|
| `page_size` | int    | no       | 20      | Clamped to a maximum of 100                |
| `page_token`| string | no       | —       | Opaque cursor from a previous response's `next_page_token` |

**Example Request:**

```bash
curl "https://api.example.com/videos?page_size=10" \
  -H "X-API-Key: $API_KEY"
```

**Response — 200 OK:**

```json
{
  "videos": [
    {
      "id": "3f29b1d4-8c1e-4a2b-9c34-7e2f6a1d5b90",
      "filename": "sample.mp4",
      "content_type": "video/mp4",
      "size_bytes": 15728640,
      "created_at": "2024-05-01T12:34:56.789Z"
    }
  ],
  "next_page_token": "eyJ0IjoxNzE0NTY3Njk2Nzg5MDAwMDAwLCJpZCI6IjNmMjliMWQ0In0"
}
```

`next_page_token` is omitted when there are no further pages.

**Error Responses:**

| Status | Condition                          |
|--------|--------------------------------------|
| 400    | Invalid `page_size` (non-numeric / ≤ 0) |
| 500    | Metadata listing failure              |

---

### GET /videos/{id}

Returns metadata for a single video.

**Auth:** `X-API-Key` header (if `API_KEY` is configured)

**Path Parameters:**

| Param | Type   | Notes                            |
|-------|--------|-------------------------------------|
| `id`  | string | UUID identifying the video          |

**Example Request:**

```bash
curl https://api.example.com/videos/3f29b1d4-8c1e-4a2b-9c34-7e2f6a1d5b90 \
  -H "X-API-Key: $API_KEY"
```

**Response — 200 OK:**

```json
{
  "id": "3f29b1d4-8c1e-4a2b-9c34-7e2f6a1d5b90",
  "filename": "sample.mp4",
  "content_type": "video/mp4",
  "size_bytes": 15728640,
  "created_at": "2024-05-01T12:34:56.789Z"
}
```

**Error Responses:**

| Status | Condition                    |
|--------|---------------------------------|
| 400    | Malformed video ID              |
| 404    | No video found with this ID     |
| 500    | Metadata retrieval failure      |

---

### GET /videos/{id}/stream

Streams the raw video bytes for playback. Supports HTTP `Range` requests
so clients (e.g. HTML5 `<video>`) can seek without downloading the entire
file.

**Auth:** `X-API-Key` header **or** `?api_key=` query parameter (if
`API_KEY` is configured) — see [Authentication](#authentication).

**Path Parameters:**

| Param | Type   | Notes                    |
|-------|--------|----------------------------|
| `id`  | string | UUID identifying the video |

**Request Headers:**

| Header  | Required | Notes                                                        |
|---------|----------|-----------------------------------------------------------------|
| `Range` | no       | `bytes=<start>-<end>`, `bytes=<start>-`, or `bytes=-<suffixLength>` |

**Example — full download:**

```bash
curl "https://api.example.com/videos/3f29b1d4-8c1e-4a2b-9c34-7e2f6a1d5b90/stream?api_key=$API_KEY" \
  -o video.mp4
```

**Response — 200 OK (no `Range` header):**

```
HTTP/1.1 200 OK
Accept-Ranges: bytes
Content-Type: video/mp4
Content-Length: 15728640

<binary video bytes>
```

**Example — ranged request (seek):**

```bash
curl -H "Range: bytes=1000000-1999999" \
  "https://api.example.com/videos/3f29b1d4-8c1e-4a2b-9c34-7e2f6a1d5b90/stream?api_key=$API_KEY" \
  -o chunk.mp4
```

**Response — 206 Partial Content:**

```
HTTP/1.1 206 Partial Content
Accept-Ranges: bytes
Content-Type: video/mp4
Content-Range: bytes 1000000-1999999/15728640
Content-Length: 1000000

<binary chunk bytes>
```

**Error Responses:**

| Status | Condition                                              |
|--------|-----------------------------------------------------------|
| 400    | Malformed video ID                                        |
| 401    | Missing/invalid API key                                   |
| 404    | Video not found                                            |
| 416    | `Range` header cannot be satisfied for the object's size   |

`416` responses include a `Content-Range: bytes */<size>` header indicating
the total resource size, per RFC 7233.

---

## Content-Type Allow-List

Only the following MIME types are accepted for upload and returned on
streaming responses:

- `video/mp4`
- `video/webm`
- `video/ogg`
- `video/quicktime`
- `video/x-matroska`

Any other content type on upload results in `415 Unsupported Media Type`.

## Pagination

`GET /videos` uses cursor-based pagination:

1. Call `GET /videos?page_size=20` (or your desired page size, ≤ 100).
2. If the response includes `next_page_token`, pass it as `page_token` on
   the next request to retrieve the following page.
3. Absence of `next_page_token` indicates the last page has been reached.

Page tokens are opaque, base64-encoded cursors and must not be
constructed manually; treat them as an opaque string.

## HTTP Range Requests

The streaming endpoint supports single-range requests per RFC 7233:

| Range Header Example      | Meaning                                  |
|----------------------------|-------------------------------------------|
| `bytes=0-999`               | First 1000 bytes                          |
| `bytes=1000-`               | From byte 1000 to end of file             |
| `bytes=-500`                | Last 500 bytes of the file                |

Multi-range requests (comma-separated ranges) are collapsed to their first
range only, which is sufficient for standard browser video-seek behavior.
Invalid or out-of-bounds ranges return `416 Range Not Satisfiable` with a
`Content-Range: bytes */<total-size>` header.
