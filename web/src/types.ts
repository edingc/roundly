/** Mirrors the JSON contracts in internal/auth, internal/course, and internal/club. */

import type { DistanceUnit } from './lib/units'

export type { DistanceUnit }

export interface User {
  id: string
  email: string
  display_name: string
  email_verified: boolean
  /**
   * Whether this account demands a mailed code when signing in with a password.
   * Opt-in, and only ever true on an instance that can send mail.
   */
  two_factor_email: boolean
  /**
   * Unused recovery codes left on the sheet. Always 0 when two-factor is off;
   * the profile prompts for a fresh sheet as this runs down.
   */
  recovery_codes_remaining: number
  has_password: boolean
  providers: string[]
  /**
   * Which unit this user reads and enters distances in. A display preference
   * only — everything is stored in yards. See lib/units.ts.
   */
  distance_unit: DistanceUnit

  /** Profile fields. All optional — a display name is the only required identity. */
  first_name: string | null
  last_name: string | null
  /**
   * Relative, signed path to the avatar image, or null.
   *
   * The query string carries an expiry and an HMAC, so this link stops working
   * within a day or so and cannot be minted by anyone but the server. Always
   * render the value the server most recently gave you rather than storing one
   * — every session refresh returns a fresh user, which is what keeps the link
   * ahead of its own expiry. The expiry is bucketed, so the string only
   * actually changes twice a day and the browser cache still hits.
   */
  avatar_url: string | null
  home_course_id: string | null
  /** Resolved server-side so the client need not fetch the course to name it. */
  home_course_name: string | null
  /** The home course's "City, Region, Country", resolved from the same row. */
  home_course_location: string | null
  location_city: string | null
  location_region: string | null
  location_country: string | null
  /**
   * Which published set of course ratings applies to this player.
   *
   * Course and slope ratings come in two sets for the same tee, because the
   * markers rate differently against the men's and women's scratch-golfer
   * models. Null means unset, which reads the men's - what every round used
   * before this existed. Nothing else in the app reads it.
   */
  gender: Gender | null

  created_at: string
  updated_at: string

  /**
   * Derived from server configuration, never stored. Use it to decide what to
   * render; every actual permission check happens server-side.
   */
  is_admin: boolean
}

/** Which set of published course ratings a player is rated against. */
export type Gender = 'men' | 'women'

/** Payload for PUT /account/profile. */
export interface ProfilePayload {
  display_name: string
  first_name?: string | null
  last_name?: string | null
  home_course_id?: string | null
  location_city?: string | null
  location_region?: string | null
  location_country?: string | null
  gender?: Gender | '' | null
}

/**
 * An API key's metadata. There is deliberately no token field: the secret is
 * returned once, by createApiKey, and never again.
 */
export interface ApiKey {
  id: string
  name: string
  key_prefix: string
  scope: string
  created_at: string
  last_used_at: string | null
  expires_at: string | null
}

/** The one and only response that carries a key's secret. */
export interface CreatedApiKey {
  key: ApiKey
  token: string
}

export interface ImportCounts {
  imported: number
  skipped: number
  failed: number
  skipped_names: string[]
  failed_names: string[]
  truncated: boolean
}

export interface ImportSummary {
  format_version: number
  profile: { fields_filled: string[] | null; fields_skipped: string[] | null }
  clubs: ImportCounts
  courses: ImportCounts
  rounds: ImportCounts
  warnings: string[] | null
}

export interface Session {
  access_token: string
  refresh_token: string
  expires_in: number
  token_type: string
  user: User
  /**
   * Present only on a two-factor login that asked to be remembered. The client
   * stores it and sends it on later sign-ins to skip the code; it never appears
   * on a refresh, because a device is trusted once rather than re-trusted.
   */
  device_token?: string
}

/**
 * Where a course is, in parts. Flat on the course rather than nested, matching
 * the wire shape, and every part independently optional: a course known only by
 * its town is a perfectly good directory entry.
 *
 * `street` is kept alongside the rest because it is what puts a map pin on the
 * clubhouse instead of the town.
 */
export interface CourseLocation {
  street: string | null
  city: string | null
  region: string | null
  postal_code: string | null
  country: string | null
}

export interface CourseSummary extends CourseLocation {
  id: string
  name: string
  phone: string | null
  website: string | null
  notes: string | null
  facility_type: string | null
  latitude: number | null
  longitude: number | null
  pinned: boolean
  /**
   * Who entered this course. Attribution only — it grants no rights, and it is
   * null once that account is deleted. Anyone signed in may edit any course.
   */
  uploaded_by: string | null
  created_at: string
  updated_at: string
  hole_count: number
  tee_count: number
}

export interface Tee {
  id: string
  course_id: string
  name: string
  color: string
  course_rating_men: number | null
  slope_rating_men: number | null
  course_rating_women: number | null
  slope_rating_women: number | null
  front9_course_rating_men: number | null
  front9_slope_rating_men: number | null
  back9_course_rating_men: number | null
  back9_slope_rating_men: number | null
  front9_course_rating_women: number | null
  front9_slope_rating_women: number | null
  back9_course_rating_women: number | null
  back9_slope_rating_women: number | null
  /** Derived server-side by summing the per-hole yardages for this tee. */
  total_yardage: number
  display_order: number
}

export interface TeeDetail {
  tee_id: string
  par: number
  yardage: number
}

export interface Hole {
  id: string
  course_id: string
  hole_number: number
  handicap_index: number | null
  tee_details: TeeDetail[]
}

export interface CourseDetail extends CourseSummary {
  tees: Tee[]
  holes: Hole[]
}

export interface CoursePage {
  items: CourseSummary[]
  total: number
  limit: number
  offset: number
}

/**
 * The full course, transferable between instances. Tees and holes are
 * matched by name and number rather than ID, since IDs are not stable across
 * app instances. Export and import share this shape.
 */
export interface CourseExport extends CourseLocation {
  format_version: number
  id?: string
  name: string
  phone: string | null
  website: string | null
  facility_type: string | null
  latitude: number | null
  longitude: number | null
  tees: TeePayload[]
  holes: Array<{
    hole_number: number
    handicap_index: number | null
    tee_details: Array<{ tee_name: string; par: number; yardage: number }>
  }>
}

/**
 * A request to take a course out of the directory. course_id goes null once the
 * course is actually removed; course_name is a snapshot so the record still
 * says what it was about.
 */
export interface RemovalRequest {
  id: string
  course_id: string | null
  course_name: string
  requested_by: string | null
  requested_by_name: string | null
  reason: string
  created_at: string
  resolved_at: string | null
  resolution: string | null
}

export interface AuthConfig {
  google_enabled: boolean
  geocoding_enabled: boolean
  /** Whether this instance can send mail at all. Gates two-factor and the
   *  confirm-your-address flow together — neither is offered without it. */
  email_enabled: boolean
  /** Whether an unconfirmed account is shut out of the app. */
  email_verification_required: boolean
}

/**
 * What /auth/login returns instead of a session when a code is needed.
 *
 * The two shapes are told apart by `two_factor_required`, which is present and
 * true only here.
 */
export interface TwoFactorChallenge {
  two_factor_required: true
  challenge_id: string
  expires_in: number
}

/**
 * What turning two-factor on returns.
 *
 * `recovery_codes` appear exactly once, here. They are stored hashed and can
 * never be shown again, so a client that does not put them in front of the user
 * at this moment has lost them.
 */
export interface TwoFactorSetup {
  user: User
  recovery_codes?: string[]
}

/** A browser this account has chosen to remember, so it is not asked for a code. */
export interface TrustedDevice {
  id: string
  label: string | null
  /** True for the browser making the request, so the list can say "this one". */
  current: boolean
  created_at: string
  last_used_at: string | null
  expires_at: string
}

/**
 * Where a club sits in the bag. Derived server-side from the stored flags, so
 * "retired but somehow still active" is not representable.
 */
export type ClubStatus = 'active' | 'benched' | 'retired'

export interface Club {
  id: string
  user_id: string
  club_type: string
  label: string
  brand: string | null
  model: string | null
  /** Degrees, fractional because wedges are sold in half degrees. */
  loft: number | null
  shaft: string | null
  flex: string | null
  notes: string | null
  /** Yards the player expects to fly this club. Always null for a putter. */
  expected_carry: number | null
  /** Yards of typical spread around that carry. Always null for a putter. */
  average_dispersion: number | null
  status: ClubStatus
  retired_at: string | null
  display_order: number
  created_at: string
  updated_at: string
}

/** The whole equipment screen in one response. */
export interface Bag {
  active: Club[]
  benched: Club[]
  retired: Club[]
  active_count: number
  club_limit: number
  /** A warning the UI surfaces; the server still accepted the state. */
  over_limit: boolean
}

/**
 * The club types and flexes the server will accept, fetched rather than
 * duplicated here so the two cannot drift apart. `club_types` arrives in bag
 * order, longest club first.
 */
export interface ClubOptions {
  club_types: string[]
  flexes: string[]
  club_limit: number
}

/** Payload for creating or editing a club. */
export interface ClubPayload {
  club_type: string
  label: string
  brand?: string | null
  model?: string | null
  loft?: number | null
  shaft?: string | null
  flex?: string | null
  notes?: string | null
  /** Rejected by the server on a putter — send null, not a number. */
  expected_carry?: number | null
  average_dispersion?: number | null
  display_order?: number
  /** Honored on create only; status changes go through setClubStatus. */
  status?: ClubStatus
}

/** Payload for creating or editing a tee. */
export interface TeePayload {
  name: string
  color: string
  course_rating_men?: number | null
  slope_rating_men?: number | null
  course_rating_women?: number | null
  slope_rating_women?: number | null
  front9_course_rating_men?: number | null
  front9_slope_rating_men?: number | null
  back9_course_rating_men?: number | null
  back9_slope_rating_men?: number | null
  front9_course_rating_women?: number | null
  front9_slope_rating_women?: number | null
  back9_course_rating_women?: number | null
  back9_slope_rating_women?: number | null
  display_order?: number
}

// ---- Rounds ----

export type RoundStatus = 'in_progress' | 'complete' | 'abandoned'
export type EntryMode = 'live' | 'manual'
export type Nine = 'front' | 'back'

/**
 * Where a tee shot finished, relative to where it was aimed.
 *
 * `hit` rather than `fairway` because a par 3 has no fairway: it means the
 * intended target was found, which is the fairway on a par 4 or 5 and the green
 * on a par 3. The UI labels it accordingly.
 */
export type TeeAccuracy =
  | 'hit'
  | 'left'
  | 'far_left'
  | 'right'
  | 'far_right'
  | 'long'
  | 'short'
  | 'mishit'

export type PenaltyType = 'ob_lost' | 'penalty_area' | 'unplayable' | 'other'

export interface RoundHole {
  hole_number: number
  /** Snapshotted from the course when the round started, not read live. */
  par: number | null
  yardage: number | null
  stroke_index: number | null
  /** Null means the hole was not completed - picked up, conceded, out of light. */
  strokes: number | null
  putts: number | null
  tee_club_id: string | null
  /** Resolved server-side so a retired club still reads as itself. */
  tee_club_label: string | null
  tee_accuracy: TeeAccuracy | null
  first_putt_feet: number | null
  fairway_bunker: boolean
  greenside_bunker: boolean
  penalties: number
  penalty_type: PenaltyType | null
}

/** A made-out-of-attempted count. The client formats the percentage, because a
 *  percentage has to invent an answer when nothing was attempted. */
export interface Tally {
  made: number
  attempted: number
}

/** Everything derivable from a round's holes. Computed on every read, never
 *  stored, so an edit can never leave a stale copy behind. */
/**
 * Holes grouped by result against par.
 *
 * Sums to the number of completed holes that had a par recorded, which is what
 * makes this the one breakdown in the app that can honestly be stacked: the
 * parts partition a whole rather than being unrelated measures sharing an axis.
 */
export interface ScoreCounts {
  eagle_or_better: number
  birdies: number
  pars: number
  bogeys: number
  double_or_worse: number
}

export interface RoundSummary {
  holes_recorded: number
  holes_completed: number
  strokes: number
  par: number
  to_par: number
  putts: number
  out_strokes: number
  in_strokes: number
  fairways: Tally
  greens_in_regulation: Tally
  scrambles: Tally
  sand_saves: Tally
  penalties: number
  fairway_bunkers: number
  greenside_bunkers: number
  scores: ScoreCounts
  putts_on_greens_hit: number
}

export interface Round {
  id: string
  /** Null once an administrator removes the course. The snapshots below are
   *  what keep the round readable when that happens. */
  course_id: string | null
  course_name: string
  tee_id: string | null
  tee_name: string
  tee_color: string | null
  course_rating: number | null
  slope_rating: number | null
  /** A local calendar date, not a timestamp. */
  played_on: string
  started_at: string | null
  completed_at: string | null
  status: RoundStatus
  entry_mode: EntryMode
  holes_intended: number
  nine: Nine | null
  notes: string | null
  created_at: string
  updated_at: string
  holes: RoundHole[]
  summary: RoundSummary
  /**
   * What this round contributes to a handicap. Null until every intended hole
   * has a score, and null on a course with no published rating. Unofficial: see
   * round.DifferentialFor in Go.
   */
  differential: number | null
}

export interface RoundPage {
  items: Round[]
  total: number
  limit: number
  offset: number
}

/** What starting a round needs. `id` is client-supplied so a round can begin
 *  with no signal and still have something for its holes to attach to. */
export interface StartRoundPayload {
  id?: string
  course_id: string
  tee_id: string
  played_on: string
  holes: number
  nine?: Nine | ''
  entry_mode: EntryMode
  notes?: string | null
}

/**
 * One hole's scoring, as sent.
 *
 * A PUT, so this is the whole hole: an omitted field is cleared, not left
 * alone. The snapshot fields (par, yardage, stroke index) are the exception -
 * the server keeps those unless a value is supplied.
 */
export interface RoundHolePayload {
  hole_number: number
  par?: number | null
  strokes: number | null
  putts: number | null
  tee_club_id: string | null
  tee_accuracy: TeeAccuracy | null
  first_putt_feet: number | null
  fairway_bunker: boolean
  greenside_bunker: boolean
  penalties: number
  penalty_type: PenaltyType | null
}

// ---- Overview ----

/** One round, for the charts. Oldest first, so a line reads left to right. */
export interface StatPoint {
  round_id: string
  played_on: string
  course_name: string
  holes: number
  strokes: number
  to_par: number
  putts: number
  fairways: Tally
  greens_in_regulation: Tally
  scores: ScoreCounts
  /** Null when the round has no rating and slope to compute one from. */
  differential: number | null
}

/**
 * The two numbers derived from score differentials.
 *
 * `unofficial` is always true and says so on the wire: both are computed from
 * gross scores rather than the net-double-bogey-adjusted ones the World
 * Handicap System uses, so they read slightly high. See internal/stats.
 */
export interface HandicapStats {
  index: number | null
  /** The mirror of the index: the average of the *worst* twelve differentials. */
  anti_cap: number | null
  differentials_available: number
  index_using: number
  anti_cap_using: number
  unofficial: boolean
}

export interface Overview {
  window: number
  rounds_counted: number
  average_score: number | null
  average_to_par: number | null
  best_to_par: number | null
  average_putts: number | null
  average_penalties: number | null
  fairways: Tally
  greens_in_regulation: Tally
  scrambles: Tally
  sand_saves: Tally
  handicap: HandicapStats | null
  series: StatPoint[]
}
