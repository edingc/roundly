import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { api, isTwoFactorChallenge, restoreSession, setSession, setSessionExpiredHandler } from './api'
import type { DistanceUnit, Session, TwoFactorChallenge, User } from '../types'

interface AuthState {
  user: User | null
  /** True until the initial session restore finishes. */
  loading: boolean
  googleEnabled: boolean
  /** Whether this instance can place a course from its address. */
  geocodingEnabled: boolean
  /** Whether this instance can send mail. Two-factor and address confirmation
   *  are both hidden when it cannot. */
  emailEnabled: boolean
  /** Whether an unconfirmed account is shut out of the app. */
  emailVerificationRequired: boolean
  signUp: (email: string, password: string, displayName: string) => Promise<void>
  /**
   * Signs in. Resolves to null when the session is live, or to the challenge
   * standing in the way when a mailed code is needed — the caller renders the
   * code step rather than this hook navigating on its behalf.
   */
  logIn: (email: string, password: string) => Promise<TwoFactorChallenge | null>
  adoptSession: (session: Session) => void
  logOut: () => Promise<void>
  refreshUser: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [googleEnabled, setGoogleEnabled] = useState(false)
  const [geocodingEnabled, setGeocodingEnabled] = useState(false)
  const [emailEnabled, setEmailEnabled] = useState(false)
  const [emailVerificationRequired, setEmailVerificationRequired] = useState(false)

  // On boot, trade the stored refresh token for a live session. This is what
  // makes a page reload keep the user signed in.
  useEffect(() => {
    let cancelled = false

    async function boot() {
      const [session, config] = await Promise.all([
        restoreSession(),
        api.authConfig().catch(() => ({
          google_enabled: false,
          geocoding_enabled: false,
          email_enabled: false,
          email_verification_required: false,
        })),
      ])
      if (cancelled) return
      setUser(session?.user ?? null)
      setGoogleEnabled(config.google_enabled)
      setGeocodingEnabled(config.geocoding_enabled)
      setEmailEnabled(config.email_enabled)
      setEmailVerificationRequired(config.email_verification_required)
      setLoading(false)
    }

    void boot()
    return () => {
      cancelled = true
    }
  }, [])

  // When a refresh fails mid-session, drop the user so the router sends them to
  // the login screen instead of leaving a broken authenticated shell.
  useEffect(() => {
    setSessionExpiredHandler(() => setUser(null))
    return () => setSessionExpiredHandler(null)
  }, [])

  const adoptSession = useCallback((session: Session) => {
    setSession(session)
    setUser(session.user)
  }, [])

  const signUp = useCallback(
    async (email: string, password: string, displayName: string) => {
      adoptSession(await api.signUp(email, password, displayName))
    },
    [adoptSession],
  )

  const logIn = useCallback(
    async (email: string, password: string) => {
      const result = await api.logIn(email, password)
      if (isTwoFactorChallenge(result)) return result
      adoptSession(result)
      return null
    },
    [adoptSession],
  )

  const logOut = useCallback(async () => {
    try {
      await api.logOut()
    } catch {
      // A failed revoke should not trap the user in a session they have left;
      // the local tokens are discarded either way.
    }
    setSession(null)
    setUser(null)
  }, [])

  const refreshUser = useCallback(async () => {
    setUser(await api.me())
  }, [])

  const value = useMemo<AuthState>(
    () => ({
      user,
      loading,
      googleEnabled,
      geocodingEnabled,
      emailEnabled,
      emailVerificationRequired,
      signUp,
      logIn,
      adoptSession,
      logOut,
      refreshUser,
    }),
    [
      user,
      loading,
      googleEnabled,
      geocodingEnabled,
      emailEnabled,
      emailVerificationRequired,
      signUp,
      logIn,
      adoptSession,
      logOut,
      refreshUser,
    ],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used inside an AuthProvider')
  return context
}

/**
 * The unit the signed-in user reads distances in, for the screens that show or
 * collect one. Falls back to yards, which is both the server default and the
 * unit everything is stored in, so a screen rendering before the user loads
 * shows stored values unconverted rather than wrong.
 */
export function useDistanceUnit(): DistanceUnit {
  const { user } = useAuth()
  return user?.distance_unit ?? 'yards'
}
