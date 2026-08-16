// Package ratelimit is a fixed-window request counter, shared by everything in
// this app that has to say "not that many, not that fast".
//
// It began inside internal/apikey and moved out when sign-in needed the same
// thing: internal/apikey imports internal/auth, so auth could not import it
// back. One implementation with two callers, rather than two implementations
// that drift.
package ratelimit

import (
	"sync"
	"time"
)

// maxTrackedKeys bounds the limiter's memory. Reaching it means something
// pathological is happening.
const maxTrackedKeys = 10_000

// Limiter is a fixed-window request counter.
//
// A token bucket would smooth bursts more gracefully, but a fixed window can
// state exactly when it resets, which is what Retry-After and X-RateLimit-Reset
// have to carry. Being able to tell a client the truth about when to come back
// is worth more here than burst smoothing, and this is small enough to verify
// by reading it.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*bucket
}

type bucket struct {
	start time.Time
	count int
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]*bucket),
	}
}

// Allow records a request against key and reports whether it may proceed,
// along with what the rate-limit headers should say.
//
// now is a parameter rather than a call to time.Now so the tests can advance
// the clock instead of sleeping.
func (l *Limiter) Allow(key string, now time.Time) (ok bool, remaining int, resetAt time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, found := l.buckets[key]
	if !found || now.Sub(b.start) >= l.window {
		// A limiter that starts refusing valid traffic because its own map
		// filled up is worse than the abuse it exists to prevent, so this fails
		// open rather than closed.
		if !found && len(l.buckets) >= maxTrackedKeys {
			return true, l.limit, now.Add(l.window)
		}
		b = &bucket{start: now}
		l.buckets[key] = b
	}

	b.count++
	resetAt = b.start.Add(l.window)
	if b.count > l.limit {
		return false, 0, resetAt
	}
	return true, l.limit - b.count, resetAt
}

// Exceeded reports whether key is already over its limit, without recording an
// attempt against it.
//
// The distinction from Allow matters for anything that counts only failures. A
// sign-in has to ask "am I still allowed to try?" before it knows whether this
// attempt is going to be one of the ones worth counting — if asking consumed
// budget, a user typing their password correctly would spend the same
// allowance as an attacker getting it wrong.
func (l *Limiter) Exceeded(key string, now time.Time) (blocked bool, resetAt time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, found := l.buckets[key]
	if !found || now.Sub(b.start) >= l.window {
		return false, now.Add(l.window)
	}
	return b.count >= l.limit, b.start.Add(l.window)
}

// Limit is the configured ceiling, for the X-RateLimit-Limit header.
func (l *Limiter) Limit() int { return l.limit }

// Sweep drops buckets whose window has fully elapsed.
func (l *Limiter) Sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.start) >= l.window {
			delete(l.buckets, k)
		}
	}
}

// StartJanitor sweeps expired buckets until ctx is done.
func (l *Limiter) StartJanitor(stop <-chan struct{}) {
	ticker := time.NewTicker(2 * l.window)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				l.Sweep(now)
			}
		}
	}()
}
