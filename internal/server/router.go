// Package server wires the domain packages into one HTTP handler.
package server

import (
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/edingc/roundly/internal/account"
	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/club"
	"github.com/edingc/roundly/internal/config"
	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/httpx"
)

// New builds the application handler: the JSON API under /api plus the embedded
// frontend on every other path.
func New(cfg *config.Config, db *database.DB, frontend http.Handler) http.Handler {
	googleProvider := auth.NewGoogleProvider(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)
	tokenIssuer := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	authService := auth.NewService(db, tokenIssuer, googleProvider)
	authHandler := auth.NewHandler(authService, googleProvider, cfg.PublicURL, cfg.IsProd())
	courseService := course.NewService(db)
	courseHandler := course.NewHandler(courseService)
	clubHandler := club.NewHandler(club.NewService(db))
	accountHandler := account.NewHandler(account.NewService(db, authService, courseService))

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(corsMiddleware(cfg.CORSOrigins))

	r.Route("/api", func(api chi.Router) {
		api.Get("/health", health)
		api.Mount("/auth", authHandler.Routes())

		// Avatars are served unauthenticated: an <img> tag cannot carry a
		// bearer token, and the SPA holds its access token in memory only. The
		// unguessable key in the path is what protects the image.
		api.Get("/avatars/{name}", accountHandler.ServeAvatar)

		// The course directory, the golf bag, and the account are entirely
		// behind authentication.
		api.Group(func(protected chi.Router) {
			protected.Use(authService.Middleware)
			courseHandler.Register(protected)
			clubHandler.Register(protected)
			accountHandler.Register(protected)
		})

		// Any unmatched /api path is a client error, not a request for the SPA.
		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.Error(w, r, httpx.NotFound("That endpoint does not exist."))
		})
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			httpx.Error(w, r, &httpx.APIError{
				Status:  http.StatusMethodNotAllowed,
				Code:    "method_not_allowed",
				Message: "That method is not allowed on this endpoint.",
			})
		})
	})

	if frontend != nil {
		r.Handle("/*", frontend)
	}

	return r
}

func health(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// corsMiddleware allows the configured origins to call the API with credentials.
// In development the Vite dev server is a different origin than the API, so this
// is required rather than optional; in production the SPA is served from the same
// origin and the allow-list is simply never consulted.
func corsMiddleware(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && slices.Contains(allowed, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Max-Age", "300")
				// Responses vary by Origin, so caches must not share them.
				w.Header().Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
