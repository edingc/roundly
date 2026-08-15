# User profile manual test checklist

Automated coverage runs with `make check`. This is the manual pass for what a test suite
cannot judge — how it looks, and whether the security properties hold against a real browser
and a real `curl`.

```bash
rm -f roundly.db*
make build && JWT_SECRET=$(openssl rand -hex 32) ./bin/roundly
```

## Automated status

Verified by `go test ./...`:

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

### Appearance and responsiveness

- [ ] Walk all four sections in light and dark mode.
- [ ] At 390px wide: no horizontal page scrolling, the menu stays on screen, and the key list
      and forms stay usable.
