/**
 * A round's statistics, as the server computed them.
 *
 * Nothing here derives anything. Greens in regulation, scrambling, and sand
 * saves are computed in Go on every read, so there is one definition of each in
 * the codebase rather than one on each side of the wire.
 */
import type { Round } from '../types'
import { formatPercent, formatTally, formatToPar } from '../lib/rounds'

export function RoundSummaryCard({ round }: { round: Round }) {
  const s = round.summary

  if (s.holes_recorded === 0) {
    return (
      <div className="card p-5 text-sm text-slate-600 dark:text-slate-400">
        Nothing recorded yet. Scores appear here as you enter them.
      </div>
    )
  }

  const partial = s.holes_completed > 0 && s.holes_completed < round.holes_intended

  return (
    <div className="card space-y-4 p-5">
      <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
        <div>
          <p className="text-3xl font-bold tabular-nums">{s.strokes || '—'}</p>
          <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
            Strokes
          </p>
        </div>
        <div>
          <p className="text-3xl font-bold tabular-nums">{formatToPar(s.to_par)}</p>
          <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
            To par
          </p>
        </div>
        {(s.out_strokes > 0 || s.in_strokes > 0) && (
          <div>
            <p className="text-lg font-semibold tabular-nums">
              {s.out_strokes || '—'} / {s.in_strokes || '—'}
            </p>
            <p className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
              Out / In
            </p>
          </div>
        )}
        {partial && (
          <p className="text-sm text-slate-500 dark:text-slate-400">
            {s.holes_completed} of {round.holes_intended} holes scored
          </p>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 border-t border-slate-100 pt-4 sm:grid-cols-4 dark:border-slate-800">
        <Stat label="Fairways" value={formatPercent(s.fairways)} detail={formatTally(s.fairways)} />
        <Stat
          label="Greens in reg"
          value={formatPercent(s.greens_in_regulation)}
          detail={formatTally(s.greens_in_regulation)}
        />
        <Stat
          label="Putts"
          value={String(s.putts)}
          detail={
            s.holes_completed > 0
              ? `${(s.putts / s.holes_completed).toFixed(1)} per hole`
              : undefined
          }
        />
        <Stat
          label="Putts on GIR"
          value={
            s.greens_in_regulation.made > 0
              ? (s.putts_on_greens_hit / s.greens_in_regulation.made).toFixed(1)
              : '—'
          }
          detail="per green hit"
        />
        <Stat
          label="Scrambling"
          value={formatPercent(s.scrambles)}
          detail={formatTally(s.scrambles)}
        />
        <Stat
          label="Sand saves"
          value={formatPercent(s.sand_saves)}
          detail={formatTally(s.sand_saves)}
        />
        <Stat label="Penalties" value={String(s.penalties)} />
        <Stat
          label="Bunkers"
          value={String(s.fairway_bunkers + s.greenside_bunkers)}
          detail={`${s.fairway_bunkers} fairway, ${s.greenside_bunkers} greenside`}
        />
      </dl>
    </div>
  )
}

function Stat({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div>
      <dt className="text-xs tracking-wide text-slate-500 uppercase dark:text-slate-400">
        {label}
      </dt>
      <dd className="text-lg font-semibold tabular-nums">{value}</dd>
      {detail && <dd className="text-xs text-slate-500 dark:text-slate-400">{detail}</dd>}
    </div>
  )
}
