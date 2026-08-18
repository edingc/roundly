import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { EllipsisIcon, cx } from './ui'

/**
 * The "more" menu on a list row.
 *
 * It exists so that a destructive action does not have to sit at full weight on
 * every row of a list. This app is used one-handed on a course, where a delete
 * button lives a thumb's width from the row somebody meant to open; behind a
 * menu it takes a deliberate second tap, and the row scans as content rather
 * than as a column of controls.
 *
 * Hand-built for the same reason UserMenu is - there is no dropdown primitive
 * here - and it gets the same three things right: it closes on an outside
 * click, closes on Escape, and hands focus back to the button it came from.
 * Unlike the header menu it opens upward-agnostic and right-aligned, since a
 * row can sit anywhere in a long list.
 */
export function RowMenu({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const menuId = useId()

  useEffect(() => {
    if (!open) return

    function onPointerDown(event: MouseEvent | TouchEvent) {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false)
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape') return
      setOpen(false)
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

  return (
    <div className="relative shrink-0" ref={containerRef}>
      <button
        ref={buttonRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        className="btn-ghost !min-h-0 !px-2 !py-1"
      >
        <EllipsisIcon className="size-4" />
      </button>

      {open && (
        <div
          id={menuId}
          role="menu"
          // Closing on the way out of any item, so a menu never outlives the
          // thing it acted on.
          onClick={() => setOpen(false)}
          className="card absolute right-0 z-30 mt-1 w-44 overflow-hidden py-1 shadow-lg"
        >
          {children}
        </div>
      )}
    </div>
  )
}

/**
 * One line in a RowMenu. `tone="danger"` is for the actions that destroy
 * something, which are red here rather than red-on-the-row: the colour is the
 * warning, and it only appears once somebody has gone looking.
 */
export function RowMenuItem({
  onClick,
  tone = 'default',
  children,
}: {
  onClick: () => void
  tone?: 'default' | 'danger'
  children: ReactNode
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={cx(
        'flex w-full items-center gap-2 px-3 py-2 text-left text-sm',
        tone === 'danger'
          ? 'text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/60'
          : 'text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800',
      )}
    >
      {children}
    </button>
  )
}
