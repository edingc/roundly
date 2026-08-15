import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../lib/api'
import { useDistanceUnit } from '../lib/auth'
import { formatDistance, type DistanceUnit } from '../lib/units'
import type { Bag, Club, ClubOptions, ClubPayload, ClubStatus } from '../types'
import {
  Alert,
  ArchiveIcon,
  ChevronDownIcon,
  ConfirmDialog,
  EmptyState,
  PageSpinner,
  PlusIcon,
  WarningIcon,
  cx,
} from '../components/ui'
import {
  ClubDialog,
  clubToForm,
  clubTypeLabel,
  emptyClubForm,
  flexLabel,
  hasFullSpecs,
  type ClubFormValues,
} from '../components/ClubForm'

/** Which dialog is open, and what it is editing. */
type Editing = { mode: 'add' } | { mode: 'edit'; club: Club } | null

/**
 * Orders a group the way a bag is actually laid out: by club type longest
 * first, then by loft ascending within a type, so a 3 wood precedes a 5 wood
 * and a 52° wedge precedes a 60°.
 *
 * This is derived rather than hand-arranged. `display_order` is the stored
 * fallback for clubs with no loft, and is what a future drag-to-reorder would
 * write to.
 */
function bagOrder(clubs: Club[], clubTypes: string[]): Club[] {
  const rankOf = (type: string) => {
    const index = clubTypes.indexOf(type)
    // An unknown type sorts last rather than tying with the driver.
    return index === -1 ? clubTypes.length : index
  }

  return [...clubs].sort((a, b) => {
    const byType = rankOf(a.club_type) - rankOf(b.club_type)
    if (byType !== 0) return byType

    // Clubs without a loft fall to the end of their type group.
    const loftA = a.loft ?? Number.POSITIVE_INFINITY
    const loftB = b.loft ?? Number.POSITIVE_INFINITY
    if (loftA !== loftB) return loftA - loftB

    return a.display_order - b.display_order
  })
}

/**
 * The secondary meta line under a club's name: brand, model, shaft, flex, and
 * dispersion. Carry is not here — it earns a place on the first line, since it
 * is the number a player actually reaches for.
 *
 * Flex is dropped for a putter to match the form, which does not collect it.
 * Reading the club type rather than trusting the value to be null matters:
 * putters created before the form hid the field may still have one stored.
 */
function clubMeta(club: Club, unit: DistanceUnit): string {
  const full = hasFullSpecs(club.club_type)
  return [
    [club.brand, club.model].filter(Boolean).join(' '),
    club.shaft,
    full && club.flex ? flexLabel(club.flex) : null,
    club.average_dispersion === null
      ? null
      : `±${formatDistance(club.average_dispersion, unit)}`,
  ]
    .filter((part): part is string => Boolean(part && part.trim()))
    .join(' · ')
}

/** A compact row action. Deliberately smaller than the app's primary buttons. */
function RowAction({
  onClick,
  disabled,
  danger,
  children,
}: {
  onClick: () => void
  disabled?: boolean
  danger?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cx(
        'btn-secondary !min-h-0 !px-2.5 !py-1.5 !text-xs',
        danger &&
          '!text-red-700 hover:!bg-red-50 dark:!text-red-300 dark:hover:!bg-red-950/50',
      )}
    >
      {children}
    </button>
  )
}

function ClubRow({
  club,
  unit,
  busy,
  onMove,
  onEdit,
  onDelete,
}: {
  club: Club
  unit: DistanceUnit
  busy: boolean
  onMove: (status: ClubStatus) => void
  onEdit: () => void
  onDelete: () => void
}) {
  const meta = clubMeta(club, unit)

  return (
    <li
      className={cx(
        'flex flex-wrap items-start gap-x-3 gap-y-2 px-4 py-3',
        busy && 'opacity-60',
      )}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <span className="font-medium">{club.label}</span>
          {hasFullSpecs(club.club_type) && club.loft !== null && (
            <span className="text-sm text-slate-500 tabular-nums dark:text-slate-400">
              {club.loft}°
            </span>
          )}
          {club.expected_carry !== null && (
            <span className="text-sm font-medium text-brand-700 tabular-nums dark:text-brand-300">
              {formatDistance(club.expected_carry, unit)}
            </span>
          )}
        </div>
        {/* A club entered as nothing but "4 Iron" gets one line and no more.
            The type leads this line when it renders, but does not earn a line
            on its own — under a label like "4 Iron" it only repeats it. */}
        {meta && (
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            {clubTypeLabel(club.club_type)} · {meta}
          </p>
        )}
        {club.notes && (
          <p className="mt-1 text-xs text-slate-500 italic dark:text-slate-400">{club.notes}</p>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        {club.status !== 'active' && (
          <RowAction onClick={() => onMove('active')} disabled={busy}>
            Put in bag
          </RowAction>
        )}
        {club.status === 'active' && (
          <RowAction onClick={() => onMove('benched')} disabled={busy}>
            Take out
          </RowAction>
        )}
        {club.status === 'retired' ? (
          <RowAction onClick={() => onMove('benched')} disabled={busy}>
            Unretire
          </RowAction>
        ) : (
          <RowAction onClick={() => onMove('retired')} disabled={busy}>
            Retire
          </RowAction>
        )}
        <RowAction onClick={onEdit} disabled={busy}>
          Edit
        </RowAction>
        <RowAction onClick={onDelete} disabled={busy} danger>
          Delete
        </RowAction>
      </div>
    </li>
  )
}

function Section({
  title,
  count,
  description,
  children,
}: {
  title: React.ReactNode
  count: number
  description?: string
  children: React.ReactNode
}) {
  return (
    <section className="card overflow-hidden">
      <div className="border-b border-slate-200 px-4 py-3 dark:border-slate-800">
        <h2 className="font-semibold">{title}</h2>
        {description && (
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{description}</p>
        )}
      </div>
      {count === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-slate-500 dark:text-slate-400">
          Nothing here.
        </p>
      ) : (
        <ul className="divide-y divide-slate-200 dark:divide-slate-800">{children}</ul>
      )}
    </section>
  )
}

export default function GolfBagPage() {
  const unit = useDistanceUnit()
  const [bag, setBag] = useState<Bag | null>(null)
  const [options, setOptions] = useState<ClubOptions | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [editing, setEditing] = useState<Editing>(null)
  const [deleting, setDeleting] = useState<Club | null>(null)
  // The club with a status change in flight, so its row can disable itself
  // without freezing the rest of the bag.
  const [busyId, setBusyId] = useState<string | null>(null)

  const [showRetired, setShowRetired] = useState(false)

  const load = useCallback(async () => {
    try {
      const [nextBag, nextOptions] = await Promise.all([api.getBag(), api.clubOptions()])
      setBag(nextBag)
      setOptions(nextOptions)
      setError(null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load your bag.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function move(club: Club, status: ClubStatus) {
    setBusyId(club.id)
    setError(null)
    try {
      await api.setClubStatus(club.id, status)
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not move that club.')
    } finally {
      setBusyId(null)
    }
  }

  async function save(payload: ClubPayload) {
    if (!editing) return
    if (editing.mode === 'add') await api.createClub(payload)
    else await api.updateClub(editing.club.id, payload)
    setEditing(null)
    await load()
  }

  const clubTypes = options?.club_types ?? []
  const flexes = options?.flexes ?? []

  const active = bag ? bagOrder(bag.active, clubTypes) : []
  const benched = bag ? bagOrder(bag.benched, clubTypes) : []
  const retired = bag ? bagOrder(bag.retired, clubTypes) : []
  const totalClubs = active.length + benched.length + retired.length

  // A new club starts as an iron, the most common thing to be adding several
  // of, falling back to whatever the server lists first if that ever changes.
  const defaultType = clubTypes.includes('iron') ? 'iron' : (clubTypes[0] ?? 'iron')
  const initialForm: ClubFormValues | null = editing
    ? editing.mode === 'edit'
      ? clubToForm(editing.club, unit)
      : emptyClubForm(defaultType)
    : null

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Golf Bag</h1>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {bag
              ? `${bag.active_count} in the bag · ${totalClubs} club${totalClubs === 1 ? '' : 's'} owned`
              : 'Loading'}
          </p>
        </div>
        <button
          type="button"
          className="btn-primary ml-auto"
          onClick={() => setEditing({ mode: 'add' })}
          disabled={loading || !options}
        >
          <PlusIcon className="size-4" />
          Add club
        </button>
      </div>

      {error && <Alert>{error}</Alert>}

      {loading && !bag ? (
        <PageSpinner label="Loading your bag" />
      ) : totalClubs === 0 ? (
        <EmptyState
          title="Your bag is empty"
          description="Add the clubs you carry. Once rounds are in, every shot you record will point back at the club that hit it."
          action={
            <button
              type="button"
              className="btn-primary mt-2"
              onClick={() => setEditing({ mode: 'add' })}
            >
              <PlusIcon className="size-4" />
              Add club
            </button>
          }
        />
      ) : (
        <>
          {bag?.over_limit && (
            <Alert tone="warning">
              <span className="flex items-start gap-2">
                <WarningIcon className="mt-0.5 size-4 shrink-0" />
                <span>
                  You have {bag.active_count} clubs in the bag. The Rules of Golf allow{' '}
                  {bag.club_limit}, so a round played with these would carry a penalty. Take some
                  out before you tee off.
                </span>
              </span>
            </Alert>
          )}

          <Section
            title={
              <span className="flex items-center gap-2">
                In the bag
                <span
                  className={cx(
                    'rounded-full px-2 py-0.5 text-xs font-medium tabular-nums',
                    bag?.over_limit
                      ? 'bg-amber-100 text-amber-900 dark:bg-amber-900 dark:text-amber-100'
                      : 'bg-brand-100 text-brand-800 dark:bg-brand-900 dark:text-brand-100',
                  )}
                >
                  {bag?.active_count} / {bag?.club_limit}
                </span>
              </span>
            }
            count={active.length}
            description="The clubs you are carrying right now."
          >
            {active.map((club) => (
              <ClubRow
                key={club.id}
                club={club}
                unit={unit}
                busy={busyId === club.id}
                onMove={(status) => void move(club, status)}
                onEdit={() => setEditing({ mode: 'edit', club })}
                onDelete={() => setDeleting(club)}
              />
            ))}
          </Section>

          <Section
            title="Not in the bag"
            count={benched.length}
            description="Clubs you own but are not carrying. Swap them in whenever."
          >
            {benched.map((club) => (
              <ClubRow
                key={club.id}
                club={club}
                unit={unit}
                busy={busyId === club.id}
                onMove={(status) => void move(club, status)}
                onEdit={() => setEditing({ mode: 'edit', club })}
                onDelete={() => setDeleting(club)}
              />
            ))}
          </Section>

          {retired.length > 0 && (
            <section className="card overflow-hidden">
              <button
                type="button"
                onClick={() => setShowRetired((open) => !open)}
                className="flex w-full items-center gap-2 px-4 py-3 text-left hover:bg-slate-50 dark:hover:bg-slate-800/50"
                aria-expanded={showRetired}
              >
                <ArchiveIcon className="size-4 text-slate-400" />
                <span className="font-semibold">Retired</span>
                <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600 tabular-nums dark:bg-slate-800 dark:text-slate-300">
                  {retired.length}
                </span>
                <ChevronDownIcon
                  className={cx(
                    'ml-auto size-4 text-slate-400 transition-transform',
                    showRetired && 'rotate-180',
                  )}
                />
              </button>
              {showRetired && (
                <>
                  <p className="border-t border-slate-200 px-4 py-2 text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
                    Retired clubs are kept for statistical purposes.
                  </p>
                  <ul className="divide-y divide-slate-200 border-t border-slate-200 dark:divide-slate-800 dark:border-slate-800">
                    {retired.map((club) => (
                      <ClubRow
                        key={club.id}
                        club={club}
                        unit={unit}
                        busy={busyId === club.id}
                        onMove={(status) => void move(club, status)}
                        onEdit={() => setEditing({ mode: 'edit', club })}
                        onDelete={() => setDeleting(club)}
                      />
                    ))}
                  </ul>
                </>
              )}
            </section>
          )}
        </>
      )}

      {editing && initialForm && (
        <ClubDialog
          title={editing.mode === 'add' ? 'Add club' : 'Edit club'}
          initial={initialForm}
          submitLabel={editing.mode === 'add' ? 'Add club' : 'Save'}
          clubTypes={clubTypes}
          flexes={flexes}
          onCancel={() => setEditing(null)}
          onSubmit={save}
        />
      )}

      {deleting && (
        <ConfirmDialog
          title={`Delete ${deleting.label}?`}
          message={
            <>
              This removes the club permanently. If you have played with it, retire it instead —
              retired clubs stay attached to the shots they hit.
            </>
          }
          onCancel={() => setDeleting(null)}
          onConfirm={async () => {
            await api.deleteClub(deleting.id)
            setDeleting(null)
            await load()
          }}
        />
      )}
    </div>
  )
}
