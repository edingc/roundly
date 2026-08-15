# Golf Scorekeeping & Stat Tracking App — Master Build Spec

The standing reference for every session. Paste the relevant phase section (or
the whole thing) to pick up work.

---

## 1. Locked-In Decisions

| Area | Decision |
|---|---|
| Backend | Go (modular monolith, package-per-domain, split into services later) |
| Router | `chi` |
| DB access | `sqlc` (portable SQL, generates typesafe Go — supports SQLite and Postgres) |
| Database | **SQLite** to start (single file, self-hosted). Design schema portable to Postgres — no `JSONB`, no `gen_random_uuid()`, no native arrays. Migrate to Postgres when multi-tenant/SaaS phase begins. |
| IDs | App-generated UUIDv7 strings (via Go `github.com/google/uuid`), stored as `TEXT`. Portable, sortable, no DB-specific ID generation. |
| Migrations | `goose` (works with both SQLite and Postgres) |
| Frontend | React + TypeScript + Vite + Tailwind CSS (PWA-enabled) |
| Auth | **Google OAuth (OIDC) as primary login + email/password as fallback.** Own session management on top: JWT access token + refresh token issued after either login path succeeds. Password path uses argon2id hashing. Each self-hosted instance registers its own Google OAuth client (client ID/secret via env vars) — no dependency on a central vendor auth server. |
| Styling | Tailwind `dark:` variants for light/dark mode from day one |
| Hosting model | Ship as single Go binary (frontend built + embedded via `go:embed`) + SQLite file. Docker Compose for easy self-host. |
| API style | REST/JSON now; can add gRPC between internal services later if split out |
| Offline support | PWA service worker for score entry in low-signal areas, sync on reconnect (build this in Score Tracker phase, not Phase 1) |

### Portability rules (so SQLite → Postgres migration is painless later)

- No JSONB — use `TEXT` columns storing JSON strings, parse in Go.
- No DB-native UUID generation — generate in application code.
- No Postgres-only functions in queries.
- Timestamps stored as ISO8601 `TEXT` (SQLite) — will map to `TIMESTAMPTZ` in
  Postgres later.
- Keep all schema/query files in `sqlc`-compatible syntax; avoid raw
  driver-specific SQL where possible.

---

## 2. Build Order (Phases)

1. **Phase 1 — Auth + Course Directory** ✅ *complete*
2. **Phase 2 — Equipment Management (Golf Bag)** ✅ *complete*
3. **Phase 3 — Score Tracker Core** (start/play/complete a round) ⬅ *next*
4. Phase 4 — Stats Dashboard (aggregates: driving accuracy by club, scoring avg, putts/round, etc.)
5. Phase 5 — Light/Dark Mode + Responsive Polish Pass
6. Phase 6 — Self-Hosted Packaging (Docker Compose, single binary)
7. Phase 7 — GPS Shot Tracking (optional layer)
8. Phase 8 — Postgres Migration + Multi-Tenant/SaaS Split

Each phase should end with: working backend endpoints, working frontend screens,
and a short manual test checklist before moving on.

---

## 3. PHASE 1 — Auth + Course Directory ✅

Delivered. Implementation notes and the API table are in
[../README.md](../README.md); the manual pass is in
[phase-1-test-checklist.md](phase-1-test-checklist.md).

### Decisions made during implementation

These were not specified up front and are worth carrying forward:

- **`total_yardage` on `tees` is derived, not stored.** The column still exists
  for schema compatibility but is left null; the API sums `hole_tee_details`.
  This keeps it from drifting from the grid, which is where yardages are entered.
- **Course permissions.** The directory is shared — any signed-in user reads any
  course, only the creator modifies it. Responses carry `can_edit`. Phase 8
  replaces this with a real permission model.
- **New courses pre-create their holes.** `POST /courses` accepts
  `hole_count` (9 or 18, default 18) and generates the hole rows so the
  par/yardage grid is immediately usable, with stroke index defaulting to the
  hole number.
- **One extra auth endpoint: `POST /auth/google/exchange`.** The OAuth callback
  is a browser redirect and cannot receive a JSON body, and a refresh token in a
  redirect URL would leak into history, logs, and `Referer`. The callback instead
  redirects with a single-use two-minute code that the SPA trades for the session
  over XHR.
- **`POST /auth/link/google` is called over XHR, not navigated to.** It is
  authenticated, and a browser navigation cannot carry a Bearer header, so it
  returns `{authorization_url}` for the client to navigate to.
- **Google auto-linking requires a provider-verified email.** Otherwise anyone
  able to set an unverified address at the provider could claim an existing
  account. Unverified emails get a conflict telling the user to sign in with
  their password and link from settings.
- **Refresh tokens are single-use and rotated.** Replaying a consumed token
  revokes every session for that user, on the assumption it leaked.
- **Search uses `instr()`, not `LIKE`.** sqlc's SQLite parser rejects the
  `ESCAPE` clause that literal `LIKE` matching would need, so a course named
  "50% Off Golf" would otherwise be unfindable.
- **`POST /auth/password`** was added so an OAuth-only user can add a password
  (and a password user can change one). It falls out of the nullable
  `password_hash` and completes the "either path resolves to one account" story.

---

## 4. PHASE 2 — Equipment Management (Golf Bag) ✅

Delivered as `internal/club` plus the `/bag` screen. The API table is in
[../README.md](../README.md); the manual pass is in
[phase-2-test-checklist.md](phase-2-test-checklist.md).

### Decisions made during implementation

- **One bag per user, not named sets.** The expected shape floated "bag or set
  grouping". In practice a player has one bag and swaps clubs in and out of it,
  so clubs are a flat per-user list with a status, and there is no membership
  table. Named sets can be added later as a grouping over the same rows without
  changing what a club *is*.
- **Three statuses, two columns, one CHECK.** `active` (in the bag), `benched`
  (owned, out of the bag), `retired` (sold, replaced, broken). Stored as an
  `active` flag plus a nullable `retired_at`, with
  `CHECK (retired_at IS NULL OR active = 0)` so a retired-and-active club cannot
  exist. The API exposes the derived status rather than the two columns.
- **Retirement is a soft delete; IDs never move.** This is the constraint that
  drove the whole design: Phase 3 rounds and Phase 4 shots will reference club
  IDs, so a club that leaves the bag must keep its row. `DELETE /clubs/{id}`
  still exists for correcting a mistyped club, and the UI steers to Retire.
  **When rounds start referencing clubs, that endpoint needs to become
  conditional** — refuse, or cascade to nothing, once a club has shots.
- **Status changes have their own endpoint.** `PUT /clubs/{id}` edits
  description only. Otherwise a stale edit form could pull a retired club back
  into the bag as a side effect of saving a loft.
- **The 14-club limit is reported, not enforced.** `GET /clubs` returns
  `active_count`, `club_limit`, and `over_limit`; the UI shows a counter and an
  amber warning. A bag mid-edit legitimately passes through 15 clubs, and
  practice bags are larger, so a 409 would fight the user for no safety gain.
- **A bag is private, unlike the course directory.** Every query is scoped by
  `user_id`. Another user's club ID returns 404, not 403 — confirming that an ID
  exists would leak more than the refusal is worth.
- **No expected-distance field.** The original sketch had one for Phase 4 to
  compare against. Dropped: Phase 4 derives club distances from recorded shots,
  and a hand-entered number would be a second, staler source of truth.
- **Bag order is derived, not arranged.** The list sorts by club type, then loft
  ascending, which produces the real layout (driver → woods → hybrids → irons →
  wedges → putter) with no reordering UI. `display_order` is stored and settable,
  and is what a future drag-to-reorder would write to.

---

## 5. How to Use This Doc

When starting a session:

> "Continuing the golf app — starting Phase 3 (Score Tracker Core) per
> docs/BUILD_SPEC.md."

The repo now carries its own context: `README.md` covers architecture, the API,
and configuration, so a fresh session only needs the phase section.
