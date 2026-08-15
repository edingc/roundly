package apikey

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

// Handler exposes key management under /api/account/keys.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Register attaches the key endpoints to an already-authenticated router.
func (h *Handler) Register(r chi.Router) {
	r.Route("/account/keys", func(kr chi.Router) {
		kr.Use(RequireUserPrincipal)
		kr.Get("/", h.list)
		kr.Post("/", h.create)
		kr.Delete("/{keyID}", h.revoke)
	})
}

// RequireUserPrincipal refuses a request authenticated by an API key.
//
// Guard already blocks all of /api/account, so this is redundant today — which
// is exactly the reason it is here. If that block is ever narrowed, key
// management must not be the thing that quietly opens up: a key that can mint
// or revoke keys is no longer read-only in any meaningful sense.
func RequireUserPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.IsAPIKey(r.Context()) {
			httpx.Error(w, r, httpx.Forbidden("API keys cannot manage API keys."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	keys, err := h.service.List(ctx, auth.MustUserID(ctx))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"keys": keys})
}

type createRequest struct {
	Name string `json:"name"`
	// ExpiresInDays is optional; zero means the key never expires.
	ExpiresInDays int `json:"expires_in_days"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	created, err := h.service.Create(ctx, auth.MustUserID(ctx), req.Name, req.ExpiresInDays)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// The only response that will ever contain this token.
	httpx.JSON(w, http.StatusCreated, created)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.Revoke(ctx, auth.MustUserID(ctx), chi.URLParam(r, "keyID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
