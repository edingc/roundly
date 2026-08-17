/**
 * Formatting for rounds. Nothing here computes a statistic - the server does
 * that on every read, so there is exactly one definition of "green in
 * regulation" in the codebase and it is in Go.
 */
import type { Nine, PenaltyType, Round, RoundHole, RoundHolePayload, Tally, TeeAccuracy } from '../types'

/** Renders a score relative to par the way a leaderboard does. */
export function formatToPar(toPar: number): string {
  if (toPar === 0) return 'E'
  return toPar > 0 ? `+${toPar}` : String(toPar)
}

/**
 * Renders a tally as "7/14 (50%)", or a dash when nothing was attempted.
 *
 * The dash is the point: "0%" of nothing reads like a terrible round rather
 * than a round with no par 4s recorded yet.
 */
export function formatTally(t: Tally): string {
  if (t.attempted === 0) return '—'
  return `${t.made}/${t.attempted} (${Math.round((t.made / t.attempted) * 100)}%)`
}

/**
 * A tally as a whole percentage, or null when nothing was attempted.
 *
 * Null rather than zero throughout: a chart leaves a gap rather than drawing a
 * drop that never happened, and a meter draws no bar rather than an empty one.
 */
export function tallyRate(t: Tally): number | null {
  if (t.attempted === 0) return null
  return Math.round((t.made / t.attempted) * 100)
}

export function formatPercent(t: Tally): string {
  const rate = tallyRate(t)
  return rate === null ? '—' : `${rate}%`
}

/**
 * Whether a round came in under the bogey golfer's benchmark - a stroke over
 * par on every hole.
 *
 * This is the one threshold the round list colours against, and the benchmark
 * is the handicap system's rather than a number invented here. Measured over
 * the holes actually completed, so a card with five holes on it is judged
 * against five holes. Level bogey golf counts as not beating it.
 */
export function beatsBogeyGolf(round: Round): boolean {
  const holes = round.summary.holes_completed
  return holes > 0 && round.summary.to_par < holes
}

/** Average putts per completed hole, to one decimal. */
export function formatPuttsPerHole(round: Round): string {
  const holes = round.summary.holes_completed
  if (holes === 0) return '—'
  return (round.summary.putts / holes).toFixed(1)
}

/**
 * The label for "found the target", which depends on the hole.
 *
 * The stored value is always `hit`; only the word changes. A par 3 has no
 * fairway, and calling the button "Fairway" there would be asking about
 * something that does not exist.
 */
export function targetLabel(par: number | null): string {
  return par === 3 ? 'Green' : 'Fairway'
}

export const ACCURACY_LABELS: Record<TeeAccuracy, string> = {
  hit: 'Hit',
  left: 'Left',
  far_left: 'Far left',
  right: 'Right',
  far_right: 'Far right',
  long: 'Long',
  short: 'Short',
  mishit: 'Mishit',
}

export const PENALTY_LABELS: Record<PenaltyType, string> = {
  ob_lost: 'OB or lost',
  penalty_area: 'Penalty area',
  unplayable: 'Unplayable',
  other: 'Other',
}

export const PENALTY_TYPES: PenaltyType[] = ['ob_lost', 'penalty_area', 'unplayable', 'other']

/** Which holes a round covers. A back nine keeps 10-18. */
export function holeNumbersFor(holes: number, nine: Nine | null | ''): number[] {
  const start = holes === 9 && nine === 'back' ? 10 : 1
  const count = holes === 9 ? 9 : 18
  return Array.from({ length: count }, (_, i) => start + i)
}

/** Turns a stored hole into the payload that would save it unchanged. */
export function toHolePayload(hole: RoundHole): RoundHolePayload {
  return {
    hole_number: hole.hole_number,
    strokes: hole.strokes,
    putts: hole.putts,
    tee_club_id: hole.tee_club_id,
    tee_accuracy: hole.tee_accuracy,
    first_putt_feet: hole.first_putt_feet,
    fairway_bunker: hole.fairway_bunker,
    greenside_bunker: hole.greenside_bunker,
    penalties: hole.penalties,
    penalty_type: hole.penalty_type,
  }
}

/** True when a hole holds nothing worth saving. Mirrors isBlank in Go. */
export function isHoleBlank(hole: RoundHole): boolean {
  return (
    hole.strokes === null &&
    hole.putts === null &&
    hole.tee_club_id === null &&
    hole.tee_accuracy === null &&
    hole.first_putt_feet === null &&
    !hole.fairway_bunker &&
    !hole.greenside_bunker &&
    hole.penalties === 0
  )
}

/** A round's date, rendered for a list. */
export function formatPlayedOn(playedOn: string): string {
  // Parsed as a local date rather than through Date(string), which reads a bare
  // YYYY-MM-DD as UTC midnight and can render as the day before.
  const [y, m, d] = playedOn.split('-').map(Number)
  if (!y || !m || !d) return playedOn
  return new Date(y, m - 1, d).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

/** Today, as the local calendar date the API wants. */
export function todayISO(): string {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

/** A differential to one decimal, which is how handicaps are published. */
export function formatDifferential(value: number | null): string {
  return value === null ? '—' : value.toFixed(1)
}

export function describeRound(round: Round): string {
  const holes = round.holes_intended === 9 ? `9 holes` : '18 holes'
  const nine = round.nine ? ` (${round.nine})` : ''
  return `${holes}${nine} · ${round.tee_name}`
}
