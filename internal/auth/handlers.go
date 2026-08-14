package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/edingc/roundly/internal/httpx"
)

// Cookie names for the OAuth redirect round trip. All are short-lived and
// cleared when the callback runs.
const (
	stateCookie    = "roundly_oauth_state"
	verifierCookie = "roundly_oauth_verifier"
	modeCookie     = "roundly_oauth_mode"
	returnCookie   = "roundly_oauth_return"
)

const (
	modeLogin = "login"
	modeLink  = "link"
)

// oauthCookieTTL bounds how long a user has to finish the Google consent screen.
const oauthCookieTTL = 10 * time.Minute

// Handler exposes the auth endpoints.
type Handler struct {
	service   *Service
	google    *GoogleProvider
	handoffs  *handoffStore
	publicURL string
	secure    bool
}

func NewHandler(service *Service, google *GoogleProvider, publicURL string, secure bool) *Handler {
	return &Handler{
		service:   service,
		google:    google,
		handoffs:  newHandoffStore(),
		publicURL: strings.TrimRight(publicURL, "/"),
		secure:    secure,
	}
}

// Routes returns the /api/auth router. Everything under /me, /link, and
// /password requires an access token; the rest is public by necessity.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/config", h.config)
	r.Post("/signup", h.signUp)
	r.Post("/login", h.logIn)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logOut)

	r.Get("/google/start", h.googleStart)
	r.Get("/google/callback", h.googleCallback)
	r.Post("/google/exchange", h.googleExchange)

	r.Group(func(pr chi.Router) {
		pr.Use(h.service.Middleware)
		pr.Get("/me", h.me)
		pr.Get("/link/google", h.googleStart)
		pr.Post("/link/google", h.googleStart)
		pr.Post("/password", h.setPassword)
	})

	return r
}

type configResponse struct {
	GoogleEnabled bool `json:"google_enabled"`
}

// config lets the frontend decide whether to render the Google button.
func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, configResponse{GoogleEnabled: h.google.Enabled()})
}

type signUpRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (h *Handler) signUp(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	email := v.Email("email", req.Email)
	v.Required("display_name", req.DisplayName)
	v.MaxLen("display_name", strings.TrimSpace(req.DisplayName), 80)
	validatePassword(v, "password", req.Password)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.service.SignUp(r.Context(), email, req.Password, req.DisplayName)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, session)
}

type logInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) logIn(w http.ResponseWriter, r *http.Request) {
	var req logInRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// Login validates only presence: telling a caller their password is too
	// short would leak the stored password policy for that account.
	v := httpx.NewValidator()
	v.Required("email", req.Email)
	v.Required("password", req.Password)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.service.LogIn(r.Context(), req.Email, req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

func (h *Handler) logOut(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.service.LogOut(r.Context(), req.RefreshToken); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, err := h.service.CurrentUser(r.Context(), MustUserID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

type setPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Handler) setPassword(w http.ResponseWriter, r *http.Request) {
	var req setPasswordRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	validatePassword(v, "new_password", req.NewPassword)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	if err := h.service.SetPassword(ctx, MustUserID(ctx), req.CurrentPassword, req.NewPassword); err != nil {
		httpx.Error(w, r, err)
		return
	}

	user, err := h.service.CurrentUser(ctx, MustUserID(ctx))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// googleStart redirects the browser to Google's consent screen.
//
// It serves both first-time login and linking Google to an existing account. In
// link mode the route sits behind auth middleware, and the authenticated user ID
// is carried in the mode cookie so the callback knows which account to attach to.
func (h *Handler) googleStart(w http.ResponseWriter, r *http.Request) {
	if !h.google.Enabled() {
		httpx.Error(w, r, httpx.BadRequest(
			"Google sign-in is not configured on this server. Set GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and GOOGLE_REDIRECT_URL."))
		return
	}

	state, err := randomToken()
	if err != nil {
		httpx.Error(w, r, httpx.Internal(err))
		return
	}
	verifier := h.google.NewVerifier()

	mode := modeLogin
	if userID, ok := UserID(r.Context()); ok {
		mode = modeLink + ":" + userID
	}

	authURL, err := h.google.AuthCodeURL(state, verifier)
	if err != nil {
		httpx.Error(w, r, httpx.Internal(err))
		return
	}

	h.setCookie(w, stateCookie, state)
	h.setCookie(w, verifierCookie, verifier)
	h.setCookie(w, modeCookie, mode)
	h.setCookie(w, returnCookie, h.safeReturnPath(r.URL.Query().Get("return_to")))

	// The frontend opens this in the same tab, so a redirect is what it wants.
	// A JSON body is returned when the caller asks for one, which is how the
	// authenticated link flow (an XHR) gets the URL.
	if wantsJSON(r) {
		httpx.JSON(w, http.StatusOK, map[string]string{"authorization_url": authURL})
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// googleCallback handles Google's redirect, then bounces the browser back to the
// SPA with a single-use handoff code instead of the tokens themselves.
func (h *Handler) googleCallback(w http.ResponseWriter, r *http.Request) {
	mode := h.readCookie(r, modeCookie)
	returnTo := h.safeReturnPath(h.readCookie(r, returnCookie))
	h.clearOAuthCookies(w)

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		h.redirectToApp(w, r, returnTo, url.Values{"error": {googleErrorMessage(errParam)}})
		return
	}

	state := h.readCookie(r, stateCookie)
	verifier := h.readCookie(r, verifierCookie)
	if state == "" || verifier == "" {
		h.redirectToApp(w, r, returnTo, url.Values{
			"error": {"That sign-in attempt expired. Please try again."},
		})
		return
	}
	// Constant-time-ish comparison is unnecessary here; the state is a
	// per-attempt random value and a mismatch is fatal either way.
	if state != r.URL.Query().Get("state") {
		h.redirectToApp(w, r, returnTo, url.Values{
			"error": {"Sign-in could not be verified. Please try again."},
		})
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		h.redirectToApp(w, r, returnTo, url.Values{
			"error": {"Google did not return an authorization code."},
		})
		return
	}

	identity, err := h.google.Exchange(r.Context(), code, verifier)
	if err != nil {
		httpx.Error(w, r, httpx.Internal(err))
		return
	}

	// Link mode: attach this Google identity to the account that started the flow.
	if linkedUserID, ok := strings.CutPrefix(mode, modeLink+":"); ok && linkedUserID != "" {
		if _, err := h.service.LinkGoogle(r.Context(), linkedUserID, identity); err != nil {
			h.redirectToApp(w, r, returnTo, url.Values{"error": {userFacing(err)}})
			return
		}
		h.redirectToApp(w, r, returnTo, url.Values{"linked": {"google"}})
		return
	}

	session, err := h.service.CompleteGoogleLogin(r.Context(), identity)
	if err != nil {
		h.redirectToApp(w, r, returnTo, url.Values{"error": {userFacing(err)}})
		return
	}

	handoff, err := h.handoffs.issue(session)
	if err != nil {
		httpx.Error(w, r, httpx.Internal(err))
		return
	}
	h.redirectToApp(w, r, "/auth/callback", url.Values{"code": {handoff}})
}

type googleExchangeRequest struct {
	Code string `json:"code"`
}

// googleExchange trades a handoff code for the session it stands for.
func (h *Handler) googleExchange(w http.ResponseWriter, r *http.Request) {
	var req googleExchangeRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session := h.handoffs.redeem(strings.TrimSpace(req.Code))
	if session == nil {
		httpx.Error(w, r, httpx.Unauthorized("That sign-in link has already been used or has expired. Please try again."))
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

func (h *Handler) setCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(oauthCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secure,
		// Lax, not Strict: the cookie has to survive Google's cross-site
		// redirect back to the callback, which Strict would drop.
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func (h *Handler) clearOAuthCookies(w http.ResponseWriter) {
	for _, name := range []string{stateCookie, verifierCookie, modeCookie, returnCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   h.secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// redirectToApp sends the browser back to the SPA at a relative path.
func (h *Handler) redirectToApp(w http.ResponseWriter, r *http.Request, path string, params url.Values) {
	if path == "" {
		path = "/"
	}
	target := h.publicURL + path
	if len(params) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + params.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// safeReturnPath keeps the post-login redirect on this origin. Without this
// check, ?return_to=https://evil.example would turn the callback into an open
// redirect that looks like it came from the app.
func (h *Handler) safeReturnPath(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "/"
	}
	// Reject anything that could be read as a scheme or a protocol-relative URL.
	if !strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "//") {
		return "/"
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Host != "" || parsed.Scheme != "" {
		return "/"
	}
	return parsed.RequestURI()
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func wantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("mode") == "json" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

func validatePassword(v *httpx.Validator, field, password string) {
	if password == "" {
		v.Add(field, "This field is required.")
		return
	}
	if len(password) < MinPasswordLength {
		v.Add(field, "Use at least 8 characters.")
		return
	}
	if len(password) > MaxPasswordLength {
		v.Add(field, "Use 128 characters or fewer.")
	}
}

// userFacing extracts a message safe to put in a redirect query parameter.
func userFacing(err error) string {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) && apiErr.Status < http.StatusInternalServerError {
		return apiErr.Message
	}
	return "Sign-in failed. Please try again."
}

func googleErrorMessage(code string) string {
	switch code {
	case "access_denied":
		return "You cancelled Google sign-in."
	default:
		return "Google sign-in failed. Please try again."
	}
}
