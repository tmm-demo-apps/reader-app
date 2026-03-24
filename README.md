# Reader App

[![CI](https://github.com/tmm-demo-apps/reader-app/workflows/CI/badge.svg)](https://github.com/tmm-demo-apps/reader-app/actions)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![GHCR](https://img.shields.io/badge/GHCR-public-blue?logo=github)](https://github.com/orgs/tmm-demo-apps/packages)

A library reader application that allows users to read books purchased from the Bookstore. Part of the VCF multi-app demo suite.

**Endpoint**: `http://reader.<your-domain>` (set via Helm's `global.domain`)

> **Portable Deployment**: This app is included in the [bookstore-app Helm chart](https://github.com/tmm-demo-apps/bookstore-app/tree/main/helm/demo-suite). Deploy the entire suite with:
> ```bash
> git clone https://github.com/tmm-demo-apps/bookstore-app.git && cd bookstore-app
> helm dependency update ./helm/demo-suite
> helm install demo ./helm/demo-suite --set global.domain=<your-domain>
> ```
> This deploys bookstore + reader + chatbot. To skip chatbot: add `--set chatbot.enabled=false`.
> No ingress controller? Add `--set ingress-nginx.enabled=true` to install one automatically.

## Features

- View purchased books library
- Read EPUB books in browser
- Table of contents navigation
- Reading progress tracking
- Font size adjustment
- Responsive design
- On-demand EPUB download from Gutenberg mirror with MinIO caching
- Optional EPUB pre-seeding via init container for restricted networks

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Library Reader Service                             │
│                              (Go + HTMX + Pico.css)                         │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Browser ──► Go Backend ──► MinIO (EPUB Store)                              │
│                  │                                                           │
│                  ├──► PostgreSQL (reading progress)                         │
│                  ├──► Redis (sessions)                                       │
│                  └──► Bookstore API (verify purchases)                      │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Go 1.25+
- Docker & Docker Compose
- Running Bookstore app (for purchase verification)

## Local Development

```bash
# Start all services
docker compose up -d

# Reader available at http://localhost:8081
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| PORT | Server port | 8081 |
| DATABASE_URL | PostgreSQL connection | postgres://reader:reader@localhost:5432/reader |
| REDIS_URL | Redis connection | redis://localhost:6379 |
| MINIO_ENDPOINT | MinIO server | localhost:9000 |
| MINIO_ACCESS_KEY | MinIO access key | minioadmin |
| MINIO_SECRET_KEY | MinIO secret key | minioadmin |
| MINIO_BUCKET | EPUB bucket name | books-epub |
| BOOKSTORE_API_URL | Bookstore service URL | http://localhost:8080 |
| SESSION_SECRET | Session encryption key | (required) |

## API Endpoints

### HTML Pages
- `GET /` - Redirect to library
- `GET /library` - User's book library
- `GET /read/{sku}` - EPUB reader interface

### HTMX Partials
- `GET /read/{sku}/toc` - Table of contents
- `GET /read/{sku}/chapter/{index}` - Chapter content
- `POST /read/{sku}/progress` - Save reading progress

### JSON API
- `GET /api/library` - List user's books
- `GET /api/books/{sku}/metadata` - Book metadata
- `GET /api/books/{sku}/progress` - Reading progress
- `PUT /api/books/{sku}/progress` - Update progress

### Health
- `GET /health` - Liveness probe
- `GET /ready` - Readiness probe

## EPUB Delivery

By default, the reader downloads EPUBs on-demand from the `aleph.pglaf.org` Gutenberg mirror:

```
User opens book -> Check MinIO cache -> Cache miss -> Download from mirror
                                                       Try EPUB3-images first
                                                       Fall back to plain EPUB
                                     -> Upload to MinIO -> Serve to user
                                     -> Cache hit -> Serve directly (~40ms)
```

For restricted networks where pods can't reach external hosts, enable the EPUB seed init container to pre-load all 150 EPUBs into MinIO from a GHCR image:

```bash
# Via Helm
helm install demo ./helm/demo-suite \
  --set global.domain=<your-domain> \
  --set reader.epubSeed.enabled=true

# Via Kustomize -- add to your overlay's kustomization.yaml:
# patches:
#   - path: epub-seed-patch.yaml
```

The init container (`ghcr.io/tmm-demo-apps/reader-epubs:v1`) runs before the reader starts, uploads any missing EPUBs to MinIO, then exits. It's idempotent -- subsequent pod restarts skip already-cached books.

### Rebuilding the EPUB Seed Image

If you need to update the EPUBs:

```bash
# 1. Download EPUBs to scripts/epubs/ (gitignored)
# 2. Rebuild and push (from repo root)
docker buildx build --platform linux/amd64 \
  -f scripts/Dockerfile.epubs \
  -t ghcr.io/tmm-demo-apps/reader-epubs:v1 --push .
```

## Kubernetes Deployment

### Helm (Recommended for New Environments)

The Reader is deployed as part of the [demo-suite Helm chart](https://github.com/tmm-demo-apps/bookstore-app/tree/main/helm/demo-suite):

```bash
# Deploy full suite (includes reader)
helm install demo ./helm/demo-suite --set global.domain=apps.your-env.com

# Deploy without reader
helm install demo ./helm/demo-suite --set reader.enabled=false

# Deploy with EPUB pre-seeding (restricted networks)
helm install demo ./helm/demo-suite \
  --set global.domain=apps.your-env.com \
  --set reader.epubSeed.enabled=true
```

### ArgoCD (Existing VCF Environment)

The Reader app is deployed to VKS-04 via ArgoCD as part of the `demo-apps` App-of-Apps.

**VCF Production Endpoint**: http://reader.corp.vmbeans.com

```bash
# Check deployment status
argocd app get reader

# Manual sync if needed
argocd app sync reader

# Or deploy manually with kubectl
kubectl apply -k kubernetes/
```

The CI pipeline automatically:
1. Builds and pushes images to **GHCR** (public) and **Harbor** (enterprise)
2. Updates `kubernetes/base/kustomization.yaml` with new image tag
3. ArgoCD auto-syncs the changes to VKS-04

## Service Dependencies

The Reader app depends on:

1. **Bookstore API** - Verifies book purchases
   - Endpoint: `BOOKSTORE_API_URL`
   - Required: `GET /api/purchases/{user_id}` and `GET /api/purchases/{user_id}/{sku}`

2. **MinIO** - Stores cached EPUB files
   - Bucket: `books-epub` (created automatically by seed container or on first download)
   - EPUBs cached on first read; subsequent reads served from MinIO

3. **PostgreSQL** - Stores reading progress and library sync
   - Separate database from Bookstore

4. **Redis** - Session management
   - Can share Redis instance with Bookstore

## Demo Script

1. Purchase a book in the Bookstore
2. Open Reader app at `/library`
3. Click "Sync from Bookstore" to sync purchases
4. Click "Start Reading" to open the reader
5. Navigate using TOC or Next/Prev buttons
6. Close and reopen - progress is saved
7. Adjust font size with A+/A- buttons

## Related Projects

- [bookstore-app](https://github.com/tmm-demo-apps/bookstore-app) - E-commerce bookstore
- [chatbot-app](https://github.com/tmm-demo-apps/chatbot-app) - AI support chatbot
