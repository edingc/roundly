import type { User } from '../types'
import { cx } from './ui'

/**
 * A user's photo, falling back to their initials.
 *
 * The image is a plain <img> against an unguessable, immutable URL — no bearer
 * token, because a tag cannot send one. The key in that URL rotates on every
 * upload, so the browser can cache it forever and a replacement invalidates the
 * old one by changing the address rather than by asking a cache nicely.
 */
export function Avatar({
  user,
  className,
}: {
  user: Pick<User, 'display_name' | 'first_name' | 'last_name' | 'avatar_url'> | null
  className?: string
}) {
  const base = 'shrink-0 overflow-hidden rounded-full'

  if (user?.avatar_url) {
    return (
      <img
        src={user.avatar_url}
        alt=""
        // Decorative: the display name is always rendered beside it, so
        // announcing the photo too would just repeat the name.
        aria-hidden="true"
        className={cx(base, 'object-cover', className)}
      />
    )
  }

  return (
    <span
      aria-hidden="true"
      className={cx(
        base,
        'flex items-center justify-center bg-brand-100 font-semibold text-brand-800',
        'dark:bg-brand-900 dark:text-brand-100',
        className,
      )}
    >
      <span className="text-[0.7em]">{initials(user)}</span>
    </span>
  )
}

/**
 * Up to two initials. Prefers the real name when there is one, because
 * "Cody Eding" reads better as CE than a display name like "cody99" does as C9.
 */
function initials(
  user: Pick<User, 'display_name' | 'first_name' | 'last_name'> | null,
): string {
  if (!user) return '?'

  const first = user.first_name?.trim()
  const last = user.last_name?.trim()
  if (first || last) {
    return ((first?.[0] ?? '') + (last?.[0] ?? '')).toUpperCase() || '?'
  }

  const parts = user.display_name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}
