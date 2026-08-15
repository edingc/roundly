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
- [ ] The **Bag** nav item highlights when you are on `/bag`.
