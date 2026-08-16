# Roundly

Self-hosted golf scorekeeping and stat tracking. A single Go binary with the
React frontend embedded, plus one SQLite file.

**Phases 1 (auth + course directory) and 2 (golf bag) are complete, plus the user
profile: account details, data export and restore, and read-only API keys.** See
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

### Enabling course geocoding

Set `NOMINATIM_URL` and a course's latitude and longitude fill themselves in
from its address when it is saved. Leave it unset — the default — and both stay
the hand-typed fields they have always been.

The public OpenStreetMap instance is free and needs no account:

```
NOMINATIM_URL=https://nominatim.openstreetmap.org
```

Two things to know before you do. First, this is the only outbound call the
course directory makes, and it sends course addresses to whoever runs that
server. Second, it binds you to the
[OSM usage policy](https://operations.osmfoundation.org/policies/nominatim/).
Roundly holds up its end of that — one request per second, results cached, this
instance identified in the `User-Agent` from `PUBLIC_URL`, and no geocoding at
all on import, since restoring an account is exactly the bulk geocoding the
policy forbids. If the directory gets busy, run your own Nominatim and point the
same variable at it.

Coordinates fill gaps and never overwrite: a course saved with a point keeps it.
Clearing both fields and saving again is how you re-place a course that moved.

### Enabling email

Set `MAIL_FROM` plus exactly one transport and this instance can send mail,
which switches on two features together: confirming an address at signup, and
optional email two-factor on the profile screen. Leave it unset — the default —
and neither is offered, which is how the app behaved before they existed.

SMTP works with any mail server:

```
MAIL_FROM="Roundly <no-reply@your-domain.example>"
SMTP_HOST=smtp.fastmail.com
SMTP_PORT=587
SMTP_USERNAME=you@example.com
SMTP_PASSWORD=an-app-specific-password
```

Resend is the alternative, and the one to use on a host that blocks outbound
587 — a great many do:

```
MAIL_FROM="Roundly <no-reply@your-domain.example>"
RESEND_API_KEY=re_...
```

Configure both and Resend wins. `MAIL_FROM` has to be an address the transport
is allowed to send as: a domain verified with Resend, or an address on the SMTP
account you authenticate with. A half-filled configuration — a transport with no
`MAIL_FROM`, credentials with no password — fails at boot rather than starting
an instance whose mail silently goes nowhere.

STARTTLS is used whenever the server offers it. A **remote** server that does
not offer it is refused rather than sent your password in the clear; a loopback
relay is exempt, since there is no network there to eavesdrop on.

**What turning this on changes for existing accounts.** Nothing retroactive:
`email_verified` is only ever set by opening a link, so accounts created before
this existed start unconfirmed and are held at the confirm screen until they
open one. On an instance with real users, expect to tell them.

**Two-factor** is opt-in per account, guards the password path only (a Google
sign-in has already been through Google's own second factor), and remembers a
browser for 30 days so it is not asking every morning. Codes go to the address
that also recovers the account, which makes it a defence against a stolen
password rather than against a compromised mailbox — the profile screen says so
in as many words.

Turning it on mints **ten recovery codes**, shown exactly once. They are the way
back in if the mailbox is gone, which on a self-hosted instance is otherwise a
lost account: there is no administrator to appeal to. Each works once, they are
stored argon2id-hashed so nobody can read them back, and a fresh sheet can be
generated from the profile at any time (which retires the old one). Signing in
with one is the "Can't access your email?" link on the code step.

### Sign-in throttling

Failed sign-ins are counted along two axes and refused with a `429` past either:

```
LOGIN_RATE_LIMIT=10     # failures per account per window
LOGIN_RATE_WINDOW=15m   # the per-IP allowance is 3x the account limit
```

Both are needed. An account-only limit lets a botnet try one password against a
million accounts; an IP-only limit lets a botnet grind one account. The per-IP
allowance is the looser of the two because a household, an office, or a carrier
NAT can put many legitimate people behind one address.

**Only failures count.** A correct sign-in costs nothing, so somebody who knows
their password is never locked out by their own traffic. The refusal is
byte-identical whether or not the account exists, so it cannot be used to
enumerate addresses.

Account creation is capped separately, and counts differently:

```
SIGNUP_RATE_LIMIT=5     # accounts per address per window, successful or not
SIGNUP_RATE_WINDOW=1h
```

**Signup counts successes too**, unlike sign-in. That is the whole difference
between them: a successful sign-in is what the endpoint is for, whereas a
successful *signup* is exactly what an abuser wants — filling an instance with
junk accounts needs no failed attempts at all. Malformed attempts count as well,
so probing which addresses are already registered is not free.

There is only one axis here, the IP. The email address is chosen by whoever is
signing up, so keying on it would hand every attacker a fresh bucket per
attempt.

Both limiters are memory-only and per-process. Restarting the server clears the
counters, and running two instances behind a load balancer gives each its own —
if you ever do that, the limits are effectively multiplied by the instance
count.

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
  account/           profile, avatars, whole-account export and merge import
  apikey/            read-only personal API keys: token, policy, limiter, guard
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
| `PUT` | `/auth/preferences` | `{distance_unit}` — `yards` or `meters` *(auth)* |

**Account** *(all require a signed-in session; an API key cannot reach any of these)*

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/me` | The signed-in user. The one account route an API key *can* read |
| `PUT` | `/account/profile` | Names, home course, location |
| `PUT` | `/account/email` | Change the login address; returns a new session |
| `POST` `DELETE` | `/account/avatar` | Upload (multipart, field `avatar`) or clear the photo |
| `GET` | `/avatars/{key}.jpg` | The image itself — unauthenticated, see below |
| `GET` | `/account/export?format=json\|csv` | Download everything you own |
| `POST` | `/account/import` | Merge a JSON backup back in |
| `GET` `POST` | `/account/keys` | List keys, or create one (the only response with a secret) |
| `DELETE` | `/account/keys/{id}` | Revoke a key |
| `DELETE` | `/account` | Delete the account; irreversible |

**Courses** *(all require auth)*

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/courses?q=&limit=&offset=` | Paginated list, caller's home course first then pinned; `q` matches the name and every part of the address |
| `POST` | `/courses` | Create, optionally with tees and a hole count |
| `GET` | `/courses/{id}` | Full detail: tees, holes, per-tee par and yardage |
| `PUT` | `/courses/{id}` | Update name, address, and phone number |
| `DELETE` | `/courses/{id}` | Remove a course — **administrator only** |
| `POST` | `/courses/{id}/removal-request` | Ask the administrator to remove a course |
| `GET` | `/admin/removal-requests` | The queue — administrator only |
| `POST` | `/admin/removal-requests/{id}/resolve` | Settle one — administrator only |
| `POST` | `/courses/{id}/tees` | Add a tee |
| `PUT` `DELETE` | `/tees/{id}` | Update or delete a tee |
| `POST` | `/courses/{id}/holes` | Add a hole |
| `PUT` `DELETE` | `/holes/{id}` | Update stroke index, or delete |
| `PUT` | `/holes/{id}/tee-details/{tee_id}` | Upsert par and yardage for one hole+tee |
| `DELETE` | `/holes/{id}/tee-details/{tee_id}` | Clear that pairing |

**Golf bag** *(all require auth; a bag is private to its owner)*

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/clubs` | The whole bag: active, benched, and retired, plus the club count |
| `POST` | `/clubs` | Add a club |
| `GET` | `/clubs/options` | Accepted club types and shaft flexes |
| `GET` | `/clubs/{id}` | One club |
| `PUT` | `/clubs/{id}` | Edit description; does not change status |
| `PUT` | `/clubs/{id}/status` | `{status}` — `active`, `benched`, or `retired` |
| `DELETE` | `/clubs/{id}` | Delete permanently (prefer retiring) |

Errors share one envelope. `fields` appears only on validation failures:

```json
{ "error": "validation_failed", "message": "…", "fields": { "par": "Must be between 3 and 6." } }
```

### Three notes on the design

**The OAuth handoff code.** A browser redirect cannot receive a JSON body, and
putting a refresh token in a redirect URL leaks it into history, logs, and the
`Referer` header. So the callback stores the session against a single-use code
that expires in two minutes and redirects to `/auth/callback?code=…`, which the
app immediately trades for the real tokens over XHR.

**Nobody owns a course.** The directory is shared reference data: any signed-in
user can read *and correct* any course. A golf course's yardages are objective
facts about the world rather than one player's property, and tying edit rights to
whoever typed them in first meant a wrong number could only ever be fixed by one
person — who may never come back. `uploaded_by` records who entered a course and
grants nothing; it goes null when that account is deleted.

Removing a course is the exception, because it is the one irreversible act:
it cascades away every tee, hole, par, and yardage with no history to restore
from. So anyone may *request* a removal with a reason, and only the site
administrator — the account named by `ADMIN_EMAIL` — can carry it out. The
request record outlives the course, keeping `course_name` as a snapshot so it
still says what was removed.

**Club status is derived, not stored twice.** A club is `active` (in the bag),
`benched` (owned, out of the bag), or `retired` (sold or replaced). Those come
from an `active` flag and a nullable `retired_at`, with a `CHECK` that a retired
club cannot also be active, so the contradictory state is unrepresentable rather
than merely avoided. Retiring is a soft delete: the row and its ID survive, which
is what lets Phase 3 rounds and Phase 4 shots keep pointing at the club that hit
them. Status changes go through their own endpoint, so saving an edit form can
never move a club between groups by accident.

Every club but a putter also carries an **expected carry** and an **average
dispersion**, both optional. Phase 4 compares recorded shots
against them rather than replacing them: the derived number does not exist until
rounds have been played, and dispersion is not something a player can eyeball.
A putter has neither, and sending either on one is a validation error rather
than a silent null. The bag screen also hides loft and shaft flex on a putter —
those are real specs, just not ones worth collecting, so that half of the rule
is presentation only and the API keeps accepting them.

**The yardage chart is a print view, not a generated file.** `/bag/chart` lays
the bag out as a sheet the player can print or, from the same dialog, save as a
PDF. There is no PDF library and no server endpoint behind it — the browser
already has both a layout engine and a PDF writer, so the chart is HTML with a
`@media print` block in `web/src/index.css`. The sheet is drawn in paper colours
on screen, which makes the preview the artifact and keeps the print rules down to
hiding the app chrome. It is ruled rather than shaded on purpose: browsers omit
background colours from printing unless the user opts in, but they always print
borders and text.

Clubs sort by carry, longest first, rather than in bag order — a chart is read by
distance, so the column has to decrease monotonically for the lookup to work. The
**Gap** column is computed from the printed numbers rather than the stored yards,
so subtracting two rows gives exactly the gap beside them. A club with no carry
prints as a dotted write-in line instead of being dropped, which makes an empty
bag's chart a useful range worksheet. Putters never appear.

**Distances are stored in yards and displayed in the user's unit.** Every
distance in the database — hole yardages, tee totals, club carry and dispersion
— is yards. `users.distance_unit` (`yards` or `meters`, set under Settings →
Distances) is a display preference applied at the input and display boundary by
`web/src/lib/units.ts`. Switching it rewrites nothing, so switching back shows
the original numbers. Course export stays in yards so a shared course file does
not depend on who exported it.

### The profile, and read-only API keys

**Avatars are stored in SQLite, in their own table.** A 256×256 JPEG is 15–30 KB, well under
the size where the filesystem starts to beat SQLite, so keeping it in the database preserves
the single-binary-plus-one-file deployment story — `cp roundly.db` is still a complete backup
— and removes every file/row consistency problem at once. The bytes live in `user_avatars`
rather than on `users` because every user query is `SELECT *`, and dragging 25 KB through each
session check would be a real cost.

Uploads are sniffed with `http.DetectContentType` rather than trusted from the multipart
header, rejected on their decoded dimensions *before* any pixels are allocated, corrected for
EXIF orientation (`image/jpeg` ignores the tag, so phone photos arrive sideways), centre-cropped,
downscaled, and re-encoded. The re-encode is what strips metadata: `jpeg.Encode` writes from
decoded pixels and has no path for EXIF at all, so GPS coordinates and camera serials cannot
survive an upload.

The image is served unauthenticated at `/api/avatars/{key}.jpg`, because an `<img>` tag cannot
send a bearer header and this SPA keeps its access token in memory only. The 22-character key
is unguessable and rotates on every upload, which is what makes `Cache-Control: immutable`
correct — the bytes at a URL genuinely never change, and replacing a photo invalidates every
cache by changing the address. The URL is a bearer capability: anyone holding it can view that
image until it is replaced, the same trade-off Gravatar makes.

**API keys are read-only, and that is enforced in three independent layers.** All three live in
one global middleware that runs *before* chi routes anything, so no endpoint added later — in
any package, in any group — can end up outside it:

1. an explicit allow-list of paths (`/api/health`, `/api/me`, `/api/courses`, `/api/courses/{id}`,
   `/api/clubs`, `/api/clubs/options`, `/api/clubs/{id}`);
2. a `GET`/`HEAD` method check;
3. an outright block on `/api/auth/*` and `/api/account/*`.

The third is not decoration. `GET /api/account/export` is a GET, so a method check alone would
let a "read-only" key download the user's entire account, and `GET /api/account/keys` would let
it enumerate their other credentials. Any one layer would usually be enough; the point of
having three is that no single mistake is.

Only a SHA-256 of the token is stored, domain-separated from the refresh-token hash so the two
can never be interchanged. A password KDF would be wrong here: the token is 256 bits of CSPRNG
output, so there is nothing to guess however fast the hash is, and running argon2id on every
request would be a denial of service the server performs on itself. `last_used_at` is coalesced
through a background flusher — at most one write per key per five minutes — so a read-only
request never has to take the single write connection.

**Deleting an account** erases the profile, photo, clubs, API keys, OAuth links,
and sessions, and frees the email address for reuse. It needs the current
password, or — for a Google-only account, which has no password to demand — a
session less than five minutes old. Courses stay in the directory with the
attribution removed, because other players depend on them and nobody owned them.
That is only possible because `uploaded_by` is `ON DELETE SET NULL`; before that,
a single uploaded course made an account permanently undeletable.

**Export and import.** `GET /account/export` returns profile, clubs, and every course you
created, with the avatar base64-embedded so a restore is actually complete. It excludes the
password hash, refresh tokens, API keys, and the OAuth provider subject. The CSV option is a ZIP
of six tables — the sixth, `hole_tee_details.csv`, is where par and yardage live, and a set
without it would silently lose every number on every scorecard. Cells beginning `=`, `+`, `-`,
or `@` are prefixed with an apostrophe, because a club labelled `=HYPERLINK(…)` is executable
content the moment the file opens in Excel.

Import merges and skips: clubs match on type plus label, courses on name among *your own*
courses. Matching courses by the ID in the file — which the single-course import does — would
let a restore rewrite a course somebody else created, since the directory is shared. Nothing is
overwritten and nothing is deleted, so importing the same file twice is a no-op and a partial
failure is fixed by running it again.

## Testing

```bash
make check    # gofmt, go vet, go test, and a frontend type-check
```

Go tests cover password hashing, token issue and verification (including
rejecting `alg: none` and foreign-key tokens), refresh rotation with replay
detection, all three Google account-resolution paths, the per-tee par case,
ownership enforcement, and cascade deletes. The bag adds the full status state
machine, ID stability across every transition and edit, the 14-club count, and
the privacy rule that another user's club reads as absent rather than forbidden.

See [docs/phase-1-test-checklist.md](docs/phase-1-test-checklist.md) for the
manual pass.
