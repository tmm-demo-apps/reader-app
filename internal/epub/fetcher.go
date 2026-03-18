package epub

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
)

const (
	// MirrorEPUBURL is the primary URL pattern — EPUB3 with images from the Gutenberg mirror
	MirrorEPUBURL = "https://aleph.pglaf.org/cache/epub/%d/pg%d-images-3.epub"
	// MirrorEPUBFallback is the fallback — plain EPUB (no images, smaller)
	MirrorEPUBFallback = "https://aleph.pglaf.org/cache/epub/%d/pg%d.epub"
)

// Fetcher handles downloading and caching EPUB files
type Fetcher struct {
	minioClient *minio.Client
	bucket      string
	db          *sql.DB
	httpClient  *http.Client
}

// NewFetcher creates a new EPUB fetcher
func NewFetcher(minioClient *minio.Client, bucket string, db *sql.DB) *Fetcher {
	// Skip TLS verification — corporate networks often do TLS inspection
	// with their own certificates, causing verification failures.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &Fetcher{
		minioClient: minioClient,
		bucket:      bucket,
		db:          db,
		httpClient: &http.Client{
			Timeout:   120 * time.Second,
			Transport: tr,
		},
	}
}

// EnsureCached ensures an EPUB is available in MinIO, downloading from the mirror if necessary
func (f *Fetcher) EnsureCached(ctx context.Context, gutenbergID int, bookSKU string) (string, error) {
	minioPath := fmt.Sprintf("%d/pg%d.epub", gutenbergID, gutenbergID)

	_, err := f.minioClient.StatObject(ctx, f.bucket, minioPath, minio.StatObjectOptions{})
	if err == nil {
		f.updateAccessTime(ctx, gutenbergID)
		return minioPath, nil
	}

	log.Printf("EPUB not cached for Gutenberg ID %d (SKU: %s), downloading from mirror...", gutenbergID, bookSKU)

	data, err := f.downloadFromMirror(gutenbergID)
	if err != nil {
		return "", fmt.Errorf("failed to download EPUB: %w", err)
	}

	reader := bytes.NewReader(data)
	_, err = f.minioClient.PutObject(ctx, f.bucket, minioPath, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/epub+zip",
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	f.saveCacheRecord(ctx, gutenbergID, bookSKU, minioPath, int64(len(data)))
	log.Printf("Cached EPUB for Gutenberg ID %d (%d bytes)", gutenbergID, len(data))

	return minioPath, nil
}

func (f *Fetcher) downloadFromMirror(gutenbergID int) ([]byte, error) {
	url := fmt.Sprintf(MirrorEPUBURL, gutenbergID, gutenbergID)
	data, err := f.downloadURL(url)
	if err == nil {
		return data, nil
	}

	log.Printf("Primary mirror URL failed for ID %d, trying fallback: %v", gutenbergID, err)
	url = fmt.Sprintf(MirrorEPUBFallback, gutenbergID, gutenbergID)
	return f.downloadURL(url)
}

func (f *Fetcher) downloadURL(url string) ([]byte, error) {
	resp, err := f.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}

func (f *Fetcher) updateAccessTime(ctx context.Context, gutenbergID int) {
	query := `UPDATE epub_cache SET last_accessed_at = $1 WHERE gutenberg_id = $2`
	_, _ = f.db.ExecContext(ctx, query, time.Now(), gutenbergID)
}

func (f *Fetcher) saveCacheRecord(ctx context.Context, gutenbergID int, bookSKU, minioPath string, size int64) {
	query := `
		INSERT INTO epub_cache (gutenberg_id, book_sku, minio_path, file_size_bytes, cached_at, last_accessed_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (gutenberg_id) DO UPDATE SET last_accessed_at = EXCLUDED.last_accessed_at
	`
	_, _ = f.db.ExecContext(ctx, query, gutenbergID, bookSKU, minioPath, size, time.Now())
}

// GetEPUBReader returns a reader for the EPUB file
func (f *Fetcher) GetEPUBReader(ctx context.Context, gutenbergID int) (io.ReadCloser, error) {
	minioPath := fmt.Sprintf("%d/pg%d.epub", gutenbergID, gutenbergID)

	obj, err := f.minioClient.GetObject(ctx, f.bucket, minioPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get EPUB from MinIO: %w", err)
	}

	return obj, nil
}
