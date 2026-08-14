import { useCallback, useEffect, useMemo, useState } from 'react'
import type { CourseDetail, Hole, Tee } from '../types'
import { api } from '../lib/api'
import { cx } from './ui'

type CellStatus = 'idle' | 'saving' | 'saved' | 'error'

interface CellDraft {
  par: string
  yardage: string
}

function cellKey(holeId: string, teeId: string): string {
  return `${holeId}:${teeId}`
}

/** Builds the editable draft state from the server's course detail. */
function buildDrafts(holes: Hole[]): Record<string, CellDraft> {
  const drafts: Record<string, CellDraft> = {}
  for (const hole of holes) {
    for (const detail of hole.tee_details) {
      drafts[cellKey(hole.id, detail.tee_id)] = {
        par: String(detail.par),
        yardage: String(detail.yardage),
      }
    }
  }
  return drafts
}

function buildHandicapDrafts(holes: Hole[]): Record<string, string> {
  const drafts: Record<string, string> = {}
  for (const hole of holes) {
    drafts[hole.id] = hole.handicap_index === null ? '' : String(hole.handicap_index)
  }
  return drafts
}

/**
 * The par/yardage table: one row per hole, one column pair per tee.
 *
 * Each cell is its own upsert against
 * `PUT /holes/{id}/tee-details/{tee_id}`, saved on blur rather than per
 * keystroke. A cell is only sent once both par and yardage are present, because
 * the API requires both together.
 */
export function ScorecardGrid({
  course,
  editable,
  onCourseChanged,
}: {
  course: CourseDetail
  editable: boolean
  onCourseChanged: () => void
}) {
  const [drafts, setDrafts] = useState<Record<string, CellDraft>>(() => buildDrafts(course.holes))
  const [handicaps, setHandicaps] = useState<Record<string, string>>(() =>
    buildHandicapDrafts(course.holes),
  )
  const [statuses, setStatuses] = useState<Record<string, CellStatus>>({})
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  // Merge the reloaded course into the local drafts, keeping what the user has
  // typed. Local values win on purpose: saving one cell reloads the whole
  // course, and replacing the drafts wholesale would wipe a value the user had
  // already typed into another cell but not yet blurred — which silently lost
  // edits. Cells for holes or tees that no longer exist are dropped.
  useEffect(() => {
    const serverDrafts = buildDrafts(course.holes)
    setDrafts((prev) => {
      const merged: Record<string, CellDraft> = {}
      for (const hole of course.holes) {
        for (const tee of course.tees) {
          const key = cellKey(hole.id, tee.id)
          const value = prev[key] ?? serverDrafts[key]
          if (value) merged[key] = value
        }
      }
      return merged
    })

    const serverHandicaps = buildHandicapDrafts(course.holes)
    setHandicaps((prev) => {
      const merged: Record<string, string> = {}
      for (const hole of course.holes) {
        merged[hole.id] = prev[hole.id] ?? serverHandicaps[hole.id]
      }
      return merged
    })
  }, [course])

  const tees = course.tees
  const holes = course.holes

  const serverValues = useMemo(() => {
    const values: Record<string, CellDraft> = {}
    for (const hole of holes) {
      for (const detail of hole.tee_details) {
        values[cellKey(hole.id, detail.tee_id)] = {
          par: String(detail.par),
          yardage: String(detail.yardage),
        }
      }
    }
    return values
  }, [holes])

  const setStatus = useCallback((key: string, status: CellStatus) => {
    setStatuses((prev) => ({ ...prev, [key]: status }))
    if (status === 'saved') {
      // Clear the tick after a moment so the grid does not stay covered in them.
      setTimeout(() => {
        setStatuses((prev) => (prev[key] === 'saved' ? { ...prev, [key]: 'idle' } : prev))
      }, 1500)
    }
  }, [])

  const saveCell = useCallback(
    async (hole: Hole, tee: Tee) => {
      const key = cellKey(hole.id, tee.id)
      const draft = drafts[key]
      if (!draft) return

      const par = Number.parseInt(draft.par, 10)
      const yardage = Number.parseInt(draft.yardage, 10)

      // Both halves are required together, so a half-filled cell just waits.
      if (!Number.isFinite(par) || !Number.isFinite(yardage)) return
      if (draft.par.trim() === '' || draft.yardage.trim() === '') return

      const previous = serverValues[key]
      if (previous && previous.par === String(par) && previous.yardage === String(yardage)) return

      setStatus(key, 'saving')
      setErrorMessage(null)
      try {
        await api.setTeeDetail(hole.id, tee.id, par, yardage)
        setStatus(key, 'saved')
        onCourseChanged()
      } catch (error) {
        setStatus(key, 'error')
        const message = (error as { message?: string }).message
        setErrorMessage(
          message ?? `Could not save hole ${hole.hole_number} for the ${tee.name} tee.`,
        )
      }
    },
    [drafts, serverValues, setStatus, onCourseChanged],
  )

  const saveHandicap = useCallback(
    async (hole: Hole) => {
      const raw = handicaps[hole.id] ?? ''
      const parsed = raw.trim() === '' ? null : Number.parseInt(raw, 10)
      if (parsed !== null && !Number.isFinite(parsed)) return
      if ((hole.handicap_index ?? null) === parsed) return

      const key = `hcp:${hole.id}`
      setStatus(key, 'saving')
      setErrorMessage(null)
      try {
        await api.updateHole(hole.id, parsed)
        setStatus(key, 'saved')
        onCourseChanged()
      } catch (error) {
        setStatus(key, 'error')
        const message = (error as { message?: string }).message
        setErrorMessage(message ?? `Could not save the stroke index for hole ${hole.hole_number}.`)
      }
    },
    [handicaps, setStatus, onCourseChanged],
  )

  function updateCell(holeId: string, teeId: string, patch: Partial<CellDraft>) {
    const key = cellKey(holeId, teeId)
    setDrafts((prev) => {
      const existing = prev[key] ?? { par: '', yardage: '' }
      return { ...prev, [key]: { ...existing, ...patch } }
    })
  }

  /**
   * Sums par and yardage for a tee over the given holes.
   *
   * Read from the local drafts rather than the server copy so the totals track
   * what is on screen, including a value typed but not yet blurred.
   */
  function totals(tee: Tee, subset: Hole[]) {
    let par = 0
    let yardage = 0
    for (const hole of subset) {
      const draft = drafts[cellKey(hole.id, tee.id)]
      if (!draft) continue
      const cellPar = Number.parseInt(draft.par, 10)
      const cellYardage = Number.parseInt(draft.yardage, 10)
      if (Number.isFinite(cellPar)) par += cellPar
      if (Number.isFinite(cellYardage)) yardage += cellYardage
    }
    return { par, yardage }
  }

  const front = holes.filter((h) => h.hole_number <= 9)
  const back = holes.filter((h) => h.hole_number > 9)

  if (tees.length === 0) {
    return (
      <div className="card p-6 text-center text-sm text-slate-600 dark:text-slate-400">
        Add a tee first — par and yardage are recorded per tee, so the table needs at least one
        column.
      </div>
    )
  }

  const cellInputClass =
    'w-full min-w-14 rounded-md border border-slate-300 bg-white px-1.5 py-1.5 text-center text-sm tabular-nums dark:border-slate-700 dark:bg-slate-950 focus:border-brand-500 focus:ring-1 focus:ring-brand-500/40 focus:outline-none disabled:opacity-60 disabled:cursor-not-allowed'

  function renderRows(subset: Hole[], label: string) {
    if (subset.length === 0) return null
    return (
      <>
        {subset.map((hole) => (
          <tr key={hole.id} className="border-t border-slate-200 dark:border-slate-800">
            <th
              scope="row"
              className="sticky left-0 z-10 bg-white px-3 py-2 text-left text-sm font-semibold dark:bg-slate-900"
            >
              {hole.hole_number}
            </th>
            <td className="px-2 py-2">
              <input
                type="number"
                inputMode="numeric"
                min={1}
                max={18}
                value={handicaps[hole.id] ?? ''}
                disabled={!editable}
                onChange={(e) => setHandicaps((prev) => ({ ...prev, [hole.id]: e.target.value }))}
                onBlur={() => void saveHandicap(hole)}
                className={cx(
                  cellInputClass,
                  statuses[`hcp:${hole.id}`] === 'error' && 'border-red-500',
                  statuses[`hcp:${hole.id}`] === 'saved' && 'border-brand-500',
                )}
                aria-label={`Stroke index for hole ${hole.hole_number}`}
              />
            </td>
            {tees.map((tee) => {
              const key = cellKey(hole.id, tee.id)
              const draft = drafts[key] ?? { par: '', yardage: '' }
              const status = statuses[key] ?? 'idle'
              return (
                <td key={tee.id} className="px-2 py-2">
                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      inputMode="numeric"
                      min={3}
                      max={6}
                      value={draft.par}
                      disabled={!editable}
                      onChange={(e) => updateCell(hole.id, tee.id, { par: e.target.value })}
                      onBlur={() => void saveCell(hole, tee)}
                      className={cx(
                        cellInputClass,
                        status === 'error' && 'border-red-500',
                        status === 'saved' && 'border-brand-500',
                      )}
                      aria-label={`Par for hole ${hole.hole_number} from the ${tee.name} tee`}
                      placeholder="Par"
                    />
                    <input
                      type="number"
                      inputMode="numeric"
                      min={1}
                      max={1000}
                      value={draft.yardage}
                      disabled={!editable}
                      onChange={(e) => updateCell(hole.id, tee.id, { yardage: e.target.value })}
                      onBlur={() => void saveCell(hole, tee)}
                      className={cx(
                        cellInputClass,
                        status === 'error' && 'border-red-500',
                        status === 'saved' && 'border-brand-500',
                      )}
                      aria-label={`Yardage for hole ${hole.hole_number} from the ${tee.name} tee`}
                      placeholder="Yds"
                    />
                  </div>
                </td>
              )
            })}
          </tr>
        ))}
        <tr className="border-t border-slate-300 bg-slate-50 dark:border-slate-700 dark:bg-slate-800/50">
          <th
            scope="row"
            className="sticky left-0 z-10 bg-slate-50 px-3 py-2 text-left text-xs font-semibold uppercase dark:bg-slate-800/50"
          >
            {label}
          </th>
          <td />
          {tees.map((tee) => {
            const { par, yardage } = totals(tee, subset)
            return (
              <td key={tee.id} className="px-2 py-2 text-center text-xs tabular-nums">
                <span className="font-semibold">{par}</span>
                <span className="text-slate-500 dark:text-slate-400"> · {yardage} yds</span>
              </td>
            )
          })}
        </tr>
      </>
    )
  }

  return (
    <div className="space-y-3">
      {errorMessage && (
        <div
          role="alert"
          className="rounded-lg border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/60 dark:text-red-200"
        >
          {errorMessage}
        </div>
      )}

      {/* The table is wider than a phone, so it scrolls horizontally with the
          hole number column pinned. */}
      <div className="card overflow-x-auto">
        <table className="w-full min-w-max border-collapse">
          <caption className="sr-only">
            Par and yardage for each hole, by tee. Values save when you leave a field.
          </caption>
          <thead>
            <tr className="bg-slate-50 dark:bg-slate-800/50">
              <th
                scope="col"
                className="sticky left-0 z-10 bg-slate-50 px-3 py-2.5 text-left text-xs font-semibold uppercase tracking-wide dark:bg-slate-800/50"
              >
                Hole
              </th>
              <th
                scope="col"
                className="px-2 py-2.5 text-center text-xs font-semibold uppercase tracking-wide"
              >
                <abbr title="Stroke index (handicap)" className="no-underline">
                  SI
                </abbr>
              </th>
              {tees.map((tee) => (
                <th key={tee.id} scope="col" className="px-2 py-2.5 text-center">
                  <span className="flex items-center justify-center gap-1.5 text-xs font-semibold">
                    <span
                      aria-hidden="true"
                      className="size-2.5 shrink-0 rounded-full ring-1 ring-black/15 dark:ring-white/25"
                      style={{ backgroundColor: tee.color }}
                    />
                    {tee.name}
                  </span>
                  <span className="mt-0.5 block text-[10px] font-normal text-slate-500 dark:text-slate-400">
                    par · yds
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {renderRows(front, 'Out')}
            {renderRows(back, 'In')}
            <tr className="border-t-2 border-slate-300 bg-slate-100 dark:border-slate-600 dark:bg-slate-800">
              <th
                scope="row"
                className="sticky left-0 z-10 bg-slate-100 px-3 py-2.5 text-left text-xs font-bold uppercase dark:bg-slate-800"
              >
                Total
              </th>
              <td />
              {tees.map((tee) => {
                const { par, yardage } = totals(tee, holes)
                return (
                  <td key={tee.id} className="px-2 py-2.5 text-center text-xs tabular-nums">
                    <span className="font-bold">{par}</span>
                    <span className="text-slate-600 dark:text-slate-300"> · {yardage} yds</span>
                  </td>
                )
              })}
            </tr>
          </tbody>
        </table>
      </div>

      {editable && (
        <p className="text-xs text-slate-500 dark:text-slate-400">
          Changes save automatically when you leave a field. Par and yardage are stored per tee, so
          the same hole can play as a par 3 from one tee and a par 4 from another.
        </p>
      )}
    </div>
  )
}
