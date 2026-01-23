package handlers

import (
	"context"
	"html/template"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/rbcervilla/redisstore/v9"

	"github.com/johnnyr0x/reader-app/internal/epub"
	"github.com/johnnyr0x/reader-app/internal/repository"
)

const sessionName = "reader_session"

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
	
	// For page templates, execute "base" which includes the page content
	// For partials, execute the partial directly
	var err error
	if name == "login.html" || name == "library.html" || name == "reader.html" {
		err = tmpl.ExecuteTemplate(w, "base", data)
	} else {
		// Partials - execute by filename
		err = tmpl.ExecuteTemplate(w, name, data)
	}
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// RequireAuth is middleware that requires authentication
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := h.getUserID(r)
		if userID == 0 {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// Add user ID to context
		ctx := context.WithValue(r.Context(), "user_id", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Health handles health check requests
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Ready handles readiness check requests
func (h *Handlers) Ready(w http.ResponseWriter, r *http.Request) {
	// Check database
	if err := h.repo.Ping(r.Context()); err != nil {
		http.Error(w, "Database not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}

// Home redirects to library
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/library", http.StatusSeeOther)
}

// LoginPage renders the login page
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "login.html", nil)
}

// Login handles login form submission
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	// For demo purposes, accept any user ID
	// In production, validate against Bookstore user database
	// User 10 has purchases in the test database
	userID := 10 // Demo user with test purchases

	session := h.getSession(r)
	session.Values["user_id"] = userID
	session.Save(r, w)

	http.Redirect(w, r, "/library", http.StatusSeeOther)
}

// Logout handles logout
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	session := h.getSession(r)
	session.Values["user_id"] = nil
	session.Options.MaxAge = -1
	session.Save(r, w)

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
