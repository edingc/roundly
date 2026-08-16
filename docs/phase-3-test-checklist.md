# Phase 3 — Rounds: manual test checklist

The automated tests cover the statistics, the snapshots, and the API. These are
the things a test cannot check: whether the thing is usable on a phone, in the
sun, between shots.

## Starting a round

- [ ] **New round** pre-fills your home course.
- [ ] Picking a course loads its tees; picking a tee highlights it.
- [ ] A course with an incomplete scorecard warns how many holes have no par, and
      still lets you start.
- [ ] A 9-hole course does not offer the 18/9 choice or ask which nine.
- [ ] On an 18-hole course, choosing 9 offers front and back.
- [ ] **Play now** lands on the hole-by-hole screen; **Enter a card** lands on the
      scorecard.

## Live entry — do this on a phone

- [ ] The hole header shows par, yardage, and stroke index from the course.
- [ ] Score opens on par, so the commonest entry is one tap.
- [ ] Putts above the score are disabled — you cannot putt more than you hit.
- [ ] Everything is reachable one-handed, and the running total stays visible
      while you scroll.
- [ ] The tee shot pad is visible **without** opening Add detail, and reads
      **Fairway** on a par 4 and **Green** on a par 3.
- [ ] Tapping Fairway records the hit; tapping it again clears it.
- [ ] **Add detail** reveals club, bunkers, first putt, and penalties.
- [ ] A penalty reason only appears once there is at least one penalty stroke.
- [ ] The screen does not sleep while the round is open. *(Leave it for two
      minutes without touching it.)*

### Offline — the part that matters

- [ ] Put the phone in airplane mode and enter three holes. Each one records
      instantly and the footer counts them as not yet saved.
- [ ] Turn the radio back on. The count drops to zero within twenty seconds
      without you doing anything.
- [ ] Enter a hole offline, force-quit the browser, reopen the round. **The hole
      is still there.**
- [ ] Offline, open a round you started earlier. It opens from the cached copy
      rather than erroring.
- [ ] Try to **Finish round** with holes still queued: it refuses and says so.
- [ ] Reconnect, finish, and confirm every hole reached the server.

## The rounds list

- [ ] Each row shows FIR, GIR, and Diff under the date.
- [ ] A round with no scores yet shows no stats strip at all, rather than three
      dashes.
- [ ] A round on a course with no published rating shows `Diff —`, and hovering
      it explains why.
- [ ] A half-finished round shows `Diff —` even though it has scores. *(A
      differential from a partial card is meaningless, not merely smaller.)*
- [ ] FIR reads `—` on a round where no tee shots were recorded.

## The scorecard

- [ ] Score, putts, and **Tee shot** are visible without opening the detail
      columns; **Show detail columns** adds club, bunkers, first putt, and
      penalties, and the table scrolls inside its own box.
- [ ] The footer shows the fairway count, and the columns stay aligned with the
      rows in both states.
- [ ] The page does not scroll sideways at 390px wide.
- [ ] Editing a cell marks the card unsaved; **Save scorecard** clears it.
- [ ] Clearing a score empties the box rather than writing a zero, and the hole
      drops out of the totals.
- [ ] Par and yardage stay put after saving a score. *(The snapshot must survive
      the edit.)*
- [ ] Totals at the foot match the summary card above.

## Statistics

*Enter a round you can check by hand.*

- [ ] Par 4, score 4, 2 putts → counts as a green in regulation.
- [ ] Par 4, score 4, 1 putt → **not** a GIR, and counts as a scramble.
- [ ] Par 3 tee shots are excluded from the fairway count. *(Fairways attempted
      should equal the number of par 4s and 5s where you recorded a tee shot.)*
- [ ] A greenside bunker on a hole you made par on counts as a sand save.
- [ ] A hole with no score still counts its tee shot toward accuracy.
- [ ] Out and In totals split at hole 9.

## Overview charts

- [ ] The putts and differential charts start their axis at **0**, so bar
      lengths are comparable rather than exaggerated.
- [ ] Score to par draws bars **below** the line for a round under par, and the
      axis is not clipped at zero.
- [ ] Greens and fairways are capped at 100% and drawn side by side, not
      stacked.
- [ ] "How the holes finished" stacks to the number of holes played in each
      round.

## Snapshots — the thing that cannot be repaired later

- [ ] Play a hole, then edit that hole's par on the **course** page.
- [ ] Reopen the round: **the round's par has not moved.**
- [ ] Ask an administrator to remove the course entirely. The round still shows
      the course name, the tee, and every par.

## Several rounds at once

- [ ] Start two rounds. Both appear under **In progress**.
- [ ] Each opens where it was left off.
- [ ] Deleting one leaves the other alone.
- [ ] Abandoning one moves it out of In progress but keeps its holes.
- [ ] A finished round can be reopened and edited.

## Nine-hole rounds

- [ ] A back nine shows holes **10 to 18**, not 1 to 9.
- [ ] Its stroke indexes are the back-nine ones from the course.
- [ ] The round rates against the nine-hole rating, not the eighteen-hole one.
      *(Check `course_rating` in the API response.)*
- [ ] A **back** nine takes the back-nine rating, not the front-nine one. *(Set
      the two to different numbers on a tee and check which one lands.)*

## Course ratings

- [ ] The control is under **Preferences**, offers Not set / Men's / Women's,
      and explains that it only chooses which published ratings a round records.
- [ ] It saves on touch, and a later profile save does not disturb it.
- [ ] Setting it to Women's and starting a round records the women's rating and
      slope; Men's records the men's.
- [ ] Leaving it unset records the men's ratings.
- [ ] Changing it afterwards does **not** move the rating on a round already
      played.
- [ ] A tee with no women's rating records no rating at all rather than
      substituting the men's.

## Clubs

- [ ] The tee club list includes retired clubs.
- [ ] A round played with a club still names it after the club is retired.
- [ ] Deleting a club that has been played is **refused**, and the message
      points at Retire.
- [ ] Deleting a club that has never been played still works.

## Your data

- [ ] Export the JSON: `format_version` is **2** and `rounds` is populated.
- [ ] Each round carries its own pars, not the course's current ones.
- [ ] The CSV archive contains `rounds.csv` and `round_holes.csv`.
- [ ] Import the file into a second account: the rounds arrive.
- [ ] Import the same file twice: the second time skips every round.

## API keys

- [ ] A read-only key can `GET /api/rounds` and `GET /api/rounds/{id}`.
- [ ] The same key gets 403 on `POST /api/rounds` and on any hole write.
