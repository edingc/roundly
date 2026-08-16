/**
 * A round's scorecard: the grid you type a card into, and the card you read
 * afterwards. One screen for both, because they are the same table and a
 * finished round stays editable - this is a logbook, not a competition record.
 *
 * Score and putts are always visible; everything else is behind a toggle.
 * Backfilling a season wants speed, and a round you paid attention to wants the
 * record, and those are different visits to the same page.
 */
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import {
  ACCURACY_LABELS,
  PENALTY_LABELS,
  PENALTY_TYPES,
  describeRound,
  formatPlayedOn,
  targetLabel,
  toHolePayload,
} from '../lib/rounds'
import type { Bag, Club, Round, RoundHolePayload, TeeAccuracy } from '../types'
import { RoundSummaryCard } from '../components/RoundSummaryCard'
import {
  Alert,
  ChevronLeftIcon,
  ConfirmDialog,
  PageSpinner,
  Spinner,
  TrashIcon,
  cx,
} from '../components/ui'

const ACCURACIES: TeeAccuracy[] = [
  'hit', 'left', 'far_left', 'right', 'far_right', 'long', 'short', 'mishit',
]

export default function RoundDetailPage() {
  const { roundID = '' } = useParams()
  const navigate = useNavigate()

  const [round, setRound] = useState<Round | null>(null)
  const [clubs, setClubs] = useState<Club[]>([])
  const [rows, setRows] = useState<RoundHolePayload[]>([])
  const [showDetail, setShowDetail] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const load = useCallback(async () => {
    try {
      const [loaded, bag] = await Promise.all([
        api.getRound(roundID),
        api.getBag().catch(() => ({
            active: [],
            benched: [],
            retired: [],
            active_count: 0,
            club_limit: 0,
            over_limit: false,
          }) as Bag),
      ])
      setRound(loaded)
      setRows(loaded.holes.map(toHolePayload))
      // Retired clubs included: a round played with a club you have since sold
      // still has to name it.
      setClubs([...bag.active, ...bag.benched, ...bag.retired])
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load that round.')
    } finally {
      setLoading(false)
    }
  }, [roundID])

  useEffect(() => {
    void load()
  }, [load])

  const dirty = useMemo(() => {
    if (!round) return false
    return JSON.stringify(rows) !== JSON.stringify(round.holes.map(toHolePayload))
  }, [rows, round])

  function patch(holeNumber: number, changes: Partial<RoundHolePayload>) {
    setRows((current) =>
      current.map((r) => (r.hole_number === holeNumber ? { ...r, ...changes } : r)),
    )
  }

  async function handleSave() {
    setSaving(true)
    setError(null)
    setNotice(null)
    try {
      const updated = await api.saveRoundHoles(roundID, rows)
      setRound(updated)
      setRows(updated.holes.map(toHolePayload))
      setNotice('Scorecard saved.')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not save that scorecard.')
    } finally {
      setSaving(false)
    }
  }

  async function changeStatus(fn: (id: string) => Promise<Round>) {
    setError(null)
    try {
      setRound(await fn(roundID))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not update that round.')
    }
  }

  if (loading) return <PageSpinner label="Loading round" />
  if (!round) return <Alert>{error ?? 'That round could not be found.'}</Alert>

  const holesByNumber = new Map(round.holes.map((h) => [h.hole_number, h]))

  return (
    <div className="space-y-6">
      <div>
        <Link to="/rounds" className="btn-ghost !min-h-0 !px-2 !py-1 text-sm">
          <ChevronLeftIcon className="size-4" />
          Rounds
        </Link>
      </div>

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{round.course_name}</h1>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {formatPlayedOn(round.played_on)} · {describeRound(round)}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {round.status === 'in_progress' && (
            <>
              <Link to={`/rounds/${round.id}/play`} className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm">
                Play hole by hole
              </Link>
              <button
                type="button"
                className="btn-primary !min-h-0 !px-3 !py-2 !text-sm"
                onClick={() => void changeStatus(api.completeRound)}
              >
                Finish round
              </button>
            </>
          )}
          {round.status !== 'in_progress' && (
            <button
              type="button"
              className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm"
              onClick={() => void changeStatus(api.reopenRound)}
            >
              Reopen
            </button>
          )}
          <button
            type="button"
            className="btn-ghost !min-h-0 !px-2 !py-2"
            aria-label="Delete this round"
            onClick={() => setConfirmDelete(true)}
          >
            <TrashIcon className="size-4" />
          </button>
        </div>
      </div>

      {error && <Alert>{error}</Alert>}
      {notice && <Alert tone="success">{notice}</Alert>}
      {round.status === 'abandoned' && (
        <Alert tone="info">
          This round was abandoned. It keeps its holes and stays out of your averages.
        </Alert>
      )}

      <RoundSummaryCard round={round} />

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-lg font-semibold">Scorecard</h2>
          <button
            type="button"
            className="btn-secondary !min-h-0 !px-3 !py-2 !text-sm"
            onClick={() => setShowDetail((v) => !v)}
            aria-expanded={showDetail}
          >
            {showDetail ? 'Hide detail columns' : 'Show detail columns'}
          </button>
        </div>

        {/* The table scrolls inside its own box rather than pushing the page
            sideways, which is what keeps eleven columns usable on a laptop. */}
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b border-slate-200 text-left text-xs tracking-wide text-slate-500 uppercase dark:border-slate-700 dark:text-slate-400">
              <tr>
                <th className="px-3 py-2">Hole</th>
                <th className="px-2 py-2">Par</th>
                <th className="px-2 py-2">Yds</th>
                <th className="px-2 py-2">Score</th>
                <th className="px-2 py-2">Putts</th>
                {/* Always visible: a fairway is a headline statistic, and it was
                    behind the detail toggle where nobody would find it. */}
                <th className="px-2 py-2">Tee shot</th>
                {showDetail && (
                  <>
                    <th className="px-2 py-2">Tee club</th>
                    <th className="px-2 py-2">Bunkers</th>
                    <th className="px-2 py-2">1st putt</th>
                    <th className="px-2 py-2">Pen</th>
                  </>
                )}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {rows.map((row) => {
                const snapshot = holesByNumber.get(row.hole_number)
                return (
                  <tr key={row.hole_number}>
                    <td className="px-3 py-1.5 font-medium tabular-nums">{row.hole_number}</td>
                    <td className="px-2 py-1.5 tabular-nums text-slate-500 dark:text-slate-400">
                      {snapshot?.par ?? '—'}
                    </td>
                    <td className="px-2 py-1.5 tabular-nums text-slate-500 dark:text-slate-400">
                      {snapshot?.yardage ?? '—'}
                    </td>
                    <td className="px-2 py-1.5">
                      <NumberCell
                        label={`Score for hole ${row.hole_number}`}
                        value={row.strokes}
                        min={1}
                        max={20}
                        onChange={(v) => patch(row.hole_number, { strokes: v })}
                      />
                    </td>
                    <td className="px-2 py-1.5">
                      <NumberCell
                        label={`Putts for hole ${row.hole_number}`}
                        value={row.putts}
                        min={0}
                        max={10}
                        onChange={(v) => patch(row.hole_number, { putts: v })}
                      />
                    </td>

                    <td className="px-2 py-1.5">
                      <select
                        aria-label={`Tee shot result for hole ${row.hole_number}`}
                        className="input !min-h-0 !py-1 !text-sm"
                        value={row.tee_accuracy ?? ''}
                        onChange={(e) =>
                          patch(row.hole_number, {
                            tee_accuracy: (e.target.value || null) as TeeAccuracy | null,
                          })
                        }
                      >
                        <option value="">—</option>
                        {ACCURACIES.map((a) => (
                          <option key={a} value={a}>
                            {a === 'hit' ? targetLabel(snapshot?.par ?? null) : ACCURACY_LABELS[a]}
                          </option>
                        ))}
                      </select>
                    </td>

                    {showDetail && (
                      <>
                        <td className="px-2 py-1.5">
                          <select
                            aria-label={`Tee club for hole ${row.hole_number}`}
                            className="input !min-h-0 !py-1 !text-sm"
                            value={row.tee_club_id ?? ''}
                            onChange={(e) =>
                              patch(row.hole_number, { tee_club_id: e.target.value || null })
                            }
                          >
                            <option value="">—</option>
                            {clubs.map((c) => (
                              <option key={c.id} value={c.id}>
                                {c.label}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td className="px-2 py-1.5">
                          <div className="flex gap-2 text-xs">
                            <label className="inline-flex items-center gap-1">
                              <input
                                type="checkbox"
                                checked={row.fairway_bunker}
                                onChange={(e) =>
                                  patch(row.hole_number, { fairway_bunker: e.target.checked })
                                }
                              />
                              Fwy
                            </label>
                            <label className="inline-flex items-center gap-1">
                              <input
                                type="checkbox"
                                checked={row.greenside_bunker}
                                onChange={(e) =>
                                  patch(row.hole_number, { greenside_bunker: e.target.checked })
                                }
                              />
                              Grn
                            </label>
                          </div>
                        </td>
                        <td className="px-2 py-1.5">
                          <NumberCell
                            label={`First putt distance for hole ${row.hole_number}`}
                            value={row.first_putt_feet}
                            min={0}
                            max={200}
                            suffix="ft"
                            onChange={(v) => patch(row.hole_number, { first_putt_feet: v })}
                          />
                        </td>
                        <td className="px-2 py-1.5">
                          <div className="flex items-center gap-1">
                            <NumberCell
                              label={`Penalty strokes for hole ${row.hole_number}`}
                              value={row.penalties === 0 ? null : row.penalties}
                              min={0}
                              max={10}
                              onChange={(v) =>
                                patch(row.hole_number, {
                                  penalties: v ?? 0,
                                  // A reason with no penalty is a contradiction
                                  // the server rejects, so clear it here too.
                                  penalty_type: v ? row.penalty_type : null,
                                })
                              }
                            />
                            {row.penalties > 0 && (
                              <select
                                aria-label={`Penalty reason for hole ${row.hole_number}`}
                                className="input !min-h-0 !py-1 !text-xs"
                                value={row.penalty_type ?? ''}
                                onChange={(e) =>
                                  patch(row.hole_number, {
                                    penalty_type: (e.target.value || null) as never,
                                  })
                                }
                              >
                                <option value="">—</option>
                                {PENALTY_TYPES.map((p) => (
                                  <option key={p} value={p}>
                                    {PENALTY_LABELS[p]}
                                  </option>
                                ))}
                              </select>
                            )}
                          </div>
                        </td>
                      </>
                    )}
                  </tr>
                )
              })}
            </tbody>
            <tfoot className="border-t border-slate-200 font-semibold dark:border-slate-700">
              <tr>
                <td className="px-3 py-2">Total</td>
                <td className="px-2 py-2 tabular-nums">{round.summary.par || '—'}</td>
                <td />
                <td className="px-2 py-2 tabular-nums">{round.summary.strokes || '—'}</td>
                <td className="px-2 py-2 tabular-nums">{round.summary.putts || '—'}</td>
                <td className="px-2 py-2 text-xs font-normal text-slate-500 dark:text-slate-400">
                  {round.summary.fairways.attempted > 0
                    ? `${round.summary.fairways.made}/${round.summary.fairways.attempted} fairways`
                    : '—'}
                </td>
                {showDetail && <td colSpan={4} />}
              </tr>
            </tfoot>
          </table>
        </div>

        <div className="flex items-center gap-3">
          <button
            type="button"
            className="btn-primary"
            disabled={saving || !dirty}
            onClick={() => void handleSave()}
          >
            {saving ? <Spinner label="Saving" /> : 'Save scorecard'}
          </button>
          {dirty && (
            <p className="text-sm text-amber-700 dark:text-amber-300">Unsaved changes.</p>
          )}
        </div>
      </section>

      {confirmDelete && (
        <ConfirmDialog
          title="Delete this round?"
          message="The scorecard and every statistic from it go with it. This cannot be undone."
          onCancel={() => setConfirmDelete(false)}
          onConfirm={async () => {
            await api.deleteRound(roundID)
            navigate('/rounds', { replace: true })
          }}
        />
      )}
    </div>
  )
}

/**
 * A numeric cell that treats an empty box as "no value", not as zero.
 *
 * That distinction is the whole reason this is not a plain number input: a hole
 * with no score is a hole that was picked up, and a hole with a score of zero
 * does not exist.
 */
function NumberCell({
  label,
  value,
  min,
  max,
  suffix,
  onChange,
}: {
  label: string
  value: number | null
  min: number
  max: number
  suffix?: string
  onChange: (value: number | null) => void
}) {
  return (
    <div className="flex items-center gap-1">
      <input
        type="number"
        inputMode="numeric"
        aria-label={label}
        className={cx('input !min-h-0 !w-16 !py-1 text-center !text-sm tabular-nums')}
        value={value ?? ''}
        min={min}
        max={max}
        onChange={(e) => {
          const raw = e.target.value
          if (raw === '') {
            onChange(null)
            return
          }
          const parsed = Number(raw)
          if (Number.isNaN(parsed)) return
          onChange(Math.min(max, Math.max(min, Math.trunc(parsed))))
        }}
      />
      {suffix && <span className="text-xs text-slate-400">{suffix}</span>}
    </div>
  )
}
