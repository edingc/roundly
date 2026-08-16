package apikey

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/edingc/roundly/internal/auth"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/ratelimit"
)

// Guard authenticates API keys and enforces that they can only read.
//
// This is installed as a global middleware, before chi routes anything, and
// that placement is the design rather than an accident. Per-route middleware
// could not give the guarantee: a route registered in the wrong group, in any
// package, at any point in the future, would silently escape it. Running before
// the router means the default is deny and a new endpoint has to be added to
// the allow-list on purpose.
//
// A request that does not present a key passes straight through untouched, so
// the JWT path is unaffected.
func Guard(service *Service, requests, failures *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Not an API request. The SPA and its assets are none of this
			//    middleware's business.
			if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			// 2. No key presented: leave the request exactly as it was.
			presented := BearerToken(r.Header.Get("Authorization"), r.Header.Get("X-API-Key"))
			if !strings.HasPrefix(presented, TokenPrefix) {
				next.ServeHTTP(w, r)
				return
			}

			// 3. Normalize the path once. Everything below decides on `p`.
			p, ok := CleanPath(r.URL.Path)
			if !ok {
				reject(w, r, "path", presented, httpx.Forbidden("That path is not available to an API key."))
				return
			}

			// 4. Hard blocks. Before the method check, because the account
			//    export is a GET and would otherwise sail through it.
			if IsBlocked(p) {
				reject(w, r, "blocked", presented, httpx.Forbidden("API keys cannot access account or authentication endpoints."))
				return
			}

			// 5. Read-only. 403 rather than 405: the route does accept this
			//    method from a signed-in session, so 405 would be false, and it
			//    would let a key-holder map the route table.
			if !IsReadMethod(r.Method) {
				reject(w, r, "method", presented, &httpx.APIError{
					Status:  http.StatusForbidden,
					Code:    "api_key_read_only",
					Message: "This API key is read-only. Only GET and HEAD requests are allowed.",
				})
				return
			}

			// 6. Allow-list.
			if !Allowed(p) {
				reject(w, r, "path", presented, httpx.Forbidden("That endpoint is not available to an API key."))
				return
			}

			// 7. Authenticate. Rate-limit failures by IP first: guessing a
			//    256-bit token is hopeless, but each attempt still costs an
			//    indexed query on the only connection the server has.
			//
			//    Checked without recording, then recorded only if the key turns
			//    out to be bad. Using Allow here instead counted every
			//    key-authenticated request against the failure budget, which
			//    silently capped a working key at this limiter's 20/min however
			//    high API_KEY_RATE_LIMIT was set.
			ip := clientIP(r)
			if blocked, resetAt := failures.Exceeded("fail:"+ip, time.Now()); blocked {
				writeRateLimited(w, r, failures, 0, resetAt)
				return
			}
			principal, err := service.Authenticate(r.Context(), presented)
			if err != nil {
				failures.Allow("fail:"+ip, time.Now())
				reject(w, r, "unauthenticated", presented, err)
				return
			}

			// 8. Per-key rate limit.
			allowed, remaining, resetAt := requests.Allow(principal.KeyID, time.Now())
			setRateHeaders(w, requests, remaining, resetAt)
			if !allowed {
				writeRateLimited(w, r, requests, remaining, resetAt)
				slog.Warn("api key rejected",
					"reason", "rate_limited",
					"key_id", principal.KeyID,
					"key_prefix", principal.KeyPrefix,
					"method", r.Method, "path", p)
				return
			}

			ctx := auth.ContextWithPrincipal(r.Context(), auth.Principal{
				Kind:      auth.PrincipalAPIKey,
				UserID:    principal.UserID,
				KeyID:     principal.KeyID,
				KeyPrefix: principal.KeyPrefix,
				Scope:     principal.Scope,
			})

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(ctx))

			// The secret never appears here — only the row ID and the public
			// prefix, which the UI already displays. RawQuery is omitted too,
			// in case someone tried passing a key that way despite it being
			// unsupported.
			slog.Info("api key request",
				"request_id", middleware.GetReqID(r.Context()),
				"key_id", principal.KeyID,
				"key_prefix", principal.KeyPrefix,
				"user_id", principal.UserID,
				"method", r.Method,
				"path", p,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
			)
		})
	}
}

func reject(w http.ResponseWriter, r *http.Request, reason, presented string, err error) {
	slog.Warn("api key rejected",
		"reason", reason,
		"method", r.Method,
		"path", r.URL.Path,
		"ip", clientIP(r),
		// Only the public prefix, and only when the value is shaped like one of
		// ours, so a hostile credential is never echoed into the log.
		"key_prefix", SafePrefix(presented),
	)
	httpx.Error(w, r, err)
}

func setRateHeaders(w http.ResponseWriter, l *ratelimit.Limiter, remaining int, resetAt time.Time) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(l.Limit()))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
}

func writeRateLimited(w http.ResponseWriter, r *http.Request, l *ratelimit.Limiter, remaining int, resetAt time.Time) {
	retry := int(time.Until(resetAt).Seconds())
	if retry < 1 {
		retry = 1
	}
	setRateHeaders(w, l, remaining, resetAt)
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	httpx.Error(w, r, httpx.TooManyRequests(
		"Too many requests. Try again in "+strconv.Itoa(retry)+" seconds."))
}

func clientIP(r *http.Request) string {
	// middleware.RealIP has already normalized RemoteAddr where a trusted proxy
	// header was present.
	host, _, found := strings.Cut(r.RemoteAddr, ":")
	if !found {
		return r.RemoteAddr
	}
	return host
}
