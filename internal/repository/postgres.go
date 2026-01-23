package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/johnnyr0x/reader-app/internal/models"
)

// PostgresRepository implements database operations
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a new PostgresRepository
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// GetUserLibrary returns all books in a user's library
func (r *PostgresRepository) GetUserLibrary(ctx context.Context, userID int) ([]models.Book, error) {
	query := `
		SELECT id, user_id, book_sku, gutenberg_id, title, author, cover_url, acquired_at
		FROM user_library
		WHERE user_id = $1
		ORDER BY acquired_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []models.Book
	for rows.Next() {
		var b models.Book
		err := rows.Scan(
			&b.ID, &b.UserID, &b.SKU, &b.GutenbergID,
			&b.Title, &b.Author, &b.CoverURL, &b.AcquiredAt,
		)
		if err != nil {
			return nil, err
		}
		books = append(books, b)
	}

	return books, rows.Err()
}

// GetLibraryBook returns a specific book from the user's library
func (r *PostgresRepository) GetLibraryBook(ctx context.Context, userID int, bookSKU string) (*models.Book, error) {
	query := `
		SELECT id, user_id, book_sku, gutenberg_id, title, author, cover_url, acquired_at
		FROM user_library
		WHERE user_id = $1 AND book_sku = $2
	`

	var b models.Book
	err := r.db.QueryRowContext(ctx, query, userID, bookSKU).Scan(
		&b.ID, &b.UserID, &b.SKU, &b.GutenbergID,
		&b.Title, &b.Author, &b.CoverURL, &b.AcquiredAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &b, nil
}

// GetLibraryBookBySKU returns a book by SKU (without user check)
func (r *PostgresRepository) GetLibraryBookBySKU(ctx context.Context, bookSKU string) (*models.Book, error) {
	query := `
		SELECT id, user_id, book_sku, gutenberg_id, title, author, cover_url, acquired_at
		FROM user_library
		WHERE book_sku = $1
		LIMIT 1
	`

	var b models.Book
	err := r.db.QueryRowContext(ctx, query, bookSKU).Scan(
		&b.ID, &b.UserID, &b.SKU, &b.GutenbergID,
		&b.Title, &b.Author, &b.CoverURL, &b.AcquiredAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &b, nil
}

// AddToLibrary adds a book to the user's library
func (r *PostgresRepository) AddToLibrary(ctx context.Context, book *models.Book) error {
	query := `
		INSERT INTO user_library (user_id, book_sku, gutenberg_id, title, author, cover_url, acquired_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, book_sku) DO UPDATE SET
			title = EXCLUDED.title,
			author = EXCLUDED.author,
			cover_url = EXCLUDED.cover_url
	`

	_, err := r.db.ExecContext(ctx, query,
		book.UserID, book.SKU, book.GutenbergID,
		book.Title, book.Author, book.CoverURL, book.AcquiredAt,
	)
	return err
}

// GetReadingProgress returns a user's reading progress for a book
func (r *PostgresRepository) GetReadingProgress(ctx context.Context, userID int, bookSKU string) (*models.ReadingProgress, error) {
	query := `
		SELECT id, user_id, book_sku, gutenberg_id, chapter_index, chapter_href,
		       position_percent, scroll_position, last_read_at, created_at
		FROM reading_progress
		WHERE user_id = $1 AND book_sku = $2
	`

	var p models.ReadingProgress
	err := r.db.QueryRowContext(ctx, query, userID, bookSKU).Scan(
		&p.ID, &p.UserID, &p.BookSKU, &p.GutenbergID,
		&p.ChapterIndex, &p.ChapterHref, &p.PositionPercent,
		&p.ScrollPosition, &p.LastReadAt, &p.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// SaveReadingProgress saves or updates a user's reading progress
func (r *PostgresRepository) SaveReadingProgress(ctx context.Context, userID int, bookSKU string, gutenbergID int, update *models.ProgressUpdate) error {
	query := `
		INSERT INTO reading_progress (user_id, book_sku, gutenberg_id, chapter_index, chapter_href, position_percent, scroll_position, last_read_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, book_sku) DO UPDATE SET
			chapter_index = EXCLUDED.chapter_index,
			chapter_href = EXCLUDED.chapter_href,
			position_percent = EXCLUDED.position_percent,
			scroll_position = EXCLUDED.scroll_position,
			last_read_at = EXCLUDED.last_read_at
	`

	_, err := r.db.ExecContext(ctx, query,
		userID, bookSKU, gutenbergID,
		update.ChapterIndex, update.ChapterHref,
		update.PositionPercent, update.ScrollPosition,
		time.Now(),
	)
	return err
}

// GetEPUBCache returns cached EPUB metadata
func (r *PostgresRepository) GetEPUBCache(ctx context.Context, gutenbergID int) (*models.EPUBCache, error) {
	query := `
		SELECT id, gutenberg_id, book_sku, minio_path, file_size_bytes, cached_at, last_accessed_at
		FROM epub_cache
		WHERE gutenberg_id = $1
	`

	var c models.EPUBCache
	err := r.db.QueryRowContext(ctx, query, gutenbergID).Scan(
		&c.ID, &c.GutenbergID, &c.BookSKU, &c.MinIOPath,
		&c.FileSizeBytes, &c.CachedAt, &c.LastAccessedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// SaveEPUBCache saves EPUB cache metadata
func (r *PostgresRepository) SaveEPUBCache(ctx context.Context, cache *models.EPUBCache) error {
	query := `
		INSERT INTO epub_cache (gutenberg_id, book_sku, minio_path, file_size_bytes, cached_at, last_accessed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (gutenberg_id) DO UPDATE SET
			last_accessed_at = EXCLUDED.last_accessed_at
	`

	_, err := r.db.ExecContext(ctx, query,
		cache.GutenbergID, cache.BookSKU, cache.MinIOPath,
		cache.FileSizeBytes, cache.CachedAt, cache.LastAccessedAt,
	)
	return err
}

// UpdateEPUBCacheAccess updates the last accessed timestamp
func (r *PostgresRepository) UpdateEPUBCacheAccess(ctx context.Context, gutenbergID int) error {
	query := `UPDATE epub_cache SET last_accessed_at = $1 WHERE gutenberg_id = $2`
	_, err := r.db.ExecContext(ctx, query, time.Now(), gutenbergID)
	return err
}

// Ping checks database connectivity
func (r *PostgresRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}
