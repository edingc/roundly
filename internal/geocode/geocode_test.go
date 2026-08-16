package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient points a client at a stub server and drops the rate limit to
// something a test can afford to wait out.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Nominatim {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewNominatim(server.URL, "Roundly/test (+https://example.test)")
	client.minInterval = time.Millisecond
	return client
}

// Nominatim returns lat and lon as strings, not JSON numbers. Decoding them as
// float64 would silently fail on every response.
func TestLookupParsesStringCoordinates(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "1831 Johnson St., Marne, MI 49435" {
			t.Errorf("q = %q, want the postal line", got)
		}
		if got := r.URL.Query().Get("format"); got != "jsonv2" {
			t.Errorf("format = %q, want jsonv2", got)
		}
		// The usage policy requires an identifiable client; an unidentified one
		// is what gets blocked.
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Roundly/") {
			t.Errorf("User-Agent = %q, want it to identify this app", ua)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"lat":"43.0331234","lon":"-85.8225678"}]`))
	})

	result, err := client.Lookup(context.Background(), "1831 Johnson St., Marne, MI 49435")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if result == nil {
		t.Fatal("result = nil, want a point")
	}
	if result.Latitude != 43.0331234 || result.Longitude != -85.8225678 {
		t.Errorf("point = %f, %f; want 43.0331234, -85.8225678", result.Latitude, result.Longitude)
	}
}

// An address OSM has never heard of is an ordinary outcome, not an error. The
// caller stores no coordinates and saves the course anyway.
func TestLookupTreatsNoMatchAsNoResult(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	result, err := client.Lookup(context.Background(), "Nowhere At All, XX")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
}

// The usage policy asks that results be cached rather than re-requested. A miss
// is cached too: re-asking about an unknown address on every save is exactly the
// repetition it warns against.
func TestLookupCachesHitsAndMisses(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Query().Get("q"), "Marne") {
			_, _ = w.Write([]byte(`[{"lat":"43.03","lon":"-85.82"}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})

	ctx := context.Background()
	for range 3 {
		if _, err := client.Lookup(ctx, "1831 Johnson St., Marne, MI"); err != nil {
			t.Fatalf("lookup hit: %v", err)
		}
		if _, err := client.Lookup(ctx, "Nowhere At All, XX"); err != nil {
			t.Fatalf("lookup miss: %v", err)
		}
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want 2 (one hit, one miss, the rest cached)", got)
	}
}

// One request a second, in aggregate, is the policy's hard limit. This proves
// the limiter actually spaces requests rather than merely intending to.
func TestLookupRateLimitsRequests(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	client.minInterval = 40 * time.Millisecond

	ctx := context.Background()
	start := time.Now()
	for i := range 3 {
		// Distinct addresses, so the cache cannot absorb them.
		if _, err := client.Lookup(ctx, string(rune('a'+i))+" Street, Somewhere"); err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
	}

	// Three requests means two waits; the first goes straight out.
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("three lookups took %v, want at least two intervals of spacing", elapsed)
	}
}

func TestLookupReportsUpstreamFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	if _, err := client.Lookup(context.Background(), "1 Fairway Dr, Marne, MI"); err == nil {
		t.Error("err = nil, want the 429 reported so the caller can log it")
	}
}

// A cancelled request must not sit out the rate-limit wait.
func TestLookupHonoursContextDuringRateLimitWait(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	client.minInterval = 10 * time.Second

	ctx := context.Background()
	if _, err := client.Lookup(ctx, "first, somewhere"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := client.Lookup(cancelled, "second, somewhere"); err == nil {
		t.Error("err = nil, want the cancelled context to end the wait")
	}
}
