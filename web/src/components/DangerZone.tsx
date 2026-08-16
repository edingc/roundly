import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api, setSession } from '../lib/api'
import { useAuth } from '../lib/auth'
import { Alert, ConfirmDialog, Field, WarningIcon } from './ui'

/**
 * Deleting the account.
 *
 * The copy is specific about what goes and what stays, because the surprising
 * part is that the courses do not: they are shared reference data that other
 * players depend on and nobody owned, so they remain in the directory with the
 * attribution removed.
 *
 * Confirmation is the password plus the account's own address typed out. The
 * password stops someone at an unlocked laptop; typing the address stops the
 * reflexive click-through that a dialog alone invites.
 */
export function DangerZone() {
  const { user, logOut } = useAuth()
  const navigate = useNavigate()

  const [confirming, setConfirming] = useState(false)
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)

  async function handleDelete() {
    setError(null)
    try {
      await api.deleteAccount(password)
      // The account is gone; drop the local session without calling logout,
      // which would now fail against a user that no longer exists.
      setSession(null)
      await logOut().catch(() => {})
      navigate('/login', { replace: true })
    } catch (err) {
      const message =
        err instanceof ApiError
          ? (err.fields.current_password ?? err.message)
          : 'Could not delete your account.'
      setError(message)
      // Rethrown so ConfirmDialog stops its spinner and stays open.
      throw new Error(message)
    }
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold text-red-700 dark:text-red-400">Danger zone</h2>

      <div className="card space-y-4 border-red-300 p-5 dark:border-red-900">
        <div>
          <h3 className="font-semibold">Delete my account</h3>
          <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">
            This removes your profile, your photo, every club in your bag, and every API key,
            and signs you out everywhere. It cannot be undone.
          </p>
        </div>

        <Alert tone="info">
          <span className="flex items-start gap-2">
            <WarningIcon className="mt-0.5 size-4 shrink-0" />
            <span>
              Courses you added stay in the directory. They are shared with everyone and other
              players may rely on them, so they remain — with your name removed from them.
            </span>
          </span>
        </Alert>

        <p className="text-sm text-slate-600 dark:text-slate-400">
          Want a copy first? Use <strong>Download my data</strong> above — afterwards there is
          nothing left to download.
        </p>

        {error && <Alert>{error}</Alert>}

        <button
          type="button"
          className="btn-danger"
          onClick={() => {
            setPassword('')
            setError(null)
            setConfirming(true)
          }}
        >
          Delete my account
        </button>
      </div>

      {confirming && user && (
        <ConfirmDialog
          title="Delete your account?"
          confirmLabel="Delete my account"
          message={
            <>
              Everything personal to this account is erased immediately and permanently. The
              courses you added stay in the directory without your name on them.
            </>
          }
          requireTyped={{
            label: 'Type your email address to confirm',
            value: user.email,
            hint: user.email,
          }}
          extra={
            user.has_password ? (
              <Field
                id="delete-password"
                label="Current password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            ) : (
              <p className="text-sm text-slate-600 dark:text-slate-400">
                You signed in with Google. If it has been more than a few minutes, sign in again
                before deleting.
              </p>
            )
          }
          onCancel={() => setConfirming(false)}
          onConfirm={handleDelete}
        />
      )}
    </section>
  )
}
