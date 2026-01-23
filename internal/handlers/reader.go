package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/johnnyr0x/reader-app/internal/models"
)

// Reader renders the EPUB reader interface
func (h *Handlers) Reader(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	bookSKU := chi.URLParam(r, "sku")

	// Verify user owns the book
	owned, err := h.bookstoreClient.VerifyPurchase(userID, bookSKU)
	if err != nil {
		// If Bookstore is down, check local library
		book, _ := h.repo.GetLibraryBook(r.Context(), userID, bookSKU)
		owned = book != nil
	}

	if !owned {
		http.Error(w, "Book not in your library", http.StatusForbidden)
		return
	}

	// Get book from library
	book, err := h.repo.GetLibraryBook(r.Context(), userID, bookSKU)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	// Ensure EPUB is cached
	_, err = h.fetcher.EnsureCached(r.Context(), book.GutenbergID, bookSKU)
	if err != nil {
		http.Error(w, "Failed to load book: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse EPUB for metadata and TOC
	parsed, err := h.parser.Parse(r.Context(), book.GutenbergID)
	if err != nil {
		http.Error(w, "Failed to parse book: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get reading progress
	progress, _ := h.repo.GetReadingProgress(r.Context(), userID, bookSKU)

	// Default to first chapter if no progress
	currentChapter := 0
	if progress != nil {
		currentChapter = progress.ChapterIndex
	}

	h.render(w, "reader.html", map[string]interface{}{
		"Book":           book,
		"Parsed":         parsed,
		"TOC":            parsed.TOC,
		"Progress":       progress,
		"CurrentChapter": currentChapter,
		"TotalChapters":  len(parsed.Chapters),
	})
}

// TableOfContents returns the TOC as an HTMX partial
func (h *Handlers) TableOfContents(w http.ResponseWriter, r *http.Request) {
	bookSKU := chi.URLParam(r, "sku")

	// Get book from library (any user, since we verified auth in middleware)
	book, err := h.repo.GetLibraryBookBySKU(r.Context(), bookSKU)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	// Parse EPUB
	parsed, err := h.parser.Parse(r.Context(), book.GutenbergID)
	if err != nil {
		http.Error(w, "Failed to parse book", http.StatusInternalServerError)
		return
	}

	h.render(w, "partials/toc.html", map[string]interface{}{
		"TOC":     parsed.TOC,
		"BookSKU": bookSKU,
	})
}

// Chapter returns a chapter's content as an HTMX partial
func (h *Handlers) Chapter(w http.ResponseWriter, r *http.Request) {
	bookSKU := chi.URLParam(r, "sku")
	chapterIndexStr := chi.URLParam(r, "index")

	chapterIndex, err := strconv.Atoi(chapterIndexStr)
	if err != nil {
		http.Error(w, "Invalid chapter index", http.StatusBadRequest)
		return
	}

	// Get book from library
	book, err := h.repo.GetLibraryBookBySKU(r.Context(), bookSKU)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	// Parse EPUB to get all chapters (needed for href mapping)
	parsed, err := h.parser.Parse(r.Context(), book.GutenbergID)
	if err != nil {
		http.Error(w, "Failed to parse book", http.StatusInternalServerError)
		return
	}

	// Get specific chapter
	chapter, err := h.parser.GetChapter(r.Context(), book.GutenbergID, chapterIndex)
	if err != nil {
		http.Error(w, "Chapter not found", http.StatusNotFound)
		return
	}

	// Build href to chapter index map for internal link navigation
	hrefMap := make(map[string]int)
	for _, ch := range parsed.Chapters {
		hrefMap[ch.Href] = ch.Index
	}

	h.render(w, "partials/chapter.html", map[string]interface{}{
		"Chapter":       chapter,
		"BookSKU":       bookSKU,
		"Book":          book,
		"IsCoverPage":   chapterIndex == 0,
		"HrefMap":       hrefMap,
		"TotalChapters": len(parsed.Chapters),
	})
}

// SaveProgress saves reading progress (HTMX endpoint)
func (h *Handlers) SaveProgress(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	bookSKU := chi.URLParam(r, "sku")

	var update models.ProgressUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get book to get GutenbergID
	book, err := h.repo.GetLibraryBook(r.Context(), userID, bookSKU)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	err = h.repo.SaveReadingProgress(r.Context(), userID, bookSKU, book.GutenbergID, &update)
	if err != nil {
		http.Error(w, "Failed to save progress", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// APIBookMetadata returns book metadata as JSON
func (h *Handlers) APIBookMetadata(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	bookSKU := chi.URLParam(r, "sku")

	book, err := h.repo.GetLibraryBook(r.Context(), userID, bookSKU)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	// Parse for full metadata
	parsed, err := h.parser.Parse(r.Context(), book.GutenbergID)
	if err != nil {
		http.Error(w, "Failed to parse book", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(parsed); err != nil {
		log.Printf("Failed to encode book metadata: %v", err)
	}
}

// APIGetProgress returns reading progress as JSON
func (h *Handlers) APIGetProgress(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	bookSKU := chi.URLParam(r, "sku")

	progress, err := h.repo.GetReadingProgress(r.Context(), userID, bookSKU)
	if err != nil {
		http.Error(w, "Failed to get progress", http.StatusInternalServerError)
		return
	}

	if progress == nil {
		progress = &models.ReadingProgress{
			BookSKU:         bookSKU,
			ChapterIndex:    0,
			PositionPercent: 0,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(progress); err != nil {
		log.Printf("Failed to encode progress: %v", err)
	}
}

// APISaveProgress saves reading progress via JSON API
func (h *Handlers) APISaveProgress(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	bookSKU := chi.URLParam(r, "sku")

	var update models.ProgressUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	book, err := h.repo.GetLibraryBook(r.Context(), userID, bookSKU)
	if err != nil || book == nil {
		http.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	err = h.repo.SaveReadingProgress(r.Context(), userID, bookSKU, book.GutenbergID, &update)
	if err != nil {
		http.Error(w, "Failed to save progress", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "saved"}); err != nil {
		log.Printf("Failed to encode save response: %v", err)
	}
}
