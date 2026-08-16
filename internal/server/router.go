// Package server wires the domain packages into one HTTP handler.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/edingc/roundly/internal/account"
	"github.com/edingc/roundly/internal/apikey"
	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/club"
	"github.com/edingc/roundly/internal/config"
	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/geocode"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/mail"
	"github.com/edingc/roundly/internal/ratelimit"
	"github.com/edingc/roundly/internal/round"
	"github.com/edingc/roundly/internal/stats"
)

// New builds the application handler: the JSON API under /api plus the embedded
// frontend on every other path.
//
// stop shuts down the background goroutines this installs — the API-key
// last-used flusher and the rate limiter's janitor. Closing it is what keeps
// them from outliving the server, which matters most in tests, where each case
// builds its own handler.
func New(cfg *config.Config, db *database.DB, frontend http.Handler, stop <-chan struct{}) (http.Handler, error) {
	googleProvider := auth.NewGoogleProvider(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)
	tokenIssuer := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)

	// A half-filled mail configuration fails the boot rather than starting an
	// instance whose verification emails silently go nowhere. No configuration
	// at all is not an error: it means the features that need mail stay off.
	mailer, err := mail.New(cfg.MailConfig())
	if err != nil {
		return nil, fmt.Errorf("configure mail: %w", err)
	}
	if mailer != nil {
		slog.Info("mail ready", "transport", mailer.Describe())
	}

	authService := auth.NewService(db, tokenIssuer, googleProvider, auth.Options{
		AdminEmail: cfg.AdminEmail,
		PublicURL:  cfg.PublicURL,
		Avatars:    auth.NewAvatarSigner(cfg.JWTSecret),
		Mailer:     mailer,
	})
	authHandler := auth.NewHandler(authService, googleProvider, auth.HandlerOptions{
		PublicURL:        cfg.PublicURL,
		Secure:           cfg.IsProd(),
		GeocodingEnabled: cfg.GeocodingEnabled(),
		LoginRateLimit:   cfg.LoginRateLimit,
		LoginRateWindow:  cfg.LoginRateWindow,
		SignupRateLimit:  cfg.SignupRateLimit,
		SignupRateWindow: cfg.SignupRateWindow,
	})
	// Nil unless an operator configured a geocoder, in which case courses keep
	// whatever coordinates were typed. The User-Agent carries this instance's
	// public URL because the OSM usage policy asks that a client be identifiable
	// and its operator reachable.
	var geocoder geocode.Geocoder
	if cfg.GeocodingEnabled() {
		geocoder = geocode.NewNominatim(cfg.NominatimURL, "Roundly/1.0 (+"+cfg.PublicURL+")")
	}

	courseService := course.NewService(db, geocoder)
	courseHandler := course.NewHandler(courseService, authService.RequireAdmin)
	roundService := round.NewService(db, courseService)
	roundHandler := round.NewHandler(roundService)
	statsHandler := stats.NewHandler(stats.NewService(db))
	// The bag needs to know whether a club has been played before it will let
	// one be deleted, which is the dependency Phase 2 predicted when it made
	// retirement a soft delete.
	clubHandler := club.NewHandler(club.NewService(db, roundService))
	accountHandler := account.NewHandler(account.NewService(db, authService, courseService))

	apiKeyService := apikey.NewService(db, cfg.APIKeyMaxPerUser)
	apiKeyHandler := apikey.NewHandler(apiKeyService)
	requestLimiter := ratelimit.New(cfg.APIKeyRateLimit, cfg.APIKeyRateWindow)
	// Failed authentications are limited by IP separately and much harder.
	// Guessing a 256-bit token is hopeless, but each attempt still costs an
	// indexed query on the only database connection this server has.
	failureLimiter := ratelimit.New(20, time.Minute)

	// A key's last-used time is written off the request path, coalesced to at
	// most one update per key per window, so that a read-only request never has
	// to take the single write connection.
	apiKeyService.StartFlusher(stop)
	// Sweeps expired refresh tokens, sign-in codes, and remembered devices.
	authService.StartJanitor(stop)
	// Sweeps the failed-sign-in counters.
	authHandler.StartJanitor(stop)
	requestLimiter.StartJanitor(stop)
	failureLimiter.StartJanitor(stop)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(corsMiddleware(cfg.CORSOrigins))
	// Last global middleware, and global rather than per-route on purpose: it
	// runs before chi routes anything, so no endpoint registered later — in any
	// group, in any package — can end up outside its reach.
	r.Use(apikey.Guard(apiKeyService, requestLimiter, failureLimiter))

	r.Route("/api", func(api chi.Router) {
		api.Get("/health", health)
		api.Mount("/auth", authHandler.Routes())

		// Avatars are served unauthenticated: an <img> tag cannot carry a
		// bearer token, and the SPA holds its access token in memory only. The
		// signature and expiry on the URL are what protect the image — see
		// auth.AvatarSigner.
		api.Get("/avatars/{name}", accountHandler.ServeAvatar)

		// The course directory, the golf bag, and the account are entirely
		// behind authentication.
		api.Group(func(protected chi.Router) {
			protected.Use(authService.Middleware)
			// Ordered after Middleware and before every domain router: an
			// unconfirmed account holds a valid session and is still refused
			// everything the app does. The auth routes are mounted above this
			// group precisely so that confirming, resending, and signing out
			// stay reachable from that state.
			protected.Use(authService.RequireVerifiedEmail)
			courseHandler.Register(protected)
			roundHandler.Register(protected)
			statsHandler.Register(protected)
			clubHandler.Register(protected)
			accountHandler.Register(protected)
			apiKeyHandler.Register(protected)
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

	return r, nil
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
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Roundly-Device")
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
