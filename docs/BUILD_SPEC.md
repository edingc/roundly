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
3. **Phase 3 — Score Tracker Core** (start/play/complete a round) ✅ *complete* —
   [plan](phase-3-plan.md), [test checklist](phase-3-test-checklist.md)
4. Phase 4 — Stats Dashboard 🔨 *started* — overview screen, handicap, anti-cap,
   and trend charts are in; per-club driving stats are not (aggregates: driving accuracy by club, scoring avg, putts/round, etc.)
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
- **Expected carry and average dispersion on every club but the putter.** This reverses a call made mid-build. The first pass dropped a
  distance field on the reasoning that Phase 4 derives distances from recorded
  shots, so a hand-entered number would be a staler second source of truth. That
  was wrong on two counts: the derived number does not exist until rounds have
  been played, and dispersion is not something a player can eyeball at all, so
  there is nothing to be stale *against*. Phase 4 now compares against these
  rather than replacing them.
- **A putter shows none of loft, flex, carry, or dispersion,** but for two
  different reasons, and the split is deliberate. Carry and dispersion describe
  a full shot a putter never hits, so the *server* rejects them. Loft and flex
  are real on a putter — 3.5° is a genuine spec — but not worth the form space,
  so they are hidden in the UI only and stay valid over the API. That avoids a
  destructive migration over existing putter rows: the UI reads the club type
  rather than trusting the column to be null, so stored values are hidden
  immediately and cleared lazily the next time the club is saved.
- **Carry and dispersion on a putter are refused, not nulled.** Both describe a
  full shot.
  Sending either on a putter is a 422 naming the field, rather than a silent
  null — a client that re-types a club as a putter without clearing the boxes
  should be told, not quietly stripped of numbers the player typed. The rule
  lives in `internal/club` rather than a CHECK constraint: SQLite cannot add a
  table-level CHECK through `ALTER TABLE`, and rebuilding the table for a domain
  rule is a poor trade against the retired/active CHECK, which guards an actual
  data-integrity invariant.
- **"Wedge" is a shaft flex.** It is a real designation (W) sold on wedge
  shafts, and it trails the list rather than slotting into it, because it is not
  a point on the ladies-to-x-stiff scale.
- **Only type and label are required.** A player who wants nothing in their bag
  but "4 Iron" should not be made to fill in a loft to save it, and the bag list
  drops the detail line entirely for a club that has no detail.

### The printable yardage chart

The point of recording a carry is having it to hand on the course, which a phone
in a pocket is not always good for. `/bag/chart` prints the bag as a sheet.

- **A print view, not a generated PDF.** The browser's print dialog already
  offers "Save as PDF", so HTML plus a `@media print` block delivers both paper
  and a file with no PDF library in the binary and no server endpoint. It also
  reflows to whatever paper the player has, which a hand-laid-out PDF would not.
- **The sheet is drawn in paper colours on screen,** so the preview *is* the
  artifact and the print rules only have to hide the app chrome. This also
  sidesteps the dark-mode trap: browsers print text colour but drop background
  colours unless the user ticks "background graphics", so a themed page would
  print white text onto white paper.
- **Ruled, not shaded,** for the same reason — borders always print, fills do
  not.
- **Sorted by carry, longest first, not in bag order.** A chart is read by
  distance, so the column has to decrease monotonically. It also stops the sheet
  from hiding a 5 wood that genuinely outcarries the 3 wood above it.
- **Gaps are computed from the printed numbers,** not the stored yards, so a
  reader who subtracts two rows gets the number beside them. Same trap as
  converting a tee total instead of summing converted holes.
- **A club with no carry prints a dotted write-in line** rather than being
  dropped, which turns a chart of an unmeasured bag into a range worksheet.
- **Columns appear only when some club has the data,** so a bag holding nothing
  but labels prints two clean columns instead of three empty ones.
- **Two formats.** A full sheet, and a 3.5in cut-out card that fits a yardage
  book — a letter page is the wrong shape for something read standing in a
  fairway.

### Distance units

Adding carry and dispersion raised the question of metres, which turned out to
be bigger than the bag: hole yardages and tee totals are distances too, and a
bag reading in metres beside a scorecard reading in yards would be worse than
either alone. So this is a whole-app preference, added here rather than deferred.

- **One per-user setting, `users.distance_unit`,** defaulting to yards. It
  applies to the scorecard grid, tee totals, the course forms, and club carry
  and dispersion.
- **Stored canonically in yards; converted only at the display and input
  boundary,** in `web/src/lib/units.ts`. A unit column per value would let one
  bag hold both at once and would force every read site to convert anyway.
  Switching the setting rewrites nothing, so switching back shows the original
  numbers exactly.
- **Course export stays in yards.** A course file moving between instances must
  not depend on the exporting user's display preference.
- **Totals are summed after conversion, not converted after summing.** The
  scorecard column rounds each hole and adds those up; converting the stored
  total instead rounds once and can differ by a metre or two, which is visible
  when the tee chip and the grid total sit on the same screen.
- **Rounding is to whole units in both directions.** A metre value can drift by
  a yard on a round trip, which is under half a metre on a distance nobody
  measures to better than a pace. Storing fractional yards would put decimals
  into a grid that has always held whole numbers.
- **A unit switch re-seeds the scorecard drafts** rather than merging them. The
  merge that protects unsaved edits after a save is wrong here: a number
  half-typed in yards means something else in metres.
- **Bag order is derived, not arranged.** The list sorts by club type, then loft
  ascending, which produces the real layout (driver → woods → hybrids → irons →
  wedges → putter) with no reordering UI. `display_order` is stored and settable,
  and is what a future drag-to-reorder would write to.

---

## 4b. USER PROFILE — account, data, and API access ✅

Delivered as `internal/account` and `internal/apikey` plus the `/profile` screen, which
replaces the old settings page. `/settings` redirects so bookmarks survive. The manual pass is
in [profile-test-checklist.md](profile-test-checklist.md).

Not a numbered phase: it is the account surface Phase 3 needs before rounds start attaching
themselves to people, done early because "download my data" gets much harder to add later.

### Decisions made during implementation

- **Settings became Profile, under the user's own menu.** An account-shaped thing belongs
  beside the person's name, not in the app's section nav. The nav is left holding Courses and
  Bag — the two things the app is actually *for*.
- **Imperial/Metric is a relabelling, not a migration.** `users.distance_unit` still stores
  `yards`/`meters` and the conversion code is untouched. The wording generalises if temperature
  or weight ever arrive, and nothing had to be rewritten to get it.
- **Avatars live in SQLite, in their own table.** The first plan was files in a data directory.
  Once the image is downscaled to a 256×256 JPEG — 15–30 KB, well under the ~100 KB where the
  filesystem starts to win — that stopped paying for itself: blob storage keeps the
  single-binary-plus-one-file deployment promise, keeps `cp roundly.db` a complete backup, and
  deletes the entire file/row consistency problem (write ordering, orphan cleanup, path
  traversal, container permissions). Its own table because every user query is `SELECT *`, and
  `toUser` runs on nearly every request.
- **The avatar URL is an unguessable, rotating key,** served unauthenticated. An `<img>` cannot
  send a bearer header and the access token is memory-only. Rotating on every upload is what
  makes `immutable` caching correct and what makes a replaced photo unreachable from a shared
  link. The URL is a bearer capability, and the UI says so.
- **Uploads are sniffed, bounded, and re-encoded.** The multipart Content-Type is
  attacker-controlled and never consulted; dimensions are checked from the header before pixels
  are allocated; EXIF orientation is applied by hand because `image/jpeg` ignores it and every
  phone photo would otherwise arrive sideways. The re-encode is what strips metadata — there is
  no EXIF path through `jpeg.Encode` at all.
- **Changing the email returns a new session and signs out everywhere else.** The access token
  carries the old address in its claims, and moving the account to a new address is the first
  thing someone holding a stolen token would do. Accounts with a password must type it;
  password-less (Google-only) accounts must instead have signed in within the last five minutes,
  because otherwise there would be no second factor at all.
- **API keys enforce read-only in three independent layers,** all in one global middleware that
  runs before routing: a path allow-list, a GET/HEAD check, and a hard block on `/api/auth/*`
  and `/api/account/*`. The block is not redundant — `GET /api/account/export` is a GET, so a
  method check alone would let a "read-only" key exfiltrate the whole account. Global rather
  than per-route so that a future endpoint registered in the wrong group cannot escape it.
- **The allow-list is a deny-by-default list, and that is the point.** A route added in Phase 3
  is invisible to every key until someone deliberately adds it. That will feel like a bug the
  first time rounds do not appear; it is the design.
- **SHA-256 for keys, not argon2id.** The token is 256 bits of CSPRNG output, so there is
  nothing to guess however fast the hash runs, and argon2id on every request would be a
  self-inflicted denial of service. Domain-separated from the refresh-token hash so the two
  classes of credential can never be interchanged.
- **Many keys per user, capped at ten.** A single key makes rotation destructive — the only way
  to replace it breaks every consumer at once — and `last_used_at` is only actionable if keys
  are distinguishable.
- **`last_used_at` is approximate on purpose.** Written by a background flusher, at most once
  per key per five minutes, so a read-only request never takes the single write connection.
- **The export excludes credentials and identifiers**: password hash, refresh tokens, API keys,
  OAuth provider subject, avatar key, and every row ID. It includes the avatar as base64, so a
  restore is complete rather than quietly losing the photo.
- **CSV is six files, not five.** Par and yardage live in `hole_tee_details`; a set without that
  file loses every number on every scorecard. Cells starting `=`, `+`, `-`, or `@` are escaped,
  because a club label is executable content in Excel otherwise.
- **Import merges and skips, and never matches courses by ID.** The directory is shared, so an
  ID match could rewrite a course another user created. Courses match on name among the
  importer's own; clubs on type plus label. Nothing is overwritten or deleted, which makes a
  double import a no-op and a partial failure recoverable by re-running it.
- **The course export's `format_version` is finally checked** — it had shipped as 1, 2, and 3
  without anything ever reading it. Versions 1–3 are accepted, since they differ only by
  optional fields and those files are already in users' hands.

### Known gaps

- **A key cannot see Phase 3 rounds** until `/api/rounds` is added to the allow-list in
  `internal/apikey/policy.go`.

---

## 4c. UNOWNED COURSES, REMOVAL REQUESTS, AND ACCOUNT DELETION ✅

Started as "let people delete their account", which turned out to be impossible:
`courses.created_by` was the only reference to `users(id)` without an `ON DELETE` clause.
Working out what *should* happen to those courses surfaced the real problem, and fixing that
made deletion trivial.

### Decisions made during implementation

- **Nobody owns a course.** Attribution was doing double duty as authorization. A golf course is
  objective real-world data, and tying edit rights to whoever entered it first meant a wrong
  yardage could only be corrected by one person — who may never come back. `created_by` becomes
  `uploaded_by`: attribution that grants nothing. `loadEditableCourse` collapses into
  `loadCourse`; `can_edit` leaves the DTO along with the two read-only UI states.
- **The column is nullable with `ON DELETE SET NULL`,** which is what makes account deletion a
  plain `DELETE` with no course handling at all. The alternatives were worse: deleting the
  courses silently blanks other players' home course, and freezing them leaves rows nobody can
  ever edit again.
- **First table rebuild in the project.** SQLite cannot alter a constraint, so the 12-step
  procedure is the only route. It needs `foreign_keys=OFF`, which goose cannot toggle inside a
  transaction — hence `-- +goose NO TRANSACTION` — and it ends with `PRAGMA foreign_key_check`,
  because nothing else here would notice a violation.
- **The risk was the scan, not the migration.** `uploaded_by` regenerates as `*string`, and
  `ListCourses` scans every row of a page, so getting the pointer wrong fails the whole listing
  rather than one course. That is what `TestCourseSurvivesItsUploader` exists to catch.
- **Removing a course is an administrator action.** Unowning courses would otherwise have left
  deletion available to everyone, which is the wrong end state: editing is recoverable by
  editing again, deleting is not. Anyone may request a removal with a reason; only the
  administrator settles it.
- **The administrator is configuration, not schema.** `ADMIN_EMAIL` names the account, because
  on a self-hosted instance the administrator is whoever runs the process, and an environment
  value cannot drift out of step with the database or be edited through the app. Unset means no
  administrator: requests queue, nothing is removed, everything else works.
- **`RequireAdmin` reads the database rather than the token claim.** The email in an access
  token is a snapshot, so trusting it would leave a 15-minute window where admin rights linger
  after they should have gone. Every way of not being the administrator returns one identical
  refusal, so nobody can probe for who is.
- **Removal requests keep nullable foreign keys and a `course_name` snapshot** — the lesson from
  `created_by`, applied immediately: a reference that cannot be released is one that blocks
  deletion later. `course_id` nulls rather than cascading, so resolving a request by removing
  the course leaves the record behind saying what was removed.
- **Import now updates a course it names rather than forking a copy.** The fork only existed to
  avoid touching someone else's property. Sharpened edge worth knowing: import is a destructive
  sync, so a stale file drops tees and holes added since it was exported — already true for the
  owner, now true for anyone, exactly as hand-editing is.
- **Deletion reuses the email-change reauthentication rules exactly**: the current password, or
  for a password-less Google account a session under five minutes old.

### Known gaps

- **An access token outlives deletion by up to 15 minutes.** `auth.Middleware` never reads the
  database — it builds the principal from claims alone. Afterwards `GET /api/me` correctly 401s,
  `GET /api/clubs` returns an empty bag with 200, and a write fails on a foreign key. Nothing
  leaks, since the data is gone; it is merely confusing. Fixing it needs either a query per
  request or a revocation list, neither worth it for that window.
- **There is no undo and no grace period.** The confirmation is the only safety net, which is
  why it demands both the password and the address typed out.

## 4d. STRUCTURED COURSE LOCATIONS ✅

Started as "make the home course a search". The search itself was easy; making its results
*useful* was not. A result list of names cannot tell two clubs called Pine Valley apart, and the
thing that would — where each one is — was locked inside a single free-text `address` column.

### Decisions made during implementation

- **`address` becomes `street` plus `city`, `region`, `postal_code`, `country`.** The parts are
  what callers actually ask for. Anything that wants "Marne, MI" out of one string is guessing at
  commas, and guessing wrong is silent.
- **The street line survives rather than being folded in.** It is what puts the map pin on the
  clubhouse instead of the town, and it is the one piece the other four cannot reconstruct. It is
  also where an address the backfill could not split stays, whole and still searchable.
- **The backfill splits left to right, one comma at a time.** SQLite evaluates every right-hand
  side against the pre-update row, so each pass can read the column it is writing — three plain
  `substr`/`instr` updates instead of one unreadable nest, no recursive CTE, no `UPDATE ... FROM`.
  It only touches the unambiguous `Street, City, REGION POSTAL` form; a suite line or a bare town
  is left alone, because a wrong city is worse than an unparsed one.
- **Nothing infers `country`.** A five-digit postal code is not proof of a country. An empty field
  asks to be filled in; a wrong one does not.
- **Every part is independently optional, including the street.** A course known only by its town
  is a perfectly good directory entry, and demanding the rest just gets filler typed into fields
  nobody can answer. Only the postal code skips the "must contain a letter" check — `49435` is
  entirely correct.
- **Export format 4 keeps reading `address`.** Files stamped 1–3 are in users' hands. Their
  `address` lands in `street` on import and is never written back out, so a file round-tripped
  through this server stops carrying the old key. This is the `minCourseExportVersion` lesson
  applied ahead of time rather than after a bug report.
- **Search covers every part of the address, not just the street.** People look for their club by
  town. That one change is what makes the picker work at all.
- **The picker is a combobox, not a `<select>`.** A dropdown of every course is fine at a dozen and
  unusable at a thousand, and it could only ever show a name. `toUser` now resolves
  `home_course_location` alongside `home_course_name`, from the row it was already reading, so a
  saved choice renders with its town without a second request.
- **`joinPlace` is duplicated in spirit in `internal/auth` rather than shared.** `internal/course`
  imports `auth` for its middleware; a shared formatter would close the cycle. Six lines is a
  cheaper price than an import graph problem.
- **The home course sorts above even pinned courses, in SQL rather than in the client.** The
  directory paginates, so "first" has to mean first of all of them; the client sort it replaces
  could only ever reach the twenty-five rows already fetched. `List` takes a viewer id and orders
  by home, then pinned, then name — ordering, never filtering, since the directory is shared.
- **Ordering joins `users` instead of comparing against a bound id.** sqlc's SQLite parser does
  not bind parameters inside an `ORDER BY`: it emits the placeholder but leaves it out of the
  params struct, so every remaining argument shifts by one and `LIMIT` silently gets the wrong
  value. In a `JOIN` condition it binds correctly, and the join replaces the lookup it would
  otherwise have cost. *(Also learned the hard way: an em dash anywhere in a `db/queries` comment
  breaks sqlc's lexer, and the error names a mangled token rather than the comment.)*
- **The "Home" chiclet is sky, not the brand green.** Green already means "pinned" on the same
  card, and amber is spoken for as this app's caution color — the API-key warning, the incomplete
  scorecard column. Sky was the only unused accent that reads as information rather than alarm.
- **Country and state get a `<datalist>`, never a `<select>`.** A select would be the third
  structured place-list this project has refused (see 00010): it goes stale, and it refuses an
  address someone lives at. A datalist suggests without constraining, so the column stays free
  text and nothing in the schema or the API changed to add it. Country names come from
  `Intl.DisplayNames` over the ISO 3166-1 codes, so they arrive in the reader's language and stay
  current with the browser instead of with this repo. A blank country falls back to the US region
  list, matching `lib/phone.ts` — domestic until told otherwise.
- **Display name moved above first and last on the profile.** It is the only required name and
  the only one anyone else sees; the optional pair belongs underneath it.

- **Coordinates come from Nominatim, and the choice was about storage rights, not price.** Most
  free geocoding tiers — Mapbox's included — forbid retaining the results, and retaining them in
  a column is the entire feature. OSM's data is ODbL, which permits it and asks for attribution.
  That single clause ruled out more candidates than any quota did.
- **Geocoding fills gaps and never corrects anybody.** A course saved with a point keeps it, and
  costs no lookup. Clearing both fields and saving is therefore also the re-place gesture, which
  is the only re-trigger there is — cheap to build, easy to explain, and it needs no "who set
  this" column to decide whether overwriting is allowed.
- **Import never geocodes.** A restore can carry sixty courses, and sixty lookups at one a second
  is both a minute of held-open request and precisely the bulk geocoding the usage policy
  forbids. Export files carry their own coordinates.
- **Every geocoding failure is silent and non-fatal.** No geocoder configured, too little address
  to place, a lookup error, a place OSM has never heard of — all return no coordinates and save
  the course anyway. None of them is a reason to refuse to record a golf course.
- **A country on its own is not enough to place.** `postalLine` returns "" without a street or a
  city, because "USA" geocodes to the middle of the country and a pin in Kansas is worse than no
  pin at all.
- **Off unless `NOMINATIM_URL` is set,** following `ADMIN_EMAIL` and the Google credentials. It
  is the only outbound call the directory makes and it ships course addresses to a third party;
  an operator should opt into that rather than find it in their egress logs. `/auth/config` grew
  a second boolean so the form shows the hint and the OSM attribution only where they are true.

### Known gaps

- **There is still no address autocomplete.** Typing a course address is five fields. Photon
  (komoot's OSM typeahead, free and keyless) or Google Places would fill all five from one search
  box; Places would reuse the existing `VITE_GOOGLE_MAPS_API_KEY` but needs the Places API
  enabled on it, bills per session, and puts a third-party script on a page that loads none.
- **The map still queries by name and address, not by the coordinates now being stored.** Once
  courses have been re-saved and populated, the embed can switch to `q=<lat>,<lng>` and the
  "map query trusts the course name" gap below closes with it.
- **Only US and Canadian subdivisions are suggested.** Everywhere else, the state field is a plain
  text box. Bundling the world's subdivisions is the staleness 00010 refused; Places Autocomplete
  is the real answer if the app ever goes international.
- **Two courses at the same address are still two courses.** Nothing dedupes on location, and the
  removal-request flow remains the only way to collapse a duplicate.
- **The map query trusts the course name.** It searches `"<name>, <address>"`, which is what makes
  a city-only course land anywhere sensible, but a badly misspelled name can now pull the pin off
  an otherwise exact street address. The unused `latitude`/`longitude` columns are the real fix.

## 4e. SIGNED AVATAR LINKS, EMAIL VERIFICATION, AND EMAIL 2FA

Three items cleared before Phase 3, all about credentials.

### Decisions made during implementation

**Avatars became signed, expiring links rather than session-guarded ones.**
The old design was a 128-bit unguessable key in the path, served
unauthenticated. Correct against enumeration and wrong about time: the URL was a
bearer token with no expiry, so a link that reached a screenshot, a synced
history, or a proxy log worked forever. The alternatives were considered and
rejected in this order:

- *A session cookie scoped to `/api/avatars`* is the strongest and was the first
  choice, until the costs were counted: it breaks sharing an avatar link at all,
  breaks avatars for the read-only API keys added in 4b, and needs
  `SameSite=None; Secure` to survive the Vite dev origin, which is exactly the
  kind of thing that works locally and fails from a phone on the LAN.
- *Fetch-to-blob in the SPA* authenticates properly and throws away HTTP caching
  along with any non-SPA use.
- *Signed expiring URLs* keep `<img>` working, need no cookie, keep API-key
  clients working, and convert "forever" into "about a day" — which was the part
  that actually mattered.

**The expiry is bucketed to a 24h boundary with a 12h floor.** A signature that
changed on every request would bust the browser cache on every `/me`. Bucketing
makes the URL byte-identical within a window, so it caches; the floor stops a
URL minted at 23:59 from expiring at midnight. Lifetimes run 12–36 hours and the
bucket flips at noon. Session refresh returns a fresh user every 15 minutes,
which is what keeps the client's copy ahead of its own expiry.

**The signing key is derived from `JWT_SECRET`, not `JWT_SECRET` itself.**
HMAC over a fixed label. A key that signs two different kinds of thing is a key
where a confusion between them becomes a forgery.

**A bad signature returns 404, not 403.** A caller who cannot present a valid
link has no business learning whether the key behind it names anything.

**`Cache-Control` went from `public` to `private`.** It was wrong the moment
these stopped being world-readable, and `max-age` now stops at the exact second
the signature does, so no cache can serve a picture whose link has lapsed.

**Mail is two backends and one interface.** SMTP via `net/smtp` for the
self-hosted case and Resend over HTTPS for hosts that block outbound 587, which
is a great many. Resend wins when both are configured, because setting an API
key is a choice. No new dependency either way: both are the standard library
plus a struct.

**A remote SMTP server that will not offer STARTTLS is refused, not
downgraded.** Sending a password in the clear is a disclosure, not a degraded
mode. Loopback is exempt — that is the local-postfix case.

**Email verification and two-factor are one switch: `mailer != nil`.** An
instance that cannot deliver a confirmation must not demand one, or nobody can
sign in. This is the same "unset the env var, hide the feature" rule as
`ADMIN_EMAIL`, `GOOGLE_*`, and `NOMINATIM_URL`, and it is the failure mode of
every secure-by-default flag that does not check whether the mechanism behind
it exists.

**`users.email_verified` was finally used.** It has been a hardcoded `0` since
migration 00001 because nothing could send the message that would set it.

**Signup issues a session and then blocks it.** Not issuing one would mean a
user whose next step depends on which browser their mail client opens.
`RequireVerifiedEmail` sits over the application routes and not over
`/api/auth`, so confirming, resending, and signing out stay reachable from that
state. It returns a distinct `email_unverified` code, because the client renders
a whole screen for it. API keys skip the check: a key can only be minted from
behind that gate, so its existence is already proof.

**Verification uses a link; two-factor uses six digits.** One
`email_challenges` table for both, because the row shape and the lifecycle are
identical and two tables would mean two copies of the sweep, the replay check,
and the rate limit. Codes are stored hashed — SHA-256 rather than argon2id,
because what a slow hash buys is resistance to offline cracking of a secret that
has already expired by the time an attacker has it.

**Six digits are protected by the attempt cap, not by entropy.** A million
possibilities is minutes for a script. Five guesses against a ten-minute code
puts a blind hit at one in two hundred thousand, and issuing a new code retires
the old one so that pressing "resend" four times does not leave four live codes.

**Two-factor is opt-in and guards the password path only.** Turning it on makes
sign-in depend on somebody else's mail server, which is the account holder's
trade to accept. A Google sign-in has already been through Google's own second
factor; re-asking would be theatre. An operator who wants it universal has the
stricter lever already — they decide whether mail is configured at all.

**Turning it on requires a confirmed address.** Arming a second factor aimed at
a mailbox nobody has proven they can read is a self-inflicted permanent lockout.

**Remembered devices are a stored token, not a fingerprint.** Fingerprinting is
unreliable enough to demand a code after a browser update and reliable enough to
be worth nobody's privacy. Trust lasts 30 days — the refresh-token span, so the
two never disagree — and is dropped wholesale when the password changes, the
address changes, or two-factor is toggled either way.

**Every wrong code gets the same message.** Mistyped, expired, used, out of
attempts, never issued. Distinguishing them tells someone holding a stolen
password which guess was close, and tells a prober whether an account has
two-factor on at all.

**Login now returns one of two shapes at 200.** A pending second factor is not
an error — the password was right — so the client discriminates on
`two_factor_required` rather than on a status code.

### Known gaps

- ~~**No recovery codes.**~~ Closed in 4f.
- ~~**Nothing rate-limits password attempts.**~~ Closed in 4f.
- **No admin override.** There is no way for the configured `ADMIN_EMAIL` to
  clear another account's two-factor or confirm an address by hand.
- **Turning email on strands existing accounts at the confirm screen** until
  each opens a link. There is no backfill, and deliberately so — marking old
  accounts verified would be recording something nobody checked — but on a
  populated instance it needs announcing.
- **`email_challenges` is swept hourly, not on read.** A spent code sits in the
  table until the janitor runs.
- **A device label is the raw User-Agent.** Honest and unreadable. No IP, no
  location, and no plan to add either.

## 4f. RECOVERY CODES AND SIGN-IN THROTTLING

The two gaps 4e left open, closed.

### Decisions made during implementation

**The rate limiter moved to `internal/ratelimit`.** It lived in
`internal/apikey`, which imports `internal/auth`, so auth could not import it
back. One implementation with two callers rather than two that drift.

**`Exceeded` was added alongside `Allow`.** Asking "am I still allowed to try?"
must not itself consume budget, because sign-in has to ask *before* it knows
whether this attempt will turn out to be a failure worth counting. Without the
split, a user typing their password correctly would spend the same allowance as
an attacker getting it wrong.

**Only failures are counted.** This is the decision that makes the limit
survivable. A limit that counts successes is one that locks out the busiest
legitimate users, which is how rate limits end up being removed.

**Two axes, and both are necessary.** Per-account defeats a distributed attack
on one person; per-IP defeats one host spraying a common password at every
address. An account-only limit lets a botnet try one password against a million
accounts; an IP-only limit lets a botnet grind one account. The per-IP allowance
is 3× the account limit, because a household, an office, or a carrier NAT can
put many real people behind one address, and locking that out on one person's
typo is a support ticket rather than a defence.

**The 429 is identical for accounts that do not exist.** A refusal that only
appears for real addresses is an enumeration oracle, and a particularly good one
because it needs no valid password to consult.

**A 429 does not count against itself.** Otherwise a client that keeps retrying
converts a fixed window into an unbounded block.

**Signup is throttled too, and counts the opposite thing.** Sign-in counts only
failures because a successful sign-in is what the endpoint is for. A successful
*signup* is the abuse, so every attempt counts — created, collided, or
malformed. Malformed included, because a script probing which addresses are
registered never needs to send a valid password.

**Signup has one axis, not two.** There is no account to key on: the address is
chosen by whoever is signing up, so keying on it would hand every attacker a
fresh bucket per attempt. The IP is the only thing they do not pick.

**The signup window is long and its count small** — an hour and five — because
that is the shape of real traffic. People sign up once; the limit needs room for
a family or an office joining on the same evening and no more.

**The limiter is memory-only and per-process.** Deliberate: this app is one
SQLite file and one process, and a database-backed counter would put a write on
the hot path of every failed login. Documented in the README, because two
instances behind a load balancer would each get their own counters.

**Recovery codes are argon2id, not SHA-256.** The opposite of the choice made
for sign-in codes in 4e, and for the reason that governed that one: lifetime. A
six-digit code has expired before anyone could crack it, so a slow hash buys
nothing. A recovery code lives until spent — possibly years — in a database that
may leak. Fifty bits of entropy plus argon2id survives that; either alone would
not.

**Verification is a linear scan.** argon2id salts every hash, so there is
nothing to look up by, and each unused code has to be tried: up to ten hashes,
about a fifth of a second. Acceptable for an operation performed once or twice
in the life of an account, and gated behind a correct password and the
challenge's own attempt cap.

**Recovery goes through the same challenge gate as a mailed code.**
`chargeChallengeAttempt` was split out of `redeemChallengeRow` precisely so both
paths spend from the same five attempts. Otherwise "use a recovery code instead"
would have been a way around the cap.

**A recovery sign-in cannot remember the device.** Somebody reaching for a
recovery code has just lost access to their email, which is the wrong moment to
hand out a thirty-day pass. The server refuses it there regardless of what the
client asks for.

**The sheet is minted when two-factor is turned on, in the same response.** An
account that is protected for a week before anybody thinks about recovery spends
that week one mailbox outage from being lost. It is also the only response that
ever carries them.

**Regenerating replaces all ten rather than topping up.** A sheet that is partly
old and partly new is one nobody can reason about.

**Codes are Crockford base32 and normalised on input.** Case folded, hyphens and
spaces dropped, O read as 0, I and L read as 1. Somebody copying ten characters
off paper will make exactly those mistakes, and refusing a correct code over the
wrong kind of one would be a lockout caused by pedantry.

**Turning two-factor off discards the codes.** A recovery code with no second
factor to recover from is a standing credential nobody remembers issuing.

### Known gaps

- **No admin override**, still. `ADMIN_EMAIL` cannot clear another account's
  two-factor, confirm an address, or reset a lockout by hand.
- **A locked-out user cannot be unlocked early.** The window has to elapse;
  there is no way for an operator to clear one bucket short of a restart, which
  clears all of them.
- **Recovery codes cannot be counted before two-factor is on**, because they do
  not exist until then. That is correct, but it means the profile screen has
  nothing to show somebody deciding whether to enable it.
- **No email is sent when a recovery code is used.** It is exactly the event
  worth telling somebody about, and the mailer is right there — but the address
  is, by assumption, the one they have lost access to.

### Accepted, not gaps: account enumeration

Two endpoints answer differently depending on whether an address is registered.
Both were looked at deliberately and both are being kept, so they are recorded
here rather than on a list of things to fix — a future reader finding them
should know they were considered.

**Signup says "An account with this email already exists."** This is the
convention almost every product follows, and the alternative has a real cost:
accepting the signup, saying "check your email", and mailing the *existing*
account a "somebody tried to sign up as you" notice. Someone who has simply
forgotten they already registered then gets a confusing email instead of an
immediate, useful error. The usual place to spend that cost is the password
reset flow, where "if an account exists, we have sent a link" is standard —
this app has no reset flow yet, and when it gets one it should be neutral.

**Login says "This account signs in with Google."** Sharper than the signup
case, because the message comes back whatever password is supplied: it needs no
valid credential to consult. It sits directly below the `VerifyPassword(password,
dummyHash)` call that exists to stop a missing account being detectable by
*timing*, which is an inconsistency worth naming — real effort closing a side
channel, immediately above a sentence that gives the same fact away in English.
It is kept because the alternative strands a Google user who does not remember
how they signed up behind a flat "that combination is not correct", which is a
real person hitting a dead end.

**Why both are acceptable here.** Enumeration matters when membership is itself
the secret — dating, health, adult, political, some financial services. That an
address has an account on a golf scorekeeping app is not that. The practical
risk it does carry is telling an attacker where to aim credential stuffing, and
the sign-in throttle added in 4f is the control for that. On a self-hosted
instance with a handful of accounts, enumeration reveals close to nothing.

**What would change the calculus**: this app becoming multi-tenant or publicly
hosted, membership becoming visible between players in a way that makes the
account list interesting, or any flow being added where the address is the
sensitive part. Revisit then, not before.

## 4g. PRE-PRODUCTION CLEANUP

A deliberate stop before Phase 3, to condense the migration history and review
what had accumulated. Nothing had been deployed, so this was the last cheap
moment to do either.

### The migration squash

**Sixteen migrations became one.** They described a schema that could be stated
once, and every one of them ran on every test database. Squashing is only
free before a deployment exists — after one, the incremental chain is the only
thing that can move a live database forward. Incremental migrations resume at
00002.

**Two things were fixed that the incremental chain could not express.** An
`ALTER TABLE ADD COLUMN` puts the column at the end of the table, so several
tables had columns in arrival order rather than any order worth reading; and
`users.updated_at` carried `DEFAULT ''`, because SQLite demands a constant
default when adding a NOT NULL column. An empty string is not a timestamp, and
anything that parsed one would have failed on it.

**`tees.total_yardage` was dropped.** It had been written on every insert and
never read: the total the API returns is summed from `hole_tee_details` so it
cannot drift from the grid, and a comment in the service said as much. sqlc
found every reference the moment the column left the schema, which is the
argument for a generated query layer in one line.

**Column order turned out not to matter**, though it was checked rather than
assumed: sqlc expands `SELECT *` into an explicit column list, so the physical
order of a database's columns never reaches a query.

**An existing database keeps working without being rebuilt.** Goose applies
migrations whose version is not recorded; version 1 is recorded, so the squashed
file is a no-op against a database that already ran the old chain. Verified by
booting the server against a copy rather than reasoning about it.

### Review findings

**A 429 rendered as nothing at all.** The throttle errors carried their retry
delay in `APIError.Fields`, and the client treated any error with fields as a
form validation failure — so it attached an error to a field named
`retry_after_seconds` that no form has, and the actual message was never shown.
Fixed on both sides: the delay moved to a standard `Retry-After` header via a
new `APIError.Headers`, and the client now keys off the error code rather than
guessing from the presence of `fields`. The guess was safe until something
other than validation put data there, which is the kind of latent coupling a
review is for.

**A comment claimed a failure was logged when it was discarded.**
`course.Service.touch` swallowed its error with `_ =` under a comment saying it
was logged. It logs now: the only ways it fails are a vanished course or a
database that has stopped accepting writes, and both are worth knowing.

**A redundant write on the email-change path.** `UpdateUserEmail` clears
`email_verified` as part of the same statement, and `MarkEmailUnverified` then
cleared it again before sending the link. The second write is gone and the
coupling is documented on the query, which is where it lives.

**A byte-sliced truncation.** The trusted-device label was cut with
`trimmed[:120]`, which splits a multi-byte rune. User-Agents are ASCII in
practice; the fix costs nothing and removes the assumption.

### What the review did not find

Worth recording, so the next pass does not redo it: no TODOs, no secrets in log
lines, no unchecked type assertions, no ignored errors beyond the two
deliberate ones (`VerifyPassword` against a dummy hash, which exists to burn
time and whose result is meaningless), and `.env` is correctly ignored by git.

### Full-codebase review

A second, exhaustive pass over all ~28k lines, after the targeted one above.

**A valid API key was capped at 20 requests a minute, whatever
`API_KEY_RATE_LIMIT` said.** The guard's failure limiter used `Allow`, which
records, rather than `Exceeded`, which asks — so every key-authenticated
request was charged to the *failure* budget, not just the failed ones. The
comment beside it said "failed authentications are limited by IP"; the code
limited all of them. Demonstrated with a test before fixing: the 21st
successful request came back 429 with the configured limit at 1000. Exactly the
same check-versus-record confusion 4f fixed in sign-in, in code written before
`Exceeded` existed — which is the argument for that method being on the type
rather than inlined at one call site.

**Club import bypassed every validation rule the API enforces.** Courses went
through `course.ValidateImport`, under a comment explaining that both paths must
not drift; clubs were written straight to the database with only a non-empty
check. A hand-edited backup could store a club type outside the list, a
900-degree loft, an unrecognised flex, or a note bounded only by the 1 MiB body.
Closed with `club.ValidateImport`, which runs the same `validateClub` the
handler does, so the two cannot diverge.

**Tees per course were unbounded.** Holes are self-limiting — numbers are
checked against 1..18 and duplicates rejected — but nothing capped tees, so one
request could create as many rows as fit in the body limit. `MaxTeesPerCourse`
is 24, applied on both the API and the import path.

**Three vulnerabilities in go-chi/chi**, found by `govulncheck` and fixed by
upgrading v5.2.2 to v5.3.0.

### What the full review cleared

Recorded so a later pass does not repeat the work:

- **No injection anywhere.** Queries are all sqlc-generated with bound
  parameters. Course search uses `instr()` rather than `LIKE`, so a `%` in a
  search term is a literal, not a wildcard.
- **No stored XSS through `course.website`**, which was the most promising
  candidate: courses are shared and the detail page renders the value as an
  `href`. The server requires an `http://` or `https://` prefix on both the API
  and import paths, so `javascript:` cannot be stored. `mapsSearchUrl`
  percent-encodes, and `phoneHref` strips to digits.
- **Avatar processing is sound.** Content type is sniffed rather than trusted,
  dimensions are checked from the header before any pixels are allocated, the
  EXIF parser is bounds-checked throughout, and re-encoding through
  `jpeg.Encode` is what strips metadata.
- **The CSV export cannot be corrupted by embedded newlines** — `encoding/csv`
  quotes them — and `safeCell` already defuses spreadsheet formula injection.
- **No data races**, under `go test -race` across every package.
- **No secrets in logs**, no `dangerouslySetInnerHTML`, no `eval`, no
  `target="_blank"` without `rel`, and `.env` is correctly ignored by git.

### Known gaps

- **The Go toolchain is a patch release behind.** `govulncheck` reports 20
  standard-library vulnerabilities in go1.26.0, all fixed in go1.26.6 —
  `crypto/x509`, `crypto/tls`, `net/http`, `net/url`, `net/mail`. Nothing to fix
  in this repo: it is a toolchain upgrade. The Dockerfile floats on
  `golang:1.26-alpine` so a rebuilt image already picks it up; a local build
  does not. Adding `toolchain go1.26.6` to go.mod would force it everywhere, at
  the cost of a toolchain download on machines that lack it.
- **No linter beyond the type checker.** `npm run lint` runs `tsc -b`; there is
  no ESLint config, so nothing catches unused variables, missing hook
  dependencies, or accessibility regressions in the frontend.
- **No CI.** Every check in this repo is one somebody remembers to run.
- **`refresh_tokens` had accumulated 360 rows for one user** before the janitor
  added in 4e existed. It sweeps hourly now, but nothing has ever verified that
  the sweep runs in a long-lived process rather than merely compiling.

## 5. How to Use This Doc

When starting a session:

> "Continuing the golf app — starting Phase 3 (Score Tracker Core) per
> docs/BUILD_SPEC.md."

The repo now carries its own context: `README.md` covers architecture, the API,
and configuration, so a fresh session only needs the phase section.
