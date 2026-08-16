import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useDistanceUnit } from '../lib/auth'
import { fromYards, unitSuffix, type DistanceUnit } from '../lib/units'
import { formatPhone, phoneHref } from '../lib/phone'
import { slugify } from '../lib/slug'
import type { CourseDetail, Tee, TeePayload } from '../types'
import { ScorecardGrid } from '../components/ScorecardGrid'
import { TeeDialog, emptyTeeForm, teeToForm } from '../components/TeeForm'
import {
  Alert,
  ChevronLeftIcon,
  ConfirmDialog,
  DownloadIcon,
  Field,
  PageSpinner,
  PencilIcon,
  cx,
  PlusIcon,
  Spinner,
  TrashIcon,
} from '../components/ui'

/**
 * Set at build time. Leave unset to hide the map rather than embed a broken
 * iframe — the same "hide the feature" fallback the Google sign-in button uses.
 */
const GOOGLE_MAPS_API_KEY = import.meta.env.VITE_GOOGLE_MAPS_API_KEY

function mapsEmbedUrl(address: string): string {
  return `https://www.google.com/maps/embed/v1/place?key=${GOOGLE_MAPS_API_KEY}&q=${encodeURIComponent(address)}&maptype=satellite`
}

function mapsSearchUrl(address: string): string {
  return `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(address)}`
}

/** Builds e.g. "M 72.4/135 · W 74.8/140", omitting a gender with no rating set. */
/**
 * A 9-hole course's one rating lives in the front9_* fields (see TeeForm),
 * since the main course_rating_men/women fields are validated on an 18-hole
 * scale. So which pair to read depends on the course's hole count.
 */
function formatRatings(tee: Tee, holeCount: number): string {
  const is9Holes = holeCount === 9
  const courseRatingMen = is9Holes ? tee.front9_course_rating_men : tee.course_rating_men
  const slopeRatingMen = is9Holes ? tee.front9_slope_rating_men : tee.slope_rating_men
  const courseRatingWomen = is9Holes ? tee.front9_course_rating_women : tee.course_rating_women
  const slopeRatingWomen = is9Holes ? tee.front9_slope_rating_women : tee.slope_rating_women

  const parts: string[] = []
  if (courseRatingMen !== null) {
    parts.push(`M ${courseRatingMen}${slopeRatingMen !== null ? `/${slopeRatingMen}` : ''}`)
  }
  if (courseRatingWomen !== null) {
    parts.push(`W ${courseRatingWomen}${slopeRatingWomen !== null ? `/${slopeRatingWomen}` : ''}`)
  }
  return parts.join(' · ')
}

/**
 * A tee's total in the display unit, summed hole by hole.
 *
 * Deliberately not `fromYards(tee.total_yardage)`. Converting the server's yard
 * total rounds once, while the scorecard column rounds each hole and adds those
 * up — on the same screen the two can differ by a metre or two. Summing the way
 * the grid does keeps the number beside the tee equal to the number at the end
 * of its row. In yards the two are identical anyway.
 */
function teeTotal(course: CourseDetail, teeId: string, unit: DistanceUnit): number {
  let total = 0
  for (const hole of course.holes) {
    const detail = hole.tee_details.find((d) => d.tee_id === teeId)
    if (detail) total += fromYards(detail.yardage, unit)
  }
  return total
}

export default function CourseDetailPage() {
  const { courseId } = useParams<{ courseId: string }>()
  const unit = useDistanceUnit()

  const [course, setCourse] = useState<CourseDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [editingDetails, setEditingDetails] = useState(false)
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [phone, setPhone] = useState('')
  const [website, setWebsite] = useState('')
  const [notes, setNotes] = useState('')
  const [facilityType, setFacilityType] = useState('')
  const [latitude, setLatitude] = useState('')
  const [longitude, setLongitude] = useState('')
  const [pinned, setPinned] = useState(false)
  const [savingDetails, setSavingDetails] = useState(false)
  const [detailErrors, setDetailErrors] = useState<Record<string, string>>({})

  const [teeDialog, setTeeDialog] = useState<{ mode: 'add' } | { mode: 'edit'; tee: Tee } | null>(
    null,
  )
  const [confirmDeleteTee, setConfirmDeleteTee] = useState<Tee | null>(null)
  const [requestingRemoval, setRequestingRemoval] = useState(false)
  const [removalReason, setRemovalReason] = useState('')
  const [removalError, setRemovalError] = useState<string | null>(null)
  const [sendingRemoval, setSendingRemoval] = useState(false)
  const [removalRequested, setRemovalRequested] = useState(false)
  const [exporting, setExporting] = useState(false)

  const load = useCallback(async () => {
    if (!courseId) return
    try {
      const detail = await api.getCourse(courseId)
      setCourse(detail)
      setName(detail.name)
      setAddress(detail.address ?? '')
      setPhone(detail.phone ?? '')
      setWebsite(detail.website ?? '')
      setNotes(detail.notes ?? '')
      setFacilityType(detail.facility_type ?? '')
      setLatitude(detail.latitude != null ? String(detail.latitude) : '')
      setLongitude(detail.longitude != null ? String(detail.longitude) : '')
      setPinned(detail.pinned)
      setError(null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not load this course.')
    } finally {
      setLoading(false)
    }
  }, [courseId])

  useEffect(() => {
    void load()
  }, [load])

  async function handleSaveDetails(event: React.FormEvent) {
    event.preventDefault()
    if (!courseId) return
    setSavingDetails(true)
    setDetailErrors({})
    try {
      const updated = await api.updateCourse(courseId, {
        name,
        address: address.trim() === '' ? null : address,
        phone: phone.trim() === '' ? null : formatPhone(phone),
        website: website.trim() === '' ? null : website,
        notes: notes.trim() === '' ? null : notes,
        facility_type: facilityType === '' ? null : facilityType,
        latitude: latitude.trim() === '' ? null : Number(latitude),
        longitude: longitude.trim() === '' ? null : Number(longitude),
        pinned,
      })
      setCourse(updated)
      setEditingDetails(false)
    } catch (err) {
      if (err instanceof ApiError && err.isValidation) setDetailErrors(err.fields)
      else setError(err instanceof ApiError ? err.message : 'Could not save the course.')
    } finally {
      setSavingDetails(false)
    }
  }

  async function handleTeeSubmit(payload: TeePayload) {
    if (!courseId || !teeDialog) return
    if (teeDialog.mode === 'add') await api.addTee(courseId, payload)
    else await api.updateTee(teeDialog.tee.id, payload)
    setTeeDialog(null)
    await load()
  }

  async function handleDeleteTee(tee: Tee) {
    await api.deleteTee(tee.id)
    await load()
  }

  /**
   * Downloads the course as pretty-printed JSON. This has to fetch and build
   * a blob URL rather than link straight to the API route: the export
   * endpoint is authenticated, and a plain browser navigation cannot carry
   * the Bearer header (see startGoogleLink in lib/api.ts for the same issue).
   */
  async function handleExport() {
    if (!courseId) return
    setExporting(true)
    try {
      const data = await api.exportCourse(courseId)
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      try {
        const a = document.createElement('a')
        a.href = url
        a.download = `${slugify(data.name)}.json`
        a.click()
      } finally {
        URL.revokeObjectURL(url)
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not export this course.')
    } finally {
      setExporting(false)
    }
  }

  async function handleRequestRemoval(event: React.FormEvent) {
    event.preventDefault()
    if (!courseId) return
    setSendingRemoval(true)
    setRemovalError(null)
    try {
      await api.requestCourseRemoval(courseId, removalReason)
      setRemovalRequested(true)
      setRequestingRemoval(false)
    } catch (err) {
      setRemovalError(
        err instanceof ApiError ? (err.fields.reason ?? err.message) : 'Could not send that request.',
      )
    } finally {
      setSendingRemoval(false)
    }
  }

  if (loading) return <PageSpinner label="Loading course" />

  if (!course) {
    return (
      <div className="space-y-4">
        <Alert>{error ?? 'That course could not be found.'}</Alert>
        <Link to="/courses" className="btn-secondary">
          <ChevronLeftIcon className="size-4" />
          Back to courses
        </Link>
      </div>
    )
  }


  return (
    <div className="space-y-6">
      <Link
        to="/courses"
        className="inline-flex items-center gap-1 text-sm text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
      >
        <ChevronLeftIcon className="size-4" />
        Courses
      </Link>

      {error && <Alert>{error}</Alert>}

      {/* Course name and address */}
      {editingDetails ? (
        <form onSubmit={handleSaveDetails} className="card space-y-4 p-5">
          <Field
            label="Course name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            error={detailErrors.name}
            required
            maxLength={120}
          />
          <Field
            label="Address"
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            error={detailErrors.address}
            maxLength={240}
            placeholder="Optional"
          />
          <Field
            label="Phone number"
            type="tel"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            error={detailErrors.phone}
            maxLength={30}
            placeholder="Optional"
          />
          <Field
            label="Website"
            type="url"
            value={website}
            onChange={(e) => setWebsite(e.target.value)}
            error={detailErrors.website}
            maxLength={2048}
            placeholder="Optional"
          />
          <div>
            <label htmlFor="edit-facility-type" className="label">
              Facility type
            </label>
            <select
              id="edit-facility-type"
              value={facilityType}
              onChange={(e) => setFacilityType(e.target.value)}
              className="input"
            >
              <option value="">—</option>
              <option value="public">Public</option>
              <option value="private">Private</option>
              <option value="military">Military</option>
              <option value="resort">Resort</option>
            </select>
            {detailErrors.facility_type && (
              <p className="field-error">{detailErrors.facility_type}</p>
            )}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field
              label="Latitude"
              type="number"
              value={latitude}
              onChange={(e) => setLatitude(e.target.value)}
              error={detailErrors.latitude}
              placeholder="Optional"
              step="any"
              min={-90}
              max={90}
            />
            <Field
              label="Longitude"
              type="number"
              value={longitude}
              onChange={(e) => setLongitude(e.target.value)}
              error={detailErrors.longitude}
              placeholder="Optional"
              step="any"
              min={-180}
              max={180}
            />
          </div>
          <div>
            <label className="label">Notes</label>
            <textarea
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              className="input"
              rows={3}
              maxLength={2000}
              placeholder="Optional"
            />
            {detailErrors.notes && <p className="field-error">{detailErrors.notes}</p>}
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={pinned}
              onChange={(e) => setPinned(e.target.checked)}
              className="rounded border-slate-300 text-brand-600 focus:ring-brand-500 dark:border-slate-600 dark:bg-slate-800"
            />
            Pin this course
          </label>
          <div className="flex gap-2">
            <button
              type="button"
              className="btn-secondary"
              onClick={() => {
                setEditingDetails(false)
                setName(course.name)
                setAddress(course.address ?? '')
                setPhone(course.phone ?? '')
                setWebsite(course.website ?? '')
                setNotes(course.notes ?? '')
                setFacilityType(course.facility_type ?? '')
                setLatitude(course.latitude != null ? String(course.latitude) : '')
                setLongitude(course.longitude != null ? String(course.longitude) : '')
                setPinned(course.pinned)
                setDetailErrors({})
              }}
            >
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={savingDetails}>
              {savingDetails ? <Spinner label="Saving" /> : 'Save'}
            </button>
          </div>
        </form>
      ) : (
        <div className="flex flex-wrap items-start gap-3">
          <div className="min-w-0">
            <h1 className="text-2xl font-bold tracking-tight break-words">{course.name}</h1>
            {course.address && (
              <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">
                <a
                  href={mapsSearchUrl(course.address)}
                  target="_blank"
                  rel="noreferrer"
                  className="hover:text-brand-700 hover:underline dark:hover:text-brand-300"
                >
                  {course.address}
                </a>
              </p>
            )}
            {course.phone && (
              <p className="mt-0.5 text-sm text-slate-600 dark:text-slate-400">
                <a
                  href={phoneHref(course.phone)}
                  className="hover:text-brand-700 hover:underline dark:hover:text-brand-300"
                >
                  {formatPhone(course.phone)}
                </a>
              </p>
            )}
            {course.website && (
              <p className="mt-0.5 text-sm text-slate-600 dark:text-slate-400">
                <a
                  href={course.website}
                  target="_blank"
                  rel="noreferrer"
                  className="hover:text-brand-700 hover:underline dark:hover:text-brand-300"
                >
                  {course.website.replace(/^https?:\/\//, '')}
                </a>
              </p>
            )}
            {(course.facility_type || course.hole_count > 0) && (
              <p className="mt-1.5 flex flex-wrap gap-1.5">
                {course.facility_type && (
                  <span className="inline-block rounded-full bg-brand-100 px-2.5 py-0.5 text-xs font-medium text-brand-800 capitalize dark:bg-brand-900 dark:text-brand-100">
                    {course.facility_type}
                  </span>
                )}
                {course.hole_count > 0 && (
                  <span className="inline-block rounded-full bg-brand-100 px-2.5 py-0.5 text-xs font-medium text-brand-800 dark:bg-brand-900 dark:text-brand-100">
                    {course.hole_count} holes
                  </span>
                )}
              </p>
            )}
            {course.notes && (
              <p className="mt-1.5 text-sm whitespace-pre-line text-slate-600 dark:text-slate-400">
                {course.notes}
              </p>
            )}
          </div>
          <div className="ml-auto flex gap-2">
            <button
              type="button"
              className="btn-secondary"
              disabled={exporting}
              onClick={() => void handleExport()}
            >
              {exporting ? (
                <Spinner label="Exporting" />
              ) : (
                <>
                  <DownloadIcon className="size-4" />
                  Export
                </>
              )}
            </button>
            <button
              type="button"
              className="btn-secondary"
              onClick={() => setEditingDetails(true)}
            >
              <PencilIcon className="size-4" />
              Edit details
            </button>
          </div>
        </div>
      )}

      {course.address && GOOGLE_MAPS_API_KEY && (
        <iframe
          title={`Map of ${course.name}`}
          src={mapsEmbedUrl(course.address)}
          className="block h-64 w-full rounded-xl border-0"
          loading="lazy"
          referrerPolicy="no-referrer-when-downgrade"
        />
      )}

      {/* Tees */}
      <section className="space-y-3">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold">Tees</h2>
          <button
            type="button"
            className="btn-secondary ml-auto !min-h-0 !px-3 !py-1.5"
            onClick={() => setTeeDialog({ mode: 'add' })}
          >
            <PlusIcon className="size-4" />
            Add tee
          </button>
        </div>

        {course.tees.length === 0 ? (
          <div className="card p-5 text-sm text-slate-600 dark:text-slate-400">
            No tees added yet.
          </div>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {[...course.tees].sort((a, b) => b.total_yardage - a.total_yardage).map((tee) => {
              const ratings = formatRatings(tee, course.holes.length)
              return (
                <li
                  key={tee.id}
                  className={'card flex items-center gap-3 px-3 py-2 pr-2'}
                >
                  <span
                    aria-hidden="true"
                    className="size-4 shrink-0 rounded-full ring-1 ring-black/15 dark:ring-white/25"
                    style={{ backgroundColor: tee.color }}
                  />
                  <div className="min-w-0">
                    <p className="text-sm font-medium">{tee.name}</p>
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                      {tee.total_yardage > 0
                        ? `${teeTotal(course, tee.id, unit)} ${unitSuffix(unit)}`
                        : 'No yardages yet'}
                      {ratings !== '' && ` · ${ratings}`}
                    </p>
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      className="btn-ghost !min-h-0 !px-2 !py-1 text-xs"
                      onClick={() => setTeeDialog({ mode: 'edit', tee })}
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      className="btn-ghost !min-h-0 !px-2 !py-1 text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-950"
                      onClick={() => setConfirmDeleteTee(tee)}
                      aria-label={`Delete the ${tee.name} tee`}
                      title={`Delete the ${tee.name} tee`}
                    >
                      <TrashIcon className="size-4" />
                    </button>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </section>

      {/* Par / yardage grid */}
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Scorecard</h2>
        <ScorecardGrid course={course} editable onCourseChanged={() => void load()} />
      </section>

      <section className="border-t border-slate-200 pt-6 dark:border-slate-800">
        {/* Nobody owns a course, so nobody removes one on their own. Removing
            it cascades away every tee, hole, par, and yardage with no undo, so
            the request goes to the site administrator instead. */}
        {removalRequested ? (
          <Alert tone="success">
            Your removal request has been sent to the site administrator.
          </Alert>
        ) : requestingRemoval ? (
          <form onSubmit={handleRequestRemoval} className="card space-y-3 p-5">
            <div>
              <label htmlFor="removal-reason" className="label">
                Why should “{course.name}” be removed?
              </label>
              <textarea
                id="removal-reason"
                value={removalReason}
                onChange={(e) => setRemovalReason(e.target.value)}
                className={cx('input', removalError && 'input-error')}
                rows={3}
                maxLength={500}
                placeholder="Duplicate of another entry, permanently closed, entered by mistake…"
                required
              />
              {removalError && <p className="field-error">{removalError}</p>}
            </div>
            <div className="flex gap-2">
              <button
                type="button"
                className="btn-secondary"
                onClick={() => setRequestingRemoval(false)}
              >
                Cancel
              </button>
              <button type="submit" className="btn-primary" disabled={sendingRemoval}>
                {sendingRemoval ? <Spinner label="Sending" /> : 'Send request'}
              </button>
            </div>
          </form>
        ) : (
          <button
            type="button"
            className="btn-ghost text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
            onClick={() => setRequestingRemoval(true)}
          >
            <TrashIcon className="size-4" />
            Request removal
          </button>
        )}
      </section>

      {teeDialog && (
        <TeeDialog
          title={teeDialog.mode === 'add' ? 'Add tee' : `Edit ${teeDialog.tee.name}`}
          initial={teeDialog.mode === 'add' ? emptyTeeForm() : teeToForm(teeDialog.tee)}
          submitLabel={teeDialog.mode === 'add' ? 'Add tee' : 'Save tee'}
          // Derived from the actual holes on the course rather than the
          // stored hole_count column, which can be stale (e.g. 0) on courses
          // created before that field existed.
          holeCount={course.holes.length}
          onCancel={() => setTeeDialog(null)}
          onSubmit={handleTeeSubmit}
        />
      )}

      {confirmDeleteTee && (
        <ConfirmDialog
          title={`Delete the ${confirmDeleteTee.name} tee?`}
          message="This also removes every par and yardage recorded for it."
          confirmLabel="Delete tee"
          onCancel={() => setConfirmDeleteTee(null)}
          onConfirm={async () => {
            await handleDeleteTee(confirmDeleteTee)
            setConfirmDeleteTee(null)
          }}
        />
      )}
    </div>
  )
}
