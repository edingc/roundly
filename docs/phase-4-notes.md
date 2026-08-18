# Phase 4 — Stats Dashboard: notes so far

The overview screen is in and is the app's landing page. This records what was
decided while building it, and what is deliberately still missing.

## Decisions

**Nothing is stored.** Every figure is computed from rounds on each request, the
same rule a single round's summary follows. Correcting a scorecard moves the
overview with it because there was never a second copy to go stale. The cost is
two queries and a pass over a few hundred rows.

**Only completed rounds count.** An in-progress round would drag every average
down by the holes not yet played, and an abandoned one is a round the player
decided not to have. Both still exist and are still readable; they are simply
not scores.

**Scoring averages need a *full* card.** A round with holes left blank is not a
score, and a nine-hole card cannot be averaged with an eighteen. Accuracy and
putting statistics are less fussy: they count whatever was recorded, because a
fairway you hit on a hole you picked up is still a fairway you hit.

**The window changes the question, not the presentation.** "Last 5" and "all
time" are different figures. The server takes the window rather than sending
everything and letting the client slice.

**The handicap is the one figure the window does not touch.** An index is defined
over the last twenty rated rounds, so narrowing the screen to "last 5" narrows
every average and leaves the handicap and anti-cap where they are. This was
wrong at first, in two ways that shared a cause - the differentials were
gathered after the window had been applied. "Last 5" computed an index from five
rounds through the short-record table, and a stretch of rounds on unrated
courses shrank the record rather than pushing it further back. The twenty is
twenty *rated* rounds, not the rated ones among the last twenty played, and it
is now gathered independently of the window in `differentialRecord`.

**An unoffered window is a 400, not a clamp.** A client asking for something
this endpoint does not do should be told, not quietly answered a different
question.

## The handicap, and why it says "unofficial"

The score differential is `(113 / slope) x (score - rating)`, and both inputs
are snapshotted on the round, which is exactly why 4e insisted on capturing
rating and slope before anything used them.

**The index follows the World Handicap System's table**, reproduced rather than
approximated: three rounds average the single best minus 2.0, nine average the
best three, twenty average the best eight. "Best 8 of 20" is only true once
there are twenty, and a new player given that formula would get a number wildly
out.

**The anti-cap is the mirror**: the average of the *worst* twelve differentials
of the last twenty. Where the index describes a good day, this describes a bad
one, and the gap between them is the honest measure of consistency. Shown beside
the index rather than on its own, because neither says much alone.

**Both are computed from gross scores, and both say so.** The World Handicap
System uses an *adjusted* gross score, capping each hole at net double bogey.
Computing that cap needs a course handicap, which needs an index, which is what
the calculation is trying to produce; the real system resolves the circularity
iteratively across a whole scoring record. Using the gross score means a blow-up
hole counts in full and both figures read slightly high.

For a personal logbook that is the right trade - the trend is honest and the
arithmetic is inspectable - and it would be the wrong trade for anything
postable. The API carries `unofficial: true` so the caveat cannot be lost
between the server and the screen.

## Charts

**Recharts**, chosen once it was clear this screen would grow many charts rather
than four. The first pass hand-rolled SVG on the usual reasoning - a polyline
and two labels do not justify a dependency - and that reasoning stops holding
the moment the fifth chart wants a legend, a second axis, and a tooltip. The
per-chart cost dominates the one-time cost.

It cost **115 KB gzipped**, taking the bundle from 153 KB to 268 KB, which is
the largest single dependency this app has taken. Split into its own chunk via
`manualChunks`, so shipping an app update re-downloads the app and not the
chart library with it - which matters here because the service worker precaches
every asset, so a changed filename is a changed download for everybody.

**Colours are CSS variables, not props.** `--chart-grid`, `--chart-series-1`
and the rest are declared in `index.css` under `:root` and `.dark`, and Recharts
writes them straight into SVG attributes. A chart therefore follows a theme
change without re-rendering, and - more to the point - no chart can *forget* to
be theme-aware. Threading resolved hex values down from a hook would have meant
every future chart remembering to do it, and one that forgot would be invisible
in dark mode.

**Bars, not lines.** The first pass drew line charts and they were wrong: a
round is a discrete event - you shot an 84, then an 89 - and a line between two
rounds draws a path through scores that were never played. Every per-round
chart is now a bar. The line wrapper was written, superseded, and deleted
rather than kept for later; a line will be right for something genuinely
continuous, such as a handicap index between rounds, and is forty lines when
that arrives.

**Stacking is used exactly once, and the restraint is the point.** "How the
holes finished" stacks eagles, birdies, pars, bogeys, and doubles-or-worse,
which is honest because those five *partition* the holes: the bar height is the
round. Greens and fairways are deliberately **grouped rather than stacked** -
they are percentages of different denominators, so a stack would total 140% and
mean nothing. The `stacked` prop documents that rule where somebody reaching
for it will read it.

**One wrapper plus a card:** `BarCompare` and `ChartCard`. `BarCompare` already
does horizontal layout for what the next charts want - carry by club, scoring by
par, putts by distance band - since "Pitching Wedge" does not fit under a
vertical bar. `ChartCard` holds the title and the empty state, so "nothing to
plot" is decided once rather than in every chart.

**Series keys are typed as Recharts' own `DataKey<T>`.** `keyof T & string`
type-checks under `tsc --noEmit` and fails under `tsc -b`, which is what the
build actually runs; using the library's type means a mistyped key is a compile
error rather than a silently empty line.

**Bar axes include zero, and the default enforces it.** A bar encodes its value
as a length, so an axis starting partway up lies about the ratio between bars:
putts running 29 to 36 on an axis beginning at 28 makes the worst round look
eight times the best. A line survives truncation because it encodes position; a
bar does not.

The default is `min(0, dataMin)` rather than a flat zero, because two measures
here legitimately go negative - a score under par, and a differential from
shooting below the course rating - and a hard floor would silently clip them.
The only override in use caps percentages at 100.

**Nulls are plotted as gaps, not zeroes.** A round that recorded no tee shots
has no fairway percentage, and drawing that as 0% would show a collapse that
never happened.

## Still missing

- **Per-club driving statistics.** The data is there - every hole records its
  tee club and where the shot finished - and nothing reads it yet. This is the
  headline Phase 4 item and the reason `expected_carry` and
  `average_dispersion` exist on a club.
- **Approach and putting by distance.** `first_putt_feet` is recorded and
  unused; make-percentage by distance band is the obvious first cut.
- **Scoring by par.** Average on par 3s, 4s, and 5s separately, which is usually
  where a player's real weakness shows.
- **Comparison against the bag's expected carry**, which is what 4's club
  distance fields were added for.
- **No handicap on the round itself.** A round shows its differential in the
  API but the scorecard does not display it.
- **The score breakdown is only charted, not tabulated.** `Summary.Scores` is on
  every round and the scorecard page does not show it, though "3 birdies, 8
  pars, 6 bogeys" is the line most people would read first.
