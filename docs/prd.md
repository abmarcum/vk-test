# Product Requirements Document (PRD)

## Simple Video Streaming Application

**Version:** 1.0
**Author:** Product Manager Agent
**Language:** Go
**Cloud Provider:** GCP
**Status:** Draft for Engineering Handoff

---

## 1. Overview

### 1.1 Purpose
Build a lightweight, production-grade video streaming application in Go that allows users to upload videos and stream them on-demand via HTTP, backed by GCP Cloud Storage for durable object storage. The system prioritizes simplicity, minimal operational surface area, and a tightly scoped codebase.

### 1.2 Problem Statement
Teams often need a minimal, self-hostable video streaming service without the overhead of adopting a full media platform (e.g., Mux, YouTube API). This application provides a bare-bones but reliable upload → store → stream pipeline deployable on GCP with minimal infrastructure and code footprint.

### 1.3 Goals
- Enable video upload via HTTP API.
- Enable video playback via HTTP range-request streaming (supports seeking).
- Store video metadata and binary content reliably using GCP-native services.
- Keep the codebase to **2–4 core Go files** — no sprawling microservices or excessive abstraction layers.
- Deployable as a single containerized service on Cloud Run (or GCE/GKE as fallback).

### 1.4 Non-Goals
- No transcoding/adaptive bitrate streaming (e.g., HLS/DASH) in v1.
- No user authentication/authorization system (basic API key gate only, optional).
- No live streaming — on-demand only.
- No web frontend UI (API-first; a minimal HTML player page is optional/stretch).
- No multi-region replication in v1.

---

## 2. Target Users
- Internal engineering teams needing a lightweight video hosting backend.
- Small applications/startups needing a self-managed streaming API without vendor lock-in.
- Developers evaluating a minimal Go + GCP reference architecture.

---

## 3. User Stories

| ID | As a... | I want to... | So that... |
|----|---------|---------------|------------|
| US-1 | Content owner | Upload a video file via API | It gets stored durably and is available for streaming |
| US-2 | End user | Stream a video by ID | I can watch it in a browser/video player with seek support |
| US-3 | Content owner | List available videos | I can see what's been uploaded |
| US-4 | Operator | Deploy the app to GCP with minimal config | I can run it in production quickly |
| US-5 | Operator | View health/readiness status | I can monitor the service in Cloud Run/K8s |

---

## 4. Functional Requirements

### 4.1 Video Upload
- `POST /videos` — multipart/form-data upload.
- Validates content type (`video/mp4`, `video/webm`, `video/quicktime`).
- Generates unique video ID (UUID).
- Streams upload directly to **GCS bucket** (no local disk buffering of full file where avoidable).
- Persists metadata (ID, filename, content-type, size, upload timestamp, GCS object path) to **Firestore** (or in-memory/GCS-JSON fallback for simplicity — see §6).
- Returns `201 Created` with video ID and metadata.

### 4.2 Video Streaming
- `GET /videos/{id}/stream` — streams video content.
- Supports **HTTP Range Requests** (`Range` header) → returns `206 Partial Content` with `Content-Range`, `Accept-Ranges: bytes`.
- Proxies bytes from GCS object to client (server acts as pass-through, avoiding full buffering).
- Sets correct `Content-Type` and `Content-Length`/`Content-Range` headers.

### 4.3 Video Listing / Metadata
- `GET /videos` — lists all video metadata (paginated, simple limit/offset or cursor).
- `GET /videos/{id}` — returns metadata for a single video (404 if not found).

### 4.4 Health & Ops
- `GET /healthz` — liveness probe (always 200 if process is up).
- `GET /readyz` — readiness probe (checks GCS/Firestore connectivity).

### 4.5 Optional (Stretch)
- Simple API key middleware via `X-API-Key` header, validated against env var / Secret Manager.
- Minimal static HTML page with `<video>` tag pointing to stream endpoint.

---

## 5. Non-Functional Requirements

| Category | Requirement |
|----------|-------------|
| **Performance** | Stream endpoint must support range requests efficiently; no full-file buffering in memory. |
| **Scalability** | Stateless service — horizontally scalable on Cloud Run; storage/state externalized to GCS/Firestore. |
| **Reliability** | Graceful shutdown, context-based cancellation, proper error handling on GCS calls. |
| **Security** | Enforce file type/size validation on upload; optional API key auth; GCS bucket private with signed URLs or service-account-mediated access only (no public bucket). |
| **Observability** | Structured logging (JSON via `log/slog` or `zerolog`); request logging middleware; basic metrics endpoint optional (`/metrics` Prometheus format — stretch). |
| **Maintainability** | Codebase strictly limited to 2–4 core files. Idiomatic Go, no unnecessary frameworks. |
| **Portability** | Single binary + Dockerfile; deployable to Cloud Run with zero code changes across environments (config via env vars). |
| **Cost** | Use GCS Standard storage class; avoid always-on compute where possible (Cloud Run scale-to-zero). |

---

## 6. Architecture & Technical Design

### 6.1 High-Level Architecture

```
                 ┌─────────────────────┐
   Client ─────► │   Cloud Run (Go)     │
  (upload/watch) │  HTTP API Service    │
                 └─────────┬────────────┘
                           │
              ┌────────────┼─────────────┐
              ▼                          ▼
     ┌──────────────────┐      ┌────────────────────┐
     │  GCS Bucket        │      │  Firestore (native)│
     │  (video objects)    │      │  (video metadata)   │
     └──────────────────┘      └────────────────────┘
```

**Rationale for simplicity:** To respect the strict 2–4 file constraint, metadata storage will use **Firestore in Native mode** as a lightweight NoSQL store (no schema migrations, no SQL driver overhead) — this keeps data-access code minimal (a single client wrapper embedded in `storage.go`). GCS handles binary object storage with native support for streamed reads/writes and range GETs.

### 6.2 GCP Services Used
| Service | Purpose |
|---------|---------|
| **Cloud Run** | Hosts the containerized Go HTTP service (stateless, autoscaling). |
| **Cloud Storage (GCS)** | Durable storage for uploaded video binaries. |
| **Firestore (Native mode)** | Stores video metadata documents (ID, name, size, content-type, path, timestamps). |
| **Secret Manager** *(optional)* | Stores API key / service credentials if not using default service account. |
| **Cloud Logging** | Aggregates structured logs from `slog`/stdout. |
| **Artifact Registry** | Stores built container images for Cloud Run deployment. |

### 6.3 Data Model (Firestore Document: `videos/{id}`)
```json
{
  "id": "uuid",
  "filename": "string",
  "content_type": "string",
  "size_bytes": "int64",
  "gcs_object": "string (bucket path)",
  "created_at": "timestamp"
}
```

---

## 7. Code File Manifest (STRICT: 2–4 Core Files)

> This is a hard constraint. The application logic must be implemented within the following files only (plus standard config like `go.mod`, `Dockerfile`, which are not counted as "core code files").

| # | File | Responsibility |
|---|------|-----------------|
| 1 | **`main.go`** | Entry point: config loading (env vars), GCP client initialization (GCS + Firestore), router setup (`net/http` + `chi`/`http.ServeMux`), middleware wiring (logging, optional API key), graceful shutdown, server start. |
| 2 | **`handlers.go`** | HTTP handler functions: `UploadVideoHandler`, `StreamVideoHandler`, `ListVideosHandler`, `GetVideoHandler`, `HealthzHandler`, `ReadyzHandler`. Contains request parsing, validation, and response writing logic. |
| 3 | **`storage.go`** | Data access layer: `StorageClient` struct wrapping GCS bucket handle + Firestore client. Methods: `SaveVideoObject`, `StreamVideoObject` (range-aware reader), `SaveMetadata`, `GetMetadata`, `ListMetadata`. Encapsulates all GCP SDK interaction. |
| 4 *(optional)* | **`models.go`** | Shared types: `Video` struct, request/response DTOs, error types/constants. *(Can be merged into `handlers.go` if the 4th file is not needed — target 3 files as ideal.)* |

**Excluded from count:** `go.mod`, `go.sum`, `Dockerfile`, `README.md`, `PRD.md`, `.dockerignore`, CI config (`cloudbuild.yaml` or `.github/workflows/*.yml`).

### 7.1 Design Principles Enforced by This Constraint
- **No repository/service/controller layering** — handlers call storage directly.
- **No dependency injection framework** — plain struct composition in `main.go`.
- **No ORM** — Firestore SDK used directly in `storage.go`.
- **Single binary, single responsibility per file.**

---

## 8. API Specification (Summary)

| Method | Path | Description | Success Response |
|--------|------|--------------|-------------------|
| POST | `/videos` | Upload a video (multipart form, field `file`) | `201` + `{id, filename, size_bytes, content_type}` |
| GET | `/videos` | List videos | `200` + `[]Video` |
| GET | `/videos/{id}` | Get video metadata | `200` + `Video` / `404` |
| GET | `/videos/{id}/stream` | Stream video content (range-aware) | `200`/`206` + video bytes |
| GET | `/healthz` | Liveness check | `200 OK` |
| GET | `/readyz` | Readiness check | `200 OK` / `503` |

---

## 9. Deployment Requirements

- **Containerization:** Multi-stage `Dockerfile` (Go build stage → distroless/alpine runtime).
- **Deployment target:** Cloud Run (primary), with env-based config for bucket name, Firestore project ID, port, max upload size.
- **IAM:** Service account with `roles/storage.objectAdmin` on target bucket and `roles/datastore.user` for Firestore.
- **Config via environment variables:**
  - `GCS_BUCKET_NAME`
  - `GCP_PROJECT_ID`
  - `PORT` (default 8080)
  - `MAX_UPLOAD_SIZE_MB` (default 500)
  - `API_KEY` (optional)

---

## 10. Success Metrics

| Metric | Target |
|--------|--------|
| Upload success rate | ≥ 99% for files < configured max size |
| Stream start latency (TTFB) | < 500ms p95 (Cloud Run warm) |
| Range request correctness | 100% — validated via seek testing in browser video element |
| Codebase file count (core logic) | Exactly 2–4 files, verified in CI |
| Cold start time (Cloud Run) | < 3s |

---

## 11. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Large uploads exhausting memory | Stream multipart body directly to GCS writer; enforce `MAX_UPLOAD_SIZE_MB`. |
| Tight file constraint limiting readability | Enforce clear internal sectioning/comments within each file; strict but well-documented code. |
| No transcoding may limit device compatibility | Document as known v1 limitation; recommend pre-encoded MP4 (H.264/AAC) uploads. |
| Public bucket misconfiguration | Bucket must be private; all access mediated through the app/service account. |
| Firestore cost at scale | Acceptable for v1 metadata volume; revisit if video count > 100k. |

---

## 12. Future Considerations (Out of Scope for v1)
- Adaptive bitrate streaming (HLS/DASH) via Transcoder API.
- Signed URL direct-to-GCS uploads (bypass server for large files).
- User authentication (Firebase Auth / IAP).
- CDN integration (Cloud CDN in front of GCS/Cloud Run).
- Metrics/tracing via Cloud Trace + OpenTelemetry.

---

## 13. Acceptance Criteria

- [ ] Application source limited to 2–4 core `.go` files as defined in §7.
- [ ] Video upload, listing, metadata retrieval, and range-based streaming all functional end-to-end against real GCS + Firestore.
- [ ] Deployable to Cloud Run via provided Dockerfile with documented env vars.
- [ ] Health/readiness endpoints implemented and verified.
- [ ] Manual test: video streamed and seekable in an HTML5 `<video>` element.

--- Cohere AI Quality Audit ---
### Product Requirements:
- Build a simple video streaming app in Go for GCP, focusing on minimalism and production-readiness.
- Support video upload, storage, and on-demand streaming via HTTP.
- Target users: internal teams, small startups, and Go/GCP developers.

### Technical Specifications:
**API Endpoints:**
- `POST /videos`: Upload video, validate type, generate ID, stream to GCS, persist metadata in Firestore.
- `GET /videos/{id}/stream`: Stream video content with HTTP Range Requests, proxy from GCS.
- `GET /videos`: List all video metadata.
- `GET /videos/{id}`: Get single video metadata.
- `GET /healthz`: Liveness probe.
- `GET /readyz`: Readiness probe (GCS/Firestore connectivity).
- Optional: API key auth via `X-API-Key`.

**Data Storage:**
- **GCS Bucket:** Store video binaries, support streamed writes/reads.
- **Firestore (Native mode):** Store video metadata (ID, name, type, size, path, timestamps).

**Data Model (Firestore Document):**
```json
{
  "id": "uuid",
  "filename": "string",
  "content_type": "string",
  "size_bytes": "int64",
  "gcs_object": "string (bucket path)",
  "created_at": "timestamp"
}
```

**Code Structure (STRICT):**
- `main.go`: Entry point, config, client init, router setup, middleware, shutdown.
- `handlers.go`: HTTP handlers for upload, stream, list, get, health, ready.
- `storage.go`: Data access layer, encapsulate GCS/Firestore SDK interaction.
- `models.go` (optional): Shared types, DTOs, error handling.

**Deployment:**
- Containerize with multi-stage Dockerfile (Go build → distroless/alpine).
- Deploy to Cloud Run with env vars: bucket name, Firestore project ID, port, max upload size, API key.
- IAM: Service account with storage/objectAdmin and datastore/user roles.

**Non-Functional Requirements:**
- Performance: Efficient range requests, no full-file buffering.
- Scalability: Stateless, horizontally scalable on Cloud Run.
- Reliability: Graceful shutdown, error handling, context-based cancellation.
- Security: File validation, private GCS bucket, optional API key auth.
- Observability: Structured logging, request logging middleware, optional metrics endpoint.
- Maintainability: Idiomatic Go, no frameworks, single responsibility per file.
- Portability: Single binary, deployable to Cloud Run across envs.
- Cost: GCS Standard storage, Cloud Run scale-to-zero.

**Success Metrics:**
- Upload success rate, stream latency, range request correctness, codebase file count, cold start time.

**Risks & Mitigations:**
- Large uploads: Stream to GCS, enforce max size.
- File constraint: Internal sectioning, clear comments.
- No transcoding: Recommend pre-encoded MP4 uploads.
- Public bucket: Ensure private access.
- Firestore cost: Acceptable for v1, revisit at scale.

**Future Scope:**
- Adaptive bitrate streaming, signed URLs, auth, CDN, metrics/tracing.

**Acceptance Criteria:**
- 2–4 core `.go` files.
- End-to-end functionality: upload, list, metadata, stream.
- Deployable to Cloud Run with env vars.
- Health/readiness endpoints verified.
- Manual test: Video streaming and seeking in HTML5.
