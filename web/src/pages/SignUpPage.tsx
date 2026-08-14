import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { ApiError, googleLoginUrl } from '../lib/api'
import { AuthShell, OrDivider } from '../components/AuthShell'
import { Alert, Field, GoogleIcon, Spinner } from '../components/ui'

export default function SignUpPage() {
  const { signUp, googleEnabled } = useAuth()
  const [searchParams] = useSearchParams()

  const [displayName, setDisplayName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const redirectError = searchParams.get('error')

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setFormError(null)
    setFieldErrors({})

    try {
      await signUp(email, password, displayName)
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
      title="Create your account"
      subtitle="Start building your course library."
      footer={
        <>
          Already have an account?{' '}
          <Link
            to="/login"
            className="font-semibold text-brand-700 hover:underline dark:text-brand-400"
          >
            Sign in
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
          <a href={googleLoginUrl('/courses')} className="btn-secondary w-full">
            <GoogleIcon className="size-5" />
            Continue with Google
          </a>
          <OrDivider />
        </>
      )}

      <form onSubmit={handleSubmit} className="space-y-4">
        <Field
          label="Name"
          name="display_name"
          autoComplete="name"
          required
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          error={fieldErrors.display_name}
          placeholder="Cody"
        />
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
          autoComplete="new-password"
          required
          minLength={8}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          error={fieldErrors.password}
          hint="At least 8 characters."
        />
        <button type="submit" disabled={submitting} className="btn-primary w-full">
          {submitting ? <Spinner label="Creating account" /> : 'Create account'}
        </button>
      </form>
    </AuthShell>
  )
}
