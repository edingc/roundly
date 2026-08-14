import { useEffect, useRef, useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { PageSpinner } from '../components/ui'

/**
 * Lands after Google sign-in and trades the one-time handoff code for a session.
 *
 * The server cannot hand tokens to a redirect without putting them in the URL,
 * so it sends a short-lived single-use code instead and this screen redeems it
 * over XHR.
 */
export default function AuthCallbackPage() {
  const [searchParams] = useSearchParams()
  const { adoptSession, user } = useAuth()
  const [error, setError] = useState<string | null>(null)
  // The code is single-use, so guard against StrictMode's double effect run.
  const redeemed = useRef(false)

  const code = searchParams.get('code')

  useEffect(() => {
    if (redeemed.current) return
    redeemed.current = true

    if (!code) {
      setError('That sign-in link was missing its code.')
      return
    }

    api
      .exchangeGoogleCode(code)
      .then(adoptSession)
      .catch(() => setError('That sign-in link has already been used or has expired.'))
  }, [code, adoptSession])

  if (error) {
    return <Navigate to={`/login?error=${encodeURIComponent(error)}`} replace />
  }
  if (user) {
    return <Navigate to="/courses" replace />
  }
  return <PageSpinner label="Finishing sign-in" />
}
