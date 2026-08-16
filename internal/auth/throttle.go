package auth

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/ratelimit"
)

// Throttling the credential endpoints.
//
// Two-factor raises the cost of a *successful* guess. It does nothing about the
// guessing, and until now nothing did: /auth/login would take attempts as fast
// as they arrived. An eight-character password is minutes at that rate.
//
// Counted along two axes, because they stop different attacks:
//
//   - By account. One address can absorb only so many wrong passwords, however
//     many machines the attempts come from. This is what defeats a distributed
//     attack on one person.
//   - By IP. One source can only be wrong so many times across all accounts.
//     This is what defeats spraying one common password at every address.
//
// Neither alone is enough: an account limit lets a botnet try one password
// against a million accounts, and an IP limit lets a botnet grind one account.
//
// Only failures count. A correct sign-in clears nothing and costs nothing, so
// somebody who knows their password is never locked out by their own traffic —
// which is the failure mode that makes people ask for the limit to be removed.
type throttle struct {
	byAccount *ratelimit.Limiter
	byIP      *ratelimit.Limiter
}

func newThrottle(limit int, window time.Duration) *throttle {
	return &throttle{
		// The IP allowance is deliberately the looser of the two. A household,
		// an office, or a mobile carrier's NAT can put many legitimate people
		// behind one address, and a shared address that locks out on one
		// person's typo is a support ticket rather than a defence.
		byAccount: ratelimit.New(limit, window),
		byIP:      ratelimit.New(limit*3, window),
	}
}

// check reports whether another attempt is allowed. It does not record one:
// recording is what failed does, so that success is free.
func (t *throttle) check(account, ip string) error {
	if account != "" {
		if blocked, resetAt := t.byAccount.Exceeded(account, time.Now()); blocked {
			return errTooManyAttempts(resetAt)
		}
	}
	if ip != "" {
		if blocked, resetAt := t.byIP.Exceeded(ip, time.Now()); blocked {
			return errTooManyAttempts(resetAt)
		}
	}
	return nil
}

// failed records one wrong answer against both axes.
func (t *throttle) failed(account, ip string) {
	now := time.Now()
	if account != "" {
		t.byAccount.Allow(account, now)
	}
	if ip != "" {
		t.byIP.Allow(ip, now)
	}
}

func (t *throttle) startJanitor(stop <-chan struct{}) {
	t.byAccount.StartJanitor(stop)
	t.byIP.StartJanitor(stop)
}

// signupThrottle caps account creation, and counts differently from the one
// above.
//
// The sign-in limiter counts only failures, because a successful sign-in is
// exactly what the endpoint is for. Signup is the reverse: a *successful*
// signup is the abuse. Filling an instance with junk accounts needs no failed
// attempts at all, so every attempt is counted, whether it created an account,
// collided with an existing address, or was rejected as invalid.
//
// One axis, not two. There is no account to key on — the address is chosen by
// whoever is signing up, so keying on it would hand every attacker a fresh
// bucket per attempt. The IP is the only thing they do not pick.
//
// The window is long and the count is small, because that is the actual shape
// of legitimate traffic: real people sign up once. The limit needs to be large
// enough for a family or an office to join on the same evening, and no larger.
type signupThrottle struct {
	byIP *ratelimit.Limiter
}

func newSignupThrottle(limit int, window time.Duration) *signupThrottle {
	return &signupThrottle{byIP: ratelimit.New(limit, window)}
}

// attempt records one signup try and reports whether it may proceed.
//
// Unlike the sign-in throttle this records as it checks, in one call: there is
// no later moment at which the attempt turns out not to have counted.
func (t *signupThrottle) attempt(ip string) error {
	if ip == "" {
		return nil
	}
	if ok, _, resetAt := t.byIP.Allow(ip, time.Now()); !ok {
		return errTooManySignups(resetAt)
	}
	return nil
}

func (t *signupThrottle) startJanitor(stop <-chan struct{}) { t.byIP.StartJanitor(stop) }

func errTooManySignups(resetAt time.Time) error {
	wait := time.Until(resetAt)
	if wait < time.Second {
		wait = time.Second
	}
	return &httpx.APIError{
		Status:  http.StatusTooManyRequests,
		Code:    "too_many_signups",
		Message: "Too many accounts have been created from this connection. Wait " + humanWait(wait) + " and try again.",
		Headers: retryAfter(wait),
	}
}

// errTooManyAttempts says when to come back rather than only that this failed.
//
// Deliberately the same answer whether or not the account exists. A refusal
// that only appears for real accounts is an account-enumeration oracle, and a
// rate limiter is a particularly good one because it needs no valid password
// to consult.
func errTooManyAttempts(resetAt time.Time) error {
	wait := time.Until(resetAt)
	if wait < time.Second {
		wait = time.Second
	}
	return &httpx.APIError{
		Status:  http.StatusTooManyRequests,
		Code:    "too_many_attempts",
		Message: "Too many sign-in attempts. Wait " + humanWait(wait) + " and try again.",
		// Retry-After, not Fields. Fields is per-field validation detail, and
		// the client treats anything that has it as a form validation failure —
		// so putting the delay there made the message invisible and attached an
		// error to a field named "retry_after_seconds" that no form has. The
		// wait is in the message, where a person reads it, and in the header,
		// where a client or proxy does.
		Headers: retryAfter(wait),
	}
}

// retryAfter renders the standard header, rounded up so it never tells a client
// to come back a fraction of a second early.
func retryAfter(wait time.Duration) map[string]string {
	return map[string]string{"Retry-After": strconv.Itoa(int(wait.Seconds()) + 1)}
}

func humanWait(d time.Duration) string {
	minutes := int(d.Round(time.Minute).Minutes())
	switch {
	case minutes <= 1:
		return "a minute"
	default:
		return strconv.Itoa(minutes) + " minutes"
	}
}

// clientIP is the address to count against.
//
// chi's RealIP middleware has already resolved X-Forwarded-For, so this reads
// what it left. Behind a proxy that does not set those headers every request
// shares one key and the IP axis becomes useless — the account axis still
// works, which is why there are two.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
