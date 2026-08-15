import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { usePreferences, type StrokeIndexLabel } from '../lib/preferences'
import { useTheme, type Theme } from '../lib/theme'
import { Alert, Field, GoogleIcon, Spinner } from '../components/ui'

const THEME_OPTIONS: Array<{ value: Theme; label: string }> = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System' },
]

const STROKE_INDEX_LABEL_OPTIONS: Array<{ value: StrokeIndexLabel; label: string }> = [
  { value: 'HDCP', label: 'HDCP' },
  { value: 'SI', label: 'SI' },
]

export default function SettingsPage() {
  const { user, googleEnabled, refreshUser } = useAuth()
  const { theme, setTheme } = useTheme()
  const { strokeIndexLabel, setStrokeIndexLabel } = usePreferences()
  const [searchParams, setSearchParams] = useSearchParams()

  const [linking, setLinking] = useState(false)
  const [linkError, setLinkError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [savingPassword, setSavingPassword] = useState(false)
  const [passwordErrors, setPasswordErrors] = useState<Record<string, string>>({})
  const [passwordNotice, setPasswordNotice] = useState<string | null>(null)

  const googleLinked = user?.providers.includes('google') ?? false

  // The Google link flow redirects back here with ?linked=google or ?error=...
  useEffect(() => {
    const linked = searchParams.get('linked')
    const error = searchParams.get('error')
    if (!linked && !error) return

    if (linked === 'google') {
      setNotice('Google is now linked to your account.')
      void refreshUser()
    }
    if (error) setLinkError(error)

    // Clear the params so a reload does not replay the message.
    const next = new URLSearchParams(searchParams)
    next.delete('linked')
    next.delete('error')
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams, refreshUser])

  async function handleLinkGoogle() {
    setLinking(true)
    setLinkError(null)
    try {
      // The endpoint is authenticated, so the URL is fetched over XHR and then
      // navigated to; a plain link could not send the access token.
      const { authorization_url } = await api.startGoogleLink('/settings')
      window.location.assign(authorization_url)
    } catch (error) {
      setLinkError(error instanceof ApiError ? error.message : 'Could not start Google linking.')
      setLinking(false)
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
        user?.has_password ? 'Your password has been changed.' : 'Password set. You can now sign in with your email.',
      )
      setCurrentPassword('')
      setNewPassword('')
      await refreshUser()
    } catch (error) {
      if (error instanceof ApiError && error.isValidation) setPasswordErrors(error.fields)
      else
        setPasswordErrors({
          new_password: error instanceof ApiError ? error.message : 'Could not save the password.',
        })
    } finally {
      setSavingPassword(false)
    }
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">Settings</h1>

      <section className="card space-y-1 p-5">
        <h2 className="text-lg font-semibold">Account</h2>
        <dl className="mt-3 space-y-2 text-sm">
          <div className="flex gap-2">
            <dt className="w-28 shrink-0 text-slate-500 dark:text-slate-400">Name</dt>
            <dd>{user?.display_name}</dd>
          </div>
          <div className="flex gap-2">
            <dt className="w-28 shrink-0 text-slate-500 dark:text-slate-400">Email</dt>
            <dd className="break-all">{user?.email}</dd>
          </div>
          <div className="flex gap-2">
            <dt className="w-28 shrink-0 text-slate-500 dark:text-slate-400">Sign-in</dt>
            <dd>
              {[user?.has_password && 'Password', googleLinked && 'Google']
                .filter(Boolean)
                .join(' + ') || '—'}
            </dd>
          </div>
        </dl>
      </section>

      <section className="card space-y-4 p-5">
        <div>
          <h2 className="text-lg font-semibold">Appearance</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Choose light, dark, or follow your device.
          </p>
        </div>
        <div className="flex gap-2" role="radiogroup" aria-label="Theme">
          {THEME_OPTIONS.map((option) => (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={theme === option.value}
              onClick={() => setTheme(option.value)}
              className={
                theme === option.value
                  ? 'btn flex-1 bg-brand-600 text-white'
                  : 'btn-secondary flex-1'
              }
            >
              {option.label}
            </button>
          ))}
        </div>
      </section>

      <section className="card space-y-4 p-5">
        <div>
          <h2 className="text-lg font-semibold">Scorecard</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            How the stroke index column is labeled.
          </p>
        </div>
        <div className="flex gap-2" role="radiogroup" aria-label="Stroke index label">
          {STROKE_INDEX_LABEL_OPTIONS.map((option) => (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={strokeIndexLabel === option.value}
              onClick={() => setStrokeIndexLabel(option.value)}
              className={
                strokeIndexLabel === option.value
                  ? 'btn flex-1 bg-brand-600 text-white'
                  : 'btn-secondary flex-1'
              }
            >
              {option.label}
            </button>
          ))}
        </div>
      </section>

      {googleEnabled && (
        <section className="card space-y-4 p-5">
          <div>
            <h2 className="text-lg font-semibold">Google</h2>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              {googleLinked
                ? 'Your Google account is linked. You can sign in either way.'
                : 'Link Google to sign in with one tap.'}
            </p>
          </div>
          {notice && <Alert tone="success">{notice}</Alert>}
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
        </section>
      )}

      <section className="card space-y-4 p-5">
        <div>
          <h2 className="text-lg font-semibold">
            {user?.has_password ? 'Change password' : 'Set a password'}
          </h2>
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
      </section>
    </div>
  )
}
