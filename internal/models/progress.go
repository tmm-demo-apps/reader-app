package models

import "time"

// ReadingProgress represents a user's reading progress for a book
type ReadingProgress struct {
	ID              int       `json:"id"`
	UserID          int       `json:"user_id"`
	BookSKU         string    `json:"book_sku"`
	GutenbergID     int       `json:"gutenberg_id"`
	ChapterIndex    int       `json:"chapter_index"`
	ChapterHref     string    `json:"chapter_href"`
	PositionPercent float64   `json:"position_percent"`
	ScrollPosition  int       `json:"scroll_position"`
	LastReadAt      time.Time `json:"last_read_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// ProgressUpdate represents an update to reading progress
type ProgressUpdate struct {
	ChapterIndex    int     `json:"chapter_index"`
	ChapterHref     string  `json:"chapter_href"`
	PositionPercent float64 `json:"position_percent"`
	ScrollPosition  int     `json:"scroll_position"`
}
