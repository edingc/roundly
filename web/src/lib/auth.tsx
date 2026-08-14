import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { api, restoreSession, setSession, setSessionExpiredHandler } from './api'
import type { Session, User } from '../types'

interface AuthState {
  user: User | null
  /** True until the initial session restore finishes. */
  loading: boolean
  googleEnabled: boolean
  signUp: (email: string, password: string, displayName: string) => Promise<void>
  logIn: (email: string, password: string) => Promise<void>
  adoptSession: (session: Session) => void
  logOut: () => Promise<void>
  refreshUser: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [googleEnabled, setGoogleEnabled] = useState(false)

  // On boot, trade the stored refresh token for a live session. This is what
  // makes a page reload keep the user signed in.
  useEffect(() => {
    let cancelled = false

    async function boot() {
      const [session, config] = await Promise.all([
        restoreSession(),
        api.authConfig().catch(() => ({ google_enabled: false })),
      ])
      if (cancelled) return
      setUser(session?.user ?? null)
      setGoogleEnabled(config.google_enabled)
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
      adoptSession(await api.logIn(email, password))
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
    () => ({ user, loading, googleEnabled, signUp, logIn, adoptSession, logOut, refreshUser }),
    [user, loading, googleEnabled, signUp, logIn, adoptSession, logOut, refreshUser],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used inside an AuthProvider')
  return context
}
