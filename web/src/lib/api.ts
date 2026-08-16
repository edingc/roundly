import type {
  ApiKey,
  AuthConfig,
  Bag,
  Club,
  ClubOptions,
  ClubPayload,
  ClubStatus,
  CreatedApiKey,
  DistanceUnit,
  CourseDetail,
  CourseExport,
  CoursePage,
  Hole,
  ImportSummary,
  ProfilePayload,
  RemovalRequest,
  Session,
  Tee,
  TeePayload,
  User,
} from '../types'

const REFRESH_TOKEN_KEY = 'roundly.refresh_token'

/**
 * ApiError carries the server's error envelope so callers can show the message
 * and attach `fields` to the inputs that failed.
 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields: Record<string, string>

  constructor(status: number, code: string, message: string, fields?: Record<string, string>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.fields = fields ?? {}
  }

  /** True when the failure was a per-field validation problem. */
  get isValidation(): boolean {
    return this.code === 'validation_failed' || Object.keys(this.fields).length > 0
  }
}

/**
 * Token state lives in this module rather than in React state.
 *
 * The access token is deliberately kept in memory only: anything in
 * localStorage is readable by any script on the page. The refresh token has to
 * survive a reload for sessions to persist, so it is the one value stored, and
 * it is single-use — the server rotates it on every refresh.
 */
let accessToken: string | null = null
let onSessionExpired: (() => void) | null = null

/**
 * Deduplicates concurrent refreshes so parallel callers — a 401 retry racing
 * the boot-time restore, or React StrictMode's double-invoked mount effect —
 * trigger only one call. Refresh tokens are single-use and rotated; a second
 * concurrent call would present the same now-consumed token, which the
 * server treats as a replay and responds to by revoking every session for
 * the user, including the one the first call just issued.
 */
let refreshInFlight: Promise<Session | null> | null = null

export function getAccessToken(): string | null {
  return accessToken
}

export function getStoredRefreshToken(): string | null {
  try {
    return localStorage.getItem(REFRESH_TOKEN_KEY)
  } catch {
    return null
  }
}

export function setSession(session: Session | null): void {
  accessToken = session?.access_token ?? null
  try {
    if (session?.refresh_token) {
      localStorage.setItem(REFRESH_TOKEN_KEY, session.refresh_token)
    } else {
      localStorage.removeItem(REFRESH_TOKEN_KEY)
    }
  } catch {
    // Private browsing can block storage. The session still works until reload.
  }
}

/** Registers the callback that clears app state when the session dies. */
export function setSessionExpiredHandler(handler: (() => void) | null): void {
  onSessionExpired = handler
}

interface RequestOptions {
  method?: string
  body?: unknown
  /** Skip the Authorization header and the retry-after-refresh behavior. */
  auth?: boolean
  signal?: AbortSignal
  /**
   * Send this instead of a JSON body, with no Content-Type of our own so the
   * browser can set the multipart boundary. Used for the avatar upload.
   */
  formData?: FormData
  /**
   * Return the raw response body rather than parsed JSON. Used for the ZIP
   * export, which is not JSON at all.
   */
  blob?: boolean
}

async function parseError(response: Response): Promise<ApiError> {
  let code = 'unknown_error'
  let message = `Request failed with status ${response.status}.`
  let fields: Record<string, string> | undefined

  try {
    const body = await response.json()
    if (typeof body?.message === 'string') message = body.message
    if (typeof body?.error === 'string') code = body.error
    if (body?.fields && typeof body.fields === 'object') fields = body.fields
  } catch {
    // A non-JSON body (a proxy error page, say) leaves the defaults in place.
  }

  return new ApiError(response.status, code, message, fields)
}

async function rawRequest<T>(path: string, options: RequestOptions): Promise<T> {
  const headers: Record<string, string> = {}
  // A blob response is a file, not JSON, so it must not claim otherwise.
  headers['Accept'] = options.blob ? '*/*' : 'application/json'
  // Deliberately no Content-Type for FormData: the browser has to set it
  // itself, because only it knows the multipart boundary it generated.
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (options.auth !== false && accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`
  }

  let body: BodyInit | undefined
  if (options.formData !== undefined) body = options.formData
  else if (options.body !== undefined) body = JSON.stringify(options.body)

  const response = await fetch(`/api${path}`, {
    method: options.method ?? 'GET',
    headers,
    body,
    // Cookies carry the OAuth state/PKCE values during the Google redirect.
    credentials: 'same-origin',
    signal: options.signal,
  })

  if (!response.ok) throw await parseError(response)
  if (response.status === 204) return undefined as T
  if (options.blob) return (await response.blob()) as T
  return (await response.json()) as T
}

/**
 * Exchanges the stored refresh token for a new session.
 *
 * Concurrent callers share one in-flight request, so a screen that fires several
 * requests at once after the access token expires does not burn several
 * single-use refresh tokens and trip the server's replay defense.
 */
async function refreshSession(): Promise<Session | null> {
  if (refreshInFlight) return refreshInFlight

  refreshInFlight = (async () => {
    const stored = getStoredRefreshToken()
    if (!stored) return null
    try {
      const session = await rawRequest<Session>('/auth/refresh', {
        method: 'POST',
        body: { refresh_token: stored },
        auth: false,
      })
      setSession(session)
      return session
    } catch {
      setSession(null)
      return null
    }
  })()

  try {
    return await refreshInFlight
  } finally {
    refreshInFlight = null
  }
}

/** Issues an authenticated request, refreshing once on a 401. */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  try {
    return await rawRequest<T>(path, options)
  } catch (error) {
    const canRetry = error instanceof ApiError && error.status === 401 && options.auth !== false
    if (!canRetry) throw error

    const session = await refreshSession()
    if (!session) {
      onSessionExpired?.()
      throw error
    }
    return await rawRequest<T>(path, options)
  }
}

/**
 * Restores a session on app start. Returns null when not signed in.
 *
 * Goes through refreshSession's shared refreshInFlight rather than issuing
 * its own request, so that React StrictMode double-invoking this effect (or
 * any other incidental double-call) doesn't present the same single-use
 * refresh token twice — see the note on refreshInFlight above.
 */
export async function restoreSession(): Promise<Session | null> {
  if (!getStoredRefreshToken()) return null
  return refreshSession()
}

export const api = {
  authConfig: () => request<AuthConfig>('/auth/config', { auth: false }),

  signUp: (email: string, password: string, displayName: string) =>
    request<Session>('/auth/signup', {
      method: 'POST',
      body: { email, password, display_name: displayName },
      auth: false,
    }),

  logIn: (email: string, password: string) =>
    request<Session>('/auth/login', {
      method: 'POST',
      body: { email, password },
      auth: false,
    }),

  /** Trades the one-time code from the Google redirect for a real session. */
  exchangeGoogleCode: (code: string) =>
    request<Session>('/auth/google/exchange', {
      method: 'POST',
      body: { code },
      auth: false,
    }),

  logOut: () => {
    const stored = getStoredRefreshToken()
    return request<void>('/auth/logout', {
      method: 'POST',
      body: { refresh_token: stored ?? '' },
    })
  },

  me: () => request<User>('/auth/me'),

  setPassword: (currentPassword: string, newPassword: string) =>
    request<User>('/auth/password', {
      method: 'POST',
      body: { current_password: currentPassword, new_password: newPassword },
    }),

  /**
   * Changes the unit distances are shown and entered in. Returns the whole
   * user so the caller refreshes its copy rather than patching one field.
   * Nothing stored is rewritten — the database stays in yards.
   */
  setDistanceUnit: (unit: DistanceUnit) =>
    request<User>('/auth/preferences', { method: 'PUT', body: { distance_unit: unit } }),

  listCourses: (params: { q?: string; limit?: number; offset?: number } = {}) => {
    const query = new URLSearchParams()
    if (params.q) query.set('q', params.q)
    if (params.limit !== undefined) query.set('limit', String(params.limit))
    if (params.offset !== undefined) query.set('offset', String(params.offset))
    const suffix = query.toString()
    return request<CoursePage>(`/courses${suffix ? `?${suffix}` : ''}`)
  },

  getCourse: (id: string) => request<CourseDetail>(`/courses/${id}`),

  createCourse: (payload: {
    name: string
    address?: string | null
    phone?: string | null
    website?: string | null
    notes?: string | null
    facility_type?: string | null
    latitude?: number | null
    longitude?: number | null
    pinned?: boolean
    hole_count?: number
    tees?: TeePayload[]
  }) => request<CourseDetail>('/courses', { method: 'POST', body: payload }),

  updateCourse: (
    id: string,
    payload: {
      name: string
      address?: string | null
      phone?: string | null
      website?: string | null
      notes?: string | null
      facility_type?: string | null
      latitude?: number | null
      longitude?: number | null
      pinned?: boolean
    },
  ) => request<CourseDetail>(`/courses/${id}`, { method: 'PUT', body: payload }),

  deleteCourse: (id: string) => request<void>(`/courses/${id}`, { method: 'DELETE' }),

  /** Fetches a course in the transferable shape used for backup/import. */
  exportCourse: (id: string) => request<CourseExport>(`/courses/${id}/export`),

  /** Recreates a course from a file previously produced by exportCourse. */
  importCourse: (payload: CourseExport) =>
    request<CourseDetail>('/courses/import', { method: 'POST', body: payload }),

  addTee: (courseId: string, payload: TeePayload) =>
    request<Tee>(`/courses/${courseId}/tees`, { method: 'POST', body: payload }),

  updateTee: (teeId: string, payload: TeePayload) =>
    request<Tee>(`/tees/${teeId}`, { method: 'PUT', body: payload }),

  deleteTee: (teeId: string) => request<void>(`/tees/${teeId}`, { method: 'DELETE' }),

  addHole: (courseId: string, holeNumber: number, handicapIndex?: number | null) =>
    request<Hole>(`/courses/${courseId}/holes`, {
      method: 'POST',
      body: { hole_number: holeNumber, handicap_index: handicapIndex ?? null },
    }),

  updateHole: (holeId: string, handicapIndex: number | null) =>
    request<Hole>(`/holes/${holeId}`, {
      method: 'PUT',
      body: { handicap_index: handicapIndex },
    }),

  deleteHole: (holeId: string) => request<void>(`/holes/${holeId}`, { method: 'DELETE' }),

  /** Upserts one cell of the par/yardage grid. */
  setTeeDetail: (holeId: string, teeId: string, par: number, yardage: number) =>
    request<Hole>(`/holes/${holeId}/tee-details/${teeId}`, {
      method: 'PUT',
      body: { par, yardage },
    }),

  clearTeeDetail: (holeId: string, teeId: string) =>
    request<void>(`/holes/${holeId}/tee-details/${teeId}`, { method: 'DELETE' }),

  /** The golf bag: active, benched, and retired clubs plus the club-count rule. */
  getBag: () => request<Bag>('/clubs'),

  /** The club types and flexes the server accepts, for building the form's selects. */
  clubOptions: () => request<ClubOptions>('/clubs/options'),

  createClub: (payload: ClubPayload) => request<Club>('/clubs', { method: 'POST', body: payload }),

  updateClub: (clubId: string, payload: ClubPayload) =>
    request<Club>(`/clubs/${clubId}`, { method: 'PUT', body: payload }),

  /**
   * Moves a club between the bag, the bench, and retirement. Separate from
   * updateClub so that saving an edit form can never silently change where a
   * club sits.
   */
  setClubStatus: (clubId: string, status: ClubStatus) =>
    request<Club>(`/clubs/${clubId}/status`, { method: 'PUT', body: { status } }),

  /**
   * Permanently removes a club. Retiring is the right move for a club that has
   * been played, since rounds and shots reference club IDs.
   */
  deleteClub: (clubId: string) => request<void>(`/clubs/${clubId}`, { method: 'DELETE' }),

  /**
   * Begins linking Google to the signed-in account.
   *
   * This has to be an XHR rather than a plain navigation, because the endpoint
   * is authenticated and a browser navigation cannot carry the Bearer header.
   * The response sets the OAuth state cookies and returns the consent URL for
   * the caller to navigate to.
   */
  startGoogleLink: (returnTo: string) =>
    request<{ authorization_url: string }>(
      `/auth/link/google?mode=json&return_to=${encodeURIComponent(returnTo)}`,
      { method: 'POST' },
    ),

  // ---- Profile ----

  updateProfile: (payload: ProfilePayload) =>
    request<User>('/account/profile', { method: 'PUT', body: payload }),

  /**
   * Changes the login address. Returns a whole new Session, not a User: the
   * old access token carries the old address in its claims, and every other
   * device is signed out on purpose.
   */
  changeEmail: (email: string, currentPassword: string) =>
    request<Session>('/account/email', {
      method: 'PUT',
      body: { email, current_password: currentPassword },
    }),

  uploadAvatar: (file: File) => {
    const form = new FormData()
    form.append('avatar', file)
    return request<User>('/account/avatar', { method: 'POST', formData: form })
  },

  deleteAvatar: () => request<User>('/account/avatar', { method: 'DELETE' }),

  // ---- Data ----

  exportAccount: () => request<unknown>('/account/export'),

  exportAccountCsv: () => request<Blob>('/account/export?format=csv', { blob: true }),

  importAccount: (payload: unknown) =>
    request<ImportSummary>('/account/import', { method: 'POST', body: payload }),

  // ---- Course removal ----

  /** Asks the site administrator to remove a course. Nobody can remove one directly. */
  requestCourseRemoval: (courseId: string, reason: string) =>
    request<RemovalRequest>(`/courses/${courseId}/removal-request`, {
      method: 'POST',
      body: { reason },
    }),

  listRemovalRequests: () =>
    request<{ requests: RemovalRequest[] }>('/admin/removal-requests'),

  resolveRemovalRequest: (requestId: string, resolution: 'removed' | 'declined') =>
    request<void>(`/admin/removal-requests/${requestId}/resolve`, {
      method: 'POST',
      body: { resolution },
    }),

  // ---- API keys ----

  listApiKeys: () => request<{ keys: ApiKey[] }>('/account/keys'),

  /** The response carries the secret. It is not retrievable afterwards. */
  createApiKey: (name: string, expiresInDays: number) =>
    request<CreatedApiKey>('/account/keys', {
      method: 'POST',
      body: { name, expires_in_days: expiresInDays },
    }),

  revokeApiKey: (keyId: string) =>
    request<void>(`/account/keys/${keyId}`, { method: 'DELETE' }),
}

/**
 * URL that begins Google sign-in. Unauthenticated, so the browser can navigate
 * to it directly and let the server issue the redirect to Google.
 */
export function googleLoginUrl(returnTo?: string): string {
  const query = returnTo ? `?return_to=${encodeURIComponent(returnTo)}` : ''
  return `/api/auth/google/start${query}`
}
