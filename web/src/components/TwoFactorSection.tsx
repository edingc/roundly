/**
 * The two-factor card on the profile screen: the switch, and the list of
 * browsers that get to skip it.
 *
 * Rendered only where it can work. An instance with no mail configuration
 * cannot send a code, so the whole section is absent rather than present and
 * broken — the same rule the Google button and the geocoding hint follow.
 */
import { useCallback, useEffect, useState } from 'react'
import { ApiError, api, setStoredDeviceToken } from '../lib/api'
import { useAuth } from '../lib/auth'
import type { TrustedDevice } from '../types'
import { Alert, Field, Spinner, TrashIcon } from './ui'
import { RecoveryCodesPanel, RecoveryCodesStatus } from './RecoveryCodes'

/** Renders an ISO timestamp as a plain date, or a dash when there isn't one. */
function shortDate(value: string | null): string {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

export function TwoFactorSection() {
  const { user, emailEnabled, refreshUser } = useAuth()

  const [password, setPassword] = useState('')
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [notice, setNotice] = useState<string | null>(null)

  const [devices, setDevices] = useState<TrustedDevice[]>([])
  const [devicesLoading, setDevicesLoading] = useState(false)

  // Held only until the user says they have saved them. Never persisted, and
  // never fetched again — the server cannot produce them a second time.
  const [freshCodes, setFreshCodes] = useState<string[] | null>(null)
  const [regenerating, setRegenerating] = useState(false)

  const enabled = user?.two_factor_email ?? false
  const hasPassword = user?.has_password ?? false
  const verified = user?.email_verified ?? false

  const loadDevices = useCallback(async () => {
    if (!enabled) {
      setDevices([])
      return
    }
    setDevicesLoading(true)
    try {
      const { items } = await api.listDevices()
      setDevices(items)
    } catch {
      // A device list that will not load is not worth an error banner over the
      // switch itself, which is the part of this card that matters.
      setDevices([])
    } finally {
      setDevicesLoading(false)
    }
  }, [enabled])

  useEffect(() => {
    void loadDevices()
  }, [loadDevices])

  if (!emailEnabled) return null

  async function handleToggle(next: boolean) {
    setSaving(true)
    setErrors({})
    setNotice(null)
    try {
      const setup = await api.setTwoFactor(next, password)
      setPassword('')
      // Shown once. If this is dropped on the floor the user has no recovery
      // path at all, so it goes into state before anything else happens.
      setFreshCodes(setup.recovery_codes ?? null)
      // Both directions clear every remembered device server-side, so this
      // browser's token is stale either way.
      setStoredDeviceToken(null)
      await refreshUser()
      setNotice(
        next
          ? 'Sign-in codes are on. You will be asked for one the next time you sign in from a new browser.'
          : 'Sign-in codes are off.',
      )
    } catch (err) {
      if (err instanceof ApiError && err.isValidation) setErrors(err.fields)
      else setErrors({ current_password: err instanceof ApiError ? err.message : 'Something went wrong.' })
    } finally {
      setSaving(false)
    }
  }

  async function handleRegenerate() {
    setRegenerating(true)
    setErrors({})
    setNotice(null)
    try {
      const { recovery_codes } = await api.regenerateRecoveryCodes(password)
      setPassword('')
      setFreshCodes(recovery_codes)
      await refreshUser()
    } catch (err) {
      if (err instanceof ApiError && err.isValidation) setErrors(err.fields)
      else setErrors({ current_password: err instanceof ApiError ? err.message : 'Something went wrong.' })
    } finally {
      setRegenerating(false)
    }
  }

  async function handleForget(device: TrustedDevice) {
    try {
      await api.forgetDevice(device.id)
      // Forgetting this browser has to drop its token too, or it keeps
      // presenting one the server has already dropped.
      if (device.current) setStoredDeviceToken(null)
      await loadDevices()
    } catch {
      setErrors({ current_password: 'Could not forget that device.' })
    }
  }

  return (
    <div className="card space-y-4 p-5">
      <div>
        <h3 className="font-semibold">Two-factor authentication</h3>
        <p className="text-sm text-slate-600 dark:text-slate-400">
          {enabled
            ? 'Signing in from a new browser needs a code emailed to you. Google sign-in is unaffected.'
            : 'Ask for a code by email when signing in with your password from a browser you have not used before.'}
        </p>
      </div>

      {notice && <Alert tone="success">{notice}</Alert>}

      {!hasPassword ? (
        <Alert tone="info">
          This account signs in with Google, which already asks for its own second factor. Set a
          password above if you want email codes as well.
        </Alert>
      ) : !verified && !enabled ? (
        <Alert tone="warning">
          Confirm your email address first. A code sent to an address you have not proven you can
          read is a lockout waiting to happen.
        </Alert>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void handleToggle(!enabled)
          }}
          className="space-y-4"
        >
          <Field
            id="two-factor-password"
            label="Current password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            error={errors.current_password}
            // Demanded in both directions: turning this off removes a
            // protection, which is exactly what someone holding a stolen
            // session would want to do.
            hint={enabled ? 'Required to turn sign-in codes off.' : 'Required to turn sign-in codes on.'}
            required
          />
          <button
            type="submit"
            className={enabled ? 'btn-secondary' : 'btn-primary'}
            disabled={saving || password === ''}
          >
            {saving ? <Spinner label="Saving" /> : enabled ? 'Turn off sign-in codes' : 'Turn on sign-in codes'}
          </button>
        </form>
      )}

      {freshCodes && <RecoveryCodesPanel codes={freshCodes} onDone={() => setFreshCodes(null)} />}

      {enabled && (
        <div className="space-y-3 border-t border-slate-200 pt-4 dark:border-slate-700">
          <div>
            <h4 className="text-sm font-semibold">Recovery codes</h4>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              The way back in if you lose access to your email. Without them, a lost mailbox is
              a lost account — there is no administrator to appeal to on a self-hosted instance.
            </p>
          </div>

          <RecoveryCodesStatus remaining={user?.recovery_codes_remaining ?? 0} />

          <form
            onSubmit={(e) => {
              e.preventDefault()
              void handleRegenerate()
            }}
            className="space-y-3"
          >
            <Field
              id="recovery-password"
              label="Current password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              error={errors.current_password}
              hint="Required to generate a new sheet."
            />
            <button
              type="submit"
              className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm"
              disabled={regenerating || password === ''}
            >
              {regenerating ? <Spinner label="Generating" /> : 'Generate a new sheet'}
            </button>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              Generating replaces all ten. Any code you have written down stops working.
            </p>
          </form>
        </div>
      )}

      {enabled && (
        <div className="space-y-2 border-t border-slate-200 pt-4 dark:border-slate-700">
          <h4 className="text-sm font-semibold">Remembered browsers</h4>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            These skip the code for 30 days. Forget any you do not recognise.
          </p>

          {devicesLoading ? (
            <Spinner label="Loading devices" />
          ) : devices.length === 0 ? (
            <p className="text-sm text-slate-500 dark:text-slate-400">
              None. Every sign-in asks for a code.
            </p>
          ) : (
            <ul className="divide-y divide-slate-200 dark:divide-slate-700">
              {devices.map((device) => (
                <li key={device.id} className="flex items-center gap-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">
                      {device.label ?? 'Unknown browser'}
                      {device.current && (
                        <span className="ml-2 rounded-md bg-accent-100 px-2 py-0.5 text-xs font-medium text-accent-800 dark:bg-accent-900 dark:text-accent-100">
                          This browser
                        </span>
                      )}
                    </p>
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                      Trusted {shortDate(device.created_at)} · last used {shortDate(device.last_used_at)}
                    </p>
                  </div>
                  <button
                    type="button"
                    className="btn-ghost !min-h-0 shrink-0 !px-2 !py-1 text-sm"
                    aria-label={`Forget ${device.label ?? 'this browser'}`}
                    onClick={() => void handleForget(device)}
                  >
                    <TrashIcon className="size-4" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {/* The one thing this feature genuinely cannot do, said plainly rather
          than left for someone to discover. */}
      <p className="text-xs text-slate-500 dark:text-slate-400">
        Codes go to your email address, which is also how you would recover this account. That
        makes this a defence against a stolen password, not against a compromised mailbox.
      </p>
    </div>
  )
}

/**
 * The confirm-your-address strip on the email card.
 *
 * Only ever visible in the window between changing an address and confirming
 * the new one — a signed-in unconfirmed account is otherwise held at
 * ConfirmEmailPage and never reaches this screen.
 */
export function EmailVerificationNotice() {
  const { user, emailEnabled } = useAuth()
  const [sending, setSending] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  if (!emailEnabled || !user || user.email_verified) return null

  async function handleResend() {
    setSending(true)
    setNotice(null)
    setError(null)
    try {
      await api.resendVerification()
      setNotice('Sent. Open the link in that email to confirm this address.')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not send that just now.')
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="space-y-2">
      <Alert tone="warning">This address has not been confirmed yet.</Alert>
      {notice && <Alert tone="success">{notice}</Alert>}
      {error && <Alert>{error}</Alert>}
      <button
        type="button"
        className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm"
        disabled={sending}
        onClick={() => void handleResend()}
      >
        {sending ? <Spinner label="Sending" /> : 'Send the confirmation link'}
      </button>
    </div>
  )
}
