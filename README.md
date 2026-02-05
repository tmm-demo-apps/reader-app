# Reader App

[![CI](https://github.com/tmm-demo-apps/reader-app/workflows/CI/badge.svg)](https://github.com/tmm-demo-apps/reader-app/actions)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)

A library reader application that allows users to read books purchased from the Bookstore. Part of the VCF multi-app demo suite.

**Live Endpoint**: http://reader.corp.vmbeans.com

## Features

- View purchased books library
- Read EPUB books in browser
- Table of contents navigation
- Reading progress tracking
- Font size adjustment
- Responsive design

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

## Kubernetes Deployment

The Reader app is deployed to VKS-04 via ArgoCD as part of the `demo-apps` App-of-Apps.

**Production Endpoint**: http://reader.corp.vmbeans.com

```bash
# Check deployment status
argocd app get reader

# Manual sync if needed
argocd app sync reader

# Or deploy manually with kubectl
kubectl apply -k kubernetes/
```

The CI pipeline automatically:
1. Builds and pushes images to Harbor
2. Updates `kustomization.yaml` with new image tag
3. ArgoCD auto-syncs the changes to VKS-04

## Service Dependencies

The Reader app depends on:

1. **Bookstore API** - Verifies book purchases
   - Endpoint: `BOOKSTORE_API_URL`
   - Required: `GET /api/purchases/{user_id}` and `GET /api/purchases/{user_id}/{sku}`

2. **MinIO** - Stores cached EPUB files
   - Can share bucket with Bookstore or use dedicated bucket

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
