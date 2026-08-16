/**
 * The user profile: identity, preferences, sign-in, data, and API access.
 *
 * Replaces the old settings page. The sections are deliberately distinct cards
 * rather than one long form, because they answer different questions and are
 * visited for different reasons.
 *
 * They are ordered by how often somebody comes back to them, and grouped by
 * what a thing *does* rather than by what it looks like. The email address is
 * the case that decides both: it reads like a profile field and behaves like a
 * password, so it sits with the password.
 *
 *   1. Personal information — name, photo, home course, where you play
 *   2. Preferences          — units, theme, labels
 *   3. Sign-in & security   — email, password, two-factor, Google
 *   4. Your data            — export and restore
 *   5. API access           — personal read-only keys
 *   6. Danger zone          — account deletion
 */
import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError, api, setSession } from '../lib/api'
import { useAuth, useDistanceUnit } from '../lib/auth'
import { usePreferences, type StrokeIndexLabel } from '../lib/preferences'
import { useTheme, type Theme } from '../lib/theme'
import { countrySuggestions, regionSuggestions } from '../lib/places'
import type { DistanceUnit, Gender } from '../types'
import { Avatar } from '../components/Avatar'
import { CourseSearchField, type SelectedCourse } from '../components/CourseSearchField'
import { ApiAccessSection } from '../components/ApiAccessSection'
import { DataSection } from '../components/DataSection'
import { DangerZone } from '../components/DangerZone'
import { EmailVerificationNotice, TwoFactorSection } from '../components/TwoFactorSection'
import {
  Alert,
  Field,
  GoogleIcon,
  SegmentedControl,
  Spinner,
  cx,
} from '../components/ui'

const THEME_OPTIONS: Array<{ value: Theme; label: string }> = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System' },
]

const STROKE_INDEX_OPTIONS: Array<{ value: StrokeIndexLabel; label: string }> = [
  { value: 'HDCP', label: 'HDCP' },
  { value: 'SI', label: 'SI' },
]

/**
 * Imperial and Metric are the labels; yards and meters are what is stored.
 * The wording generalises if temperature or weight ever arrive, and it changes
 * nothing about the column or the conversion code.
 */
/**
 * Blank is a real option, not a missing one: unset means the men's ratings,
 * which is what every round used before this field existed.
 */
const GENDER_OPTIONS: Array<{ value: Gender | ''; label: string }> = [
  { value: '', label: 'Not set' },
  { value: 'men', label: "Men's" },
  { value: 'women', label: "Women's" },
]

const UNIT_OPTIONS: Array<{ value: DistanceUnit; label: string }> = [
  { value: 'yards', label: 'Imperial' },
  { value: 'meters', label: 'Metric' },
]

export default function ProfilePage() {
  const { user, googleEnabled, refreshUser } = useAuth()
  const { theme, setTheme } = useTheme()
  const { strokeIndexLabel, setStrokeIndexLabel } = usePreferences()
  const distanceUnit = useDistanceUnit()
  const [searchParams, setSearchParams] = useSearchParams()

  // Details form
  const [firstName, setFirstName] = useState('')
  const [lastName, setLastName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [homeCourse, setHomeCourse] = useState<SelectedCourse | null>(null)
  const [city, setCity] = useState('')
  const [region, setRegion] = useState('')
  const [country, setCountry] = useState('')
  const [gender, setGender] = useState<Gender | ''>('')
  const [savingProfile, setSavingProfile] = useState(false)
  const [profileErrors, setProfileErrors] = useState<Record<string, string>>({})
  const [profileNotice, setProfileNotice] = useState<string | null>(null)

  // Avatar
  const [uploadingAvatar, setUploadingAvatar] = useState(false)
  const [avatarError, setAvatarError] = useState<string | null>(null)

  // Email
  const [email, setEmail] = useState('')
  const [emailPassword, setEmailPassword] = useState('')
  const [savingEmail, setSavingEmail] = useState(false)
  const [emailErrors, setEmailErrors] = useState<Record<string, string>>({})
  const [emailNotice, setEmailNotice] = useState<string | null>(null)

  // Units
  const [savingUnit, setSavingUnit] = useState(false)
  const [unitError, setUnitError] = useState<string | null>(null)
  const [preferenceError, setPreferenceError] = useState<string | null>(null)

  // Password + Google
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [savingPassword, setSavingPassword] = useState(false)
  const [passwordErrors, setPasswordErrors] = useState<Record<string, string>>({})
  const [passwordNotice, setPasswordNotice] = useState<string | null>(null)
  const [linking, setLinking] = useState(false)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [linkNotice, setLinkNotice] = useState<string | null>(null)

  const googleLinked = user?.providers.includes('google') ?? false

  // Seed the forms from the loaded user.
  useEffect(() => {
    if (!user) return
    setFirstName(user.first_name ?? '')
    setLastName(user.last_name ?? '')
    setDisplayName(user.display_name)
    setHomeCourse(
      user.home_course_id
        ? {
            id: user.home_course_id,
            name: user.home_course_name ?? '',
            location: user.home_course_location ?? '',
          }
        : null,
    )
    setCity(user.location_city ?? '')
    setRegion(user.location_region ?? '')
    setCountry(user.location_country ?? '')
    setGender(user.gender ?? '')
    setEmail(user.email)
  }, [user])

  // The Google link flow redirects back here with ?linked=google or ?error=…
  useEffect(() => {
    const linked = searchParams.get('linked')
    const error = searchParams.get('error')
    if (!linked && !error) return

    if (linked === 'google') {
      setLinkNotice('Google is now linked to your account.')
      void refreshUser()
    }
    if (error) setLinkError(error)

    const next = new URLSearchParams(searchParams)
    next.delete('linked')
    next.delete('error')
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams, refreshUser])

  const applyError = useCallback(
    (err: unknown, fallbackField: string, setErrors: (e: Record<string, string>) => void) => {
      if (err instanceof ApiError && err.isValidation) setErrors(err.fields)
      else setErrors({ [fallbackField]: err instanceof ApiError ? err.message : 'Something went wrong.' })
    },
    [],
  )

  async function handleSaveProfile(event: React.FormEvent) {
    event.preventDefault()
    setSavingProfile(true)
    setProfileErrors({})
    setProfileNotice(null)
    try {
      await api.updateProfile({
        display_name: displayName,
        first_name: firstName,
        last_name: lastName,
        home_course_id: homeCourse?.id ?? null,
        location_city: city,
        location_region: region,
        location_country: country,
      })
      await refreshUser()
      setProfileNotice('Profile saved.')
    } catch (err) {
      applyError(err, 'display_name', setProfileErrors)
    } finally {
      setSavingProfile(false)
    }
  }

  async function handleAvatarChange(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = '' // lets the same file be picked again after an error
    if (!file) return

    setUploadingAvatar(true)
    setAvatarError(null)
    try {
      await api.uploadAvatar(file)
      await refreshUser()
    } catch (err) {
      setAvatarError(
        err instanceof ApiError ? (err.fields.avatar ?? err.message) : 'Could not upload that image.',
      )
    } finally {
      setUploadingAvatar(false)
    }
  }

  async function handleRemoveAvatar() {
    setUploadingAvatar(true)
    setAvatarError(null)
    try {
      await api.deleteAvatar()
      await refreshUser()
    } catch (err) {
      setAvatarError(err instanceof ApiError ? err.message : 'Could not remove that image.')
    } finally {
      setUploadingAvatar(false)
    }
  }

  async function handleSaveEmail(event: React.FormEvent) {
    event.preventDefault()
    setSavingEmail(true)
    setEmailErrors({})
    setEmailNotice(null)
    try {
      // Returns a fresh session: the old access token names the old address,
      // and every other device has just been signed out.
      const session = await api.changeEmail(email, emailPassword)
      setSession(session)
      await refreshUser()
      setEmailPassword('')
      setEmailNotice('Email updated. You have been signed out on your other devices.')
    } catch (err) {
      applyError(err, 'email', setEmailErrors)
    } finally {
      setSavingEmail(false)
    }
  }

  // Saved the moment it is touched, like every other preference on this screen,
  // and through its own endpoint rather than the profile form above: the two are
  // written separately so that saving a name cannot disturb which ratings a
  // round records.
  async function handleGenderChange(next: Gender | '') {
    const previous = gender
    setGender(next)
    setPreferenceError(null)
    try {
      await api.setGender(next)
      await refreshUser()
    } catch (err) {
      setGender(previous)
      setPreferenceError(err instanceof ApiError ? err.message : 'Could not save that.')
    }
  }

  async function handleUnitChange(unit: DistanceUnit) {
    if (unit === distanceUnit) return
    setSavingUnit(true)
    setUnitError(null)
    try {
      await api.setDistanceUnit(unit)
      await refreshUser()
    } catch (err) {
      setUnitError(err instanceof ApiError ? err.message : 'Could not change the unit.')
    } finally {
      setSavingUnit(false)
    }
  }

  async function handlePasswordSubmit(event: React.FormEvent) {
    event.preventDefault()
    setSavingPassword(true)
    setPasswordErrors({})
    setPasswordNotice(null)
    try {
      await api.setPassword(currentPassword, newPassword)
      setPasswordNotice(
        user?.has_password
          ? 'Your password has been changed.'
          : 'Password set. You can now sign in with your email.',
      )
      setCurrentPassword('')
      setNewPassword('')
      await refreshUser()
    } catch (err) {
      applyError(err, 'new_password', setPasswordErrors)
    } finally {
      setSavingPassword(false)
    }
  }

  async function handleLinkGoogle() {
    setLinking(true)
    setLinkError(null)
    try {
      const { authorization_url } = await api.startGoogleLink('/profile')
      window.location.assign(authorization_url)
    } catch (err) {
      setLinkError(err instanceof ApiError ? err.message : 'Could not start Google linking.')
      setLinking(false)
    }
  }

  const joined = user?.created_at
    ? new Date(user.created_at).toLocaleDateString(undefined, {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
      })
    : '—'

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Profile</h1>
      </div>

      {/* ---------- 1. Profile information ---------- */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Personal information</h2>

        <div className="card space-y-5 p-5">
          <div className="flex flex-wrap items-center gap-4">
            <Avatar user={user} className="size-20 text-3xl" />
            <div className="space-y-2">
              <div className="flex flex-wrap gap-2">
                <label
                  className={cx('btn-secondary !min-h-0 cursor-pointer !px-3 !py-2 !text-sm', uploadingAvatar && 'opacity-60')}
                >
                  {uploadingAvatar ? <Spinner label="Uploading" /> : user?.avatar_url ? 'Change photo' : 'Upload photo'}
                  <input
                    type="file"
                    // HEIC is what an iPhone sends unless the accept list is
                    // explicit, and the server does not decode it.
                    accept="image/jpeg,image/png"
                    className="hidden"
                    disabled={uploadingAvatar}
                    onChange={(e) => void handleAvatarChange(e)}
                  />
                </label>
                {user?.avatar_url && (
                  <button
                    type="button"
                    className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm"
                    disabled={uploadingAvatar}
                    onClick={() => void handleRemoveAvatar()}
                  >
                    Remove
                  </button>
                )}
              </div>
              <p className="text-xs text-slate-500 dark:text-slate-400">
                JPEG or PNG, up to 4 MB.
              </p>
            </div>
          </div>

          {avatarError && <Alert>{avatarError}</Alert>}
          {profileNotice && <Alert tone="success">{profileNotice}</Alert>}

          <form onSubmit={handleSaveProfile} className="space-y-4">
            {/* Display name leads because it is the only required one, and
                the only one anybody else ever sees. First and last are the
                optional detail underneath it. */}
            <Field
              id="display-name"
              label="Display name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              error={profileErrors.display_name}
              required
              maxLength={80}
            />

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field
                id="first-name"
                label="First name"
                value={firstName}
                onChange={(e) => setFirstName(e.target.value)}
                error={profileErrors.first_name}
                optional
                maxLength={60}
              />
              <Field
                id="last-name"
                label="Last name"
                value={lastName}
                onChange={(e) => setLastName(e.target.value)}
                error={profileErrors.last_name}
                optional
                maxLength={60}
              />
            </div>

            <CourseSearchField
              label="Home course"
              value={homeCourse}
              onChange={setHomeCourse}
              error={profileErrors.home_course_id}
              optional
            />

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <Field
                id="location-city"
                label="City"
                value={city}
                onChange={(e) => setCity(e.target.value)}
                error={profileErrors.location_city}
                optional
                maxLength={80}
              />
              <Field
                id="location-region"
                label="State or province"
                value={region}
                onChange={(e) => setRegion(e.target.value)}
                error={profileErrors.location_region}
                options={regionSuggestions(country)}
                optional
                maxLength={80}
              />
              <Field
                id="location-country"
                label="Country"
                value={country}
                onChange={(e) => setCountry(e.target.value)}
                error={profileErrors.location_country}
                options={countrySuggestions()}
                optional
                maxLength={80}
              />
            </div>

            <div className="flex items-center gap-4">
              <button type="submit" className="btn-primary" disabled={savingProfile}>
                {savingProfile ? <Spinner label="Saving" /> : 'Save profile'}
              </button>
              <p className="text-sm text-slate-500 dark:text-slate-400">Joined {joined}</p>
            </div>
          </form>
        </div>
      </section>

      {/* ---------- 2. Preferences ---------- */}
      {/* Above sign-in because it is the section people come back to. Changing
          a unit is routine; changing a credential is a once-a-year act, and
          burying the routine thing under it makes the common case the long
          scroll. */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Preferences</h2>

        <div className="card space-y-5 p-5">
          <p className="text-sm text-slate-600 dark:text-slate-400">
            How the app looks, what units it reads in, and which course ratings your rounds are
            recorded against.
          </p>

          {unitError && <Alert>{unitError}</Alert>}
          {preferenceError && <Alert>{preferenceError}</Alert>}

          {/* First in the section, because it is the one preference that
              changes what gets stored rather than only how it is displayed. */}
          <div className="space-y-2">
            <span className="label">Course ratings</span>
            <SegmentedControl
              label="Course ratings"
              value={gender}
              options={GENDER_OPTIONS}
              onChange={(next) => void handleGenderChange(next)}
            />
            <p className="text-sm text-slate-500 dark:text-slate-400">
              Which set of published course and slope ratings your rounds are recorded against.
              The same tee is rated separately for men and women. Left unset, rounds use the
              men's ratings. Changing this does not alter rounds already played.
            </p>
          </div>

          <div className="space-y-2">
            <span className="label">Units</span>
            <SegmentedControl
              label="Units"
              value={distanceUnit}
              options={UNIT_OPTIONS}
              onChange={(unit) => void handleUnitChange(unit)}
              disabled={savingUnit}
            />
            <p className="text-sm text-slate-500 dark:text-slate-400">
              Hole distances, tee totals, and club carry. Nothing stored is rewritten, so
              switching back shows the same numbers.
            </p>
          </div>

          <div className="space-y-2">
            <span className="label">Theme</span>
            <SegmentedControl label="Theme" value={theme} options={THEME_OPTIONS} onChange={setTheme} />
          </div>

          <div className="space-y-2">
            <span className="label">Stroke index label</span>
            <SegmentedControl
              label="Stroke index label"
              value={strokeIndexLabel}
              options={STROKE_INDEX_OPTIONS}
              onChange={setStrokeIndexLabel}
            />
          </div>
        </div>
      </section>

      {/* ---------- 3. Sign-in and security ---------- */}
      {/* The address and the password sit together because they are the same
          kind of thing: both are credentials, both demand the current password
          to change, and both sign every other device out. Grouping the address
          with the display name — which is where it used to live — grouped it by
          what it looks like rather than by what it does. */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Sign-in &amp; security</h2>

        <div className="card space-y-4 p-5">
          <div>
            <h3 className="font-semibold">Email address</h3>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              This is how you sign in. Changing it requires your password and signs you out
              everywhere else.
            </p>
          </div>
          {emailNotice && <Alert tone="success">{emailNotice}</Alert>}
          <EmailVerificationNotice />
          <form onSubmit={handleSaveEmail} className="space-y-4">
            <Field
              id="account-email"
              label="Email"
              type="email"
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              error={emailErrors.email}
              required
            />
            {user?.has_password && (
              <Field
                id="email-password"
                label="Current password"
                type="password"
                autoComplete="current-password"
                value={emailPassword}
                onChange={(e) => setEmailPassword(e.target.value)}
                error={emailErrors.current_password}
                hint="Required to change your email address."
              />
            )}
            <button
              type="submit"
              className="btn-secondary"
              disabled={savingEmail || email === user?.email}
            >
              {savingEmail ? <Spinner label="Saving" /> : 'Change email'}
            </button>
          </form>
        </div>

        <div className="card space-y-4 p-5">
          <div>
            <h3 className="font-semibold">
              {user?.has_password ? 'Change password' : 'Set a password'}
            </h3>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              {user?.has_password
                ? 'Enter your current password, then the new one.'
                : 'You signed in with Google. Setting a password adds email sign-in as a backup.'}
            </p>
          </div>
          {passwordNotice && <Alert tone="success">{passwordNotice}</Alert>}
          <form onSubmit={handlePasswordSubmit} className="space-y-4">
            {user?.has_password && (
              <Field
                id="current-password"
                label="Current password"
                type="password"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
                error={passwordErrors.current_password}
                required
              />
            )}
            <Field
              id="new-password"
              label="New password"
              type="password"
              autoComplete="new-password"
              minLength={8}
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              error={passwordErrors.new_password}
              hint="At least 8 characters."
              required
            />
            <button type="submit" className="btn-primary" disabled={savingPassword}>
              {savingPassword ? <Spinner label="Saving" /> : 'Save password'}
            </button>
          </form>
        </div>

        <TwoFactorSection />

        {googleEnabled && (
          <div className="card space-y-4 p-5">
            <div>
              <h3 className="font-semibold">Google</h3>
              <p className="text-sm text-slate-600 dark:text-slate-400">
                {googleLinked
                  ? 'Your Google account is linked. You can sign in either way.'
                  : 'Link Google to sign in with one tap.'}
              </p>
            </div>
            {linkNotice && <Alert tone="success">{linkNotice}</Alert>}
            {linkError && <Alert>{linkError}</Alert>}
            {googleLinked ? (
              <p className="inline-flex items-center gap-2 rounded-lg bg-brand-50 px-3 py-2 text-sm text-brand-900 dark:bg-brand-950/60 dark:text-brand-200">
                <GoogleIcon className="size-4" />
                Linked
              </p>
            ) : (
              <button
                type="button"
                className="btn-secondary"
                disabled={linking}
                onClick={() => void handleLinkGoogle()}
              >
                {linking ? <Spinner label="Redirecting" /> : <GoogleIcon className="size-5" />}
                Link Google account
              </button>
            )}
          </div>
        )}
      </section>

      {/* ---------- 4. Download my data ---------- */}
      {/* Moved down out of the middle of the page: it used to sit between the
          profile and the credentials, splitting the two halves of "your
          account" with an errand nobody runs weekly. */}
      <DataSection onImported={() => void refreshUser()} />

      {/* ---------- 5. API access ---------- */}
      <ApiAccessSection />

      <DangerZone />
    </div>
  )
}
