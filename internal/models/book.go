package models

import "time"

// Book represents a book in the user's library
type Book struct {
	ID          int       `json:"id"`
	UserID      int       `json:"user_id"`
	SKU         string    `json:"sku"`
	GutenbergID int       `json:"gutenberg_id"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	CoverURL    string    `json:"cover_url"`
	AcquiredAt  time.Time `json:"acquired_at"`

	// Not stored in DB, computed at runtime
	Progress *ReadingProgress `json:"progress,omitempty"`
}

// TOCEntry represents a table of contents entry
type TOCEntry struct {
	Title string `json:"title"`
	Href  string `json:"href"`
	Level int    `json:"level"` // Nesting level (1 = top level)
	Index int    `json:"index"` // Chapter index for navigation
}

// Chapter represents a parsed chapter from the EPUB
type Chapter struct {
	Index   int    `json:"index"`
	Href    string `json:"href"`
	Title   string `json:"title"`
	Content string `json:"content"` // HTML content
}

// ParsedBook represents a fully parsed EPUB file
type ParsedBook struct {
	GutenbergID int        `json:"gutenberg_id"`
	SKU         string     `json:"sku"`
	Title       string     `json:"title"`
	Author      string     `json:"author"`
	Language    string     `json:"language"`
	TOC         []TOCEntry `json:"toc"`
	Chapters    []Chapter  `json:"chapters"`
}

// EPUBCache represents cached EPUB metadata
type EPUBCache struct {
	ID             int       `json:"id"`
	GutenbergID    int       `json:"gutenberg_id"`
	BookSKU        string    `json:"book_sku"`
	MinIOPath      string    `json:"minio_path"`
	FileSizeBytes  int64     `json:"file_size_bytes"`
	CachedAt       time.Time `json:"cached_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
}
