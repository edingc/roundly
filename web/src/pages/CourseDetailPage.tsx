import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import type { CourseDetail, Tee, TeePayload } from '../types'
import { ScorecardGrid } from '../components/ScorecardGrid'
import { TeeDialog, emptyTeeForm, teeToForm } from '../components/TeeForm'
import {
  Alert,
  ChevronLeftIcon,
  Field,
  PageSpinner,
  PlusIcon,
  Spinner,
  TrashIcon,
  cx,
} from '../components/ui'

export default function CourseDetailPage() {
  const { courseId } = useParams<{ courseId: string }>()
  const navigate = useNavigate()

  const [course, setCourse] = useState<CourseDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [editingDetails, setEditingDetails] = useState(false)
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [savingDetails, setSavingDetails] = useState(false)
  const [detailErrors, setDetailErrors] = useState<Record<string, string>>({})

  const [teeDialog, setTeeDialog] = useState<{ mode: 'add' } | { mode: 'edit'; tee: Tee } | null>(
    null,
  )
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    if (!courseId) return
    try {
      const detail = await api.getCourse(courseId)
      setCourse(detail)
      setName(detail.name)
      setAddress(detail.address ?? '')
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
    try {
      await api.deleteTee(tee.id)
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not delete the tee.')
    }
  }

  async function handleDeleteCourse() {
    if (!courseId) return
    setDeleting(true)
    try {
      await api.deleteCourse(courseId)
      navigate('/courses', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not delete the course.')
      setDeleting(false)
      setConfirmDelete(false)
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

  const editable = course.can_edit

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
          <div className="flex gap-2">
            <button
              type="button"
              className="btn-secondary"
              onClick={() => {
                setEditingDetails(false)
                setName(course.name)
                setAddress(course.address ?? '')
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
              <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">{course.address}</p>
            )}
            {!editable && (
              <p className="mt-2 inline-block rounded-full bg-slate-100 px-2.5 py-1 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                Added by someone else — read only
              </p>
            )}
          </div>
          {editable && (
            <button
              type="button"
              className="btn-secondary ml-auto"
              onClick={() => setEditingDetails(true)}
            >
              Edit details
            </button>
          )}
        </div>
      )}

      {/* Tees */}
      <section className="space-y-3">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold">Tees</h2>
          {editable && (
            <button
              type="button"
              className="btn-secondary ml-auto !min-h-0 !px-3 !py-1.5"
              onClick={() => setTeeDialog({ mode: 'add' })}
            >
              <PlusIcon className="size-4" />
              Add tee
            </button>
          )}
        </div>

        {course.tees.length === 0 ? (
          <div className="card p-5 text-sm text-slate-600 dark:text-slate-400">
            No tees yet. Add one to start filling in par and yardage.
          </div>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {course.tees.map((tee) => (
              <li
                key={tee.id}
                className={cx(
                  'card flex items-center gap-3 px-3 py-2',
                  editable && 'pr-2',
                )}
              >
                <span
                  aria-hidden="true"
                  className="size-4 shrink-0 rounded-full ring-1 ring-black/15 dark:ring-white/25"
                  style={{ backgroundColor: tee.color }}
                />
                <div className="min-w-0">
                  <p className="text-sm font-medium">{tee.name}</p>
                  <p className="text-xs text-slate-500 dark:text-slate-400">
                    {tee.total_yardage > 0 ? `${tee.total_yardage} yds` : 'No yardages yet'}
                    {tee.course_rating !== null && ` · ${tee.course_rating}`}
                    {tee.slope_rating !== null && ` / ${tee.slope_rating}`}
                  </p>
                </div>
                {editable && (
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
                      onClick={() => void handleDeleteTee(tee)}
                      aria-label={`Delete the ${tee.name} tee`}
                      title={`Delete the ${tee.name} tee`}
                    >
                      <TrashIcon className="size-4" />
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      {/* Par / yardage grid */}
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Scorecard</h2>
        <ScorecardGrid course={course} editable={editable} onCourseChanged={() => void load()} />
      </section>

      {editable && (
        <section className="border-t border-slate-200 pt-6 dark:border-slate-800">
          {confirmDelete ? (
            <div className="card space-y-3 border-red-300 p-5 dark:border-red-900">
              <p className="text-sm font-medium">
                Delete “{course.name}”? This also removes its tees, holes, and every par and
                yardage you have entered.
              </p>
              <div className="flex gap-2">
                <button
                  type="button"
                  className="btn-secondary"
                  onClick={() => setConfirmDelete(false)}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn-danger"
                  disabled={deleting}
                  onClick={() => void handleDeleteCourse()}
                >
                  {deleting ? <Spinner label="Deleting" /> : 'Delete course'}
                </button>
              </div>
            </div>
          ) : (
            <button
              type="button"
              className="btn-ghost text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950"
              onClick={() => setConfirmDelete(true)}
            >
              <TrashIcon className="size-4" />
              Delete course
            </button>
          )}
        </section>
      )}

      {teeDialog && (
        <TeeDialog
          title={teeDialog.mode === 'add' ? 'Add tee' : `Edit ${teeDialog.tee.name}`}
          initial={teeDialog.mode === 'add' ? emptyTeeForm() : teeToForm(teeDialog.tee)}
          submitLabel={teeDialog.mode === 'add' ? 'Add tee' : 'Save tee'}
          onCancel={() => setTeeDialog(null)}
          onSubmit={handleTeeSubmit}
        />
      )}
    </div>
  )
}
