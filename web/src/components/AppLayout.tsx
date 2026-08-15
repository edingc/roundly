import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useState } from 'react'
import { useAuth } from '../lib/auth'
import { useTheme } from '../lib/theme'
import { FlagIcon, MoonIcon, SunIcon, cx } from './ui'

function ThemeToggle() {
  const { resolved, toggle } = useTheme()
  return (
    <button
      type="button"
      onClick={toggle}
      className="btn-ghost !min-h-0 !px-2 !py-2"
      aria-label={resolved === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
      title={resolved === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
    >
      {resolved === 'dark' ? <SunIcon className="size-5" /> : <MoonIcon className="size-5" />}
    </button>
  )
}

/** The signed-in chrome: header, nav, and the routed page body. */
export function AppLayout() {
  const { user, logOut } = useAuth()
  const navigate = useNavigate()
  const [signingOut, setSigningOut] = useState(false)

  async function handleSignOut() {
    setSigningOut(true)
    await logOut()
    navigate('/login', { replace: true })
  }

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    cx(
      'rounded-lg px-3 py-2 text-sm font-medium transition-colors',
      isActive
        ? 'bg-brand-100 text-brand-800 dark:bg-brand-900 dark:text-brand-100'
        : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100',
    )

  return (
    <div className="flex min-h-full flex-col">
      <header className="no-print sticky top-0 z-20 border-b border-slate-200 bg-white/90 backdrop-blur dark:border-slate-800 dark:bg-slate-950/90">
        <div className="mx-auto flex max-w-5xl items-center gap-2 px-4 py-3">
          <NavLink to="/courses" className="flex items-center gap-2 font-semibold tracking-tight">
            <span className="flex size-8 items-center justify-center rounded-lg bg-brand-600 text-white">
              <FlagIcon className="size-5" />
            </span>
            <span>Roundly</span>
          </NavLink>

          <nav className="ml-2 flex items-center gap-1">
            <NavLink to="/courses" className={navLinkClass}>
              Courses
            </NavLink>
            <NavLink to="/bag" className={navLinkClass}>
              Bag
            </NavLink>
            <NavLink to="/settings" className={navLinkClass}>
              Settings
            </NavLink>
          </nav>

          <div className="ml-auto flex items-center gap-1">
            <ThemeToggle />
            {/* The name is redundant on a phone, where the space matters more. */}
            <span className="hidden text-sm text-slate-500 sm:inline dark:text-slate-400">
              {user?.display_name}
            </span>
            <button
              type="button"
              onClick={handleSignOut}
              disabled={signingOut}
              className="btn-ghost !min-h-0 !px-2.5 !py-2"
            >
              {signingOut ? 'Signing out…' : 'Sign out'}
            </button>
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-6 pb-[max(1.5rem,env(safe-area-inset-bottom))]">
        <Outlet />
      </main>
    </div>
  )
}
