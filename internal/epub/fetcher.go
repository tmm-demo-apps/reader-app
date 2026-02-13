package epub

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
)

const (
	// GutenbergEPUBURL is the URL pattern for downloading EPUB files
	GutenbergEPUBURL = "https://www.gutenberg.org/ebooks/%d.epub3.images"
	// Alternative formats if .epub3.images fails
	GutenbergEPUBFallback = "https://www.gutenberg.org/ebooks/%d.epub.images"
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
	// Skip TLS verification for Gutenberg downloads - some corporate networks
	// have proxies/firewalls that do TLS inspection with their own certificates.
	// This is safe for downloading public book EPUB files.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &Fetcher{
		minioClient: minioClient,
		bucket:      bucket,
		db:          db,
		httpClient: &http.Client{
			Timeout:   60 * time.Second, // EPUBs can be large
			Transport: tr,
		},
	}
}

// EnsureCached ensures an EPUB is available in MinIO, downloading if necessary
func (f *Fetcher) EnsureCached(ctx context.Context, gutenbergID int, bookSKU string) (string, error) {
	minioPath := fmt.Sprintf("%d/pg%d.epub", gutenbergID, gutenbergID)

	// Check if already cached in MinIO
	_, err := f.minioClient.StatObject(ctx, f.bucket, minioPath, minio.StatObjectOptions{})
	if err == nil {
		// Already cached, update access time
		f.updateAccessTime(ctx, gutenbergID)
		return minioPath, nil
	}

	// Not cached, download from Gutenberg
	data, err := f.downloadFromGutenberg(gutenbergID)
	if err != nil {
		return "", fmt.Errorf("failed to download EPUB: %w", err)
	}

	// Upload to MinIO
	reader := bytes.NewReader(data)
	_, err = f.minioClient.PutObject(ctx, f.bucket, minioPath, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/epub+zip",
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	// Save cache record
	f.saveCacheRecord(ctx, gutenbergID, bookSKU, minioPath, int64(len(data)))

	return minioPath, nil
}

// downloadFromGutenberg downloads an EPUB from Project Gutenberg
func (f *Fetcher) downloadFromGutenberg(gutenbergID int) ([]byte, error) {
	// Try EPUB3 first
	url := fmt.Sprintf(GutenbergEPUBURL, gutenbergID)
	data, err := f.downloadURL(url)
	if err == nil {
		return data, nil
	}

	// Fallback to regular EPUB
	url = fmt.Sprintf(GutenbergEPUBFallback, gutenbergID)
	return f.downloadURL(url)
}

func (f *Fetcher) downloadURL(url string) ([]byte, error) {
	resp, err := f.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
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
