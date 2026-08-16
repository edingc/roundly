-- name: CreateRound :exec
INSERT INTO rounds (
    id, user_id, course_id, course_name, tee_id, tee_name, tee_color,
    course_rating, slope_rating, played_on, started_at, status, entry_mode,
    holes_intended, nine, notes, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- Scoped by user on every read. Another player's round id returns no rows,
-- which the service turns into a 404 rather than a 403: confirming that an id
-- exists leaks more than the refusal is worth. Same rule as the golf bag.
-- name: GetRound :one
SELECT * FROM rounds WHERE id = ? AND user_id = ?;

-- name: ListRounds :many
SELECT * FROM rounds
WHERE user_id = ?
ORDER BY played_on DESC, created_at DESC
LIMIT ? OFFSET ?;

-- name: CountRounds :one
SELECT COUNT(*) FROM rounds WHERE user_id = ?;

-- Several rounds may be open at once, so this is a list rather than a lookup.
-- A player who abandons one to weather and starts another the next day should
-- not have to tidy up before they can play.
-- name: ListRoundsByStatus :many
SELECT * FROM rounds
WHERE user_id = ? AND status = ?
ORDER BY started_at DESC, created_at DESC;

-- Metadata only. Holes are written through their own statement so that a stale
-- form cannot change something the user was not looking at.
-- name: UpdateRound :exec
UPDATE rounds
SET played_on = ?, notes = ?, updated_at = ?
WHERE id = ? AND user_id = ?;

-- name: SetRoundStatus :exec
UPDATE rounds
SET status = ?, completed_at = ?, updated_at = ?
WHERE id = ? AND user_id = ?;

-- name: DeleteRound :exec
DELETE FROM rounds WHERE id = ? AND user_id = ?;

-- name: ListRoundHoles :many
SELECT * FROM round_holes WHERE round_id = ? ORDER BY hole_number;

-- The live path writes one of these per hole, and the manual path writes
-- eighteen in a transaction. Both go through this same upsert, keyed on
-- (round_id, hole_number), which is what makes a queued write safe to replay:
-- the same hole sent twice is the same hole, not two.
-- name: UpsertRoundHole :exec
INSERT INTO round_holes (
    id, round_id, hole_number, par, yardage, stroke_index,
    strokes, putts, tee_club_id, tee_accuracy, first_putt_feet,
    fairway_bunker, greenside_bunker, penalties, penalty_type
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(round_id, hole_number) DO UPDATE SET
    -- The snapshots are server-owned: written once when the round starts, and
    -- kept unless a value is actually supplied. COALESCE is what lets a score
    -- edit leave them alone - without it, saving a putt count would blank the
    -- par the whole statistics layer depends on. Supplying one still works,
    -- which is how a hole missing its par gets filled in mid-round.
    par = COALESCE(excluded.par, round_holes.par),
    yardage = COALESCE(excluded.yardage, round_holes.yardage),
    stroke_index = COALESCE(excluded.stroke_index, round_holes.stroke_index),
    -- The scoring fields replace outright, NULL included: this is a PUT, the
    -- payload is the whole hole, and clearing a mis-tapped score has to work.
    strokes = excluded.strokes,
    putts = excluded.putts,
    tee_club_id = excluded.tee_club_id,
    tee_accuracy = excluded.tee_accuracy,
    first_putt_feet = excluded.first_putt_feet,
    fairway_bunker = excluded.fairway_bunker,
    greenside_bunker = excluded.greenside_bunker,
    penalties = excluded.penalties,
    penalty_type = excluded.penalty_type;

-- name: DeleteRoundHole :exec
DELETE FROM round_holes WHERE round_id = ? AND hole_number = ?;

-- Guards DELETE /clubs/{id}. A club that has been played has to keep its row,
-- or the rounds played with it lose the only record of what was in hand.
-- name: CountRoundHolesByClub :one
SELECT COUNT(*) FROM round_holes WHERE tee_club_id = ?;

-- Everything a backup needs, in one pass rather than a query per round.
-- name: ListAllRoundHolesByUser :many
SELECT round_holes.* FROM round_holes
JOIN rounds ON rounds.id = round_holes.round_id
WHERE rounds.user_id = ?
ORDER BY round_holes.round_id, round_holes.hole_number;

-- name: ListAllRounds :many
SELECT * FROM rounds
WHERE user_id = ?
ORDER BY played_on DESC, created_at DESC;
