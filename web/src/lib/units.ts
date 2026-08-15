/**
 * Distance units.
 *
 * Everything in the database is stored in yards — hole yardages, tee totals,
 * club carry and dispersion. This module is the only place that converts, and
 * it does so at the display and input boundary. Storing a unit alongside each
 * value would let one bag hold yards and metres at once and force every read
 * site to convert anyway.
 *
 * The preference lives on the user (`user.distance_unit`) rather than in
 * localStorage, so it follows the account across devices.
 */
export type DistanceUnit = 'yards' | 'meters'

/** Exact by definition: one international yard is 0.9144 metres. */
const METERS_PER_YARD = 0.9144

export function isDistanceUnit(value: string): value is DistanceUnit {
  return value === 'yards' || value === 'meters'
}

/** Converts a stored yard value into the unit the user reads, to a whole number. */
export function fromYards(yards: number, unit: DistanceUnit): number {
  if (unit === 'yards') return Math.round(yards)
  return Math.round(yards * METERS_PER_YARD)
}

/**
 * Converts a value the user typed back into the yards that get stored.
 *
 * Rounding here and in fromYards means a metre value can drift by a yard on a
 * round trip. That is under half a metre on a distance nobody measures to
 * better than a pace, and the alternative — storing fractional yards — would
 * put decimals into a scorecard grid that has always held whole numbers.
 */
export function toYards(value: number, unit: DistanceUnit): number {
  if (unit === 'yards') return Math.round(value)
  return Math.round(value / METERS_PER_YARD)
}

/** The short suffix used inline next to a number. */
export function unitSuffix(unit: DistanceUnit): string {
  return unit === 'yards' ? 'yds' : 'm'
}

/** The word used in form labels and settings. */
export function unitLabel(unit: DistanceUnit): string {
  return unit === 'yards' ? 'Yards' : 'Meters'
}

/** The abbreviation for the scorecard's distance row header, where space is tight. */
export function unitColumnLabel(unit: DistanceUnit): string {
  return unit === 'yards' ? 'Yds' : 'M'
}

/** "158 yds" / "144 m", from a stored yard value. */
export function formatDistance(yards: number, unit: DistanceUnit): string {
  return `${fromYards(yards, unit)} ${unitSuffix(unit)}`
}

/**
 * Converts a bound expressed in yards into the user's unit, so a number input's
 * min/max and the message beside it agree with what the user is typing. Bounds
 * round outward, so converting never excludes a value the server would accept.
 */
export function boundFromYards(yards: number, unit: DistanceUnit, edge: 'min' | 'max'): number {
  if (unit === 'yards') return yards
  const converted = yards * METERS_PER_YARD
  return edge === 'min' ? Math.floor(converted) : Math.ceil(converted)
}
