# Phase 3 — Rounds: plan

**Status: built.** The plan below is kept as written, so that what was intended
can be read against what happened. Where the two differ, §11 says so.

Manual passes are in [phase-3-test-checklist.md](phase-3-test-checklist.md).

## 1. Scope

**In:** starting, playing, and completing a round; two entry modes; per-hole
capture; a per-round summary.

**Out, and where it goes instead:**

| Not in Phase 3 | Why | Where |
|---|---|---|
| Cross-round aggregates | Needs rounds to exist first | Phase 4 |
| GPS / shot tracking | Deferred by decision | Phase 7 |
| Handicap index | Needs 20 rounds to mean anything | later |
| Multiple players in one round | Solo is the whole current product | later, see §9 |
| Stableford, match play | Stroke play is what the stats assume | later |

A round summary (this round's GIR, fairways, putts) *is* in scope. Comparing
rounds to each other is not.

## 2. Decisions taken before building

1. **Live rounds are local-first.** The browser holds the in-progress round and
   is the source of truth until it completes.
2. **Manual entry has two depths on one screen** — score and putts always
   visible, detail columns behind a toggle.
3. **Everything the course tells us is snapshotted onto the round.**
4. **Bunkers are two flags; penalties are a count plus an optional reason;
   putting distance is the first putt, in feet.**
5. **GIR, scrambling, and sand saves are derived, never entered.**

### Why snapshotting is not optional

Courses are shared reference data and anyone signed in may correct one — that
was the whole of decision 4c. An administrator may remove one outright.

So if a round merely *pointed* at a course, then a stranger fixing a par from 4
to 5 next season would silently restate what you shot against: your GIR
percentage moves, your scoring average moves, and nothing anywhere says so. A
removed course would take your rounds' pars with it.

This is also the one mistake in this phase that cannot be repaired later — the
original values are simply gone. Snapshot: par, yardage, and stroke index per
hole; course name, tee name, rating, and slope per round.

Rating and slope get stored **even though handicaps are out of scope**, because
they are the inputs to a WHS score differential. Captured now, a handicap index
is arithmetic later; skipped now, it is impossible for every round already
played.

## 3. Data model

```sql
CREATE TABLE rounds (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Nullable link, snapshotted name. An administrator can remove a course;
    -- a round played there is still a round you played.
    course_id TEXT REFERENCES courses(id) ON DELETE SET NULL,
    course_name TEXT NOT NULL,
    tee_id TEXT REFERENCES tees(id) ON DELETE SET NULL,
    tee_name TEXT NOT NULL,
    tee_color TEXT,

    -- As they stood on the day. See §2.
    course_rating REAL,
    slope_rating INTEGER,

    -- A local calendar date ('2026-09-03'), not a timestamp: nobody's round is
    -- "on the 3rd in UTC". Separate from created_at because a manual round is
    -- entered in February for a round played in September.
    played_on TEXT NOT NULL,
    started_at TEXT,                   -- live rounds only
    completed_at TEXT,

    status TEXT NOT NULL DEFAULT 'in_progress',
    entry_mode TEXT NOT NULL,
    holes_intended INTEGER NOT NULL,   -- 9 or 18
    nine TEXT,                         -- 'front' | 'back', NULL when 18

    notes TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,

    CHECK (status IN ('in_progress', 'complete', 'abandoned')),
    CHECK (entry_mode IN ('live', 'manual')),
    CHECK (holes_intended IN (9, 18)),
    CHECK (nine IS NULL OR nine IN ('front', 'back')),
    -- A completed round has a completion time and vice versa, the same
    -- both-or-neither rule course_removal_requests uses for its resolution.
    CHECK ((status = 'complete') = (completed_at IS NOT NULL))
);

CREATE INDEX idx_rounds_user_played ON rounds(user_id, played_on DESC);
CREATE INDEX idx_rounds_user_status ON rounds(user_id, status);

CREATE TABLE round_holes (
    id TEXT PRIMARY KEY,
    round_id TEXT NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    hole_number INTEGER NOT NULL,

    -- Snapshots, same argument as the round's rating. par is nullable because
    -- a course in the directory may have an incomplete scorecard; a hole with
    -- no par is simply left out of the stats that need one.
    par INTEGER,
    yardage INTEGER,
    stroke_index INTEGER,

    -- NULL strokes means the hole was not completed — picked up, conceded,
    -- ran out of light. Distinct from 0, which is not a score anyone shoots.
    strokes INTEGER,
    putts INTEGER,

    -- ON DELETE SET NULL is the backstop; §8 makes deleting a referenced club
    -- refuse outright, so this should never fire.
    tee_club_id TEXT REFERENCES clubs(id) ON DELETE SET NULL,
    tee_accuracy TEXT,

    first_putt_feet INTEGER,
    fairway_bunker INTEGER NOT NULL DEFAULT 0,
    greenside_bunker INTEGER NOT NULL DEFAULT 0,
    penalties INTEGER NOT NULL DEFAULT 0,
    penalty_type TEXT,

    UNIQUE(round_id, hole_number),
    CHECK (hole_number BETWEEN 1 AND 18),
    CHECK (strokes IS NULL OR strokes BETWEEN 1 AND 20),
    CHECK (putts IS NULL OR putts BETWEEN 0 AND 10),
    -- You cannot putt more times than you hit the ball.
    CHECK (strokes IS NULL OR putts IS NULL OR putts <= strokes),
    CHECK (tee_accuracy IS NULL OR tee_accuracy IN
        ('hit', 'left', 'far_left', 'right', 'far_right', 'long', 'short', 'mishit')),
    CHECK (penalty_type IS NULL OR penalty_type IN
        ('ob_lost', 'penalty_area', 'unplayable', 'other'))
);

CREATE INDEX idx_round_holes_round ON round_holes(round_id, hole_number);
CREATE INDEX idx_round_holes_club ON round_holes(tee_club_id);
```

### On `tee_accuracy = 'hit'`

Stored as `hit` rather than `fairway` because a par 3 has no fairway. It means
**found the intended target** — the fairway on a par 4 or 5, the green on a par
3 — and the UI labels the button accordingly. Naming the column's value after
the par-4 case would have made every par-3 row a small lie.

## 4. Derived, never stored

Computing these rather than storing them is what stops a round from disagreeing
with itself after an edit.

| Stat | Definition | Denominator |
|---|---|---|
| Fairways hit | `tee_accuracy = 'hit'` | par 4 and 5 holes only |
| Green in regulation | `strokes - putts <= par - 2` | holes with par and a score |
| Scrambling | missed GIR and `strokes <= par` | holes where GIR was missed |
| Sand save | `greenside_bunker` and `strokes <= par` | holes in a greenside bunker |
| Putts per hole | `sum(putts) / holes` | holes with putts recorded |
| Putts per GIR | `sum(putts where GIR) / GIR count` | greens hit |
| Score to par | `sum(strokes) - sum(par)` | completed holes |

Two things worth stating out loud, because both surprise people:

**Fairways exclude par 3s.** Including them would understate driving accuracy by
roughly a fifth, and no published statistic counts them.

**Deriving GIR from score and putts has one known false positive, and it is
worth accepting rather than fixing.** The formula treats "strokes that were not
putts" as "strokes taken to reach the green", which is right whenever the ball
finished on the green. It is wrong when the hole was finished from off it:

| Hole | Reality | `strokes - putts <= par - 2` | Verdict |
|---|---|---|---|
| Par 4, drive + chip-in, 0 putts | Never hit the green | `2 - 0 = 2 <= 2` → GIR | **false positive** |
| Par 4, missed green, chip, 1 putt | Missed, scrambled a par | `4 - 1 = 3 > 2` → miss | correct |
| Par 4, green in 2, 2 putts | Textbook GIR | `4 - 2 = 2 <= 2` → GIR | correct |

So holing out from off the green is credited as a green hit. Every app that
does not track shot by shot has this, because "did the ball stop on the
putting surface" is not recoverable from a score and a putt count. The
alternative is a per-hole "did you hit the green" toggle, which is one more tap
on every hole for an event that happens a handful of times a season — a bad
trade for a live scorecard. Revisit if Phase 7 ever tracks shots, where it
falls out for free.

Note that the *common* short-game hole — miss, chip, one putt — is classified
correctly, and is exactly what scrambling then measures.

## 5. Local-first live rounds

The problem: golf courses are where phones lose signal, and a round that cannot
save the front nine is worse than useless.

**The round is created client-side with a client-generated UUIDv7.** This is the
part that makes offline actually work — without it, a round begun out of signal
has no id to attach holes to. `POST /api/rounds` accepts an `id`, validates its
shape, and is idempotent: the same id twice is the same round, not two.

```
Start round      -> write to localStorage, POST when possible
Enter hole 1..18 -> write locally (instant), PUT when possible
                    failure -> stays queued, header shows the backlog
Finish           -> requires the queue to drain first
```

**Every write is an idempotent upsert** keyed on `(round_id, hole_number)`,
which is what makes blind retry safe. A queued hole replayed twice is the same
hole.

**The round is device-bound while in progress.** Start on a phone and you cannot
pick it up on a tablet mid-round. That is the honest cost of this design, and
the escape hatch is that each hole is pushed as it is entered — so once a round
completes, the server copy is authoritative and complete. Cross-device
hand-off would need the server to be the source of truth mid-round, which is
exactly what a dead zone makes impossible.

Also: **`navigator.wakeLock`** while a live round is open, so the phone stops
sleeping between shots. Released on completion and on tab hide.

## 6. API

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/rounds` | Start a round, or record a manual one whole. Accepts a client-supplied `id`; idempotent. |
| `GET` | `/api/rounds` | Paginated, newest `played_on` first. `?status=in_progress` finds a round to resume. |
| `GET` | `/api/rounds/{id}` | The round with all holes and its computed summary. |
| `PUT` | `/api/rounds/{id}` | Metadata only: date, notes, tee, status. Never holes. |
| `PUT` | `/api/rounds/{id}/holes/{n}` | Upsert one hole. The live path. Idempotent. |
| `PUT` | `/api/rounds/{id}/holes` | Upsert many. The manual path — one save for the grid. |
| `POST` | `/api/rounds/{id}/complete` | Finalise: stamps `completed_at`, sets status. |
| `POST` | `/api/rounds/{id}/abandon` | Rain, injury, nine-and-out. |
| `DELETE` | `/api/rounds/{id}` | Remove. |

Metadata and holes are separate endpoints for the same reason `PUT /clubs/{id}`
and `PUT /clubs/{id}/status` are: a stale form must not be able to change
something the user was not looking at.

Every round is scoped by `user_id`. Another user's round id returns **404, not
403** — matching the bag, since confirming an id exists leaks more than the
refusal is worth.

## 7. Screens

### Live — mobile, one hole at a time

```
┌─────────────────────────────┐
│ ← Hole 7           +3  (34) │   sticky: running total, vs par
│    Par 4 · 388 yds · SI 3   │
├─────────────────────────────┤
│         Score               │
│      [ − ]  5  [ + ]        │   stepper, opens on par
│                             │
│         Putts               │
│    [0] [1] [2] [3] [4] [5]  │
├─────────────────────────────┤
│  ▾ Detail                   │   collapsed by default
│                             │
│  Tee club   [ Driver  ▾ ]   │
│                             │
│           [ Long ]          │   accuracy, as a dispersion pad
│  [FL] [ L ] [FWY] [ R ] [FR]│   "FWY" reads "GREEN" on a par 3
│           [ Short ]         │
│           [Mishit]          │
│                             │
│  [ ] Fairway bunker         │
│  [ ] Greenside bunker       │
│  First putt   [ 22 ] ft     │
│  Penalties  [−] 0 [+]       │
├─────────────────────────────┤
│  ◀ Prev        Next ▶       │
└─────────────────────────────┘
   ⓘ 2 holes not yet saved        sync state, only when non-zero
```

Score and putts are two taps and reachable one-handed; everything else is
optional and behind the toggle. A hole can be left entirely blank.

### Manual — desktop grid

```
Course [ Sand Creek ▾ ]  Tee [ White ▾ ]  Date [ 2026-09-03 ]  ○ 18  ● Front 9

 Hole  Par  Yds  Score  Putts │ Club     Accuracy  F.Bunk  G.Bunk  1st Putt  Pen
   1    4   388   [ 5]  [ 2]  │ [Driver] [Right ]   [ ]     [x]     [ 18 ]   [0]
   2    3   165   [ 3]  [ 1]  │ [7 iron] [Hit   ]   [ ]     [ ]     [  9 ]   [0]
   …
 OUT   36        [41]  [15]

                                          [ ▸ Show detail columns ]
```

Detail columns collapsed by default: backfilling a season wants speed, a round
you paid attention to wants the record. One save for the whole grid.

## 8. Work this phase forces elsewhere

Each of these is a change to existing, working code — worth listing so none is
discovered halfway through.

- **`DELETE /clubs/{id}` must become conditional.** `BUILD_SPEC.md` §4 line 116
  predicted this; the endpoint is currently unguarded, so deleting a club today
  would strip it out of historical rounds. It should refuse once a club appears
  in any round, with a message steering to Retire. Retired clubs stay fully
  selectable and displayable — a 2024 round still has to render the 3-wood you
  have since sold.
- **`/api/rounds` and `/api/rounds/{}` join the API-key allow-list** in
  `internal/apikey/policy.go`, read-only. Without it a key cannot see rounds at
  all — the allow-list denies by default, which is the design.
- **The account export gains rounds, and `accountExportFormatVersion` goes
  1 → 2.** It currently carries profile, clubs, and courses; leaving rounds out
  would make "take your data" quietly untrue. The CSV archive gains
  `rounds.csv` and `round_holes.csv`, joined on the same name-not-id convention
  the other files use. Import needs the same non-destructive merge rule, keyed
  on `(played_on, course_name)`.
- **Deleting an account already cascades** through `user_id`, so rounds need no
  new handling there — worth verifying rather than assuming.
- **Course removal becomes lossier than it looks.** An administrator removing a
  course nulls `course_id` on every round played there. The snapshots mean the
  rounds survive intact; the removal-request screen should say so.

## 9. Open questions

Flagged rather than decided, because each is cheap now and expensive later.

1. **One in-progress round at a time?** Starting a second while one is open
   probably ought to prompt rather than silently create. Recommendation: allow
   only one, offer to resume or abandon.
2. **A course with an incomplete scorecard.** `par` is nullable, so a round can
   start on a tee with missing pars — but then GIR is unavailable for those
   holes. Recommendation: warn at the start screen, let the round proceed, and
   let par be filled in-round.
3. **Editing a completed round.** Needed — everyone mis-taps a putt count. The
   question is whether an edit should be visible as such. Recommendation: allow
   silent edits; this is a personal logbook, not a competition record.
4. **Multiplayer.** The schema above is solo: one `user_id` per round. Adding
   playing partners later means a `round_players` table and moving the per-hole
   rows under it — a real migration, not a column. Worth being sure solo is the
   right call before building.
5. **9-hole rounds on an 18-hole course** store `nine = 'front' | 'back'` and
   hole numbers 1–9 or 10–18. Confirm that back-nine rounds should keep their
   real hole numbers rather than renumbering to 1–9.

## 10. Suggested build order

Each step ends working, which is what keeps a half-finished phase useful.

1. Migration `00002_rounds.sql`, queries, sqlc — schema only.
2. `internal/round`: service, validation, derived stats, tests. No HTTP.
3. Endpoints, plus the allow-list change.
4. Manual entry — the desktop grid. Simpler, and it proves the API without any
   sync machinery.
5. Live entry — the mobile flow, then the local-first queue on top of it.
6. Round list and round summary screens.
7. Export/import, format version 2.
8. `docs/phase-3-test-checklist.md`, and fold the decisions into `BUILD_SPEC.md`.


## 11. What changed during the build

Four things came out differently from the plan above.

**The migration is 00017, not 00002, and this was found the hard way.** A
database that ran the original sixteen migrations still records every one of
their version numbers, so goose saw `00002_rounds.sql` as already applied and
skipped it **without a word**. The server started perfectly and the first round
failed with `no such table: rounds`. Renumbering above the old high-water mark
makes it apply on a database of either vintage. The claim in `00001_init.sql`
that "incremental migrations resume at 00002" was simply wrong, and now says so.

**Holes are pre-created when a round starts**, rather than appearing on first
write. That is the moment the snapshot has to be taken, and it means the client
has the pars before a single score exists - which both screens need in order to
draw anything.

**The hole upsert keeps its snapshots with COALESCE.** The plan said the
scoring fields replace outright, which they do; it did not say what happens to
par when a save omits it. Without the COALESCE, saving a putt count blanked the
par that every statistic depends on.

**A round can be reopened.** The plan had complete and abandon; abandoning by
accident needed to be undoable, and since edits to a finished round are allowed
anyway, reopening is the same idea named honestly.

Everything else was built as written, including the parts most likely to have
been quietly dropped: the client-supplied round id, the idempotent upsert, the
conditional club delete, the allow-list entry, and format version 2.
