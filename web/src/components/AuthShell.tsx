import type { ReactNode } from 'react'
import { useTheme } from '../lib/theme'
import { FlagIcon, MoonIcon, SunIcon } from './ui'

/** The centered card layout shared by the login and signup screens. */
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string
  subtitle: string
  children: ReactNode
  footer: ReactNode
}) {
  const { resolved, toggle } = useTheme()

  return (
    <div className="flex min-h-full flex-col items-center justify-center px-4 py-10">
      <button
        type="button"
        onClick={toggle}
        className="btn-ghost fixed top-4 right-4 !min-h-0 !px-2 !py-2"
        aria-label={resolved === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
      >
        {resolved === 'dark' ? <SunIcon className="size-5" /> : <MoonIcon className="size-5" />}
      </button>

      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3 text-center">
          <span className="flex size-12 items-center justify-center rounded-xl bg-brand-600 text-white dark:bg-brand-500 dark:text-brand-950">
            <FlagIcon className="size-7" />
          </span>
          <div>
            <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
            <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">{subtitle}</p>
          </div>
        </div>

        <div className="card p-6">{children}</div>

        <div className="mt-6 text-center text-sm text-slate-600 dark:text-slate-400">{footer}</div>
      </div>
    </div>
  )
}

/** The "or" rule between the Google button and the password form. */
export function OrDivider() {
  return (
    <div className="relative my-5">
      <div aria-hidden="true" className="absolute inset-0 flex items-center">
        <div className="w-full border-t border-slate-200 dark:border-slate-700" />
      </div>
      <div className="relative flex justify-center">
        <span className="bg-white px-3 text-xs font-medium tracking-wide text-slate-500 uppercase dark:bg-slate-900 dark:text-slate-400">
          or
        </span>
      </div>
    </div>
  )
}
