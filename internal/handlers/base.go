package handlers

import (
	"context"
	"html/template"
	"log"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/rbcervilla/redisstore/v9"

	"github.com/johnnyr0x/reader-app/internal/epub"
	"github.com/johnnyr0x/reader-app/internal/repository"
)

const sessionName = "reader_session"

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const userIDKey contextKey = "user_id"

// Handlers contains all HTTP handlers
type Handlers struct {
	repo            *repository.PostgresRepository
	bookstoreClient *repository.BookstoreClient
	fetcher         *epub.Fetcher
	parser          *epub.Parser
	sessionStore    *redisstore.RedisStore
	templates       map[string]*template.Template
}

// NewHandlers creates a new Handlers instance
func NewHandlers(
	repo *repository.PostgresRepository,
	bookstoreClient *repository.BookstoreClient,
	fetcher *epub.Fetcher,
	parser *epub.Parser,
	sessionStore *redisstore.RedisStore,
	templates map[string]*template.Template,
) *Handlers {
	return &Handlers{
		repo:            repo,
		bookstoreClient: bookstoreClient,
		fetcher:         fetcher,
		parser:          parser,
		sessionStore:    sessionStore,
		templates:       templates,
	}
}

// getSession returns the session for the request
func (h *Handlers) getSession(r *http.Request) *sessions.Session {
	session, _ := h.sessionStore.Get(r, sessionName)
	return session
}

// getUserID returns the user ID from the session, or 0 if not logged in
func (h *Handlers) getUserID(r *http.Request) int {
	session := h.getSession(r)
	userID, ok := session.Values["user_id"].(int)
	if !ok {
		return 0
	}
	return userID
}

// render renders a template with the given data
func (h *Handlers) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	tmpl, ok := h.templates[name]
	if !ok {
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}

	// Inject common template data (like BookstoreURL)
	templateData := h.injectCommonData(data)

	// For page templates, execute "base" which includes the page content
	// For partials, execute the partial directly
	var err error
	if name == "login.html" || name == "library.html" || name == "reader.html" {
		err = tmpl.ExecuteTemplate(w, "base", templateData)
	} else {
		// Partials - execute by filename
		err = tmpl.ExecuteTemplate(w, name, templateData)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// injectCommonData adds common template data like BookstoreURL
func (h *Handlers) injectCommonData(data interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	// Copy existing data if it's a map
	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			result[k] = v
		}
	}
	
	// Add common data
	result["BookstoreURL"] = h.bookstoreClient.BrowserURL()
	
	return result
}

// RequireAuth is middleware that requires authentication
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := h.getUserID(r)
		if userID == 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// Add user ID to context using typed key
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Health handles health check requests
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// Ready handles readiness check requests
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	// Check database
	if err := h.repo.Ping(r.Context()); err != nil {
		http.Error(w, "Database not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ready"))
}

// Home redirects to library
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/library", http.StatusSeeOther)
}

// LoginPage renders the login page
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login.html", map[string]interface{}{})
}

// Login handles login form submission
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		h.render(w, "login.html", map[string]interface{}{
			"Error": "Email and password are required",
		})
		return
	}

	// Authenticate against Bookstore API
	authResp, err := h.bookstoreClient.Authenticate(email, password)
	if err != nil {
		log.Printf("Authentication failed for %s: %v", email, err)
		h.render(w, "login.html", map[string]interface{}{
			"Error": "Invalid email or password. Please use your Bookstore credentials.",
		})
		return
	}

	// Save user ID to session
	session := h.getSession(r)
	session.Values["user_id"] = authResp.UserID
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session: %v", err)
	}

	log.Printf("User %s (ID: %d) logged in successfully", email, authResp.UserID)
	http.Redirect(w, r, "/library", http.StatusSeeOther)
}

// Logout handles logout
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	session := h.getSession(r)
	session.Values["user_id"] = nil
	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session: %v", err)
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
