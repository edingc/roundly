/**
 * What a signed-in but unconfirmed account sees instead of the app.
 *
 * This is the client half of auth.RequireVerifiedEmail: the server refuses
 * every application endpoint with `email_unverified`, and this screen is what
 * makes that refusal into something a person can act on rather than a wall of
 * failed requests.
 */
import { useState } from 'react'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { AuthShell } from '../components/AuthShell'
import { Alert, Spinner } from '../components/ui'

export default function ConfirmEmailPage() {
  const { user, logOut, refreshUser } = useAuth()
  const [sending, setSending] = useState(false)
  const [checking, setChecking] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function handleResend() {
    setSending(true)
    setNotice(null)
    setError(null)
    try {
      await api.resendVerification()
      setNotice('Sent. Give it a minute, and check your spam folder.')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not send that just now.')
    } finally {
      setSending(false)
    }
  }

  // For the common case of confirming in another browser: rather than making
  // somebody reload and wonder, this re-reads the account and lets the router
  // move on by itself once the flag has flipped.
  async function handleCheck() {
    setChecking(true)
    setNotice(null)
    setError(null)
    try {
      await refreshUser()
      setNotice('Still unconfirmed. Open the link in the email, then check again.')
    } catch {
      setError('Could not check just now.')
    } finally {
      setChecking(false)
    }
  }

  return (
    <AuthShell
      title="Confirm your email"
      subtitle={`We sent a link to ${user?.email ?? 'your address'}.`}
      footer={
        <button type="button" onClick={() => void logOut()} className="font-semibold hover:underline">
          Sign out
        </button>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-slate-600 dark:text-slate-400">
          Open the link in that email to finish setting up your account. Until then the rest of
          Roundly is closed — your address is how you sign in and how you get back in, so it has
          to be one you can actually read.
        </p>

        {notice && <Alert tone="info">{notice}</Alert>}
        {error && <Alert>{error}</Alert>}

        <div className="flex flex-col gap-2">
          <button
            type="button"
            className="btn-primary w-full"
            disabled={checking}
            onClick={() => void handleCheck()}
          >
            {checking ? <Spinner label="Checking" /> : "I have confirmed it"}
          </button>
          <button
            type="button"
            className="btn-secondary w-full"
            disabled={sending}
            onClick={() => void handleResend()}
          >
            {sending ? <Spinner label="Sending" /> : 'Send the link again'}
          </button>
        </div>

        <p className="text-xs text-slate-500 dark:text-slate-400">
          Wrong address? Sign out and sign up again — the account you are in now has nothing in
          it yet.
        </p>
      </div>
    </AuthShell>
  )
}
