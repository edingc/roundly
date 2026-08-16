// Package geocode turns a postal address into coordinates.
//
// The implementation is Nominatim, the OpenStreetMap geocoder, chosen over the
// commercial options for one reason that outranks the pricing: most free
// geocoding tiers forbid storing the results, and storing them in a column is
// exactly what this app does. OSM's data is ODbL, which permits it, and asks
// only for attribution.
//
// Using it obliges the operator to follow the OSM usage policy, which is why
// this package rate-limits itself, caches, identifies the instance in its
// User-Agent, and is off unless NOMINATIM_URL is set.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Result is a resolved point. Nothing else from the response is kept: this
// exists to fill two columns.
type Result struct {
	Latitude  float64
	Longitude float64
}

// Geocoder is what the course service depends on. A nil Geocoder disables the
// feature, which is the state of any instance that has not configured a URL.
type Geocoder interface {
	// Lookup returns nil, nil when the address is real but not found. That is
	// an ordinary outcome, not an error: plenty of courses sit on roads no map
	// has named.
	Lookup(ctx context.Context, address string) (*Result, error)
}

const (
	// minInterval is the OSM usage policy's absolute limit: one request a
	// second, in aggregate, across the whole instance. Geocoding happens once
	// when a course is saved, so this is never the bottleneck in practice — it
	// is a guarantee that a burst cannot make this app a bad citizen.
	minInterval = time.Second

	// maxCacheEntries bounds a cache whose keys come from user input. The
	// policy asks that results be cached; it does not ask for an LRU, and at
	// this size the whole map can simply be dropped when it fills.
	maxCacheEntries = 512

	// requestTimeout is deliberately shorter than the server's own 30s request
	// timeout. A save that waits on a slow geocoder is worse than a save that
	// completes without coordinates.
	requestTimeout = 5 * time.Second
)

// Nominatim is a client for a Nominatim instance — either OSM's public one or
// a self-hosted copy.
type Nominatim struct {
	baseURL   string
	userAgent string
	client    *http.Client

	// minInterval is a field rather than the constant so tests do not have to
	// spend a real second proving the rate limit works.
	minInterval time.Duration

	// Two locks, not one: a cached lookup should not queue behind a request
	// that is sleeping out the rate limit.
	callMu   sync.Mutex
	lastCall time.Time

	cacheMu sync.Mutex
	cache   map[string]*Result
}

// NewNominatim builds a client. userAgent must identify this instance — the OSM
// usage policy requires it, and an unidentified client is the one they block.
func NewNominatim(baseURL, userAgent string) *Nominatim {
	return &Nominatim{
		baseURL:     strings.TrimRight(baseURL, "/"),
		userAgent:   userAgent,
		client:      &http.Client{Timeout: requestTimeout},
		minInterval: minInterval,
		cache:       make(map[string]*Result),
	}
}

func (n *Nominatim) Lookup(ctx context.Context, address string) (*Result, error) {
	if address == "" {
		return nil, nil
	}

	if cached, ok := n.cached(address); ok {
		return cached, nil
	}

	if err := n.waitTurn(ctx); err != nil {
		return nil, err
	}

	result, err := n.request(ctx, address)
	if err != nil {
		return nil, err
	}

	// A miss is cached too. Re-asking for an address OSM does not know, every
	// time the course is saved, is the repetition the policy asks us to avoid.
	n.remember(address, result)
	return result, nil
}

func (n *Nominatim) cached(address string) (*Result, bool) {
	n.cacheMu.Lock()
	defer n.cacheMu.Unlock()
	result, ok := n.cache[address]
	return result, ok
}

func (n *Nominatim) remember(address string, result *Result) {
	n.cacheMu.Lock()
	defer n.cacheMu.Unlock()
	if len(n.cache) >= maxCacheEntries {
		n.cache = make(map[string]*Result)
	}
	n.cache[address] = result
}

// waitTurn blocks until at least minInterval has passed since the last request,
// or until the caller gives up. Holding callMu across the wait is the point:
// it is what serializes concurrent saves into one request a second.
func (n *Nominatim) waitTurn(ctx context.Context) error {
	n.callMu.Lock()
	defer n.callMu.Unlock()

	wait := n.minInterval - time.Since(n.lastCall)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	n.lastCall = time.Now()
	return nil
}

func (n *Nominatim) request(ctx context.Context, address string) (*Result, error) {
	query := url.Values{}
	query.Set("q", address)
	query.Set("format", "jsonv2")
	query.Set("limit", "1")
	// The address is already structured on this side; only the point is wanted
	// back, and asking for less is politer.
	query.Set("addressdetails", "0")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.baseURL+"/search?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build geocode request: %w", err)
	}
	req.Header.Set("User-Agent", n.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocode request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocode request: unexpected status %d", resp.StatusCode)
	}

	// Nominatim returns latitude and longitude as strings, not numbers.
	var payload []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode geocode response: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}

	lat, err := strconv.ParseFloat(payload[0].Lat, 64)
	if err != nil {
		return nil, fmt.Errorf("parse latitude %q: %w", payload[0].Lat, err)
	}
	lon, err := strconv.ParseFloat(payload[0].Lon, 64)
	if err != nil {
		return nil, fmt.Errorf("parse longitude %q: %w", payload[0].Lon, err)
	}
	// Defensive: a point outside these bounds would fail the same validation
	// the course endpoints apply to a hand-typed one, so reject it here rather
	// than store something the API would have refused.
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return nil, fmt.Errorf("geocode returned an out-of-range point: %f, %f", lat, lon)
	}

	return &Result{Latitude: lat, Longitude: lon}, nil
}
