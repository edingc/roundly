# Phase 2 manual test checklist

Automated coverage runs with `make check`. This is the manual pass for the things
a test suite cannot judge — how it looks, and how the bag feels to edit one-handed.

Start from a clean database so the empty state is visible:

```bash
rm -f roundly.db*
make build && JWT_SECRET=$(openssl rand -hex 32) ./bin/roundly
```

## Automated status

Verified by `go test ./internal/club/...` and a driven pass against the built
binary:

- [x] A new club defaults to active; optional fields trim, and blank ones clear
- [x] Every status transition, in both directions, including un-retiring
- [x] `retired_at` records when the club left the bag and does not move if a
      retired club is retired again
- [x] The club ID survives every transition and every edit — the property Phase 3
      and Phase 4 depend on
- [x] Editing a club leaves its status alone
- [x] The 14th club is fine, the 15th is accepted and flagged `over_limit`, and
      benched and retired clubs do not count toward it
- [x] A second user sees an empty bag and gets 404 (not 403) on another user's
      club, for all of get, update, status, and delete
- [x] The schema rejects a club that is both retired and active
- [x] Club type, shaft flex, loft range, and status values are validated
- [x] Derived bag order puts the putter last despite its low loft
- [x] Expected carry and average dispersion round-trip, and clear when blanked
- [x] A putter is refused both, by field, on create and on update
- [x] Carry and dispersion bounds, including dispersion of zero being allowed
- [x] "Wedge" is accepted as a shaft flex and normalized from any casing
- [x] A club saves with nothing but a type and a label, everything else null
- [x] `distance_unit` defaults to yards, persists, and rejects anything else
      without changing the stored value

## Manual pass

### Building a bag

- [ ] With no clubs, `/bag` shows the empty state and an add button.
- [ ] Add a driver: type Driver, label "Driver", loft 10.5, a brand and model,
      a shaft and flex. It appears under **In the bag**.
- [ ] Add a club with only a type and a label. It saves — everything else is
      optional.
- [ ] Try to save with the label blank. The form refuses before sending.
- [ ] Enter 155 in the loft box. The server rejects it and the message lands on
      the loft field, not as a page-level error.
- [ ] Give an iron an expected carry and an average dispersion. The carry shows
      on the club's first line in brand green; the dispersion shows as `±n yds`
      on the meta line.
- [ ] Set the type to Putter. Loft, flex, expected carry, and average
      dispersion all disappear from the form, leaving type, label, brand,
      model, shaft, and notes.
- [ ] Save that putter. It saves cleanly — the form sends null for each hidden
      field rather than the values it was holding.
- [ ] Edit an existing iron that has a loft and distances and re-type it as a
      putter. Same result: it saves, and none of those values come back if you
      switch it to an iron again.
- [ ] A putter row shows no loft and no flex, even if the club was created
      before those fields were hidden.
- [ ] Pick "Wedge (W)" as the shaft flex on a wedge. It saves and shows in the
      meta line.
- [ ] Add enough clubs to reach 14. The counter reads **14 / 14** in green with
      no warning.
- [ ] Add a 15th. It is accepted, the counter turns amber and reads **15 / 14**,
      and the warning banner explains the penalty.

### Moving clubs around

- [ ] "Take out" on a club moves it to **Not in the bag**, and the counter drops.
- [ ] Its action changes to "Put in bag". Clicking that returns it, and the
      counter goes back up.
- [ ] "Retire" a club. It leaves both lists and the **Retired** section appears
      with a count.
- [ ] Expand **Retired**. The club is there with the note about past rounds.
- [ ] "Unretire" it. It lands in **Not in the bag**, not straight into the bag.
- [ ] "Put in bag" on a retired club puts it in the bag in one step.
- [ ] Edit a retired club's loft. It stays retired after saving.
- [ ] Reload after each of the above. Everything is where you left it.

### Deleting

- [ ] "Delete" asks for confirmation and says to retire instead if the club has
      been played.
- [ ] Cancel leaves the club alone; confirming removes it from every section.

### Ordering

- [ ] With a full bag, confirm the order reads driver → woods → hybrids → irons
      → wedges → putter, with the putter last despite its low loft.
- [ ] Add a 3 wood after a 5 wood. The 3 wood sorts *above* it, by loft.

### Distance units

- [ ] Settings → Distances offers Yards and Meters, starting on Yards.
- [ ] Switch to Meters. The bag's carry and dispersion, the scorecard grid, and
      the tee totals all convert; loft stays in degrees.
- [ ] The scorecard's distance row header reads `M`, not `Yds`.
- [ ] A tee's total beside its name matches the `TOT` cell at the end of its
      scorecard row. *(These round differently if the total is converted rather
      than summed after conversion — that mismatch is the bug this catches.)*
- [ ] Type a hole distance in metres. It saves, and still reads back as what you
      typed.
- [ ] Switch back to Yards. Every number is the original one — nothing was
      rewritten by the switch.
- [ ] Export a course while in Meters. The file contains yards.
- [ ] Add a club with a carry while in Meters, then switch to Yards and confirm
      the club shows the equivalent yard figure.
- [ ] In the club form, the unit sits inside the carry and dispersion inputs
      against the right edge, and follows the setting (`yds` / `m`).
- [ ] Clicking that unit puts the caret in the input rather than doing nothing.
- [ ] Type the largest allowed carry. The digits do not run under the unit.

### The yardage chart

Reachable from **Yardage chart** on the bag screen, at `/bag/chart`.

- [ ] The button is absent when the bag is empty and appears once a club exists.
- [ ] The sheet lists every club in the bag except the putter, longest carry
      first, with the date and your name in the header.
- [ ] Subtract any two carries in the Carry column. The difference matches the
      **Gap** printed on the upper row of the pair.
- [ ] A club with no expected carry sits at the bottom with a dotted line in
      place of a number, and a note above the sheet says so.
- [ ] Give a club a carry that outranks the club above it in the bag. It moves up
      the chart, and the gaps stay positive.
- [ ] Tick **Include clubs not in the bag**. Benched clubs appear in carry order
      and the gaps recompute around them. Retired clubs never appear.
- [ ] A bag holding only a putter says there is nothing to chart, in a neutral
      tone rather than red, and the Print button is disabled.
- [ ] A bag where no club has a carry prints every line blank, and the Loft, Gap,
      and Spread columns disappear rather than printing empty.
- [ ] With those columns gone, the key under the table stops explaining them.
- [ ] Switch to **Pocket card**. The card is 3.5in wide with a dashed cut border
      and lists club and carry only.

Then print (or open the print preview) in **both** light and dark mode:

- [ ] The header, nav, format controls, and the "cut along the dashed line" hint
      are all absent from the page.
- [ ] The sheet is black on white in both themes. *(This is the one that catches
      a themed page printing white text onto white paper.)*
- [ ] Print with "background graphics" **off** — the default. Every rule and
      number is still there.
- [ ] The pocket card keeps its dashed border.
- [ ] Switch to Meters first. The carries, spreads, and gaps convert; loft stays
      in degrees, and the header reads "distances in metres".

### Privacy

- [ ] In a second browser profile, sign up as a different user. Their bag is
      empty — the first user's clubs are not visible anywhere.
- [ ] There is no read-only view of someone else's bag, unlike courses.

### Appearance and responsiveness

- [ ] Toggle dark mode. The amber over-limit banner and counter stay readable.
- [ ] At a 390px-wide viewport: no horizontal page scrolling, the row action
      buttons wrap rather than overflow, and they are comfortable to tap.
- [ ] Check the bag list, the add dialog, and the delete confirmation in both
      light and dark at mobile and desktop widths.
- [ ] At 390px, the yardage chart's table scrolls sideways *inside* the sheet.
      The page itself must not scroll sideways.
- [ ] The **Bag** nav item highlights when you are on `/bag`.
