import { useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { ApiError, api, googleLoginUrl } from '../lib/api'
import type { TwoFactorChallenge } from '../types'
import { AuthShell, OrDivider } from '../components/AuthShell'
import { Alert, Field, GoogleIcon, Spinner } from '../components/ui'

export default function LoginPage() {
  const { logIn, adoptSession, googleEnabled } = useAuth()
  const location = useLocation()
  const [searchParams] = useSearchParams()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  // Set when the password was right but a mailed code is still needed. Its
  // presence is what swaps the form for the code step; the password is dropped
  // from state at the same moment, since it has done its job.
  const [challenge, setChallenge] = useState<TwoFactorChallenge | null>(null)
  const [code, setCode] = useState('')
  const [rememberDevice, setRememberDevice] = useState(true)
  // Swaps the six-digit box for the recovery-code one. Same challenge, and the
  // same five attempts — this is a different answer, not a way around the cap.
  const [usingRecoveryCode, setUsingRecoveryCode] = useState(false)

  // The Google callback reports failures by redirecting back here with ?error.
  const redirectError = searchParams.get('error')
  const returnTo = (location.state as { returnTo?: string } | null)?.returnTo ?? '/overview'

  /** Turns any thrown failure into something the form can show. */
  function showError(error: unknown) {
    if (error instanceof ApiError) {
      if (error.isValidation) setFieldErrors(error.fields)
      else setFormError(error.message)
    } else {
      setFormError('Could not reach the server. Check your connection and try again.')
    }
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setFormError(null)
    setFieldErrors({})

    try {
      const pending = await logIn(email, password)
      if (pending) {
        setChallenge(pending)
        setPassword('')
      }
      // Otherwise navigation is handled by RedirectIfSignedIn once `user` is set.
    } catch (error) {
      showError(error)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleVerify(event: React.FormEvent) {
    event.preventDefault()
    if (!challenge) return
    setSubmitting(true)
    setFormError(null)
    setFieldErrors({})

    try {
      const session = usingRecoveryCode
        ? await api.verifyRecoveryCode(challenge.challenge_id, code)
        : await api.verifyTwoFactor(challenge.challenge_id, code, rememberDevice)
      adoptSession(session)
    } catch (error) {
      showError(error)
      setCode('')
    } finally {
      setSubmitting(false)
    }
  }

  /** Abandons the code step and goes back to the password form. */
  function startOver() {
    setChallenge(null)
    setCode('')
    setUsingRecoveryCode(false)
    setFormError(null)
    setFieldErrors({})
  }

  /** Switches between the emailed code and a recovery code from the sheet. */
  function toggleRecovery() {
    setUsingRecoveryCode((current) => !current)
    setCode('')
    setFormError(null)
    setFieldErrors({})
  }

  if (challenge) {
    return (
      <AuthShell
        title={usingRecoveryCode ? 'Use a recovery code' : 'Check your email'}
        subtitle={
          usingRecoveryCode
            ? 'Enter one of the codes from the sheet you saved when you turned on two-factor.'
            : `We sent a six-digit code to ${email}.`
        }
        footer={
          <button type="button" onClick={startOver} className="font-semibold hover:underline">
            Use a different account
          </button>
        }
      >
        {formError && (
          <div className="mb-4">
            <Alert>{formError}</Alert>
          </div>
        )}

        <form onSubmit={handleVerify} className="space-y-4">
          {usingRecoveryCode ? (
            <Field
              label="Recovery code"
              name="recovery_code"
              // Off: a recovery code is not a password and has no business in
              // anybody's password manager under this account's entry.
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              autoFocus
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
              error={fieldErrors.recovery_code}
              placeholder="ABCDE-FGHJK"
              hint="Case and hyphens do not matter."
              className="text-center text-xl tracking-widest uppercase"
            />
          ) : (
            <Field
              label="Sign-in code"
              name="code"
              // one-time-code lets a phone offer the code straight from the
              // notification, which is most of what makes this bearable.
              autoComplete="one-time-code"
              inputMode="numeric"
              // Not `pattern`, and not maxLength 6: a pasted code often arrives
              // with a space or a stray character, and the server strips those.
              autoFocus
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
              error={fieldErrors.code}
              placeholder="123456"
              className="text-center text-2xl tracking-[0.4em]"
            />
          )}

          {/* Not offered on the recovery path. Somebody reaching for a recovery
              code has just lost access to their email, which is a bad moment to
              also hand out a thirty-day pass — and the server refuses it there
              regardless. */}
          {!usingRecoveryCode && (
            <label className="flex items-start gap-2 text-sm text-slate-600 dark:text-slate-400">
              <input
                type="checkbox"
                className="mt-0.5 size-4 rounded border-slate-300 text-brand-600 focus:ring-accent-500 dark:border-slate-600"
                checked={rememberDevice}
                onChange={(e) => setRememberDevice(e.target.checked)}
              />
              <span>
                Trust this browser for 30 days. Only do this on a device that is yours.
              </span>
            </label>
          )}

          <button type="submit" disabled={submitting} className="btn-primary w-full">
            {submitting ? <Spinner label="Checking" /> : 'Sign in'}
          </button>
        </form>

        <button
          type="button"
          onClick={toggleRecovery}
          className="mt-4 w-full text-sm font-semibold text-accent-700 hover:underline dark:text-accent-300"
        >
          {usingRecoveryCode ? 'Use the emailed code instead' : "Can't access your email?"}
        </button>

        <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">
          {usingRecoveryCode
            ? 'Each recovery code works once. Generate a new sheet from your profile after signing in.'
            : 'The code expires in 10 minutes. If it does not arrive, check your spam folder, then sign in again to send a new one.'}
        </p>
      </AuthShell>
    )
  }

  return (
    <AuthShell
      title="Welcome back"
      subtitle="Please sign in."
      footer={
        <>
          Don&apos;t have an account?{' '}
          <Link
            to="/signup"
            className="font-semibold text-accent-700 hover:underline dark:text-accent-300"
          >
            Sign up
          </Link>
        </>
      }
    >
      {(formError || redirectError) && (
        <div className="mb-4">
          <Alert>{formError ?? redirectError}</Alert>
        </div>
      )}

      {googleEnabled && (
        <>
          <a href={googleLoginUrl(returnTo)} className="btn-secondary w-full">
            <GoogleIcon className="size-5" />
            Continue with Google
          </a>
          <OrDivider />
        </>
      )}

      <form onSubmit={handleSubmit} className="space-y-4">
        <Field
          label="Email"
          type="email"
          name="email"
          autoComplete="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          error={fieldErrors.email}
          placeholder="you@example.com"
        />
        <Field
          label="Password"
          type="password"
          name="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={fieldErrors.password}
        />
        <button type="submit" disabled={submitting} className="btn-primary w-full">
          {submitting ? <Spinner label="Signing in" /> : 'Sign in'}
        </button>
      </form>
    </AuthShell>
  )
}
