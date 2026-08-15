import { useState } from 'react'
import type { InputHTMLAttributes, ReactNode } from 'react'

/** Joins class names, dropping falsy entries. */
export function cx(...classes: Array<string | false | null | undefined>): string {
  return classes.filter(Boolean).join(' ')
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <span role="status" aria-label={label} className="inline-block">
      <svg className="size-5 animate-spin text-current" viewBox="0 0 24 24" fill="none">
        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
        <path
          className="opacity-90"
          fill="currentColor"
          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
        />
      </svg>
    </span>
  )
}

export function PageSpinner({ label = 'Loading' }: { label?: string }) {
  return (
    <div className="flex min-h-64 items-center justify-center text-slate-400">
      <Spinner label={label} />
    </div>
  )
}

/** A dismissible-looking banner for request-level errors. */
export function Alert({
  tone = 'error',
  children,
}: {
  tone?: 'error' | 'success' | 'info' | 'warning'
  children: ReactNode
}) {
  const tones = {
    error:
      'border-red-300 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950/60 dark:text-red-200',
    success:
      'border-brand-300 bg-brand-50 text-brand-900 dark:border-brand-800 dark:bg-brand-950/60 dark:text-brand-100',
    info: 'border-slate-300 bg-slate-50 text-slate-700 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200',
    // A rule the player is breaking, not a request that failed — amber rather
    // than red, and it never blocks the edit.
    warning:
      'border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/60 dark:text-amber-100',
  }
  return (
    <div role={tone === 'error' ? 'alert' : 'status'} className={cx('rounded-lg border px-4 py-3 text-sm', tones[tone])}>
      {children}
    </div>
  )
}

interface FieldProps extends InputHTMLAttributes<HTMLInputElement> {
  label: string
  error?: string
  hint?: string
  /**
   * A unit rendered inside the input against its right edge, e.g. "yds".
   *
   * Decorative: it is hidden from assistive tech, because a screen reader
   * would otherwise read it adrift from the value it belongs to. A field using
   * one has to put the unit back into the accessible name itself, with an
   * `aria-label` that still contains the visible label text ("Expected carry
   * in yards"), or into its `hint`.
   */
  suffix?: string
}

/** A labelled input that wires up its own error and hint associations. */
export function Field({ label, error, hint, suffix, id, className, ...rest }: FieldProps) {
  const inputId = id ?? `field-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`
  const errorId = `${inputId}-error`
  const hintId = `${inputId}-hint`

  return (
    <div>
      <label htmlFor={inputId} className="label">
        {label}
      </label>
      <div className="relative">
        <input
          id={inputId}
          className={cx('input', suffix && 'pr-12', error && 'input-error', className)}
          aria-invalid={error ? true : undefined}
          aria-describedby={cx(error && errorId, hint && hintId) || undefined}
          {...rest}
        />
        {suffix && (
          <span
            aria-hidden="true"
            // pointer-events-none so a click on the unit still lands in the
            // input underneath and puts the caret where the user aimed.
            className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-slate-500 dark:text-slate-400"
          >
            {suffix}
          </span>
        )}
      </div>
      {hint && !error && (
        <p id={hintId} className="mt-1.5 text-sm text-slate-500 dark:text-slate-400">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className="field-error">
          {error}
        </p>
      )}
    </div>
  )
}

/**
 * A modal that requires an explicit confirm click before a destructive action
 * runs, so nothing is deleted from a single accidental tap.
 */
export function ConfirmDialog({
  title,
  message,
  confirmLabel = 'Delete',
  danger = true,
  onCancel,
  onConfirm,
}: {
  title: string
  message: ReactNode
  confirmLabel?: string
  danger?: boolean
  onCancel: () => void
  onConfirm: () => Promise<void>
}) {
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleConfirm() {
    setConfirming(true)
    setError(null)
    try {
      await onConfirm()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'That could not be completed.')
      setConfirming(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-30 flex items-end justify-center bg-black/50 p-0 sm:items-center sm:p-4"
      role="alertdialog"
      aria-modal="true"
      aria-label={title}
    >
      <div className="card max-h-[90vh] w-full max-w-md overflow-y-auto rounded-b-none p-6 sm:rounded-xl">
        <h2 className="mb-2 text-lg font-semibold">{title}</h2>
        <p className="text-sm text-slate-600 dark:text-slate-400">{message}</p>
        {error && (
          <div className="mt-4">
            <Alert>{error}</Alert>
          </div>
        )}
        <div className="mt-6 flex gap-2">
          <button
            type="button"
            onClick={onCancel}
            disabled={confirming}
            className="btn-secondary flex-1"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => void handleConfirm()}
            disabled={confirming}
            className={cx('flex-1', danger ? 'btn-danger' : 'btn-primary')}
          >
            {confirming ? <Spinner label="Working" /> : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

/** An empty-state block for lists with nothing in them. */
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description: string
  action?: ReactNode
}) {
  return (
    <div className="card flex flex-col items-center gap-3 px-6 py-14 text-center">
      <div className="flex size-12 items-center justify-center rounded-full bg-brand-100 text-brand-700 dark:bg-brand-900 dark:text-brand-100">
        <FlagIcon className="size-6" />
      </div>
      <h2 className="text-lg font-semibold">{title}</h2>
      <p className="max-w-sm text-sm text-slate-600 dark:text-slate-400">{description}</p>
      {action}
    </div>
  )
}

/** A color-coded chip representing a tee. */
export function TeeChip({
  name,
  color,
  className,
}: {
  name: string
  color: string
  className?: string
}) {
  return (
    <span
      className={cx(
        'inline-flex items-center gap-1.5 rounded-full border border-slate-200 bg-white px-2.5 py-1',
        'text-xs font-medium text-slate-700',
        'dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200',
        className,
      )}
    >
      <span
        aria-hidden="true"
        className="size-2.5 shrink-0 rounded-full ring-1 ring-black/15 dark:ring-white/25"
        style={{ backgroundColor: color }}
      />
      {name}
    </span>
  )
}

// export function FlagIcon({ className }: { className?: string }) {
//   return (
//     <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
//       <path strokeLinecap="round" strokeLinejoin="round" d="M5 21V4m0 0l10 3-10 3" />
//       <path strokeLinecap="round" strokeLinejoin="round" d="M5 10l10 3 4-3-4-3" opacity="0.5" />
//     </svg>
//   )
// }

export function FlagIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 512 512"
      role="img"
      aria-label="Roundly"
    >
      <rect
        width="512"
        height="512"
        rx="112"
        fill="oklch(0.54 0.15 150)"
      />

      <g
        stroke="#ffffff"
        strokeWidth="26"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      >
        <path d="M170 400V128" />
        <path
          d="M170 128l150 46-150 46z"
          fill="#facc15"
          stroke="#facc15"
        />
      </g>

      <circle cx="300" cy="392" r="34" fill="#ffffff" />

      <g fill="oklch(0.54 0.15 150)">
        <circle cx="290" cy="382" r="4" />
        <circle cx="304" cy="384" r="4" />
        <circle cx="297" cy="396" r="4" />
        <circle cx="311" cy="398" r="4" />
      </g>
    </svg>
  )
}


export function GoogleIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="#4285F4"
        d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 01-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
      />
      <path
        fill="#34A853"
        d="M12 23c2.97 0 5.46-.98 7.28-2.65l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84A11 11 0 0012 23z"
      />
      <path
        fill="#FBBC05"
        d="M5.84 14.09a6.6 6.6 0 010-4.18V7.07H2.18a11 11 0 000 9.86l3.66-2.84z"
      />
      <path
        fill="#EA4335"
        d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
      />
    </svg>
  )
}

export function SunIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <circle cx="12" cy="12" r="4" />
      <path
        strokeLinecap="round"
        d="M12 2v2m0 16v2M4.93 4.93l1.41 1.41m11.32 11.32l1.41 1.41M2 12h2m16 0h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"
      />
    </svg>
  )
}

export function MoonIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
    </svg>
  )
}

export function PlusIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" d="M12 5v14M5 12h14" />
    </svg>
  )
}

export function TrashIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M3 6h18M8 6V4h8v2m-9 0l1 14h8l1-14"
      />
    </svg>
  )
}

export function ChevronLeftIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" />
    </svg>
  )
}

export function SearchIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path strokeLinecap="round" d="M20 20l-3.5-3.5" />
    </svg>
  )
}

export function DownloadIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v11m0 0l-4-4m4 4l4-4M4 19h16" />
    </svg>
  )
}

export function UploadIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 15V4m0 0l-4 4m4-4l4 4M4 19h16" />
    </svg>
  )
}

export function PencilIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M16.5 3.5a2.12 2.12 0 013 3L7 19l-4 1 1-4 12.5-12.5z"
      />
    </svg>
  )
}

export function ArchiveIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M3 7h18v3H3zM5 10v9a1 1 0 001 1h12a1 1 0 001-1v-9" />
      <path strokeLinecap="round" d="M10 14h4" />
    </svg>
  )
}

export function ChevronDownIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M6 9l6 6 6-6" />
    </svg>
  )
}

export function WarningIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M12 4l9 16H3l9-16z" />
      <path strokeLinecap="round" d="M12 10v4m0 3v.01" />
    </svg>
  )
}

export function PrinterIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path strokeLinecap="round" strokeLinejoin="round" d="M7 9V4h10v5" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M7 18H5a2 2 0 01-2-2v-4a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2h-2" />
      <path strokeLinecap="round" strokeLinejoin="round" d="M7 15h10v5H7z" />
    </svg>
  )
}

export function PinIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M14.5 2a1 1 0 01.7 1.7l-.5.5.9 4.6 2.6 2.6a1 1 0 01-.7 1.7H13v5a1 1 0 01-2 0v-5H6.5a1 1 0 01-.7-1.7l2.6-2.6.9-4.6-.5-.5A1 1 0 019.5 2z" />
    </svg>
  )
}
