package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/rbcervilla/redisstore/v9"
	"github.com/redis/go-redis/v9"

	"github.com/johnnyr0x/reader-app/internal/epub"
	"github.com/johnnyr0x/reader-app/internal/handlers"
	"github.com/johnnyr0x/reader-app/internal/repository"
)

func main() {
	// Load configuration from environment
	port := getEnv("PORT", "8081")
	dbURL := getEnv("DATABASE_URL", "postgres://reader:reader@localhost:5432/reader?sslmode=disable")
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379")
	minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	minioAccessKey := getEnv("MINIO_ACCESS_KEY", "minioadmin")
	minioSecretKey := getEnv("MINIO_SECRET_KEY", "minioadmin")
	minioUseSSL := getEnv("MINIO_USE_SSL", "false") == "true"
	minioBucket := getEnv("MINIO_BUCKET", "books-epub")
	bookstoreURL := getEnv("BOOKSTORE_API_URL", "http://localhost:8080")
	bookstoreBrowserURL := getEnv("BOOKSTORE_BROWSER_URL", "") // URL for browser links (defaults to API URL)
	sessionSecret := getEnv("SESSION_SECRET", "reader-session-secret-change-me")

	// Connect to PostgreSQL
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	// Connect to Redis
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")

	// Set up session store
	sessionStore, err := redisstore.NewRedisStore(ctx, redisClient)
	if err != nil {
		log.Fatalf("Failed to create session store: %v", err)
	}
	sessionStore.KeyPrefix("reader_session_")
	sessionStore.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	})
	_ = sessionSecret // Used for cookie encryption

	// Connect to MinIO
	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: minioUseSSL,
	})
	if err != nil {
		log.Fatalf("Failed to connect to MinIO: %v", err)
	}

	// Ensure bucket exists
	exists, err := minioClient.BucketExists(ctx, minioBucket)
	if err != nil {
		log.Fatalf("Failed to check MinIO bucket: %v", err)
	}
	if !exists {
		if err := minioClient.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatalf("Failed to create MinIO bucket: %v", err)
		}
		log.Printf("Created MinIO bucket: %s", minioBucket)
	}
	log.Println("Connected to MinIO")

	// Initialize components
	repo := repository.NewPostgresRepository(db)
	bookstoreClient := repository.NewBookstoreClient(bookstoreURL, bookstoreBrowserURL)
	epubFetcher := epub.NewFetcher(minioClient, minioBucket, db)
	epubParser := epub.NewParser(minioClient, minioBucket)

	// Parse templates
	templates, err := parseTemplates()
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}

	// Initialize handlers
	h := handlers.NewHandlers(
		repo,
		bookstoreClient,
		epubFetcher,
		epubParser,
		sessionStore,
		templates,
	)

	// Set up router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(60 * time.Second))

	// Static files
	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Health endpoints
	r.Get("/health", h.Health)
	r.Get("/ready", h.Ready)

	// Public routes
	r.Get("/", h.Home)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/logout", h.Logout)

	// Protected routes (require authentication)
	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)

		// Library
		r.Get("/library", h.Library)

		// Reader
		r.Get("/read/{sku}", h.Reader)
		r.Get("/read/{sku}/toc", h.TableOfContents)
		r.Get("/read/{sku}/chapter/{index}", h.Chapter)
		r.Post("/read/{sku}/progress", h.SaveProgress)

		// API endpoints
		r.Get("/api/library", h.APILibrary)
		r.Get("/api/books/{sku}/metadata", h.APIBookMetadata)
		r.Get("/api/books/{sku}/progress", h.APIGetProgress)
		r.Put("/api/books/{sku}/progress", h.APISaveProgress)

		// Internal endpoints
		r.Post("/internal/sync-library", h.SyncLibrary)
	})

	// Create server
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting Reader server on port %s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseTemplates() (map[string]*template.Template, error) {
	funcMap := template.FuncMap{
		"sub":      func(a, b int) int { return a - b },
		"add":      func(a, b int) int { return a + b },
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		"toJSON": func(v interface{}) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("{}")
			}
			return template.JS(b)
		},
	}

	templates := make(map[string]*template.Template)

	// Parse each page template with base
	pages := []string{"login.html", "library.html", "reader.html"}
	for _, page := range pages {
		t, err := template.New("").Funcs(funcMap).ParseFiles(
			"templates/base.html",
			"templates/"+page,
		)
		if err != nil {
			return nil, err
		}
		templates[page] = t
	}

	// Parse partials separately
	partials := []string{"partials/toc.html", "partials/chapter.html"}
	for _, partial := range partials {
		t, err := template.New("").Funcs(funcMap).ParseFiles("templates/" + partial)
		if err != nil {
			return nil, err
		}
		templates[partial] = t
	}

	return templates, nil
}
