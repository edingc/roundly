/**
 * The landing screen: where the game is, at a glance.
 *
 * Everything here is computed server-side from completed rounds and nothing is
 * stored, so correcting a scorecard moves these numbers with it. The window
 * selector changes the question rather than the presentation - "the last five"
 * and "all time" are different figures, not the same one zoomed.
 */
import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { formatPlayedOn, formatToPar } from '../lib/rounds'
import type { Overview, Tally } from '../types'
import { BarCompare, ChartCard, SERIES_COLORS } from '../components/charts'
import { Alert, EmptyState, PageSpinner, PlusIcon } from '../components/ui'

/** The windows the server offers. 0 means every round. */
const WINDOWS = [
  { value: 5, label: 'Last 5' },
  { value: 10, label: 'Last 10' },
  { value: 20, label: 'Last 20' },
  { value: 50, label: 'Last 50' },
  { value: 0, label: 'All time' },
]

/** A tally as a percentage, or null when nothing was attempted - null so a
 *  chart leaves a gap rather than drawing a drop to zero that never happened. */
function rate(t: Tally): number | null {
  if (t.attempted === 0) return null
  return Math.round((t.made / t.attempted) * 100)
}

function pct(t: Tally): string {
  if (t.attempted === 0) return '—'
  return `${Math.round((t.made / t.attempted) * 100)}%`
}

function oneDecimal(v: number | null): string {
  return v === null ? '—' : v.toFixed(1)
}

export default function OverviewPage() {
  const [window, setWindow] = useState(20)
  const [overview, setOverview] = useState<Overview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (rounds: number) => {
    setError(null)
    try {
      setOverview(await api.overview(rounds))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load your stats.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(window)
  }, [load, window])

  if (loading) return <PageSpinner label="Loading your stats" />
  if (error && !overview) return <Alert>{error}</Alert>

  if (!overview || overview.rounds_counted === 0) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold tracking-tight">Overview</h1>
        <EmptyState
          title="No finished rounds yet"
          description="Play or enter a round and this fills in: scoring, greens, fairways, putting, and a handicap once there are three cards to compute one from."
          action={
            <Link to="/rounds/new" className="btn-primary">
              <PlusIcon className="size-5" />
              New round
            </Link>
          }
        />
      </div>
    )
  }

  // One row per round, shaped for the charts. Null rather than zero wherever a
  // round did not record something: Recharts skips a null point, and plotting a
  // missing fairway percentage as 0% would draw a cliff that never happened.
  const chartData = overview.series.map((p) => ({
    date: formatPlayedOn(p.played_on),
    course: p.course_name,
    toPar: p.to_par,
    strokes: p.strokes,
    putts: p.putts,
    gir: rate(p.greens_in_regulation),
    fir: rate(p.fairways),
    differential: p.differential,
    eagles: p.scores.eagle_or_better,
    birdies: p.scores.birdies,
    pars: p.scores.pars,
    bogeys: p.scores.bogeys,
    doubles: p.scores.double_or_worse,
  }))
  const hasDifferentials = chartData.some((p) => p.differential !== null)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Overview</h1>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {overview.rounds_counted} finished {overview.rounds_counted === 1 ? 'round' : 'rounds'}
            {overview.window > 0 && overview.rounds_counted >= overview.window
              ? ` · most recent ${overview.window}`
              : ''}
          </p>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <span className="text-slate-600 dark:text-slate-400">Showing</span>
          <select
            className="input !min-h-0 !w-auto !py-2"
            value={window}
            onChange={(e) => setWindow(Number(e.target.value))}
            aria-label="How many rounds to include"
          >
            {WINDOWS.map((w) => (
              <option key={w.value} value={w.value}>
                {w.label}
              </option>
            ))}
          </select>
        </label>
      </div>

      {error && <Alert>{error}</Alert>}

      {/* The two headline numbers, side by side on purpose: the gap between
          them is the honest measure of consistency, and neither says much
          alone. */}
      <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <HandicapCard overview={overview} />
        <div className="card space-y-1 p-5">
          <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
            Scoring average
          </p>
          <p className="text-4xl font-bold tabular-nums">{oneDecimal(overview.average_score)}</p>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {overview.average_to_par !== null && (
              <>
                {overview.average_to_par > 0 ? '+' : ''}
                {overview.average_to_par.toFixed(1)} to par
              </>
            )}
            {overview.best_to_par !== null && (
              <> · best {formatToPar(overview.best_to_par)}</>
            )}
          </p>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Complete rounds only. A card with holes left blank is not a score.
          </p>
        </div>
      </section>

      <section className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <StatTile label="Greens in reg" value={pct(overview.greens_in_regulation)} tally={overview.greens_in_regulation} />
        <StatTile label="Fairways" value={pct(overview.fairways)} tally={overview.fairways} />
        <StatTile label="Putts / round" value={oneDecimal(overview.average_putts)} />
        <StatTile label="Scrambling" value={pct(overview.scrambles)} tally={overview.scrambles} />
        <StatTile label="Sand saves" value={pct(overview.sand_saves)} tally={overview.sand_saves} />
        <StatTile label="Penalties / round" value={oneDecimal(overview.average_penalties)} />
      </section>

      <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Bars rather than a line, throughout. A round is a discrete event -
            you played an 84 and then an 89 - and a line between them draws a
            path through scores that were never shot. */}
        <ChartCard
          title="Score to par"
          subtitle="One bar per round. The dashed line is level par."
          empty={chartData.length === 0}
        >
          <BarCompare
            data={chartData}
            xKey="date"
            reference={{ value: 0, label: 'E' }}
            // Bars leave the zero line in the direction the score went, and the
            // colour says which without reading the axis.
            barColor={(v) => (v <= 0 ? SERIES_COLORS[0] : 'var(--chart-series-3)')}
            series={[{ key: 'toPar', label: 'To par', format: (v) => formatToPar(Math.round(v)) }]}
          />
        </ChartCard>

        <ChartCard
          title="How the holes finished"
          subtitle="Every hole of every round, by result against par."
          empty={chartData.length === 0}
        >
          {/* The one chart here that is honestly stacked: these five partition
              the holes, so the bar height is the round. Stacking greens against
              fairways would total two percentages of different denominators,
              which is a number about nothing. */}
          <BarCompare
            data={chartData}
            xKey="date"
            stacked
            series={[
              { key: 'eagles', label: 'Eagle+', color: 'var(--chart-series-4)' },
              { key: 'birdies', label: 'Birdie', color: 'var(--chart-series-1)' },
              { key: 'pars', label: 'Par', color: 'var(--chart-series-2)' },
              { key: 'bogeys', label: 'Bogey', color: 'var(--chart-series-3)' },
              { key: 'doubles', label: 'Double+', color: 'var(--color-red-500)' },
            ]}
          />
        </ChartCard>

        <ChartCard
          title="Greens and fairways"
          subtitle="Per round, side by side. Fairways count par 4s and 5s only."
          empty={chartData.length === 0}
        >
          {/* Grouped, not stacked: two percentages of different denominators do
              not add up to anything. Side by side answers the question that is
              actually being asked - which one is carrying the other. */}
          <BarCompare
            data={chartData}
            xKey="date"
            yDomain={[0, 100]}
            series={[
              { key: 'gir', label: 'Greens in reg', format: (v) => `${Math.round(v)}%` },
              { key: 'fir', label: 'Fairways', format: (v) => `${Math.round(v)}%` },
            ]}
          />
        </ChartCard>

        <ChartCard
          title="Score differential"
          subtitle="What each round contributes to the handicap. Lower is better."
          empty={!hasDifferentials}
          emptyMessage="Needs complete rounds on courses with a published rating and slope."
        >
          <BarCompare
            data={chartData}
            xKey="date"
            series={[{ key: 'differential', label: 'Differential', format: (v) => v.toFixed(1) }]}
          />
        </ChartCard>

        <ChartCard
          title="Putts"
          subtitle="Per round. Lower is better."
          empty={chartData.length === 0}
          className="lg:col-span-2"
        >
          <BarCompare
            data={chartData}
            xKey="date"
            series={[{ key: 'putts', label: 'Putts', format: (v) => String(Math.round(v)) }]}
          />
        </ChartCard>
      </section>
    </div>
  )
}

function HandicapCard({ overview }: { overview: Overview }) {
  const h = overview.handicap

  if (!h || h.index === null) {
    return (
      <div className="card space-y-1 p-5">
        <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
          Handicap
        </p>
        <p className="text-4xl font-bold text-slate-300 tabular-nums dark:text-slate-600">—</p>
        <p className="text-sm text-slate-600 dark:text-slate-400">
          {h && h.differentials_available > 0
            ? `${h.differentials_available} of the 3 rounds needed.`
            : 'Needs three complete rounds on a course with a published rating and slope.'}
        </p>
      </div>
    )
  }

  return (
    <div className="card space-y-3 p-5">
      <div className="flex items-baseline gap-6">
        <div>
          <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
            Handicap
          </p>
          <p className="text-4xl font-bold tabular-nums">{h.index.toFixed(1)}</p>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            best {h.index_using} of {h.differentials_available}
          </p>
        </div>
        <div>
          <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
            Anti-cap
          </p>
          <p className="text-4xl font-bold tabular-nums">
            {h.anti_cap === null ? '—' : h.anti_cap.toFixed(1)}
          </p>
          {h.anti_cap !== null && (
            <p className="text-xs text-slate-500 dark:text-slate-400">
              worst {h.anti_cap_using} of {h.differentials_available}
            </p>
          )}
        </div>
      </div>
      {/* Said plainly rather than buried: these are computed from gross scores,
          not the net-double-bogey-adjusted ones a governing body would use. */}
      <p className="text-xs text-slate-500 dark:text-slate-400">
        Unofficial. Computed from your gross scores rather than the adjusted ones the World
        Handicap System uses, so both read a little high — the trend is honest, the number is
        not postable.
      </p>
    </div>
  )
}

function StatTile({ label, value, tally }: { label: string; value: string; tally?: Tally }) {
  return (
    <div className="card p-4">
      <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">{label}</p>
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      {tally && tally.attempted > 0 && (
        <p className="text-xs text-slate-500 dark:text-slate-400">
          {tally.made} of {tally.attempted}
        </p>
      )}
    </div>
  )
}
