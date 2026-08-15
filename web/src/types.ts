/** Mirrors the JSON contracts in internal/auth, internal/course, and internal/club. */

import type { DistanceUnit } from './lib/units'

export type { DistanceUnit }

export interface User {
  id: string
  email: string
  display_name: string
  email_verified: boolean
  has_password: boolean
  providers: string[]
  /**
   * Which unit this user reads and enters distances in. A display preference
   * only — everything is stored in yards. See lib/units.ts.
   */
  distance_unit: DistanceUnit
  created_at: string
}

export interface Session {
  access_token: string
  refresh_token: string
  expires_in: number
  token_type: string
  user: User
}

export interface CourseSummary {
  id: string
  name: string
  address: string | null
  phone: string | null
  website: string | null
  notes: string | null
  facility_type: string | null
  latitude: number | null
  longitude: number | null
  pinned: boolean
  created_by: string
  created_at: string
  updated_at: string
  can_edit: boolean
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
export interface CourseExport {
  format_version: number
  id?: string
  name: string
  address: string | null
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

export interface AuthConfig {
  google_enabled: boolean
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
