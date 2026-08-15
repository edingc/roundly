import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import type { CourseDetail, Hole, Tee } from '../types'
import { api } from '../lib/api'
import { usePreferences } from '../lib/preferences'
import { cx } from './ui'

type CellStatus = 'idle' | 'saving' | 'saved' | 'error'

interface CellDraft {
  par: string
  yardage: string
}

function cellKey(holeId: string, teeId: string): string {
  return `${holeId}:${teeId}`
}

/** True when exactly one of par/yardage is filled in — the pair the API requires together. */
function isHalfFilled(draft: CellDraft): boolean {
  return (draft.par.trim() !== '') !== (draft.yardage.trim() !== '')
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
  const { strokeIndexLabel } = usePreferences()
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

  // Longest to shortest, so the scorecard reads like a real one (back tees
  // on the left working down to forward tees).
  const tees = [...course.tees].sort((a, b) => b.total_yardage - a.total_yardage)
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
        Add a tee first.
      </div>
    )
  }

  // Ruled-grid cells, like a paper card: no floating input boxes, just a thin
  // border per cell (from the table's own border-collapse) and a background
  // tint for save status instead of a border color, so the grid lines stay
  // uniform.
  const cellInputClass =
    'block w-full bg-transparent px-1 py-2 text-center text-sm tabular-nums focus:outline-none focus:ring-2 focus:ring-inset focus:ring-brand-500/60 disabled:cursor-not-allowed disabled:opacity-60'

  const gridLine = 'border border-slate-200 dark:border-slate-800'
  // A heavier rule between one tee's columns and the next, echoing the ruled
  // sections of a real card.
  const teeGroupStart = 'border-l-2 border-l-slate-300 dark:border-l-slate-600'

  // `half` marks a cell with only par or only yardage typed in — the API
  // needs both, so it's just sitting there unsaved rather than erroring.
  function statusClass(status: CellStatus, half = false) {
    if (status === 'error') return 'bg-red-50 ring-2 ring-inset ring-red-400 dark:bg-red-950/50'
    if (status === 'saved') return 'bg-brand-50 dark:bg-brand-950/40'
    if (half) return 'bg-amber-50 ring-1 ring-inset ring-amber-400 dark:bg-amber-950/40 dark:ring-amber-600'
    return ''
  }

  function renderRows(subset: Hole[], label: string) {
    if (subset.length === 0) return null
    return (
      <>
        {subset.map((hole) => (
          <tr key={hole.id}>
            <th
              scope="row"
              className={cx(
                gridLine,
                'sticky left-0 z-10 bg-white px-2 py-2 text-center text-sm font-bold dark:bg-slate-900',
              )}
            >
              {hole.hole_number}
            </th>
            <td className={cx(gridLine, 'p-0')}>
              <input
                type="number"
                inputMode="numeric"
                min={1}
                max={18}
                value={handicaps[hole.id] ?? ''}
                disabled={!editable}
                onChange={(e) => setHandicaps((prev) => ({ ...prev, [hole.id]: e.target.value }))}
                onBlur={() => void saveHandicap(hole)}
                className={cx(cellInputClass, statusClass(statuses[`hcp:${hole.id}`] ?? 'idle'))}
                aria-label={`Stroke index for hole ${hole.hole_number}`}
              />
            </td>
            {tees.map((tee) => {
              const key = cellKey(hole.id, tee.id)
              const draft = drafts[key] ?? { par: '', yardage: '' }
              const status = statuses[key] ?? 'idle'
              const half = isHalfFilled(draft)
              return (
                <Fragment key={tee.id}>
                  <td className={cx(gridLine, teeGroupStart, 'w-11 p-0')}>
                    <input
                      type="number"
                      inputMode="numeric"
                      min={3}
                      max={6}
                      value={draft.par}
                      disabled={!editable}
                      onChange={(e) => updateCell(hole.id, tee.id, { par: e.target.value })}
                      onBlur={() => void saveCell(hole, tee)}
                      className={cx(cellInputClass, statusClass(status, half))}
                      aria-label={`Par for hole ${hole.hole_number} from the ${tee.name} tee`}
                      title={half && draft.par.trim() === '' ? 'Add a par to save this hole.' : undefined}
                      placeholder="—"
                    />
                  </td>
                  <td className={cx(gridLine, 'w-14 p-0')}>
                    <input
                      type="number"
                      inputMode="numeric"
                      min={1}
                      max={1000}
                      value={draft.yardage}
                      disabled={!editable}
                      onChange={(e) => updateCell(hole.id, tee.id, { yardage: e.target.value })}
                      onBlur={() => void saveCell(hole, tee)}
                      className={cx(cellInputClass, statusClass(status, half))}
                      aria-label={`Yardage for hole ${hole.hole_number} from the ${tee.name} tee`}
                      title={
                        half && draft.yardage.trim() === '' ? 'Add a yardage to save this hole.' : undefined
                      }
                      placeholder="—"
                    />
                  </td>
                </Fragment>
              )
            })}
          </tr>
        ))}
        <tr>
          <th
            scope="row"
            className={cx(
              gridLine,
              'sticky left-0 z-10 bg-slate-50 px-2 py-1.5 text-center text-xs font-bold uppercase tracking-wide dark:bg-slate-800/70',
            )}
          >
            {label}
          </th>
          <td className={cx(gridLine, 'bg-slate-50 dark:bg-slate-800/70')} />
          {tees.map((tee) => {
            const { par, yardage } = totals(tee, subset)
            return (
              <td
                key={tee.id}
                colSpan={2}
                className={cx(
                  gridLine,
                  teeGroupStart,
                  'bg-slate-50 px-1 py-1.5 text-center text-xs tabular-nums dark:bg-slate-800/70',
                )}
              >
                <span className="font-semibold">{par}</span>
                <span className="text-slate-500 dark:text-slate-400"> · {yardage}</span>
              </td>
            )
          })}
        </tr>
      </>
    )
  }

  function verticalTable() {
    return (
      <table className="w-full border-collapse">
        <caption className="sr-only">
          Par and yardage for each hole, by tee. Values save when you leave a field.
        </caption>
        <thead>
          <tr>
            <th
              scope="col"
              rowSpan={2}
              className={cx(
                gridLine,
                'sticky left-0 z-10 bg-slate-50 px-2 py-1.5 text-center text-xs font-semibold uppercase tracking-wide dark:bg-slate-800/70',
              )}
            >
              Hole
            </th>
            <th
              scope="col"
              rowSpan={2}
              className={cx(
                gridLine,
                'bg-slate-50 px-1 py-1.5 text-center text-xs font-semibold uppercase tracking-wide dark:bg-slate-800/70',
              )}
            >
              <abbr title="Stroke index (handicap)" className="no-underline">
                {strokeIndexLabel}
              </abbr>
            </th>
            {tees.map((tee) => (
              <th
                key={tee.id}
                scope="colgroup"
                colSpan={2}
                className={cx(gridLine, teeGroupStart, 'relative bg-slate-50 px-1 pt-2 pb-1 dark:bg-slate-800/70')}
              >
                <span
                  aria-hidden="true"
                  className="absolute inset-x-0 top-0 h-1"
                  style={{ backgroundColor: tee.color }}
                />
                <span className="block truncate text-center text-xs font-bold uppercase tracking-wide">
                  {tee.name}
                </span>
              </th>
            ))}
          </tr>
          <tr>
            {tees.map((tee) => (
              <Fragment key={tee.id}>
                <th
                  scope="col"
                  className={cx(
                    gridLine,
                    teeGroupStart,
                    'bg-slate-50 px-1 py-1 text-center text-[10px] font-medium text-slate-500 dark:bg-slate-800/70 dark:text-slate-400',
                  )}
                >
                  Par
                </th>
                <th
                  scope="col"
                  className={cx(
                    gridLine,
                    'bg-slate-50 px-1 py-1 text-center text-[10px] font-medium text-slate-500 dark:bg-slate-800/70 dark:text-slate-400',
                  )}
                >
                  Yds
                </th>
              </Fragment>
            ))}
          </tr>
        </thead>
        <tbody>
          {renderRows(front, 'Out')}
          {renderRows(back, 'In')}
          <tr>
            <th
              scope="row"
              className={cx(
                gridLine,
                'sticky left-0 z-10 bg-slate-100 px-2 py-2 text-center text-xs font-bold uppercase dark:bg-slate-800',
              )}
            >
              Total
            </th>
            <td className={cx(gridLine, 'bg-slate-100 dark:bg-slate-800')} />
            {tees.map((tee) => {
              const { par, yardage } = totals(tee, holes)
              return (
                <td
                  key={tee.id}
                  colSpan={2}
                  className={cx(
                    gridLine,
                    teeGroupStart,
                    'bg-slate-100 px-1 py-2 text-center text-xs tabular-nums dark:bg-slate-800',
                  )}
                >
                  <span className="font-bold">{par}</span>
                  <span className="text-slate-600 dark:text-slate-300"> · {yardage} yds</span>
                </td>
              )
            })}
          </tr>
        </tbody>
      </table>
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

      {/* Below md, holes run down the page (one row each) so the pinned
          columns stay narrow on a phone. At md and up there's enough width
          for an actual scorecard layout — holes across the top, a row per
          tee — which also means extra tees add rows instead of ever-more
          columns. Both tables stay mounted; only one is visible at a time,
          which is simpler and more robust than a resize listener. */}
      <div className="card overflow-x-auto md:hidden">{verticalTable()}</div>
      <div className="card hidden overflow-x-auto md:block">
        <HorizontalScorecard
          tees={tees}
          front={front}
          back={back}
          holes={holes}
          drafts={drafts}
          handicaps={handicaps}
          statuses={statuses}
          editable={editable}
          gridLine={gridLine}
          cellInputClass={cellInputClass}
          statusClass={statusClass}
          totals={totals}
          updateCell={updateCell}
          saveCell={saveCell}
          setHandicaps={setHandicaps}
          saveHandicap={saveHandicap}
        />
      </div>

      {editable && (
        <p className="text-xs text-slate-500 dark:text-slate-400">
          A hole needs both par and yardage to save automatically.
        </p>
      )}
    </div>
  )
}

type Column = { kind: 'hole'; hole: Hole } | { kind: 'out' } | { kind: 'in' } | { kind: 'total' }

function columnWidth(col: Column): string {
  if (col.kind === 'hole') return 'w-9'
  if (col.kind === 'total') return 'w-12'
  return 'w-11'
}

function columnBg(col: Column): string {
  if (col.kind === 'hole') return ''
  if (col.kind === 'total') return 'bg-slate-100 dark:bg-slate-800'
  return 'bg-slate-50 dark:bg-slate-800/70'
}

function columnLabel(col: Column): string {
  if (col.kind === 'hole') return String(col.hole.hole_number)
  if (col.kind === 'out') return 'Out'
  if (col.kind === 'in') return 'In'
  return 'Tot'
}

function columnKey(col: Column): string {
  return col.kind === 'hole' ? col.hole.id : col.kind
}

/**
 * The scorecard-style layout: holes run left to right, one row pair (par,
 * yardage) per tee. Kept as a separate component, rather than inline in
 * ScorecardGrid, purely to give the column-building logic its own scope.
 */
function HorizontalScorecard({
  tees,
  front,
  back,
  holes,
  drafts,
  handicaps,
  statuses,
  editable,
  gridLine,
  cellInputClass,
  statusClass,
  totals,
  updateCell,
  saveCell,
  setHandicaps,
  saveHandicap,
}: {
  tees: Tee[]
  front: Hole[]
  back: Hole[]
  holes: Hole[]
  drafts: Record<string, CellDraft>
  handicaps: Record<string, string>
  statuses: Record<string, CellStatus>
  editable: boolean
  gridLine: string
  cellInputClass: string
  // `half` marks a cell whose row is partly filled in; the callers below pass
  // it, and the implementation has always accepted it.
  statusClass: (status: CellStatus, half?: boolean) => string
  totals: (tee: Tee, subset: Hole[]) => { par: number; yardage: number }
  updateCell: (holeId: string, teeId: string, patch: Partial<CellDraft>) => void
  saveCell: (hole: Hole, tee: Tee) => Promise<void>
  setHandicaps: (updater: (prev: Record<string, string>) => Record<string, string>) => void
  saveHandicap: (hole: Hole) => Promise<void>
}) {
  const { strokeIndexLabel } = usePreferences()
  const hasBack = back.length > 0
  const columns: Column[] = [
    ...front.map((hole): Column => ({ kind: 'hole', hole })),
    { kind: 'out' },
    ...(hasBack ? back.map((hole): Column => ({ kind: 'hole', hole })) : []),
    ...(hasBack ? ([{ kind: 'in' }] as Column[]) : []),
    { kind: 'total' },
  ]

  // A heavier rule above each tee's row pair, echoing the ruled sections of a
  // real card (the horizontal counterpart of the vertical layout's
  // between-tee column rule).
  const teeGroupTop = 'border-t-2 border-t-slate-300 dark:border-t-slate-600'

  return (
    <table className="w-full border-collapse">
      <caption className="sr-only">
        Par and yardage for each hole, by tee. Values save when you leave a field.
      </caption>
      <thead>
        <tr>
          <th
            colSpan={2}
            scope="col"
            className={cx(
              gridLine,
              'sticky left-0 z-10 bg-slate-50 px-2 py-1.5 text-left text-xs font-semibold uppercase tracking-wide dark:bg-slate-800/70',
            )}
          >
            Hole
          </th>
          {columns.map((col) => (
            <th
              key={columnKey(col)}
              scope="col"
              className={cx(
                gridLine,
                columnWidth(col),
                columnBg(col),
                'px-1 py-1.5 text-center text-xs font-bold tabular-nums',
                col.kind !== 'hole' && 'uppercase tracking-wide',
              )}
            >
              {columnLabel(col)}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        <tr>
          <th
            colSpan={2}
            scope="row"
            className={cx(
              gridLine,
              'sticky left-0 z-10 bg-slate-50 px-2 py-1.5 text-left text-xs font-semibold uppercase tracking-wide dark:bg-slate-800/70',
            )}
          >
            <abbr title="Stroke index (handicap)" className="no-underline">
              {strokeIndexLabel}
            </abbr>
          </th>
          {columns.map((col) =>
            col.kind === 'hole' ? (
              <td key={columnKey(col)} className={cx(gridLine, columnWidth(col), 'p-0')}>
                <input
                  type="number"
                  inputMode="numeric"
                  min={1}
                  max={18}
                  value={handicaps[col.hole.id] ?? ''}
                  disabled={!editable}
                  onChange={(e) =>
                    setHandicaps((prev) => ({ ...prev, [col.hole.id]: e.target.value }))
                  }
                  onBlur={() => void saveHandicap(col.hole)}
                  className={cx(
                    cellInputClass,
                    statusClass(statuses[`hcp:${col.hole.id}`] ?? 'idle'),
                  )}
                  aria-label={`Stroke index for hole ${col.hole.hole_number}`}
                />
              </td>
            ) : (
              <td
                key={columnKey(col)}
                className={cx(gridLine, columnWidth(col), columnBg(col))}
              />
            ),
          )}
        </tr>

        {tees.map((tee) => {
          const outTotals = totals(tee, front)
          const inTotals = totals(tee, back)
          const allTotals = totals(tee, holes)

          function valueFor(col: Column, field: 'par' | 'yardage'): number {
            if (col.kind === 'out') return outTotals[field]
            if (col.kind === 'in') return inTotals[field]
            return allTotals[field]
          }

          return (
            <Fragment key={tee.id}>
              <tr>
                <th
                  rowSpan={2}
                  scope="row"
                  className={cx(
                    gridLine,
                    teeGroupTop,
                    'sticky left-0 z-10 w-24 bg-white px-2 py-1 text-left align-middle dark:bg-slate-900',
                  )}
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    <span
                      aria-hidden="true"
                      className="size-2.5 shrink-0 rounded-full ring-1 ring-black/15 dark:ring-white/25"
                      style={{ backgroundColor: tee.color }}
                    />
                    <span className="truncate text-xs font-bold uppercase tracking-wide">
                      {tee.name}
                    </span>
                  </span>
                </th>
                <th
                  scope="row"
                  className={cx(
                    gridLine,
                    teeGroupTop,
                    'sticky left-24 z-10 w-11 bg-white px-1 py-1 text-center text-[10px] font-medium text-slate-500 dark:bg-slate-900 dark:text-slate-400',
                  )}
                >
                  Par
                </th>
                {columns.map((col) => {
                  if (col.kind === 'hole') {
                    const hole = col.hole
                    const key = cellKey(hole.id, tee.id)
                    const draft = drafts[key] ?? { par: '', yardage: '' }
                    const status = statuses[key] ?? 'idle'
                    const half = isHalfFilled(draft)
                    return (
                      <td
                        key={hole.id}
                        className={cx(gridLine, teeGroupTop, columnWidth(col), 'p-0')}
                      >
                        <input
                          type="number"
                          inputMode="numeric"
                          min={3}
                          max={6}
                          value={draft.par}
                          disabled={!editable}
                          onChange={(e) => updateCell(hole.id, tee.id, { par: e.target.value })}
                          onBlur={() => void saveCell(hole, tee)}
                          className={cx(cellInputClass, statusClass(status, half))}
                          aria-label={`Par for hole ${hole.hole_number} from the ${tee.name} tee`}
                          title={half && draft.par.trim() === '' ? 'Add a par to save this hole.' : undefined}
                          placeholder="—"
                        />
                      </td>
                    )
                  }
                  return (
                    <td
                      key={columnKey(col)}
                      className={cx(
                        gridLine,
                        teeGroupTop,
                        columnWidth(col),
                        columnBg(col),
                        'px-1 py-1 text-center text-xs font-semibold tabular-nums',
                      )}
                    >
                      {valueFor(col, 'par')}
                    </td>
                  )
                })}
              </tr>
              <tr>
                <th
                  scope="row"
                  className={cx(
                    gridLine,
                    'sticky left-24 z-10 w-11 bg-white px-1 py-1 text-center text-[10px] font-medium text-slate-500 dark:bg-slate-900 dark:text-slate-400',
                  )}
                >
                  Yds
                </th>
                {columns.map((col) => {
                  if (col.kind === 'hole') {
                    const hole = col.hole
                    const key = cellKey(hole.id, tee.id)
                    const draft = drafts[key] ?? { par: '', yardage: '' }
                    const status = statuses[key] ?? 'idle'
                    const half = isHalfFilled(draft)
                    return (
                      <td key={hole.id} className={cx(gridLine, columnWidth(col), 'p-0')}>
                        <input
                          type="number"
                          inputMode="numeric"
                          min={1}
                          max={1000}
                          value={draft.yardage}
                          disabled={!editable}
                          onChange={(e) =>
                            updateCell(hole.id, tee.id, { yardage: e.target.value })
                          }
                          onBlur={() => void saveCell(hole, tee)}
                          className={cx(cellInputClass, statusClass(status, half))}
                          aria-label={`Yardage for hole ${hole.hole_number} from the ${tee.name} tee`}
                          title={
                            half && draft.yardage.trim() === '' ? 'Add a yardage to save this hole.' : undefined
                          }
                          placeholder="—"
                        />
                      </td>
                    )
                  }
                  return (
                    <td
                      key={columnKey(col)}
                      className={cx(
                        gridLine,
                        columnWidth(col),
                        columnBg(col),
                        'px-1 py-1 text-center text-xs tabular-nums text-slate-600 dark:text-slate-300',
                      )}
                    >
                      {valueFor(col, 'yardage')}
                    </td>
                  )
                })}
              </tr>
            </Fragment>
          )
        })}
      </tbody>
    </table>
  )
}
