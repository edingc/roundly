/** Mirrors the JSON contracts in internal/auth and internal/course. */

export interface User {
  id: string
  email: string
  display_name: string
  email_verified: boolean
  has_password: boolean
  providers: string[]
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
