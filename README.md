# Roundly

Self-hosted golf scorekeeping and stat tracking. A single Go binary with the
React frontend embedded, plus one SQLite file.

**Phase 1 (auth + course directory) is complete.** See
[docs/BUILD_SPEC.md](docs/BUILD_SPEC.md) for the full roadmap.

## Quick start

```bash
make setup          # install frontend deps and Go tools
make build          # build the frontend and embed it into bin/roundly
JWT_SECRET=$(openssl rand -hex 32) ./bin/roundly
```

Then open http://localhost:8080 and create an account.

`JWT_SECRET` is optional in development (a random one is generated per start,
which means restarting signs you out) and required when `ENV=production`.

### Development with hot reload

Two terminals, because the API and the Vite dev server run separately:

```bash
make run    # Go API on :8080
make dev    # Vite on :5173, proxying /api to :8080
```

Work against http://localhost:5173. The proxy keeps the browser on one origin so
cookies and OAuth redirects behave the same as in production.

### Docker

```bash
cp .env.example .env      # set JWT_SECRET at minimum
docker compose up -d
```

## Configuration

Every setting is an environment variable; see [.env.example](.env.example) for
the annotated list. The ones that matter most:

| Variable | Default | Notes |
|---|---|---|
| `JWT_SECRET` | random in dev | Required in production, ≥32 chars |
| `DATABASE_URL` | `roundly.db` | SQLite path, created and migrated on start |
| `PUBLIC_URL` | `http://localhost:5173` | Where the browser reaches this instance |
| `GOOGLE_CLIENT_ID` / `_SECRET` / `_REDIRECT_URL` | unset | Optional; unset means password-only |

### Enabling Google sign-in

Each instance registers its own OAuth client, so there is no dependency on a
shared auth server:

1. At [Google Cloud Console credentials](https://console.cloud.google.com/apis/credentials),
   create an **OAuth 2.0 Client ID** of type *Web application*.
2. Add an authorized redirect URI matching your `GOOGLE_REDIRECT_URL` exactly —
   `https://your-domain.example/api/auth/google/callback` in production, or
   `http://localhost:5173/api/auth/google/callback` for local development
   through the Vite proxy.
3. Put the client ID, secret, and redirect URL in the environment.

The frontend hides the Google button when these are unset, so the app runs fine
without them.

### Enabling the course map

Course detail pages can embed a Google Map of the course's address:

1. At the same [credentials console](https://console.cloud.google.com/apis/credentials),
   create a key and enable the **Maps Embed API** for it. Restrict it to your
   site's domain(s), since it ships inside the built frontend.
2. Copy [web/.env.example](web/.env.example) to `web/.env` and set
   `VITE_GOOGLE_MAPS_API_KEY`.

This is a frontend build-time setting, unrelated to the Go server's environment
variables above. Leave it unset to hide the map — the address and phone number
on the detail page stay clickable either way.

## Architecture

```
cmd/server/          entrypoint: config, database, HTTP server, signal handling
internal/
  auth/              both login paths, sessions, argon2id, Google OIDC, middleware
  course/            course/tee/hole CRUD and the per-tee par & yardage mapping
  config/            environment configuration
  database/          SQLite connection, migrations, sqlc-generated queries
  httpx/             shared JSON request/response and validation conventions
  server/            router wiring and SPA file serving
  id/, timex/        UUIDv7 generation and the stored timestamp format
db/
  migrations/        goose migrations (embedded)
  queries/           sqlc sources
web/                 React + TypeScript + Vite + Tailwind PWA (embedded via go:embed)
```

The frontend build lands in `web/dist` and is embedded with `go:embed`, so
`make build` produces one deployable file. A placeholder `web/dist/index.html`
is committed so `go build` works on a clean checkout.

### Data model notes

Par and yardage live in `hole_tee_details`, keyed by hole *and* tee. That is what
lets one hole play as a par 3 from the forward tee and a par 4 from the back,
which a par column on `holes` could not express. A tee's `total_yardage` is
derived by summing that table rather than stored, so it cannot drift from the
grid.

### Portability

The schema and queries stay SQLite/Postgres-neutral for the Phase 8 migration:
IDs are UUIDv7 strings generated in Go, timestamps are ISO8601 text written
through `internal/timex`, there is no JSONB or native array use, and the one
driver-specific behavior (detecting a UNIQUE violation from an error string) is
isolated to a single helper in `internal/auth`.

## API

All routes are under `/api`. Everything except the auth endpoints below requires
`Authorization: Bearer <access_token>`.

**Auth**

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/auth/config` | Whether Google sign-in is configured |
| `POST` | `/auth/signup` | `{email, password, display_name}` |
| `POST` | `/auth/login` | `{email, password}` |
| `GET` | `/auth/google/start` | Redirect to Google's consent screen |
| `GET` | `/auth/google/callback` | Google's redirect target |
| `POST` | `/auth/google/exchange` | Trade the one-time handoff code for a session |
| `POST` | `/auth/refresh` | `{refresh_token}`, rotates the token |
| `POST` | `/auth/logout` | `{refresh_token}`, revokes it |
| `GET` | `/auth/me` | Current user *(auth)* |
| `POST` | `/auth/link/google` | Begin linking Google to this account *(auth)* |
| `POST` | `/auth/password` | Set or change the password *(auth)* |

**Courses** *(all require auth)*

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/courses?q=&limit=&offset=` | Paginated list with search |
| `POST` | `/courses` | Create, optionally with tees and a hole count |
| `GET` | `/courses/{id}` | Full detail: tees, holes, per-tee par and yardage |
| `PUT` | `/courses/{id}` | Update name, address, and phone number |
| `DELETE` | `/courses/{id}` | Delete, cascading to tees, holes, and details |
| `POST` | `/courses/{id}/tees` | Add a tee |
| `PUT` `DELETE` | `/tees/{id}` | Update or delete a tee |
| `POST` | `/courses/{id}/holes` | Add a hole |
| `PUT` `DELETE` | `/holes/{id}` | Update stroke index, or delete |
| `PUT` | `/holes/{id}/tee-details/{tee_id}` | Upsert par and yardage for one hole+tee |
| `DELETE` | `/holes/{id}/tee-details/{tee_id}` | Clear that pairing |

Errors share one envelope. `fields` appears only on validation failures:

```json
{ "error": "validation_failed", "message": "…", "fields": { "par": "Must be between 3 and 6." } }
```

### Two notes on the design

**The OAuth handoff code.** A browser redirect cannot receive a JSON body, and
putting a refresh token in a redirect URL leaks it into history, logs, and the
`Referer` header. So the callback stores the session against a single-use code
that expires in two minutes and redirects to `/auth/callback?code=…`, which the
app immediately trades for the real tokens over XHR.

**Course permissions.** The directory is shared: any signed-in user can read any
course, but only its creator can modify it. Responses carry `can_edit` so the UI
does not have to re-derive the rule. This becomes a real permission model in
Phase 8.

## Testing

```bash
make check    # gofmt, go vet, go test, and a frontend type-check
```

Go tests cover password hashing, token issue and verification (including
rejecting `alg: none` and foreign-key tokens), refresh rotation with replay
detection, all three Google account-resolution paths, the per-tee par case,
ownership enforcement, and cascade deletes.

See [docs/phase-1-test-checklist.md](docs/phase-1-test-checklist.md) for the
manual pass.
