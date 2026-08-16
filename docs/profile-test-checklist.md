# User profile manual test checklist

Automated coverage runs with `make check`. This is the manual pass for what a test suite
cannot judge — how it looks, and whether the security properties hold against a real browser
and a real `curl`.

```bash
rm -f roundly.db*
make build && JWT_SECRET=$(openssl rand -hex 32) ./bin/roundly
```

## Automated status

Verified by `go test ./...`, including `internal/server/router_test.go`, which drives the real
handler because these guarantees come from middleware *order* and a stubbed router would pass
while the running server was open:

- [x] Every write verb on every write route is refused for an API key, **and** no row is created
- [x] `/api/auth/*` is unreachable by a key, including `/api/auth/../auth/me`, `/api//auth/me`,
      `/api/auth/me/`, and `/api/./auth/me`
- [x] `/api/account/*` is unreachable, and the response body leaks no account data
- [x] A key reaches only the allow-listed reads; a GET that exists but is off the list is 403
- [x] User A's key cannot see user B's clubs (404, matching the bag's existing rule)
- [x] Revoked, expired, and unknown keys all return an **identical** 401
- [x] The rate limit trips at the configured count, with `Retry-After` and reset headers
- [x] A signed-in session is completely unaffected — writes, auth routes, and account routes all
      still work, with no rate limiting
- [x] A key-authenticated request resolves to the right user through `auth.MustUserID`
- [x] Avatar processing: square output, size caps, sniffing over Content-Type, EXIF stripped,
      all eight orientations, transparency flattened, key format validated
- [x] Import twice creates nothing the second time; `format_version` missing or too new is 400

## Manual pass

### The menu and the page

- [ ] The header shows your avatar (or initials) and name; **Settings** is gone from the nav.
- [ ] The menu opens on click, closes on an outside click, and closes on Escape with focus
      returning to the button.
- [ ] An old `/settings` bookmark lands on `/profile`.

### Profile information

- [ ] Save with only a display name. It works — every other field is optional.
- [ ] Clear the display name and save. Refused, on that field.
- [ ] Fill in first and last name. The header initials change from the display name's to `CE`.
- [ ] Set a home course. It survives a reload and shows the course's name.
- [ ] Paste a name containing a newline. It is stored on one line.
- [ ] Joined date matches the account's creation date.

### Photo

- [ ] Upload a landscape JPEG. It appears square, in the header and on the page, immediately.
- [ ] Upload a **portrait photo taken on a phone**. It is upright, not sideways. *(This is the
      one that catches a missing EXIF rotation.)*
- [ ] Upload a transparent PNG. The transparent parts are white, not black.
- [ ] Try a file over 4 MB. Refused, with the message on the photo field.
- [ ] Rename a PDF to `.png` and upload it. Refused — the bytes are sniffed, not the name.
- [ ] Note the photo URL, then upload a different photo. The old URL 404s.
- [ ] Remove the photo. It reverts to initials and the URL 404s.

### Email

- [ ] Change the email with the wrong password. Refused on the password field.
- [ ] Change it with the right password. It succeeds, you stay signed in *here*, and a second
      browser signed into the same account is signed out.
- [ ] Try an address another account already uses. Refused on the email field.
- [ ] Save the same address unchanged. Succeeds and does nothing.

### Preferences

- [ ] Switch to **Metric**. The bag, the scorecard, and the yardage chart all follow.
- [ ] Switch back to **Imperial**. Every number is the original one.
- [ ] Theme and stroke-index label still work as they did on the old settings page.

### Download my data

- [ ] Download JSON. It contains your profile, clubs, and courses, and the avatar as base64.
- [ ] Search it for `password_hash`, `refresh_token`, `provider_subject`, `avatar_key`. None
      appear.
- [ ] Download CSV. The ZIP holds **six** files, including `hole_tee_details.csv` with par and
      yardage.
- [ ] Name a club `=HYPERLINK("http://evil","x")` and export CSV. The cell opens in a
      spreadsheet as text, prefixed with an apostrophe — not as a live formula.
- [ ] Restore your own JSON file. It reports everything skipped and adds nothing.
- [ ] Restore it into a second account. Clubs and courses appear; the email does **not** change.
- [ ] Restore a file with `format_version` set to 99. Refused, with a message about the version.

### API access

- [ ] Create a key. The secret is shown once, with a warning, and copy works.
- [ ] Reload. The key is listed by prefix only; the full secret is nowhere.
- [ ] With `curl`, the key reads `/api/me`, `/api/clubs`, and `/api/courses`.
- [ ] `curl -X POST` with the key on any endpoint returns 403 `api_key_read_only`.
- [ ] `curl` the key against `/api/account/export` and `/api/auth/me`. Both 403. *(This is the
      one that matters most — the export is a GET.)*
- [ ] Exceed the rate limit. 429 with `Retry-After`.
- [ ] Revoke a key. Confirmation is required; afterwards the key returns 401.
- [ ] Check the server log. It records the key's prefix and never the secret.

### Courses are unowned

- [ ] Sign in as a second user and edit a course the first one added — name, tees, and a
      yardage. All allowed. There is no "read only" badge anywhere.
- [ ] The course detail page offers **Request removal**, not Delete.
- [ ] Send a request. It reports back that the administrator has been told.
- [ ] Try to request removal of the same course again. Refused — one pending request at a time.

### Administration

Start the server with `ADMIN_EMAIL=you@example.com`.

- [ ] Only that account sees **Administration** in the user menu.
- [ ] `/admin` as anyone else redirects away; `curl` against it returns 403.
- [ ] The queue shows the course, who asked, when, and why.
- [ ] **Keep the course** clears the request and leaves the course alone.
- [ ] **Remove it** asks for confirmation, then deletes the course.
- [ ] After a removal, the course is gone but the request record survives with the course name
      it was about. *(Check the database; this is the audit trail.)*
- [ ] With `ADMIN_EMAIL` unset, nobody is an administrator: no menu entry, and both the queue
      and `DELETE /api/courses/{id}` return 403 for everyone.

### Deleting an account

- [ ] The danger zone says plainly that courses stay in the directory.
- [ ] The confirm button stays disabled until the email address is typed exactly.
- [ ] A wrong password is refused and the account survives.
- [ ] After deleting: signed out, the email can be used for a fresh signup, and the clubs, photo,
      and API keys are gone.
- [ ] **The course you added is still there, with no attribution** — and another player who had
      it as their home course still has it. *(This is the cross-user damage the design exists to
      prevent.)*

### Appearance and responsiveness

- [ ] Walk all four sections in light and dark mode.
- [ ] At 390px wide: no horizontal page scrolling, the menu stays on screen, and the key list
      and forms stay usable.
