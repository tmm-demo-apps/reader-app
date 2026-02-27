package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BookstoreClient communicates with the Bookstore API
type BookstoreClient struct {
	baseURL    string
	browserURL string // URL for browser-facing links (may differ from API URL in K8s)
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
// browserURL is the URL users access from their browser (may differ from API URL in K8s)
func NewBookstoreClient(baseURL, browserURL string) *BookstoreClient {
	if browserURL == "" {
		browserURL = baseURL // Default to API URL if not specified
	}
	return &BookstoreClient{
		baseURL:    baseURL,
		browserURL: browserURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// BaseURL returns the bookstore base URL (for API calls)
func (c *BookstoreClient) BaseURL() string {
	return c.baseURL
}

// BrowserURL returns the bookstore URL for browser-facing links
func (c *BookstoreClient) BrowserURL() string {
	return c.browserURL
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

// AuthResponse is the response from the Bookstore auth API
type AuthResponse struct {
	UserID int    `json:"user_id"`
	Email  string `json:"email"`
}

// VerifyToken validates a one-time auth token against the Bookstore
func (c *BookstoreClient) VerifyToken(token string) (*AuthResponse, error) {
	url := fmt.Sprintf("%s/api/auth/verify-token", c.baseURL)

	reqBody, err := json.Marshal(map[string]string{
		"token": token,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token verification failed: status %d", resp.StatusCode)
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &authResp, nil
}

// Authenticate validates credentials against the Bookstore and returns user info
func (c *BookstoreClient) Authenticate(email, password string) (*AuthResponse, error) {
	url := fmt.Sprintf("%s/api/auth", c.baseURL)

	reqBody, err := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal auth request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid credentials")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authentication failed: status %d", resp.StatusCode)
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("failed to decode auth response: %w", err)
	}

	return &authResp, nil
}
