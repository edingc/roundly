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
  course_rating: number | null
  slope_rating: number | null
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

export interface AuthConfig {
  google_enabled: boolean
}

/** Payload for creating or editing a tee. */
export interface TeePayload {
  name: string
  color: string
  course_rating?: number | null
  slope_rating?: number | null
  display_order?: number
}
