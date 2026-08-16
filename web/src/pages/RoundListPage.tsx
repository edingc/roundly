/**
 * Every round, newest first, with the in-progress ones lifted to the top.
 *
 * Several rounds can be open at once - somebody who abandons one to weather and
 * starts another the next morning should not have to tidy up before playing -
 * so this is a picker rather than a single "resume" button.
 */
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import {
  describeRound,
  formatDifferential,
  formatPercent,
  formatPlayedOn,
  formatToPar,
} from '../lib/rounds'
import type { Round } from '../types'
import {
  Alert,
  ConfirmDialog,
  EmptyState,
  PageSpinner,
  PlusIcon,
  TrashIcon,
  cx,
} from '../components/ui'

const PAGE_SIZE = 25

export default function RoundListPage() {
  const [rounds, setRounds] = useState<Round[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<Round | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const page = await api.listRounds({ limit: PAGE_SIZE })
      setRounds(page.items)
      setTotal(page.total)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load your rounds.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  if (loading) return <PageSpinner label="Loading rounds" />

  const open = rounds.filter((r) => r.status === 'in_progress')
  const finished = rounds.filter((r) => r.status !== 'in_progress')

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Rounds</h1>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {total === 0 ? 'Nothing recorded yet.' : `${total} recorded.`}
          </p>
        </div>
        <Link to="/rounds/new" className="btn-primary">
          <PlusIcon className="size-5" />
          New round
        </Link>
      </div>

      {error && <Alert>{error}</Alert>}

      {open.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-lg font-semibold">In progress</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Pick one up where you left off, or clear it out.
          </p>
          <ul className="space-y-2">
            {open.map((round) => (
              <RoundRow key={round.id} round={round} onDelete={() => setDeleting(round)} />
            ))}
          </ul>
        </section>
      )}

      {finished.length > 0 && (
        <section className="space-y-3">
          {open.length > 0 && <h2 className="text-lg font-semibold">Finished</h2>}
          <ul className="space-y-2">
            {finished.map((round) => (
              <RoundRow key={round.id} round={round} onDelete={() => setDeleting(round)} />
            ))}
          </ul>
        </section>
      )}

      {rounds.length === 0 && !error && (
        <EmptyState
          title="No rounds yet"
          description="Start one on the course to score hole by hole, or enter a card you have already played."
          action={
            <Link to="/rounds/new" className="btn-primary">
              <PlusIcon className="size-5" />
              New round
            </Link>
          }
        />
      )}

      {deleting && (
        <ConfirmDialog
          title="Delete this round?"
          message={`${deleting.course_name} on ${formatPlayedOn(deleting.played_on)}. This cannot be undone.`}
          onCancel={() => setDeleting(null)}
          onConfirm={async () => {
            await api.deleteRound(deleting.id)
            setDeleting(null)
            await load()
          }}
        />
      )}
    </div>
  )
}

/** One label-and-value pair on a round row. */
function Stat({ label, value, title }: { label: string; value: string; title?: string }) {
  return (
    <div className="flex items-baseline gap-1" title={title}>
      <dt className="tracking-wide uppercase">{label}</dt>
      <dd className="font-medium text-slate-700 tabular-nums dark:text-slate-300">{value}</dd>
    </div>
  )
}

function RoundRow({ round, onDelete }: { round: Round; onDelete: () => void }) {
  const inProgress = round.status === 'in_progress'
  // An in-progress round opens where it is played; a finished one opens on its
  // card.
  const href = inProgress ? `/rounds/${round.id}/play` : `/rounds/${round.id}`

  return (
    <li className="card flex items-center gap-3 p-4">
      <Link to={href} className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-semibold">{round.course_name}</span>
          {inProgress && (
            <span className="rounded-full bg-sky-100 px-2 py-0.5 text-xs font-medium text-sky-800 dark:bg-sky-900 dark:text-sky-100">
              In progress
            </span>
          )}
          {round.status === 'abandoned' && (
            <span className="rounded-full bg-slate-200 px-2 py-0.5 text-xs font-medium text-slate-700 dark:bg-slate-700 dark:text-slate-200">
              Abandoned
            </span>
          )}
        </div>
        <p className="mt-0.5 text-sm text-slate-600 dark:text-slate-400">
          {formatPlayedOn(round.played_on)} · {describeRound(round)}
        </p>

        {/* The three numbers worth seeing without opening the round. Hidden
            entirely rather than shown as three dashes when a round has none of
            them - an in-progress card with no scores yet says nothing here. */}
        {round.summary.holes_recorded > 0 && (
          <dl className="mt-1 flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-slate-500 dark:text-slate-400">
            <Stat label="FIR" value={formatPercent(round.summary.fairways)} />
            <Stat label="GIR" value={formatPercent(round.summary.greens_in_regulation)} />
            <Stat
              label="Diff"
              value={formatDifferential(round.differential)}
              // A differential needs a complete card and a rated course, so it
              // is the one that is legitimately absent most often.
              title={
                round.differential === null
                  ? 'Needs a complete round on a course with a published rating and slope.'
                  : 'Score differential. Unofficial: computed from your gross score.'
              }
            />
          </dl>
        )}
      </Link>

      <div className="shrink-0 text-right">
        {round.summary.holes_completed > 0 ? (
          <>
            <p className="text-xl font-semibold tabular-nums">{round.summary.strokes}</p>
            <p
              className={cx(
                'text-xs font-medium tabular-nums',
                round.summary.to_par > 0
                  ? 'text-slate-500 dark:text-slate-400'
                  : 'text-brand-700 dark:text-brand-300',
              )}
            >
              {formatToPar(round.summary.to_par)}
              {round.summary.holes_completed < round.holes_intended &&
                ` · ${round.summary.holes_completed} holes`}
            </p>
          </>
        ) : (
          <p className="text-sm text-slate-500 dark:text-slate-400">No scores</p>
        )}
      </div>

      <button
        type="button"
        onClick={onDelete}
        aria-label={`Delete the round at ${round.course_name}`}
        className="btn-ghost !min-h-0 shrink-0 !px-2 !py-1"
      >
        <TrashIcon className="size-4" />
      </button>
    </li>
  )
}
