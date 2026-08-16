package stats

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
)

// Handler exposes the overview endpoint.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Register attaches the endpoint to an already-authenticated router.
func (h *Handler) Register(r chi.Router) {
	r.Get("/stats/overview", h.overview)
}

// defaultWindow is twenty rounds, which is what the World Handicap System
// looks at and therefore the window in which the handicap figure means what it
// says.
const defaultWindow = 20

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	window := defaultWindow
	if raw := strings.TrimSpace(r.URL.Query().Get("rounds")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		// Rejected rather than clamped: a client asking for a window this
		// endpoint does not offer should be told so, not quietly answered a
		// different question.
		if err != nil || !slices.Contains(Windows, parsed) {
			httpx.Error(w, r, httpx.BadRequest("That is not a window this endpoint offers."))
			return
		}
		window = parsed
	}

	ctx := r.Context()
	overview, err := h.service.Overview(ctx, auth.MustUserID(ctx), window)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, overview)
}
