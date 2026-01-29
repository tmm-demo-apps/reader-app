package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/johnnyr0x/reader-app/internal/models"
)

// Library renders the user's book library
func (h *Handlers) Library(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)

	// Get user's library
	books, err := h.repo.GetUserLibrary(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load library", http.StatusInternalServerError)
		return
	}

	// Get reading progress for each book
	for i := range books {
		progress, _ := h.repo.GetReadingProgress(r.Context(), userID, books[i].SKU)
		books[i].Progress = progress
	}

	h.render(w, "library.html", map[string]interface{}{
		"Books":  books,
		"UserID": userID,
	})
}

// APILibrary returns the user's library as JSON
func (h *Handlers) APILibrary(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)

	books, err := h.repo.GetUserLibrary(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load library", http.StatusInternalServerError)
		return
	}

	// Get reading progress for each book
	for i := range books {
		progress, _ := h.repo.GetReadingProgress(r.Context(), userID, books[i].SKU)
		books[i].Progress = progress
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"books": books,
	}); err != nil {
		log.Printf("Failed to encode library response: %v", err)
	}
}

// SyncLibrary syncs the user's library from the Bookstore
// Handles Bookstore being unavailable gracefully
func (h *Handlers) SyncLibrary(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)

	// Get purchases from Bookstore
	purchases, err := h.bookstoreClient.GetUserPurchases(userID)
	if err != nil {
		// Bookstore is unavailable - return current library count instead of failing
		books, libErr := h.repo.GetUserLibrary(r.Context(), userID)
		if libErr != nil {
			http.Error(w, "Failed to access library", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"synced":          0,
			"existing":        len(books),
			"bookstore_error": "Bookstore unavailable, showing cached library",
		}); err != nil {
			log.Printf("Failed to encode sync response: %v", err)
		}
		return
	}

	// Add each purchase to local library
	synced := 0
	// Use bookstore browser URL for cover images
	// This should match the URL users access bookstore from (not internal K8s URL)
	browserBookstoreURL := h.bookstoreClient.BrowserURL()
	for _, p := range purchases {
		// Prefix cover URL with bookstore URL if it's a relative path
		coverURL := p.CoverURL
		if coverURL != "" && !strings.HasPrefix(coverURL, "http") {
			coverURL = browserBookstoreURL + coverURL
		}

		book := &models.Book{
			UserID:      userID,
			SKU:         p.SKU,
			GutenbergID: p.GutenbergID,
			Title:       p.Title,
			Author:      p.Author,
			CoverURL:    coverURL,
			AcquiredAt:  p.PurchasedAt,
		}
		if err := h.repo.AddToLibrary(r.Context(), book); err != nil {
			// Log error but continue
			continue
		}
		synced++
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"synced": synced,
	}); err != nil {
		log.Printf("Failed to encode sync response: %v", err)
	}
}
