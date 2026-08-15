package apikey

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// ScopeRead is the only scope. The column exists so that adding a write scope
// later is a deliberate migration and code audit rather than a value change.
const ScopeRead = "read"

const (
	// A key's last-used time is worth minute-level accuracy: it answers "is
	// anything still using this?", not "when exactly". Coalescing to one write
	// per key per window is what keeps a read-only request from taking the
	// single write connection.
	lastUsedGranularity = 5 * time.Minute
	lastUsedFlushEvery  = 30 * time.Second
	lastUsedQueueDepth  = 256

	maxKeyNameLen = 60
	maxExpiryDays = 3650
)

// Key is the metadata for an API key. There is deliberately no token field:
// the secret is returned exactly once, by Create, in a separate struct. Adding
// one here would be the mistake this shape exists to prevent.
type Key struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	KeyPrefix  string  `json:"key_prefix"`
	Scope      string  `json:"scope"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	ExpiresAt  *string `json:"expires_at"`
}

// CreatedKey is the one and only response that carries the secret.
type CreatedKey struct {
	Key Key `json:"key"`
	// Token is shown once and is not recoverable afterwards; only its hash is
	// stored.
	Token string `json:"token"`
}

// Principal is the identity resolved from a key.
type Principal struct {
	UserID    string
	KeyID     string
	KeyPrefix string
	Scope     string
}

// Service manages API keys and authenticates them.
type Service struct {
	db     *database.DB
	maxPer int

	mu    sync.Mutex
	seen  map[string]time.Time
	queue chan string
}

func NewService(db *database.DB, maxPerUser int) *Service {
	return &Service{
		db:     db,
		maxPer: maxPerUser,
		seen:   make(map[string]time.Time),
		queue:  make(chan string, lastUsedQueueDepth),
	}
}

// Create issues a new key for a user.
func (s *Service) Create(ctx context.Context, userID, name string, expiresInDays int) (*CreatedKey, error) {
	v := httpx.NewValidator()
	name = v.SingleLine("name", name)
	if v.Required("name", name) {
		v.MaxLen("name", name, maxKeyNameLen)
	}
	if expiresInDays != 0 {
		v.IntBetween("expires_in_days", expiresInDays, 1, maxExpiryDays)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	count, err := s.db.Queries.CountActiveAPIKeys(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("count api keys: %w", err))
	}
	if int(count) >= s.maxPer {
		return nil, httpx.Conflict(fmt.Sprintf(
			"You already have %d API keys. Revoke one before creating another.", s.maxPer))
	}

	token, hash, prefix, err := NewToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}

	var expiresAt *string
	if expiresInDays > 0 {
		exp := timex.Format(time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour))
		expiresAt = &exp
	}

	row := sqlc.CreateAPIKeyParams{
		ID:        id.New(),
		UserID:    userID,
		Name:      name,
		KeyHash:   hash,
		KeyPrefix: prefix,
		Scope:     ScopeRead,
		CreatedAt: timex.Now(),
		ExpiresAt: expiresAt,
	}
	if err := s.db.Queries.CreateAPIKey(ctx, row); err != nil {
		return nil, httpx.Internal(fmt.Errorf("create api key: %w", err))
	}

	slog.Info("api key created", "user_id", userID, "key_id", row.ID, "key_prefix", prefix)

	return &CreatedKey{
		Key: Key{
			ID:        row.ID,
			Name:      name,
			KeyPrefix: prefix,
			Scope:     ScopeRead,
			CreatedAt: row.CreatedAt,
			ExpiresAt: expiresAt,
		},
		Token: token,
	}, nil
}

// List returns a user's live keys, metadata only.
func (s *Service) List(ctx context.Context, userID string) ([]Key, error) {
	rows, err := s.db.Queries.ListAPIKeysByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list api keys: %w", err))
	}
	keys := make([]Key, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, Key{
			ID:         r.ID,
			Name:       r.Name,
			KeyPrefix:  r.KeyPrefix,
			Scope:      r.Scope,
			CreatedAt:  r.CreatedAt,
			LastUsedAt: r.LastUsedAt,
			ExpiresAt:  r.ExpiresAt,
		})
	}
	return keys, nil
}

// Revoke retires a key. The user_id predicate in the query is the ownership
// check: another user's key id matches nothing rather than revoking theirs.
func (s *Service) Revoke(ctx context.Context, userID, keyID string) error {
	if _, err := s.db.Queries.GetAPIKeyForUser(ctx, sqlc.GetAPIKeyForUserParams{
		ID:     keyID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("That API key does not exist.")
		}
		return httpx.Internal(fmt.Errorf("load api key: %w", err))
	}

	now := timex.Now()
	if err := s.db.Queries.RevokeAPIKey(ctx, sqlc.RevokeAPIKeyParams{
		RevokedAt: &now,
		ID:        keyID,
		UserID:    userID,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("revoke api key: %w", err))
	}
	slog.Info("api key revoked", "user_id", userID, "key_id", keyID)
	return nil
}

// errRejected is the single response for every authentication failure.
//
// Unknown, revoked, and expired all return exactly this, so a prober cannot
// learn which of the three they hit — in particular, cannot use the difference
// to confirm that a key once existed.
func errRejected() error {
	return httpx.Unauthorized("That API key is not valid.")
}

// Authenticate resolves a presented token to its owner.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if !LooksLikeToken(token) {
		return Principal{}, errRejected()
	}

	row, err := s.db.Queries.GetAPIKeyByHash(ctx, HashToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Principal{}, errRejected()
		}
		return Principal{}, httpx.Internal(fmt.Errorf("load api key: %w", err))
	}
	if row.RevokedAt != nil {
		return Principal{}, errRejected()
	}
	if row.ExpiresAt != nil && timex.Expired(*row.ExpiresAt) {
		return Principal{}, errRejected()
	}
	if row.Scope != ScopeRead {
		// Defensive: the CHECK constraint makes this unreachable today, and it
		// must stay unreachable if a write scope is ever added without also
		// revisiting Guard.
		return Principal{}, errRejected()
	}

	s.noteUsed(row.ID)

	return Principal{
		UserID:    row.UserID,
		KeyID:     row.ID,
		KeyPrefix: row.KeyPrefix,
		Scope:     row.Scope,
	}, nil
}

// noteUsed queues a last-used update. It never touches the database, because it
// runs on the request path.
func (s *Service) noteUsed(keyID string) {
	s.mu.Lock()
	last, ok := s.seen[keyID]
	if ok && time.Since(last) < lastUsedGranularity {
		s.mu.Unlock()
		return
	}
	s.seen[keyID] = time.Now()
	s.mu.Unlock()

	select {
	case s.queue <- keyID:
	default:
		// A full queue means the flusher is behind. Dropping a last-used
		// update is strictly better than blocking a request over one.
	}
}

// StartFlusher drains queued last-used updates on a ticker until stop closes,
// coalescing to one UPDATE per distinct key per flush.
func (s *Service) StartFlusher(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(lastUsedFlushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				s.flush()
				return
			case <-ticker.C:
				s.flush()
			}
		}
	}()
}

func (s *Service) flush() {
	pending := make(map[string]bool)
	for {
		select {
		case keyID := <-s.queue:
			pending[keyID] = true
			continue
		default:
		}
		break
	}
	if len(pending) == 0 {
		return
	}

	now := timex.Now()
	ctx := context.Background()
	if err := s.db.InTx(func(q *sqlc.Queries) error {
		for keyID := range pending {
			if err := q.TouchAPIKeyLastUsed(ctx, sqlc.TouchAPIKeyLastUsedParams{
				LastUsedAt: &now,
				ID:         keyID,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		slog.Warn("flush api key last-used", "error", err, "keys", len(pending))
	}
}

// BearerToken extracts a presented API key from a request's headers, or "" if
// there is not one. A query parameter is deliberately never accepted: it would
// put the secret into proxy logs, browser history, and Referer headers, and
// supporting it once means supporting it forever.
func BearerToken(authorization, apiKeyHeader string) string {
	if v := strings.TrimSpace(apiKeyHeader); v != "" {
		return v
	}
	header := strings.TrimSpace(authorization)
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
