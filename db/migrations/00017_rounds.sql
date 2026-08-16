-- +goose Up
-- +goose StatementBegin

-- Rounds: the thing the course directory and the golf bag were built for.
--
-- Numbered 17 rather than 2, and the gap is deliberate. The sixteen migrations
-- that preceded 00001_init were squashed into it, but a database that ran the
-- original chain still records every one of their version numbers - so a new
-- migration numbered 2 would be seen as already applied and silently skipped.
-- The server would start, and the first round would fail with "no such table".
-- Starting above the old high-water mark is what makes this apply on a database
-- of either vintage.
--
-- The design decision that shapes every column here is that a round snapshots
-- the course rather than pointing at it. Courses are shared reference data that
-- anyone signed in may correct, and an administrator may remove outright. A
-- round that merely referenced one would silently restate what you shot against
-- the moment a stranger fixed a par - your greens-in-regulation percentage
-- would move, your scoring average would move, and nothing would say so.
--
-- It is also the one mistake in this feature that cannot be repaired later: the
-- values as they stood on the day are simply gone once the course changes. So
-- par, yardage, and stroke index are copied onto each hole, and the course
-- name, tee, rating, and slope onto the round.
--
-- Rounds are solo and always will be. There is no round_players table and no
-- plan for one: this is a personal logbook, not a social product.
CREATE TABLE rounds (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Nullable link, snapshotted name. Removing a course must not remove the
    -- rounds played there.
    course_id TEXT REFERENCES courses(id) ON DELETE SET NULL,
    course_name TEXT NOT NULL,
    tee_id TEXT REFERENCES tees(id) ON DELETE SET NULL,
    tee_name TEXT NOT NULL,
    tee_color TEXT,

    -- Stored even though handicaps are out of scope for this phase, because
    -- they are the inputs to a WHS score differential. Captured now, an index
    -- is arithmetic later; skipped now, it is impossible for every round
    -- already played.
    course_rating REAL,
    slope_rating INTEGER,

    -- A local calendar date ('2026-09-03'), not a timestamp: nobody's round is
    -- "on the 3rd in UTC". Separate from created_at because a round played in
    -- September may well be typed in during February.
    played_on TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,

    status TEXT NOT NULL DEFAULT 'in_progress',
    entry_mode TEXT NOT NULL,
    -- 9 or 18. What the player set out to do, which is not always what they
    -- finished - an abandoned round keeps its intent.
    holes_intended INTEGER NOT NULL,
    -- Which nine, when playing nine of an eighteen-hole course. NULL for a full
    -- round. A back nine keeps hole numbers 10-18 rather than renumbering: the
    -- card says 10, the stroke indexes are the back-nine ones, and renumbering
    -- would make a round unreadable against the scorecard it was played on.
    nine TEXT,

    notes TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,

    CHECK (status IN ('in_progress', 'complete', 'abandoned')),
    CHECK (entry_mode IN ('live', 'manual')),
    CHECK (holes_intended IN (9, 18)),
    CHECK (nine IS NULL OR nine IN ('front', 'back')),
    -- A completed round has a completion time and one that has not is missing
    -- it. The same both-or-neither rule course_removal_requests applies to its
    -- resolution, and for the same reason: the pair cannot drift.
    CHECK ((status = 'complete') = (completed_at IS NOT NULL))
);

-- The round list, newest first.
CREATE INDEX idx_rounds_user_played ON rounds(user_id, played_on DESC);
-- Finding rounds to resume. Several may be open at once - a player who starts a
-- round, abandons it to weather, and starts another the next day should not be
-- made to tidy up before they can play.
CREATE INDEX idx_rounds_user_status ON rounds(user_id, status);
CREATE INDEX idx_rounds_course_id ON rounds(course_id);

CREATE TABLE round_holes (
    id TEXT PRIMARY KEY,
    round_id TEXT NOT NULL REFERENCES rounds(id) ON DELETE CASCADE,
    hole_number INTEGER NOT NULL,

    -- Snapshots. par is nullable because a course in the directory may have an
    -- incomplete scorecard: a hole with no par is left out of the statistics
    -- that need one rather than blocking the round.
    par INTEGER,
    yardage INTEGER,
    stroke_index INTEGER,

    -- NULL strokes means the hole was not completed: picked up, conceded, ran
    -- out of light. Distinct from 0, which is not a score anybody shoots.
    strokes INTEGER,
    putts INTEGER,

    -- ON DELETE SET NULL is the backstop only. Deleting a club that appears in
    -- a round is refused outright by the API, which steers to Retire instead :
    -- a club that leaves the bag has to keep its row precisely so that rounds
    -- played with it stay readable.
    tee_club_id TEXT REFERENCES clubs(id) ON DELETE SET NULL,

    -- Where the tee shot finished, relative to where it was aimed. 'hit' rather
    -- than 'fairway' because a par 3 has no fairway: it means the intended
    -- target was found, which is the fairway on a par 4 or 5 and the green on a
    -- par 3. Naming the value after the par-4 case would make every par-3 row a
    -- small lie.
    tee_accuracy TEXT,

    -- Feet, not yards. It is how putting is always described, and the app's
    -- yards/metres preference does not sensibly apply to it.
    first_putt_feet INTEGER,
    fairway_bunker INTEGER NOT NULL DEFAULT 0,
    greenside_bunker INTEGER NOT NULL DEFAULT 0,
    penalties INTEGER NOT NULL DEFAULT 0,
    -- The count is what feeds statistics; the reason is what makes a round
    -- readable a year later. Optional, because in the moment nobody wants to
    -- categorise their misfortune.
    penalty_type TEXT,

    UNIQUE(round_id, hole_number),
    CHECK (hole_number BETWEEN 1 AND 18),
    CHECK (strokes IS NULL OR strokes BETWEEN 1 AND 20),
    CHECK (putts IS NULL OR putts BETWEEN 0 AND 10),
    -- You cannot putt more times than you hit the ball.
    CHECK (strokes IS NULL OR putts IS NULL OR putts <= strokes),
    CHECK (first_putt_feet IS NULL OR first_putt_feet BETWEEN 0 AND 200),
    CHECK (penalties BETWEEN 0 AND 10),
    CHECK (tee_accuracy IS NULL OR tee_accuracy IN
        ('hit', 'left', 'far_left', 'right', 'far_right', 'long', 'short', 'mishit')),
    CHECK (penalty_type IS NULL OR penalty_type IN
        ('ob_lost', 'penalty_area', 'unplayable', 'other'))
);

CREATE INDEX idx_round_holes_round ON round_holes(round_id, hole_number);
-- Phase 4 wants driving accuracy grouped by club, which reads this way round.
CREATE INDEX idx_round_holes_club ON round_holes(tee_club_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_round_holes_club;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_round_holes_round;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS round_holes;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_rounds_course_id;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_rounds_user_status;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_rounds_user_played;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS rounds;
-- +goose StatementEnd
