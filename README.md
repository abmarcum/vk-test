# Simple Video Streaming Application

A lightweight, production-grade Go service for uploading, listing, and
streaming video content, backed by **Google Cloud Storage** (binary video
bytes) and **Google Cloud Firestore** (video metadata). Designed to run as
a stateless container on **Google Cloud Run** (or any container platform).

---

## Table of Contents

- [Features](#features)
- [Architecture Overview](#architecture-overview)
- [Prerequisites](#prerequisites)
- [Configuration](#configuration)
- [Build Instructions](#build-instructions)
- [Running Locally](#running-locally)
- [Deployment Instructions](#deployment-instructions)
  - [Docker](#1-docker-build--run)
  - [Docker Compose](#2-docker-compose-local-multi-service-dev)
  - [Google Cloud Run](#3-deploy-to-google-cloud-run)
  - [Kubernetes](#4-deploy-to-kubernetes)
- [API Overview](#api-overview)
- [Security Notes](#security-notes)
- [Testing](#testing)
- [License & Documentation](#license--documentation)

---

## Features

- **Chunked, memory-safe uploads** — video bytes are streamed directly into
  GCS via `io.Copy`; the full file is never buffered in memory.
- **HTTP Range / seek support** — the streaming endpoint honors the `Range`
  header so browsers can seek within a video without downloading it fully.
- **Metadata persistence** — video metadata (filename, content type, size,
  creation time) is stored in Firestore and returned via a paginated list API.
- **Content-type allow-listing** — only `video/mp4`, `video/webm`,
  `video/ogg`, `video/quicktime`, and `video/x-matroska` are accepted.
- **Optional API key authentication** — enabled by setting `API_KEY`;
  disabled (open) by default for local development.
- **Cloud Run–ready** — structured JSON logging, graceful shutdown,
  configurable timeouts, and health/readiness probes (`/healthz`, `/readyz`).
- **Security-hardened HTTP layer** — panic recovery, security headers,
  constant-time API key comparison, request body size limits, opt-in CORS.

## Architecture Overview

| File          | Responsibility                                                        |
|---------------|------------------------------------------------------------------------|
| `main.go`     | Configuration loading, HTTP router/middleware wiring, graceful shutdown |
| `handlers.go` | HTTP handler layer (upload, list, get, stream, health/readiness)        |
| `storage.go`  | Data-access layer wrapping the GCS and Firestore SDKs                   |
| `models.go`   | Shared domain types, DTOs, and sentinel errors                         |

Request flow:

```
Client
  │
  ▼
net/http ServeMux
  │  recover → logging → security headers → auth → body-limit
  ▼
handlers.go (HandlerSet)
  │
  ▼
storage.go (StorageClient)
  │            │
  ▼            ▼
GCS (bytes)  Firestore (metadata)
```

## Prerequisites

- **Go** 1.22 or later (uses `net/http`'s method-aware `ServeMux`)
- A **GCP project** with the following APIs enabled:
  - Cloud Storage API
  - Cloud Firestore API (Native mode)
- A **GCS bucket** to store video objects
- A **Firestore database** (collection `videos` by default)
- Application Default Credentials (ADC) available to the process:
  - Locally: `gcloud auth application-default login`, or a service account
    key file referenced via `GOOGLE_APPLICATION_CREDENTIALS`
  - On Cloud Run: an attached service account with `roles/storage.objectAdmin`
    and `roles/datastore.user`
- **Docker** (for containerized builds/deployment)
- Optional: `gcloud` CLI, `kubectl`, `docker-compose`

## Configuration

All configuration is sourced from environment variables (see `main.go`).

| Variable                      | Required | Default | Description                                                        |
|--------------------------------|----------|---------|----------------------------------------------------------------------|
| `PORT`                         | no       | `8080`  | HTTP listen port (set automatically by Cloud Run)                   |
| `GCS_BUCKET_NAME`               | **yes**  | —       | Target GCS bucket for video objects                                  |
| `GCP_PROJECT_ID`                | **yes**  | —       | GCP project used to construct GCS/Firestore clients                  |
| `MAX_UPLOAD_SIZE_MB`            | no       | `500`   | Maximum accepted upload size, in megabytes                           |
| `API_KEY`                       | no       | *(empty)* | Shared secret for `X-API-Key` auth; empty disables auth            |
| `ALLOWED_ORIGIN`                | no       | *(empty)* | Enables CORS for this exact origin; empty disables CORS headers    |
| `STATIC_DIR`                    | no       | `web`   | Directory serving the optional static video-player UI                |
| `READ_HEADER_TIMEOUT_SECONDS`   | no       | `10`    | HTTP read-header timeout                                             |
| `READ_TIMEOUT_SECONDS`          | no       | `0` (unbounded) | Full request read timeout (unbounded to allow large uploads) |
| `WRITE_TIMEOUT_SECONDS`         | no       | `0` (unbounded) | Response write timeout (unbounded to allow long streams)     |
| `IDLE_TIMEOUT_SECONDS`          | no       | `120`   | Keep-alive idle timeout                                               |
| `SHUTDOWN_TIMEOUT_SECONDS`      | no       | `25`    | Grace period for in-flight requests during shutdown                  |
| `GOOGLE_APPLICATION_CREDENTIALS`| no*      | —       | Path to a service account key file (not needed on Cloud Run/GKE with Workload Identity) |

`GCS_BUCKET_NAME`, `GCP_PROJECT_ID`, and a positive `MAX_UPLOAD_SIZE_MB` are
validated at startup; the process exits with a non-zero code and a logged
error if they are missing or invalid.

---

## Build Instructions

These steps assume a Go module named e.g. `github.com/yourorg/video-streaming-app`
containing `main.go`, `handlers.go`, `storage.go`, and `models.go` at the
module root.

### 1. Install dependencies

```bash
# Initialize go.mod if one does not already exist
go mod init github.com/yourorg/video-streaming-app

# Fetch required dependencies
go get cloud.google.com/go/storage
go get cloud.google.com/go/firestore
go get github.com/google/uuid
go get google.golang.org/api/iterator
go get google.golang.org/grpc

# Tidy and verify the module graph
go mod tidy
```

### 2. Vet and format

```bash
go vet ./...
gofmt -l .
```

### 3. Run unit tests

```bash
go test ./... -v
```

### 4. Compile a local binary

```bash
go build -o bin/video-streaming-app .
```

### 5. Cross-compile for Linux (container target)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o bin/video-streaming-app .
```

---

## Running Locally

```bash
export GCP_PROJECT_ID="my-gcp-project"
export GCS_BUCKET_NAME="my-video-bucket"
export API_KEY="local-dev-key"          # optional
export GOOGLE_APPLICATION_CREDENTIALS="$HOME/.config/gcloud/application_default_credentials.json"

./bin/video-streaming-app
```

The server listens on `PORT` (default `8080`). Verify it is up:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Upload a test video:

```bash
curl -X POST http://localhost:8080/videos \
  -H "X-API-Key: local-dev-key" \
  -F "file=@sample.mp4;type=video/mp4"
```

---

## Deployment Instructions

### 1. Docker build & run

Create a `Dockerfile` at the repository root:

```dockerfile
# ---- build stage ----
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/video-streaming-app .

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/video-streaming-app ./video-streaming-app
COPY web ./web
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/video-streaming-app"]
```

Build and run:

```bash
docker build -t video-streaming-app:latest .

docker run -p 8080:8080 \
  -e GCP_PROJECT_ID="my-gcp-project" \
  -e GCS_BUCKET_NAME="my-video-bucket" \
  -e API_KEY="local-dev-key" \
  -v "$HOME/.config/gcloud/application_default_credentials.json":/secrets/adc.json:ro \
  -e GOOGLE_APPLICATION_CREDENTIALS=/secrets/adc.json \
  video-streaming-app:latest
```

### 2. Docker Compose (local multi-service dev)

`docker-compose.yml`:

```yaml
version: "3.9"
services:
  video-streaming-app:
    build: .
    image: video-streaming-app:latest
    ports:
      - "8080:8080"
    environment:
      GCP_PROJECT_ID: "my-gcp-project"
      GCS_BUCKET_NAME: "my-video-bucket"
      API_KEY: "local-dev-key"
      ALLOWED_ORIGIN: "http://localhost:3000"
      GOOGLE_APPLICATION_CREDENTIALS: /secrets/adc.json
    volumes:
      - "${HOME}/.config/gcloud/application_default_credentials.json:/secrets/adc.json:ro"
```

```bash
docker-compose up --build
```

### 3. Deploy to Google Cloud Run

```bash
# Authenticate and set the active project
gcloud auth login
gcloud config set project my-gcp-project

# Build and push the image with Cloud Build
gcloud builds submit --tag gcr.io/my-gcp-project/video-streaming-app:latest

# Deploy to Cloud Run
gcloud run deploy video-streaming-app \
  --image gcr.io/my-gcp-project/video-streaming-app:latest \
  --platform managed \
  --region us-central1 \
  --service-account video-streaming-app-sa@my-gcp-project.iam.gserviceaccount.com \
  --set-env-vars GCP_PROJECT_ID=my-gcp-project,GCS_BUCKET_NAME=my-video-bucket \
  --set-secrets API_KEY=video-streaming-api-key:latest \
  --allow-unauthenticated \
  --memory 512Mi \
  --cpu 1 \
  --timeout 300
```

Grant the runtime service account the required IAM roles:

```bash
gcloud projects add-iam-policy-binding my-gcp-project \
  --member="serviceAccount:video-streaming-app-sa@my-gcp-project.iam.gserviceaccount.com" \
  --role="roles/storage.objectAdmin"

gcloud projects add-iam-policy-binding my-gcp-project \
  --member="serviceAccount:video-streaming-app-sa@my-gcp-project.iam.gserviceaccount.com" \
  --role="roles/datastore.user"
```

### 4. Deploy to Kubernetes

`k8s/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: video-streaming-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: video-streaming-app
  template:
    metadata:
      labels:
        app: video-streaming-app
    spec:
      serviceAccountName: video-streaming-app-ksa
      containers:
        - name: video-streaming-app
          image: gcr.io/my-gcp-project/video-streaming-app:latest
          ports:
            - containerPort: 8080
          env:
            - name: GCP_PROJECT_ID
              value: "my-gcp-project"
            - name: GCS_BUCKET_NAME
              value: "my-video-bucket"
            - name: API_KEY
              valueFrom:
                secretKeyRef:
                  name: video-streaming-secrets
                  key: api-key
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 15
---
apiVersion: v1
kind: Service
metadata:
  name: video-streaming-app
spec:
  selector:
    app: video-streaming-app
  ports:
    - port: 80
      targetPort: 8080
  type: LoadBalancer
```

Apply:

```bash
kubectl create secret generic video-streaming-secrets \
  --from-literal=api-key='local-dev-key'

kubectl apply -f k8s/deployment.yaml
kubectl get pods -w
```

---

## API Overview

Full endpoint reference, request/response schemas, error formats, and
`curl` examples are documented in **[docs/api.md](docs/api.md)**.

Quick summary:

| Method | Path                    | Auth              | Description                        |
|--------|-------------------------|-------------------|-------------------------------------|
| GET    | `/healthz`               | none              | Liveness probe                      |
| GET    | `/readyz`                 | none              | Readiness probe (checks GCS/Firestore) |
| POST   | `/videos`                 | API key           | Upload a video (multipart/form-data) |
| GET    | `/videos`                 | API key           | List video metadata (paginated)     |
| GET    | `/videos/{id}`             | API key           | Fetch a single video's metadata     |
| GET    | `/videos/{id}/stream`      | API key or query param | Stream video bytes with Range support |

## Security Notes

- API key comparison uses `crypto/subtle.ConstantTimeCompare` to avoid
  timing side channels.
- The streaming endpoint uniquely accepts the API key as a `?api_key=`
  query parameter (in addition to the `X-API-Key` header) because HTML5
  `<video>` elements cannot attach custom headers. This trade-off is
  intentionally scoped to a single read-only endpoint.
- Uploaded content types are validated against an explicit allow-list.
- Video/document identifiers are validated to prevent path traversal into
  GCS object names.
- All internal error detail is masked from clients; only generic messages
  are returned, with full detail logged server-side via structured JSON logs.

## Testing

```bash
go test ./... -race -cover
```

Integration testing against real GCS/Firestore requires a test project and
bucket; point `GCP_PROJECT_ID` / `GCS_BUCKET_NAME` at disposable test
resources and run the binary locally as described above.

---

## License & Documentation

[MIT License](LICENSE) | [API Docs](docs/api.md)

This project is licensed under the **MIT License** — see the [LICENSE](LICENSE)
file for the full license text.
```

---
