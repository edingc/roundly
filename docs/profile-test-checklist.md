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
- [ ] Set a home course by typing a course **name**. Results show each course's town beneath it.
- [ ] Search by **town** instead ("marne"). The courses there come back.
- [ ] Pick one with the arrow keys and Enter, without touching the mouse.
- [ ] Save, reload. The choice survives and shows both its name and its town.
- [ ] Press Clear, then save. The home course is unset rather than left as it was.
- [ ] Type a term nothing matches. The list says so instead of going blank.
- [ ] On the courses screen, the home course is first — above a pinned course — with a blue
      **Home** chiclet. Pin it too and both chiclets show.
- [ ] Sign in as someone else. That course is back in its name position, with no chiclet.
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

### Optional fields

- [ ] First name, last name, city, state, and country all say **Optional** *inside* the box,
      not in a caption under it, and the word disappears as soon as you type.
- [ ] Home course says `Optional — search by name or town`, and drops the "Optional" once a
      course is picked.
- [ ] Display name, which is required, says nothing.

### Photos

*Run these with the network tab open.*

- [ ] The `avatar_url` in `/api/auth/me` carries `?exp=…&sig=…`.
- [ ] Pasting that full URL into a new tab shows the image.
- [ ] Stripping the query string off it returns **404**, not 403 and not the image. *(This is
      the old, permanent URL; it must be dead.)*
- [ ] Changing one character of `sig` returns 404.
- [ ] The response has `Cache-Control: private`, `X-Robots-Tag: noindex`, and
      `Referrer-Policy: no-referrer`.
- [ ] Uploading a new photo changes the path, and the previous URL 404s immediately.

### Confirming an email address

*Needs `MAIL_FROM` plus a transport. With mail unset, none of this should appear at all.*

- [ ] A fresh signup lands on **Confirm your email** and cannot reach courses, the bag, or the
      profile.
- [ ] **Send the link again** delivers a second email, and the first link then fails.
- [ ] Opening the link in a **different browser** — signed out — still confirms it.
- [ ] Opening the same link twice: the second time says the link did not work.
- [ ] Back in the original tab, **I have confirmed it** lets you into the app.
- [ ] Changing the email address un-confirms the account and mails a link to the new address.
- [ ] With mail unconfigured, signup goes straight into the app, exactly as before.

### Two-factor

- [ ] The two-factor card is absent when mail is unconfigured.
- [ ] On a Google-only account it explains that there is no password to guard.
- [ ] On an unconfirmed account it refuses and says to confirm the address first.
- [ ] A wrong current password is refused.
- [ ] Turning it on, then signing out and back in: the password step is followed by a code step.
- [ ] A wrong code is refused; five wrong codes kill the challenge so even the right one fails.
- [ ] **Trust this browser** ticked, then sign out and in again: no code.
- [ ] The same account in a private window: still asked for a code.
- [ ] The remembered browser appears under **Remembered browsers**, tagged *This browser*.
- [ ] Forgetting it means the next sign-in asks for a code again.
- [ ] Changing the password forgets every remembered browser.
- [ ] Turning two-factor off asks for the password, and sign-in goes straight through afterwards.

### Where an unauthenticated visitor lands

*Test the first two in a browser that has never signed in — a private window is
the quickest way.*

- [ ] A brand-new browser opening the app lands on **Sign up**, not Sign in.
- [ ] Signing up, then signing out, then reopening the app lands on **Sign in**.
      *(Having an account is not a reason to be asked to make another.)*
- [ ] Letting the session expire, or having it revoked from another device,
      also lands on **Sign in**.
- [ ] `/login` and `/signup` still work when typed directly, and each links to
      the other.
- [ ] Deleting the account lands on **Sign up**, and reopening the app after
      that still offers Sign up.

### Course ratings

- [ ] The control is in **Preferences**, not Personal information.
- [ ] It saves the moment you touch it — there is no Save button in that section.
- [ ] Changing it and then reloading the page keeps the new value.
- [ ] Change it, then save the **profile** form above: the rating setting is
      unchanged. *(The two are written by different endpoints precisely so that
      saving a name cannot disturb it.)*
- [ ] Change the units: the rating setting is unchanged.
- [ ] Setting it back to **Not set** sticks, and rounds then use the men's
      ratings.

### Section order

- [ ] The page reads: Personal information, Preferences, Sign-in & security, Your data,
      API access, Danger zone.
- [ ] Email and password are in the same section, email first.
- [ ] Changing the email still signs out other devices and still demands the current password —
      the move was cosmetic and must not have altered either.

### Appearance and responsiveness

- [ ] Walk all four sections in light and dark mode.
- [ ] At 390px wide: no horizontal page scrolling, the menu stays on screen, and the key list
      and forms stay usable.
