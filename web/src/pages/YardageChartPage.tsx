/**
 * The printable yardage chart.
 *
 * This is a print view rather than a generated PDF. The browser's own print
 * dialog already offers "Save as PDF", so laying the chart out in HTML and CSS
 * gets both paper and a file without shipping a PDF library inside the binary,
 * and the result reflows for whatever paper the player actually has.
 *
 * The sheet is drawn in paper colours on screen — white, black, ruled — so the
 * preview is the artifact. The print rules in index.css only take the app
 * chrome away.
 */
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useAuth, useDistanceUnit } from '../lib/auth'
import { chartRows, type ChartRow } from '../lib/bag'
import { unitSuffix, type DistanceUnit } from '../lib/units'
import type { Bag, ClubOptions } from '../types'
import { Alert, ChevronLeftIcon, PageSpinner, PrinterIcon, cx } from '../components/ui'

type Layout = 'sheet' | 'card'

/** Which optional columns have any data to show, so an unfilled bag prints clean. */
interface Columns {
  loft: boolean
  gap: boolean
  dispersion: boolean
}

function columnsFor(rows: ChartRow[]): Columns {
  return {
    loft: rows.some((row) => row.club.loft !== null),
    gap: rows.some((row) => row.gap !== null),
    dispersion: rows.some((row) => row.dispersion !== null),
  }
}

/** "yards" / "metres", for the sentence-case places the chart spells it out. */
function unitWord(unit: DistanceUnit): string {
  return unit === 'yards' ? 'yards' : 'metres'
}

/** A dotted rule to write a missing number on, once the sheet is on paper. */
function WriteIn() {
  return (
    <span
      className="inline-block w-14 border-b border-dotted border-slate-600 align-baseline"
      aria-label="blank, to fill in"
    >
      &nbsp;
    </span>
  )
}

function printedOn(): string {
  return new Date().toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })
}

/** The full-page chart: every club, with the gaps between them. */
function Sheet({
  rows,
  unit,
  playerName,
}: {
  rows: ChartRow[]
  unit: DistanceUnit
  playerName: string
}) {
  const columns = columnsFor(rows)
  const headerCell = 'pb-1 text-xs font-semibold tracking-wide text-slate-600 uppercase'
  const numberCell = 'py-1.5 text-right tabular-nums'

  return (
    <div className="print-sheet mx-auto w-full max-w-[7.5in] rounded-lg border border-slate-300 bg-white p-5 text-slate-900 shadow-sm sm:p-8">
      <div className="flex items-baseline justify-between border-b-2 border-slate-800 pb-2">
        <h2 className="text-xl font-bold tracking-tight">Yardage Chart</h2>
        <span className="text-xs tracking-widest text-slate-500 uppercase">Roundly</span>
      </div>
      {/* The date is not decoration: a printed chart goes stale as the player's
          distances move, and this is the only thing on the paper that says how
          stale it might be. */}
      <p className="mt-1.5 text-xs text-slate-600">
        {playerName} · {printedOn()} · distances in {unitWord(unit)}
      </p>

      {/* Four numeric columns plus a label do not fit a phone. The table scrolls
          inside the sheet rather than making the whole page scroll sideways —
          and chart-scroll is released again for print, where the paper is wide
          enough and a clipped column would simply be missing. */}
      <div className="chart-scroll overflow-x-auto">
        <table className="mt-5 w-full min-w-[20rem] border-collapse text-sm">
          <thead>
            <tr className="border-b border-slate-400">
              <th className={cx(headerCell, 'text-left')}>Club</th>
              {columns.loft && <th className={cx(headerCell, 'text-right')}>Loft</th>}
              <th className={cx(headerCell, 'text-right')}>Carry</th>
              {columns.gap && <th className={cx(headerCell, 'text-right')}>Gap</th>}
              {columns.dispersion && <th className={cx(headerCell, 'text-right')}>Spread</th>}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.club.id} className="border-b border-slate-200">
                {/* The label alone. A club type beside it reads "Driver Driver"
                    and "5 iron Iron" on almost every line, because a label
                    people actually type already names the club. */}
                <td className="py-1.5 pr-3 font-medium">{row.club.label}</td>
                {columns.loft && (
                  <td className={cx(numberCell, 'w-14 text-slate-600')}>
                    {row.club.loft === null ? '' : `${row.club.loft}°`}
                  </td>
                )}
                <td className={cx(numberCell, 'w-20 text-base font-semibold')}>
                  {row.carry === null ? <WriteIn /> : row.carry}
                </td>
                {columns.gap && (
                  <td className={cx(numberCell, 'w-14 text-slate-600')}>
                    {row.gap === null ? '' : row.gap}
                  </td>
                )}
                {columns.dispersion && (
                  <td className={cx(numberCell, 'w-16 text-slate-600')}>
                    {row.dispersion === null ? '' : `±${row.dispersion}`}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* The key explains the columns that are actually on this sheet. A bag
          with no dispersion recorded drops that column, and a legend for a
          column that is not there is worse than no legend. */}
      <p className="mt-4 text-[0.65rem] leading-relaxed text-slate-500">
        All distances in {unitWord(unit)}. Carry is how far the ball flies, not where it finishes.
        {columns.gap && ' Gap is the drop to the next club down.'}
        {columns.dispersion && ' Spread is the average dispersion left/right of the carry.'}
      </p>
    </div>
  )
}

/**
 * The cut-out version: club and carry only, sized to a yardage-book or
 * scorecard pocket. A full sheet is the wrong shape for the thing a player is
 * actually doing with it, which is standing in a fairway holding it.
 */
function PocketCard({ rows, unit }: { rows: ChartRow[]; unit: DistanceUnit }) {
  return (
    <div className="print-sheet mx-auto w-full max-w-[7.5in] rounded-lg border border-slate-300 bg-white p-5 shadow-sm sm:p-8">
      {/* The card is a fixed 3.5in so the print is the real size; on a narrow
          phone that is wider than the sheet's content box, so it scrolls here
          rather than dragging the page sideways. */}
      <div className="chart-scroll overflow-x-auto">
        <div className="mx-auto w-[3.5in] border border-dashed border-slate-500 bg-white p-3 text-slate-900">
          <div className="flex items-baseline justify-between border-b border-slate-800 pb-1">
            <span className="text-sm font-bold tracking-tight">Carry ({unitSuffix(unit)})</span>
            <span className="text-[0.6rem] text-slate-500">{printedOn()}</span>
          </div>
          <ul>
            {rows.map((row) => (
              <li
                key={row.club.id}
                className="print-row flex items-baseline justify-between border-b border-slate-200 py-[0.15rem] text-xs"
              >
                <span className="truncate pr-2 font-medium">{row.club.label}</span>
                <span className="shrink-0 font-semibold tabular-nums">
                  {row.carry === null ? <WriteIn /> : row.carry}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
      <p className="no-print mt-4 text-center text-xs text-slate-500">Cut along the dashed line.</p>
    </div>
  )
}

export default function YardageChartPage() {
  const unit = useDistanceUnit()
  const { user } = useAuth()

  const [bag, setBag] = useState<Bag | null>(null)
  const [options, setOptions] = useState<ClubOptions | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [layout, setLayout] = useState<Layout>('sheet')
  // Off by default: the chart answers "what is in my bag right now", and a
  // benched club is not an option on the next shot.
  const [includeBenched, setIncludeBenched] = useState(false)

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

  // Retired clubs are never charted — they are kept for the shots they hit, not
  // for the shots still to come.
  const source = bag ? (includeBenched ? [...bag.active, ...bag.benched] : bag.active) : []
  const rows = chartRows(source, options?.club_types ?? [], unit)
  const missing = rows.filter((row) => row.carry === null).length

  if (loading && !bag) return <PageSpinner label="Building your chart" />

  return (
    <div className="space-y-5">
      <div className="no-print space-y-4">
        {/* Its own row above the title, matching the course detail screen. */}
        <Link
          to="/bag"
          className="inline-flex items-center gap-1 text-sm text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
        >
          <ChevronLeftIcon className="size-4" />
          Bag
        </Link>

        <div className="flex flex-wrap items-center gap-3">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Yardage Chart</h1>
            <p className="text-sm text-slate-600 dark:text-slate-400">
              Print it, or save it as a PDF from the print dialog.
            </p>
          </div>
          <button
            type="button"
            className="btn-primary ml-auto"
            onClick={() => window.print()}
            disabled={rows.length === 0}
          >
            <PrinterIcon className="size-4" />
            Print
          </button>
        </div>

        {error && <Alert>{error}</Alert>}

        {/* Nothing to chart means nothing to choose about how to chart it. */}
        <div
          className={cx(
            'card flex flex-wrap items-center gap-x-6 gap-y-3 p-4',
            rows.length === 0 && 'hidden',
          )}
        >
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-slate-700 dark:text-slate-300">Format</span>
            <div className="flex rounded-lg border border-slate-300 p-0.5 dark:border-slate-700">
              {(['sheet', 'card'] as const).map((option) => (
                <button
                  key={option}
                  type="button"
                  onClick={() => setLayout(option)}
                  aria-pressed={layout === option}
                  className={cx(
                    'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                    layout === option
                      ? 'bg-brand-600 text-white'
                      : 'text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800',
                  )}
                >
                  {option === 'sheet' ? 'Full sheet' : 'Pocket card'}
                </button>
              ))}
            </div>
          </div>

          <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300">
            <input
              type="checkbox"
              checked={includeBenched}
              onChange={(e) => setIncludeBenched(e.target.checked)}
              className="size-4 rounded border-slate-300 text-brand-600 focus:ring-brand-500 dark:border-slate-600"
            />
            Include clubs not in the bag
          </label>
        </div>

        {/* Informational, not a warning: a blank line is a usable sheet, not a
            rule the player is breaking. */}
        {rows.length > 0 && missing > 0 && (
          <Alert tone="info">
            {missing === rows.length
              ? 'None of these clubs has an expected carry yet, so every line prints blank. Take the sheet to the range and fill it in, then type the numbers into your bag.'
              : `${missing} of these ${rows.length} clubs has no expected carry, so ${missing === 1 ? 'that line prints' : 'those lines print'} blank to fill in by hand.`}
          </Alert>
        )}
      </div>

      {rows.length === 0 ? (
        // An empty bag is a state to explain, not a failure to report.
        <Alert tone="info">
          There is nothing to chart yet. A yardage chart covers the clubs you swing, so a bag
          holding only a putter — or only retired clubs — has no lines.{' '}
          <Link to="/bag" className="font-medium underline">
            Add a club
          </Link>
          .
        </Alert>
      ) : layout === 'sheet' ? (
        <Sheet rows={rows} unit={unit} playerName={user?.display_name ?? 'Yardage chart'} />
      ) : (
        <PocketCard rows={rows} unit={unit} />
      )}
    </div>
  )
}
