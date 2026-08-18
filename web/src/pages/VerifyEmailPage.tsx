/**
 * Where a confirmation link lands.
 *
 * Deliberately usable signed out. The link is opened by whichever browser the
 * mail client hands it to, which is routinely not the one that signed up, and a
 * login wall here would strand people holding a single-use token.
 */
import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { AuthShell } from '../components/AuthShell'
import { Alert, PageSpinner } from '../components/ui'

type Status = 'checking' | 'confirmed' | 'failed'

export default function VerifyEmailPage() {
  const [searchParams] = useSearchParams()
  const { user, refreshUser } = useAuth()
  const token = searchParams.get('token') ?? ''

  const [status, setStatus] = useState<Status>('checking')
  const [message, setMessage] = useState('')

  // The token is single-use, so this must fire exactly once. StrictMode
  // double-invokes effects in development, and without the guard the second
  // call would spend the token and report the first success as a failure.
  const attempted = useRef(false)

  useEffect(() => {
    if (attempted.current) return
    attempted.current = true

    if (!token) {
      setStatus('failed')
      setMessage('That link is missing its token. Open the link from the email itself.')
      return
    }

    let cancelled = false
    void (async () => {
      try {
        await api.verifyEmail(token)
        if (cancelled) return
        setStatus('confirmed')
        // Only when somebody is signed in here: this page is reachable without
        // a session, and refreshing one that does not exist would 401.
        if (user) await refreshUser()
      } catch (error) {
        if (cancelled) return
        setStatus('failed')
        setMessage(
          error instanceof ApiError
            ? error.message
            : 'Could not reach the server. Check your connection and try again.',
        )
      }
    })()

    return () => {
      cancelled = true
    }
    // Runs once. user and refreshUser are read, not depended on: re-running
    // this effect would replay a token that is already spent.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token])

  if (status === 'checking') {
    return <PageSpinner label="Confirming your email address" />
  }

  if (status === 'confirmed') {
    return (
      <AuthShell
        title="Email confirmed"
        subtitle="Your address is confirmed and your account is ready."
        footer={
          <Link to="/courses" className="font-semibold text-accent-700 hover:underline dark:text-accent-300">
            Go to Roundly
          </Link>
        }
      >
        <Alert tone="success">
          {user
            ? 'You are all set. Everything in the app is open to you now.'
            : 'You are all set. Sign in to start using Roundly.'}
        </Alert>
      </AuthShell>
    )
  }

  return (
    <AuthShell
      title="That link did not work"
      subtitle="Confirmation links are single-use and expire after a day."
      footer={
        <Link to="/login" className="font-semibold text-accent-700 hover:underline dark:text-accent-300">
          Back to sign in
        </Link>
      }
    >
      <Alert>{message}</Alert>
      <p className="mt-4 text-sm text-slate-600 dark:text-slate-400">
        Sign in and use the resend button on your profile to get a fresh one.
      </p>
    </AuthShell>
  )
}
