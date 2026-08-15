import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useAuth } from './lib/auth'
import { AppLayout } from './components/AppLayout'
import { PageSpinner } from './components/ui'
import LoginPage from './pages/LoginPage'
import SignUpPage from './pages/SignUpPage'
import AuthCallbackPage from './pages/AuthCallbackPage'
import CourseListPage from './pages/CourseListPage'
import CourseDetailPage from './pages/CourseDetailPage'
import AddCoursePage from './pages/AddCoursePage'
import GolfBagPage from './pages/GolfBagPage'
import YardageChartPage from './pages/YardageChartPage'
import SettingsPage from './pages/SettingsPage'

/** Sends unauthenticated visitors to the login screen, remembering where they were. */
function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth()
  const location = useLocation()

  if (loading) return <PageSpinner label="Restoring your session" />
  if (!user) {
    const returnTo = `${location.pathname}${location.search}`
    return <Navigate to="/login" state={{ returnTo }} replace />
  }
  return <>{children}</>
}

/** Keeps signed-in users away from the login and signup screens. */
function RedirectIfSignedIn({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth()
  if (loading) return <PageSpinner label="Restoring your session" />
  if (user) return <Navigate to="/courses" replace />
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

      <Route
        element={
          <RequireAuth>
            <AppLayout />
          </RequireAuth>
        }
      >
        <Route path="/courses" element={<CourseListPage />} />
        <Route path="/courses/new" element={<AddCoursePage />} />
        <Route path="/courses/:courseId" element={<CourseDetailPage />} />
        <Route path="/bag" element={<GolfBagPage />} />
        <Route path="/bag/chart" element={<YardageChartPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>

      <Route path="/" element={<Navigate to="/courses" replace />} />
      <Route path="*" element={<Navigate to="/courses" replace />} />
    </Routes>
  )
}
