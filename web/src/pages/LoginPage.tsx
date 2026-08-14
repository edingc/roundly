import { useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { ApiError, googleLoginUrl } from '../lib/api'
import { AuthShell, OrDivider } from '../components/AuthShell'
import { Alert, Field, GoogleIcon, Spinner } from '../components/ui'

export default function LoginPage() {
  const { logIn, googleEnabled } = useAuth()
  const location = useLocation()
  const [searchParams] = useSearchParams()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  // The Google callback reports failures by redirecting back here with ?error.
  const redirectError = searchParams.get('error')
  const returnTo = (location.state as { returnTo?: string } | null)?.returnTo ?? '/courses'

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setFormError(null)
    setFieldErrors({})

    try {
      await logIn(email, password)
      // Navigation is handled by RedirectIfSignedIn once `user` is set.
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.isValidation) setFieldErrors(error.fields)
        else setFormError(error.message)
      } else {
        setFormError('Could not reach the server. Check your connection and try again.')
      }
      setSubmitting(false)
    }
  }

  return (
    <AuthShell
      title="Welcome"
      subtitle="Please sign in."
      footer={
        <>
          Don&apos;t have an account?{' '}
          <Link
            to="/signup"
            className="font-semibold text-brand-700 hover:underline dark:text-brand-300"
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
