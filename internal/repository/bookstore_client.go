package repository

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BookstoreClient communicates with the Bookstore API
type BookstoreClient struct {
	baseURL    string
	httpClient *http.Client
}

// PurchasedBook represents a book purchased from the Bookstore
type PurchasedBook struct {
	SKU         string    `json:"sku"`
	GutenbergID int       `json:"gutenberg_id"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	CoverURL    string    `json:"cover_url"`
	PurchasedAt time.Time `json:"purchased_at"`
}

// PurchasesResponse is the API response for user purchases
type PurchasesResponse struct {
	Purchases []PurchasedBook `json:"purchases"`
}

// NewBookstoreClient creates a new BookstoreClient
func NewBookstoreClient(baseURL string) *BookstoreClient {
	return &BookstoreClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// BaseURL returns the bookstore base URL
func (c *BookstoreClient) BaseURL() string {
	return c.baseURL
}

// VerifyPurchase checks if a user owns a specific book
func (c *BookstoreClient) VerifyPurchase(userID int, bookSKU string) (bool, error) {
	url := fmt.Sprintf("%s/api/purchases/%d/%s", c.baseURL, userID, bookSKU)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return false, fmt.Errorf("failed to verify purchase: %w", err)
	}
	defer resp.Body.Close()

	// 200 = owned, 404 = not owned
	return resp.StatusCode == http.StatusOK, nil
}

// GetUserPurchases returns all books purchased by a user
func (c *BookstoreClient) GetUserPurchases(userID int) ([]PurchasedBook, error) {
	url := fmt.Sprintf("%s/api/purchases/%d", c.baseURL, userID)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get purchases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result PurchasesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Purchases, nil
}

// Health checks if the Bookstore API is available
func (c *BookstoreClient) Health() error {
	url := fmt.Sprintf("%s/health", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("bookstore health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bookstore unhealthy: status %d", resp.StatusCode)
	}

	return nil
}
