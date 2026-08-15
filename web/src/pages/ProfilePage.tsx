/**
 * The user profile: identity, preferences, data, sign-in, and API access.
 *
 * Replaces the old settings page. The four sections are deliberately distinct
 * cards rather than one long form, because they answer four different
 * questions and are visited for different reasons.
 */
import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError, api, setSession } from '../lib/api'
import { useAuth, useDistanceUnit } from '../lib/auth'
import { usePreferences, type StrokeIndexLabel } from '../lib/preferences'
import { useTheme, type Theme } from '../lib/theme'
import type { CourseSummary, DistanceUnit } from '../types'
import { Avatar } from '../components/Avatar'
import { DataSection } from '../components/DataSection'
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

  const [courses, setCourses] = useState<CourseSummary[]>([])

  // Details form
  const [firstName, setFirstName] = useState('')
  const [lastName, setLastName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [homeCourseId, setHomeCourseId] = useState('')
  const [city, setCity] = useState('')
  const [region, setRegion] = useState('')
  const [country, setCountry] = useState('')
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
    setHomeCourseId(user.home_course_id ?? '')
    setCity(user.location_city ?? '')
    setRegion(user.location_region ?? '')
    setCountry(user.location_country ?? '')
    setEmail(user.email)
  }, [user])

  useEffect(() => {
    api
      .listCourses({ limit: 100 })
      .then((page) => setCourses(page.items))
      .catch(() => setCourses([]))
  }, [])

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
        home_course_id: homeCourseId || null,
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
        <p className="text-sm text-slate-600 dark:text-slate-400">
          Your account, your data, and how you reach it.
        </p>
      </div>

      {/* ---------- 1. Profile information ---------- */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Profile information</h2>

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
                JPEG or PNG, up to 4 MB. Cropped to a square. Served from an unlisted link, so
                anyone with that link can view it.
              </p>
            </div>
          </div>

          {avatarError && <Alert>{avatarError}</Alert>}
          {profileNotice && <Alert tone="success">{profileNotice}</Alert>}

          <form onSubmit={handleSaveProfile} className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field
                id="first-name"
                label="First name"
                value={firstName}
                onChange={(e) => setFirstName(e.target.value)}
                error={profileErrors.first_name}
                hint="Optional"
                maxLength={60}
              />
              <Field
                id="last-name"
                label="Last name"
                value={lastName}
                onChange={(e) => setLastName(e.target.value)}
                error={profileErrors.last_name}
                hint="Optional"
                maxLength={60}
              />
            </div>

            <Field
              id="display-name"
              label="Display name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              error={profileErrors.display_name}
              hint="How you appear in the app. This is the only name that is required."
              required
              maxLength={80}
            />

            <div>
              <label htmlFor="home-course" className="label">
                Home course
              </label>
              <select
                id="home-course"
                value={homeCourseId}
                onChange={(e) => setHomeCourseId(e.target.value)}
                className={cx('input', profileErrors.home_course_id && 'input-error')}
              >
                <option value="">Not set</option>
                {courses.map((course) => (
                  <option key={course.id} value={course.id}>
                    {course.name}
                  </option>
                ))}
              </select>
              {profileErrors.home_course_id ? (
                <p className="field-error">{profileErrors.home_course_id}</p>
              ) : (
                <p className="mt-1.5 text-sm text-slate-500 dark:text-slate-400">
                  Optional. Any course in the directory.
                </p>
              )}
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <Field
                id="location-city"
                label="City"
                value={city}
                onChange={(e) => setCity(e.target.value)}
                error={profileErrors.location_city}
                hint="Optional"
                maxLength={80}
              />
              <Field
                id="location-region"
                label="State or province"
                value={region}
                onChange={(e) => setRegion(e.target.value)}
                error={profileErrors.location_region}
                hint="Optional"
                maxLength={80}
              />
              <Field
                id="location-country"
                label="Country"
                value={country}
                onChange={(e) => setCountry(e.target.value)}
                error={profileErrors.location_country}
                hint="Optional"
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

        {/* Email lives in its own card because changing it is a different kind
            of act: it needs the password and it signs other devices out. */}
        <div className="card space-y-4 p-5">
          <div>
            <h3 className="font-semibold">Email address</h3>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              This is how you sign in. Changing it requires your password and signs you out
              everywhere else.
            </p>
          </div>
          {emailNotice && <Alert tone="success">{emailNotice}</Alert>}
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

        <div className="card space-y-5 p-5">
          <div>
            <h3 className="font-semibold">Preferences</h3>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              How the app looks and what units it reads in.
            </p>
          </div>

          {unitError && <Alert>{unitError}</Alert>}

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

      {/* ---------- 2. Download my data ---------- */}
      <DataSection onImported={() => void refreshUser()} />

      {/* ---------- 3. Password and sign-in ---------- */}
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Password &amp; sign-in</h2>

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
    </div>
  )
}
