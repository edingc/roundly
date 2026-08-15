import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { Avatar } from './Avatar'
import { ChevronDownIcon, cx } from './ui'

/**
 * The signed-in user's menu in the header.
 *
 * Hand-built because this codebase has no dropdown primitive and no positioning
 * library, so the three things a menu has to get right are done explicitly:
 * it closes on an outside click, closes on Escape, and returns focus to the
 * button it came from so keyboard navigation does not get stranded.
 */
export function UserMenu() {
  const { user, logOut } = useAuth()
  const navigate = useNavigate()

  const [open, setOpen] = useState(false)
  const [signingOut, setSigningOut] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return

    function onPointerDown(event: MouseEvent | TouchEvent) {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false)
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape') return
      setOpen(false)
      // Escape should hand control back where it came from, not drop it on the
      // document.
      buttonRef.current?.focus()
    }

    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('touchstart', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('touchstart', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  async function handleSignOut() {
    setSigningOut(true)
    await logOut()
    navigate('/login', { replace: true })
  }

  const itemClass =
    'flex w-full items-center gap-2 px-3 py-2 text-left text-sm text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800'

  return (
    <div className="relative" ref={containerRef}>
      <button
        ref={buttonRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex items-center gap-2 rounded-lg px-1.5 py-1.5 transition-colors hover:bg-slate-100 dark:hover:bg-slate-800"
      >
        <Avatar user={user} className="size-8" />
        {/* The name is redundant on a phone, where the space matters more. */}
        <span className="hidden max-w-32 truncate text-sm text-slate-600 sm:inline dark:text-slate-300">
          {user?.display_name}
        </span>
        <ChevronDownIcon
          className={cx('size-4 text-slate-400 transition-transform', open && 'rotate-180')}
        />
      </button>

      {open && (
        // z-30 to clear the sticky z-20 header it hangs from.
        <div
          role="menu"
          className="card absolute right-0 z-30 mt-1 w-56 overflow-hidden py-1 shadow-lg"
        >
          <div className="border-b border-slate-200 px-3 py-2 dark:border-slate-800">
            <p className="truncate text-sm font-medium">{user?.display_name}</p>
            <p className="truncate text-xs text-slate-500 dark:text-slate-400">{user?.email}</p>
          </div>

          <Link to="/profile" role="menuitem" className={itemClass} onClick={() => setOpen(false)}>
            Profile
          </Link>

          <button
            type="button"
            role="menuitem"
            onClick={() => void handleSignOut()}
            disabled={signingOut}
            className={cx(itemClass, 'disabled:opacity-60')}
          >
            {signingOut ? 'Signing out…' : 'Sign out'}
          </button>
        </div>
      )}
    </div>
  )
}
