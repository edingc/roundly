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
  Gender,
  CourseDetail,
  CourseExport,
  CoursePage,
  Hole,
  ImportSummary,
  ProfilePayload,
  RemovalRequest,
  Overview,
  Round,
  RoundHolePayload,
  RoundPage,
  RoundStatus,
  Session,
  StartRoundPayload,
  Tee,
  TeePayload,
  TrustedDevice,
  TwoFactorChallenge,
  TwoFactorSetup,
  User,
} from '../types'

const REFRESH_TOKEN_KEY = 'roundly.refresh_token'

/**
 * The trusted-device token, kept per browser.
 *
 * localStorage rather than memory because the entire point is to outlive the
 * session: it is what stops this browser being asked for a code again next
 * week. It is not an access credential — it never opens the account on its own,
 * and a sign-in still needs the password — which is what makes storing it a
 * different proposition from storing the access token.
 */
const DEVICE_TOKEN_KEY = 'roundly.device_token'

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

  /**
   * True when the failure was a per-field validation problem.
   *
   * Keyed on the code alone. It used to also treat any error carrying `fields`
   * as validation, which was fine until an unrelated error put something there
   * — then its message vanished and the UI attached an error to a field that
   * did not exist. The server now reserves `fields` for validation, and this
   * no longer guesses.
   */
  get isValidation(): boolean {
    return this.code === 'validation_failed'
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
  // Only ever written, never cleared from here: the field is absent on every
  // refresh, and treating absent as "forget this device" would un-trust the
  // browser fifteen minutes after it was trusted.
  if (session?.device_token) setStoredDeviceToken(session.device_token)
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

export function getStoredDeviceToken(): string | null {
  try {
    return localStorage.getItem(DEVICE_TOKEN_KEY)
  } catch {
    return null
  }
}

/**
 * Records a device token, or clears it.
 *
 * Cleared when a device is forgotten from the profile screen, so this browser
 * stops presenting a token the server has already dropped.
 */
export function setStoredDeviceToken(token: string | null): void {
  try {
    if (token) localStorage.setItem(DEVICE_TOKEN_KEY, token)
    else localStorage.removeItem(DEVICE_TOKEN_KEY)
  } catch {
    // Private browsing can block storage; the user is simply asked for a code
    // every time, which is the safe direction to fail in.
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
  /** Send the stored trusted-device token, for the endpoint that lists them. */
  deviceHeader?: boolean
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
  // Only the device list uses this, and only to mark which row is this browser.
  if (options.deviceHeader) {
    const deviceToken = getStoredDeviceToken()
    if (deviceToken) headers['X-Roundly-Device'] = deviceToken
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

  /**
   * Signs in with a password.
   *
   * Returns either a Session or a TwoFactorChallenge — the password was correct
   * in both cases. Callers discriminate on `two_factor_required`; see
   * isTwoFactorChallenge.
   */
  logIn: (email: string, password: string) =>
    request<Session | TwoFactorChallenge>('/auth/login', {
      method: 'POST',
      body: { email, password, device_token: getStoredDeviceToken() ?? '' },
      auth: false,
    }),

  /** Finishes a login that stopped for a mailed code. */
  verifyTwoFactor: (challengeId: string, code: string, rememberDevice: boolean) =>
    request<Session>('/auth/two-factor/verify', {
      method: 'POST',
      body: { challenge_id: challengeId, code, remember_device: rememberDevice },
      auth: false,
    }),

  /**
   * Turns email sign-in codes on or off. Requires the current password either
   * way. Enabling also mints the recovery sheet, which comes back in this one
   * response and nowhere else.
   */
  setTwoFactor: (enabled: boolean, currentPassword: string) =>
    request<TwoFactorSetup>('/auth/two-factor', {
      method: 'PUT',
      body: { enabled, current_password: currentPassword },
    }),

  /** Replaces the recovery sheet. The old codes stop working immediately. */
  regenerateRecoveryCodes: (currentPassword: string) =>
    request<{ recovery_codes: string[] }>('/auth/two-factor/recovery-codes', {
      method: 'POST',
      body: { current_password: currentPassword },
    }),

  /**
   * Finishes a login with a recovery code, for somebody who can no longer read
   * the address the sign-in code was sent to.
   */
  verifyRecoveryCode: (challengeId: string, recoveryCode: string) =>
    request<Session>('/auth/two-factor/recovery', {
      method: 'POST',
      body: { challenge_id: challengeId, recovery_code: recoveryCode },
      auth: false,
    }),

  listDevices: () => request<{ items: TrustedDevice[] }>('/auth/devices', { deviceHeader: true }),

  forgetDevice: (deviceId: string) =>
    request<void>(`/auth/devices/${deviceId}`, { method: 'DELETE' }),

  /**
   * Redeems the token from a confirmation link. Unauthenticated: the link is
   * opened by whichever browser the mail client hands it to.
   */
  verifyEmail: (token: string) =>
    request<User>('/auth/verify-email', { method: 'POST', body: { token }, auth: false }),

  resendVerification: () =>
    request<void>('/auth/verify-email/resend', { method: 'POST', body: {} }),

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

  /**
   * Records which published set of course ratings a player's rounds use.
   *
   * An empty string clears it back to unset, which is a real answer rather than
   * a missing one: unset selects the men's ratings. Sent on its own, because
   * omitted preferences are left alone.
   */
  setGender: (gender: Gender | '') =>
    request<User>('/auth/preferences', { method: 'PUT', body: { gender } }),

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
    street?: string | null
    city?: string | null
    region?: string | null
    postal_code?: string | null
    country?: string | null
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
      street?: string | null
      city?: string | null
      region?: string | null
      postal_code?: string | null
      country?: string | null
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

  /** Erases the account. Irreversible; there is no undo endpoint. */
  deleteAccount: (currentPassword: string) =>
    request<void>('/account', { method: 'DELETE', body: { current_password: currentPassword } }),

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

  // ---- Overview ----

  /**
   * The dashboard. `rounds` is a window from the fixed set the server offers;
   * 0 means every round.
   */
  overview: (rounds?: number) =>
    request<Overview>(`/stats/overview${rounds === undefined ? '' : `?rounds=${rounds}`}`),

  // ---- Rounds ----

  /**
   * Starts a round, or records a manual one. Idempotent on `id`: the offline
   * queue retries, and a retry must not produce a second copy of the same
   * afternoon.
   */
  startRound: (payload: StartRoundPayload) =>
    request<Round>('/rounds', { method: 'POST', body: payload }),

  listRounds: (params: { limit?: number; offset?: number } = {}) => {
    const query = new URLSearchParams()
    if (params.limit !== undefined) query.set('limit', String(params.limit))
    if (params.offset !== undefined) query.set('offset', String(params.offset))
    const suffix = query.toString()
    return request<RoundPage>(`/rounds${suffix ? `?${suffix}` : ''}`)
  },

  /** Rounds in one state. Several may be in progress at once, so this is a list. */
  listRoundsByStatus: (status: RoundStatus) =>
    request<RoundPage>(`/rounds?status=${status}`),

  getRound: (roundID: string) => request<Round>(`/rounds/${roundID}`),

  updateRound: (roundID: string, payload: { played_on: string; notes: string | null }) =>
    request<Round>(`/rounds/${roundID}`, { method: 'PUT', body: payload }),

  /** One hole. The live path, and safe to replay. */
  saveRoundHole: (roundID: string, hole: RoundHolePayload) =>
    request<Round>(`/rounds/${roundID}/holes/${hole.hole_number}`, {
      method: 'PUT',
      body: hole,
    }),

  /** Every hole at once. The manual path: one save for the whole grid. */
  saveRoundHoles: (roundID: string, holes: RoundHolePayload[]) =>
    request<Round>(`/rounds/${roundID}/holes`, { method: 'PUT', body: { holes } }),

  completeRound: (roundID: string) =>
    request<Round>(`/rounds/${roundID}/complete`, { method: 'POST', body: {} }),

  abandonRound: (roundID: string) =>
    request<Round>(`/rounds/${roundID}/abandon`, { method: 'POST', body: {} }),

  /** Undoes an accidental abandon, and reopens a finished round for editing. */
  reopenRound: (roundID: string) =>
    request<Round>(`/rounds/${roundID}/reopen`, { method: 'POST', body: {} }),

  deleteRound: (roundID: string) => request<void>(`/rounds/${roundID}`, { method: 'DELETE' }),

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
 * Tells the two shapes of a login response apart.
 *
 * A type guard rather than a status check, because both outcomes are a 200: the
 * password was right either way, and a pending second factor is not an error.
 */
export function isTwoFactorChallenge(
  result: Session | TwoFactorChallenge,
): result is TwoFactorChallenge {
  return 'two_factor_required' in result && result.two_factor_required
}

/**
 * URL that begins Google sign-in. Unauthenticated, so the browser can navigate
 * to it directly and let the server issue the redirect to Google.
 */
export function googleLoginUrl(returnTo?: string): string {
  const query = returnTo ? `?return_to=${encodeURIComponent(returnTo)}` : ''
  return `/api/auth/google/start${query}`
}
