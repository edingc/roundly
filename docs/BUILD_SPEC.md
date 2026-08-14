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
2. **Phase 2 — Equipment Management** ⬅ *next*
3. Phase 3 — Score Tracker Core (start/play/complete a round)
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

## 4. PHASE 2 SPEC — Equipment Management

Not yet specified. Expected shape, to be confirmed before building:

- Clubs a player carries: type (driver / wood / hybrid / iron / wedge / putter),
  loft, shaft, and a display label.
- Bag or set grouping, since players swap clubs between rounds.
- Per-club typical distance, which Phase 4 will compare against actual shots.
- Retiring a club without deleting it, so historical round data stays intact.

The Phase 4 stats goal ("driving accuracy by club") is the constraint that
matters here: rounds will reference a club, so clubs need stable IDs and
soft retirement rather than hard deletes.

---

## 5. How to Use This Doc

When starting a session:

> "Continuing the golf app — starting Phase 2 (Equipment Management) per
> docs/BUILD_SPEC.md."

The repo now carries its own context: `README.md` covers architecture, the API,
and configuration, so a fresh session only needs the phase section.
