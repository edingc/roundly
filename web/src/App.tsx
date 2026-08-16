import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useAuth } from './lib/auth'
import { hasSignedInBefore } from './lib/api'
import { AppLayout } from './components/AppLayout'
import { PageSpinner } from './components/ui'
import LoginPage from './pages/LoginPage'
import SignUpPage from './pages/SignUpPage'
import AuthCallbackPage from './pages/AuthCallbackPage'
import VerifyEmailPage from './pages/VerifyEmailPage'
import ConfirmEmailPage from './pages/ConfirmEmailPage'
import OverviewPage from './pages/OverviewPage'
import CourseListPage from './pages/CourseListPage'
import CourseDetailPage from './pages/CourseDetailPage'
import AddCoursePage from './pages/AddCoursePage'
import GolfBagPage from './pages/GolfBagPage'
import RoundListPage from './pages/RoundListPage'
import NewRoundPage from './pages/NewRoundPage'
import RoundDetailPage from './pages/RoundDetailPage'
import LiveRoundPage from './pages/LiveRoundPage'
import YardageChartPage from './pages/YardageChartPage'
import ProfilePage from './pages/ProfilePage'
import AdminPage from './pages/AdminPage'

/**
 * Sends unauthenticated visitors to sign up or sign in, remembering where they
 * were.
 *
 * Signup is the default because a freshly deployed instance has no accounts,
 * and a login form is a dead end for the person who just installed it. A
 * browser that has signed in before goes to the login form instead: an expired
 * session is not a reason to ask somebody to create a second account.
 */
function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading, emailVerificationRequired } = useAuth()
  const location = useLocation()

  if (loading) return <PageSpinner label="Restoring your session" />
  if (!user) {
    const returnTo = `${location.pathname}${location.search}`
    return <Navigate to={hasSignedInBefore() ? '/login' : '/signup'} state={{ returnTo }} replace />
  }
  // The server refuses every application endpoint for an unconfirmed account,
  // so rendering the app here would only produce a screen of failed requests.
  // This is the same rule, shown as a screen somebody can act on — and it is
  // deliberately in front of the layout rather than inside it, because there is
  // nothing to navigate to.
  if (emailVerificationRequired && !user.email_verified) {
    return <ConfirmEmailPage />
  }
  return <>{children}</>
}

/** Keeps signed-in users away from the login and signup screens. */
function RedirectIfSignedIn({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth()
  if (loading) return <PageSpinner label="Restoring your session" />
  if (user) return <Navigate to="/overview" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route
        path="/login"
        element={
          <RedirectIfSignedIn>
            <LoginPage />
          </RedirectIfSignedIn>
        }
      />
      <Route
        path="/signup"
        element={
          <RedirectIfSignedIn>
            <SignUpPage />
          </RedirectIfSignedIn>
        }
      />
      {/* Where the Google callback lands to redeem its one-time code. */}
      <Route path="/auth/callback" element={<AuthCallbackPage />} />
      {/* Where a confirmation link lands. Outside RequireAuth on purpose: the
          mail client picks the browser, and it is rarely the signed-in one. */}
      <Route path="/verify-email" element={<VerifyEmailPage />} />

      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route path="/overview" element={<OverviewPage />} />
        <Route path="/courses" element={<CourseListPage />} />
        <Route path="/courses/new" element={<AddCoursePage />} />
        <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        <Route path="/rounds" element={<RoundListPage />} />
        <Route path="/rounds/new" element={<NewRoundPage />} />
        {/* :roundID/play before :roundID would shadow nothing, but keeping the
            more specific path first matches how the rest of this file reads. */}
        <Route path="/rounds/:roundID/play" element={<LiveRoundPage />} />
        <Route path="/rounds/:roundID" element={<RoundDetailPage />} />
        <Route path="/bag" element={<GolfBagPage />} />
        <Route path="/bag/chart" element={<YardageChartPage />} />
        <Route path="/profile" element={<ProfilePage />} />
        <Route path="/admin" element={<AdminPage />} />
        {/* Settings became Profile. Kept so existing bookmarks still land. */}
        <Route path="/settings" element={<Navigate to="/profile" replace />} />
      </Route>

      <Route path="/" element={<Navigate to="/overview" replace />} />
      <Route path="*" element={<Navigate to="/overview" replace />} />
    </Routes>
  )
}
