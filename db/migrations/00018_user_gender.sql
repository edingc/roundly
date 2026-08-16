-- +goose Up
-- +goose StatementBegin

-- Which set of course ratings applies to this player.
--
-- Course and slope ratings are published in two sets, because that is how they
-- are computed: the same physical tee markers rate differently against the
-- men's and women's scratch and bogey golfer models, so a scorecard prints both
-- for one tee. The tees table has carried both pairs since migration 3; nothing
-- until now chose between them, and every round silently took the men's.
--
-- Nullable, and unset means the men's ratings - which is exactly what the code
-- did before this column existed, so no existing round changes meaning. It sits
-- with the rest of the optional profile: somebody who does not want to answer
-- gets the same behaviour they already had.
--
-- Named for the rating set rather than for the person, in the sense that its
-- only use is picking a column. Nothing else in the app reads it.
ALTER TABLE users ADD COLUMN gender TEXT
    CHECK (gender IS NULL OR gender IN ('men', 'women'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN gender;
-- +goose StatementEnd
