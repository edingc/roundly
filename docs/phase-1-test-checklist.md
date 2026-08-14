# Phase 1 manual test checklist

Automated coverage runs with `make check`. This is the manual pass for the things
a test suite cannot judge — how it looks, and the Google flow, which needs real
credentials.

Start from a clean database so the empty states are visible:

```bash
rm -f roundly.db*
make build && JWT_SECRET=$(openssl rand -hex 32) ./bin/roundly
```

## Automated status

Verified by `go test ./...` and a Playwright pass against the built binary:

- [x] Email/password signup, login, logout, and session persistence across reload
- [x] Refresh token rotation, with replay of a consumed token killing the session
- [x] A returning Google user resolves to the same account (unit-tested against a
      stubbed identity, since it needs no network)
- [x] Password signup then Google login on the same verified email resolves to
      one account; an *unverified* provider email is refused instead of linking
- [x] Course with two custom tees (custom name and color)
- [x] Same hole with a different par per tee — par 4 from the back, par 3 forward
- [x] Editing and deleting a course, tee, and hole, with cascades leaving no orphans
- [x] Only the creator can modify a course; others get a read-only view
- [x] Mobile (390×844) and desktop (1280×900) layouts, light and dark

## Manual pass

### Auth

- [ ] Sign up with email and password. You land on the course list signed in.
- [ ] Reload the page. You are still signed in.
- [ ] Sign out. Visiting `/courses` directly bounces you to `/login`.
- [ ] Log back in. Wrong password shows an error without saying whether the
      email exists.
- [ ] Sign up again with the same email. The email field shows a specific error.

### Google sign-in

Requires `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and `GOOGLE_REDIRECT_URL`.
With them unset, confirm the Google button is simply absent.

- [ ] "Continue with Google" reaches Google's consent screen.
- [ ] Approving it returns you to the app, signed in.
- [ ] Sign out and repeat. You get the *same* account, not a duplicate — check
      Settings shows one account with your original name.
- [ ] Cancel at the consent screen. You return to `/login` with a readable
      message rather than a stack trace.
- [ ] Sign in with a password account, then Settings → "Link Google account"
      using a Google account with the *same* email. It links to the one account,
      and Settings then shows sign-in as "Password + Google".
- [ ] Confirm both paths still work afterwards: sign out, sign in with the
      password; sign out, sign in with Google. Same account both times.

### Courses

- [ ] With no courses, the list shows the empty state and an add button.
- [ ] Add a course: step 1 name and address, step 2 two tees with distinct
      colors, step 3 the par/yardage grid.
- [ ] In the grid, set hole 1 to par 4 / 420 from the back tee and par 3 / 165
      from the forward tee. Leave the field — the border flashes to confirm.
- [ ] Reload. Both values are still there and still different.
- [ ] Type into several cells quickly without pausing between them. Every value
      survives. *(This is the specific bug that shipped broken once: saving one
      cell reloads the course, which used to wipe neighbouring unsaved edits.)*
- [ ] Check the Out / In / Total rows sum par and yardage per tee.
- [ ] Edit a stroke index. It persists across a reload.
- [ ] Rename a tee and change its color. The grid column header updates.
- [ ] Delete a tee. Its column and its par/yardage values go with it.
- [ ] Search the list by name, then by address, then by something with no match.
- [ ] Delete the course. It asks for confirmation first.

### Permissions

- [ ] In a second browser profile, sign up as a different user.
- [ ] That user sees the first user's course, marked read only, with no edit
      controls and non-editable grid inputs.

### Appearance and responsiveness

- [ ] Toggle dark mode from the header. It survives a reload with no white flash
      on load.
- [ ] Settings → Appearance → System follows the OS preference; changing the OS
      theme updates the app live.
- [ ] At a 390px-wide viewport: no horizontal page scrolling, the scorecard
      scrolls sideways on its own with the hole column pinned, and tap targets
      are comfortable.
- [ ] Check the login screen, course list, course detail, and settings in both
      light and dark at both mobile and desktop widths.

### PWA

- [ ] The browser offers to install the app.
- [ ] The installed app opens standalone with the Roundly icon.

Offline support is intentionally not part of Phase 1 — it arrives with the score
tracker in Phase 3.
