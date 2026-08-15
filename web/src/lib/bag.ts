/**
 * Bag-level derivations, shared by the bag screen and the printed yardage chart.
 *
 * Everything here falls out of the club rows themselves — the order a bag reads
 * in, which fields a club type carries, the gaps between clubs — so there is
 * nothing stored to keep in sync and nothing to migrate when a club changes.
 */
import type { Club } from '../types'
import { fromYards, type DistanceUnit } from './units'

/**
 * Whether a club type gets the full spec set: loft, flex, expected carry, and
 * average dispersion. Only a putter does not.
 *
 * Two different reasons are bundled here, and they differ in how hard the rule
 * is. Carry and dispersion describe a full shot, which a putter never hits, so
 * the *server* rejects them on one. Loft and flex are real on a putter — 3.5°
 * is a genuine spec — but not worth the form space, so they are merely hidden
 * in the UI and stay perfectly valid over the API.
 *
 * Kept as one helper so the form, the bag list, and the chart cannot disagree
 * about which fields a putter has.
 */
export function hasFullSpecs(clubType: string): boolean {
  return clubType !== 'putter'
}

/**
 * Orders a group the way a bag is actually laid out: by club type longest
 * first, then by loft ascending within a type, so a 3 wood precedes a 5 wood
 * and a 52° wedge precedes a 60°.
 *
 * This is derived rather than hand-arranged. `display_order` is the stored
 * fallback for clubs with no loft, and is what a future drag-to-reorder would
 * write to.
 */
export function bagOrder(clubs: Club[], clubTypes: string[]): Club[] {
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
 * One printed line of the yardage chart.
 *
 * Every distance here is already in the user's unit. The chart renders straight
 * from these, so no display code has to remember that the database is in yards.
 */
export interface ChartRow {
  club: Club
  /** Carry, or null when the club has none recorded. */
  carry: number | null
  /** How much shorter the next club on the chart is. */
  gap: number | null
  /** Average dispersion, or null when the club has none recorded. */
  dispersion: number | null
}

/**
 * Builds the printed chart from a set of clubs.
 *
 * Clubs with a carry sort by that carry, longest first, rather than by bag
 * order. A yardage chart is read by distance — "I have 150 in, what do I hit" —
 * so a column that decreases monotonically is what makes the lookup work. It
 * also stops the chart from hiding a 5 wood that genuinely outcarries the 3
 * wood above it in the bag: on this chart that club sits where it really is,
 * and the gap column shows the overlap rather than printing a negative number.
 *
 * Clubs with no carry keep bag order and follow at the end, as blank lines to
 * fill in at the range.
 */
export function chartRows(clubs: Club[], clubTypes: string[], unit: DistanceUnit): ChartRow[] {
  // A putter has no carry by definition, so it is not a line on a carry chart.
  const eligible = bagOrder(
    clubs.filter((club) => hasFullSpecs(club.club_type)),
    clubTypes,
  )

  const rated = eligible
    .flatMap((club) =>
      club.expected_carry === null ? [] : [{ club, carry: fromYards(club.expected_carry, unit) }],
    )
    .sort((a, b) => b.carry - a.carry)

  const unrated = eligible
    .filter((club) => club.expected_carry === null)
    .map((club) => ({ club, carry: null }))

  const ordered: Array<{ club: Club; carry: number | null }> = [...rated, ...unrated]

  // The gap comes from the converted numbers, not the stored yards, so the
  // column's arithmetic is self-consistent: a reader who subtracts two printed
  // carries gets the number printed beside them. Converting the difference
  // instead rounds separately and can disagree by a metre — the same trap as
  // converting a tee total rather than summing converted holes.
  return ordered.map((row, index) => {
    const next = ordered[index + 1]
    const gap =
      row.carry !== null && next !== undefined && next.carry !== null ? row.carry - next.carry : null
    const { average_dispersion: dispersion } = row.club
    return {
      ...row,
      gap,
      dispersion: dispersion === null ? null : fromYards(dispersion, unit),
    }
  })
}
