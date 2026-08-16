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

// deviceTokenHeader carries the trusted-device token on requests that have no
// body to put it in. Only the device list uses it, and only to mark which row
// is the browser doing the asking.
const deviceTokenHeader = "X-Roundly-Device"

// Handler exposes the auth endpoints.
type Handler struct {
	service   *Service
	google    *GoogleProvider
	handoffs  *handoffStore
	publicURL string
	secure    bool
	// geocodingEnabled is reported by /auth/config and used for nothing else
	// here. It rides along because that endpoint is where the frontend already
	// asks what this instance can do, and a second config endpoint answering
	// one boolean would be worse.
	geocodingEnabled bool
	// attempts caps wrong answers on the credential endpoints. It lives on the
	// handler rather than the service because it counts by IP as well as by
	// account, and the request is the only thing that knows the IP.
	attempts *throttle
	// signups caps account creation. Separate from attempts because it counts
	// the opposite thing — see signupThrottle.
	signups *signupThrottle
}

// HandlerOptions is the handler's configuration, as a struct for the same
// reason Options is: the list had grown past the point where a reader could
// tell two adjacent booleans apart at the call site.
type HandlerOptions struct {
	PublicURL        string
	Secure           bool
	GeocodingEnabled bool
	// LoginRateLimit is how many failed attempts one account may make in
	// LoginRateWindow. The per-IP allowance is three times this.
	LoginRateLimit  int
	LoginRateWindow time.Duration
	// SignupRateLimit is how many accounts may be created from one address in
	// SignupRateWindow, successful or not.
	SignupRateLimit  int
	SignupRateWindow time.Duration
}

func NewHandler(service *Service, google *GoogleProvider, opts HandlerOptions) *Handler {
	limit := opts.LoginRateLimit
	if limit <= 0 {
		limit = 10
	}
	window := opts.LoginRateWindow
	if window <= 0 {
		window = 15 * time.Minute
	}
	signupLimit := opts.SignupRateLimit
	if signupLimit <= 0 {
		signupLimit = 5
	}
	signupWindow := opts.SignupRateWindow
	if signupWindow <= 0 {
		signupWindow = time.Hour
	}
	return &Handler{
		service:          service,
		google:           google,
		handoffs:         newHandoffStore(),
		publicURL:        strings.TrimRight(opts.PublicURL, "/"),
		secure:           opts.Secure,
		geocodingEnabled: opts.GeocodingEnabled,
		attempts:         newThrottle(limit, window),
		signups:          newSignupThrottle(signupLimit, signupWindow),
	}
}

// StartJanitor sweeps the attempt counters until stop is closed.
func (h *Handler) StartJanitor(stop <-chan struct{}) {
	h.attempts.startJanitor(stop)
	h.signups.startJanitor(stop)
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

	r.Post("/two-factor/verify", h.twoFactorVerify)
	r.Post("/two-factor/recovery", h.twoFactorRecovery)
	r.Post("/verify-email", h.verifyEmail)

	r.Get("/google/start", h.googleStart)
	r.Get("/google/callback", h.googleCallback)
	r.Post("/google/exchange", h.googleExchange)

	r.Group(func(pr chi.Router) {
		pr.Use(h.service.Middleware)
		pr.Get("/me", h.me)
		pr.Get("/link/google", h.googleStart)
		pr.Post("/link/google", h.googleStart)
		pr.Post("/password", h.setPassword)
		pr.Put("/preferences", h.setPreferences)
		pr.Put("/two-factor", h.setTwoFactor)
		pr.Post("/two-factor/recovery-codes", h.regenerateRecoveryCodes)
		pr.Get("/devices", h.listDevices)
		pr.Delete("/devices/{deviceID}", h.forgetDevice)
		pr.Post("/verify-email/resend", h.resendVerification)
	})

	return r
}

type configResponse struct {
	GoogleEnabled    bool `json:"google_enabled"`
	GeocodingEnabled bool `json:"geocoding_enabled"`
	// EmailEnabled says whether this instance can send mail at all, which is
	// what gates both the confirm-your-address flow and the two-factor setting.
	// The frontend hides both when it is false rather than offering a switch
	// that cannot work.
	EmailEnabled bool `json:"email_enabled"`
	// EmailVerificationRequired says whether an unconfirmed account is blocked
	// from the app. Today it tracks EmailEnabled exactly; it is reported
	// separately because the client renders a different screen for each and
	// should not have to know they happen to coincide.
	EmailVerificationRequired bool `json:"email_verification_required"`
}

// config tells the frontend which optional features this instance has been
// given credentials for, so it can render the Google button and the address
// geocoding hint only where they mean something.
//
// It is unauthenticated and says nothing but yes or no: an instance's feature
// set is already obvious from its login screen.
func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, configResponse{
		GoogleEnabled:             h.google.Enabled(),
		GeocodingEnabled:          h.geocodingEnabled,
		EmailEnabled:              h.service.MailEnabled(),
		EmailVerificationRequired: h.service.EmailVerificationRequired(),
	})
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

	// Counted before validation rather than after. A script probing which
	// addresses are already registered never needs to send a valid password,
	// and charging only well-formed attempts would leave that free.
	if err := h.signups.attempt(clientIP(r)); err != nil {
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
	// DeviceToken is what a previous two-factor login handed this client to
	// skip the code next time. Absent on a first sign-in and on every client
	// that has never completed one.
	DeviceToken string `json:"device_token"`
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

	// Keyed on the normalised address so that casing and dots cannot be used to
	// get a fresh bucket per attempt.
	account := httpx.NormalizeEmail(req.Email)
	ip := clientIP(r)
	if err := h.attempts.check(account, ip); err != nil {
		httpx.Error(w, r, err)
		return
	}

	result, err := h.service.LogIn(r.Context(), req.Email, req.Password, req.DeviceToken)
	if err != nil {
		// Only the caller's own mistakes are counted. A 500 is this server
		// failing, and charging the user for it would turn one outage into a
		// lockout that outlives it.
		if isClientError(err) {
			h.attempts.failed(account, ip)
		}
		httpx.Error(w, r, err)
		return
	}
	// Two shapes, one status. A pending second factor is not an error — the
	// password was right — so it is a 200 carrying a challenge, and the client
	// tells them apart by the two_factor_required field.
	if result.Challenge != nil {
		httpx.JSON(w, http.StatusOK, result.Challenge)
		return
	}
	httpx.JSON(w, http.StatusOK, result.Session)
}

type twoFactorVerifyRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"code"`
	// RememberDevice asks for a device token so this browser is not challenged
	// again for thirty days.
	RememberDevice bool `json:"remember_device"`
}

// twoFactorVerify finishes a login that stopped for a code.
func (h *Handler) twoFactorVerify(w http.ResponseWriter, r *http.Request) {
	var req twoFactorVerifyRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	v.Required("challenge_id", req.ChallengeID)
	v.Required("code", req.Code)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// Counted by IP alone: the request names a challenge, not an account, and
	// resolving one to the other before the code is checked would make this
	// endpoint answer questions about which challenges exist. The challenge's
	// own five-attempt cap is the tighter limit; this is what stops somebody
	// working through challenges rather than through codes.
	ip := clientIP(r)
	if err := h.attempts.check("", ip); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.service.CompleteTwoFactor(
		r.Context(), req.ChallengeID, req.Code, req.RememberDevice, deviceLabel(r))
	if err != nil {
		if isClientError(err) {
			h.attempts.failed("", ip)
		}
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

type twoFactorSettingRequest struct {
	Enabled  bool   `json:"enabled"`
	Password string `json:"current_password"`
}

// setTwoFactor turns email sign-in codes on or off for the caller.
func (h *Handler) setTwoFactor(w http.ResponseWriter, r *http.Request) {
	// An API key must never be able to change how its owner signs in. The key
	// is read-only by policy, but this is the guarantee rather than that.
	if IsAPIKey(r.Context()) {
		httpx.Error(w, r, httpx.Forbidden("API keys cannot change sign-in settings."))
		return
	}

	var req twoFactorSettingRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	setup, err := h.service.SetTwoFactor(ctx, MustUserID(ctx), req.Password, req.Enabled)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// Carries the recovery codes when enabling. This is the only response that
	// ever will — they are stored hashed and cannot be shown again.
	httpx.JSON(w, http.StatusOK, setup)
}

type recoveryCodesRequest struct {
	Password string `json:"current_password"`
}

// regenerateRecoveryCodes replaces the caller's sheet and returns the new one.
//
// A POST rather than a GET because it is destructive: the old codes stop
// working the moment this succeeds, and a URL that could be prefetched or
// retried must not be able to invalidate somebody's printed sheet.
func (h *Handler) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	if IsAPIKey(r.Context()) {
		httpx.Error(w, r, httpx.Forbidden("API keys cannot change sign-in settings."))
		return
	}

	var req recoveryCodesRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	codes, err := h.service.GenerateRecoveryCodes(ctx, MustUserID(ctx), req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

type recoveryLoginRequest struct {
	ChallengeID string `json:"challenge_id"`
	Code        string `json:"recovery_code"`
}

// twoFactorRecovery completes a login with a recovery code instead of a mailed
// one, for the person who can no longer read their email.
func (h *Handler) twoFactorRecovery(w http.ResponseWriter, r *http.Request) {
	var req recoveryLoginRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	v := httpx.NewValidator()
	v.Required("challenge_id", req.ChallengeID)
	v.Required("recovery_code", req.Code)
	if err := v.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}

	// Throttled on the same counters as the code path: a recovery code is
	// lower entropy than a session token and higher value than a mailed code,
	// so it is the last place to leave unmetered.
	ip := clientIP(r)
	if err := h.attempts.check("", ip); err != nil {
		httpx.Error(w, r, err)
		return
	}

	session, err := h.service.RedeemRecoveryCode(r.Context(), req.ChallengeID, req.Code)
	if err != nil {
		if isClientError(err) {
			h.attempts.failed("", ip)
		}
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, session)
}

// listDevices returns the browsers this account has chosen to remember.
func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The current device token arrives in a header rather than the body: this
	// is a GET, and the only thing it is used for is marking one row "this one".
	devices, err := h.service.ListTrustedDevices(ctx, MustUserID(ctx), r.Header.Get(deviceTokenHeader))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": devices})
}

// forgetDevice drops one remembered browser, which will be asked for a code the
// next time it signs in.
func (h *Handler) forgetDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.ForgetDevice(ctx, MustUserID(ctx), chi.URLParam(r, "deviceID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

// verifyEmail redeems the token from a confirmation link.
//
// Unauthenticated: see Service.VerifyEmail. It returns the user so a client that
// happens to be signed in as that person can refresh in place.
func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	user, err := h.service.VerifyEmail(r.Context(), req.Token)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// resendVerification mails a fresh confirmation link to the caller.
func (h *Handler) resendVerification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.service.SendVerificationEmail(ctx, MustUserID(ctx)); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// deviceLabel describes the browser for the owner's own device list.
//
// The User-Agent, truncated, and nothing else — no fingerprint, no IP. It is a
// label to recognise a row by, not an identifier, and it is never used to
// decide anything.
func deviceLabel(r *http.Request) string {
	agent := strings.TrimSpace(r.UserAgent())
	if agent == "" {
		return "Unknown browser"
	}
	return agent
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

// preferencesRequest carries whichever preferences the client is changing.
//
// Pointers, so an omitted field means "leave it alone" rather than "clear it".
// The screen saves each control the moment it is touched, and a request that
// carried the whole set would let a stale copy of one preference overwrite a
// change to another.
type preferencesRequest struct {
	DistanceUnit *string `json:"distance_unit"`
	// Gender is a tri-state: absent leaves it, "" clears it back to unset, and
	// a value sets it. Unset is meaningful rather than missing - it selects the
	// men's ratings, which is what every round used before the column existed.
	Gender *string `json:"gender"`
}

// setPreferences updates the caller's display preferences and returns the whole
// user, so the client refreshes its copy in one round trip rather than
// patching a field locally and hoping it matches.
func (h *Handler) setPreferences(w http.ResponseWriter, r *http.Request) {
	var req preferencesRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ctx := r.Context()
	userID := MustUserID(ctx)

	if req.DistanceUnit != nil {
		if _, err := h.service.SetDistanceUnit(ctx, userID, strings.ToLower(strings.TrimSpace(*req.DistanceUnit))); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	if req.Gender != nil {
		if _, err := h.service.SetGender(ctx, userID, normalizeGender(*req.Gender)); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}

	// Reloaded once at the end rather than returning whichever setter ran last,
	// so the response is the whole user however many preferences changed.
	user, err := h.service.CurrentUser(ctx, userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// normalizeGender turns the wire value into what the column stores: a blank
// string is how the client says "unset", and unset is NULL.
func normalizeGender(raw string) *string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return nil
	}
	return &trimmed
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

// isClientError reports whether err is the caller's fault, and so whether it
// should count against their allowance.
func isClientError(err error) bool {
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	// A 429 is excluded so that being refused does not itself extend the
	// refusal, which would turn a fixed window into an unbounded one for any
	// client that keeps retrying.
	return apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != http.StatusTooManyRequests
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
