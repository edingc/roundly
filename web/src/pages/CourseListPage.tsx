import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import type { CourseExport, CoursePage } from '../types'
import { formatCourseLocation } from '../lib/location'
import {
  Alert,
  EmptyState,
  HomeIcon,
  PageSpinner,
  PinIcon,
  PlusIcon,
  SearchIcon,
  Spinner,
  UploadIcon,
  cx,
} from '../components/ui'

const PAGE_SIZE = 25

export default function CourseListPage() {
  const navigate = useNavigate()
  const { user } = useAuth()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [search, setSearch] = useState('')
  const [page, setPage] = useState<CoursePage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)

  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState<string | null>(null)

  // Debounce so typing in the search box does not fire a request per keystroke.
  useEffect(() => {
    const controller = new AbortController()
    const timer = setTimeout(
      () => {
        setLoading(true)
        api
          .listCourses({ q: search.trim() || undefined, limit: PAGE_SIZE, offset })
          .then((result) => {
            setPage(result)
            setError(null)
          })
          .catch((err) => {
            if (controller.signal.aborted) return
            setError(err instanceof ApiError ? err.message : 'Could not load courses.')
          })
          .finally(() => {
            if (!controller.signal.aborted) setLoading(false)
          })
      },
      search ? 250 : 0,
    )

    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [search, offset])

  // A new search term invalidates the current page position.
  useEffect(() => {
    setOffset(0)
  }, [search])

  /** Reads a file exported by another course, then recreates it here. */
  async function handleImportFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = '' // lets the same file be picked again after an error
    if (!file) return

    setImporting(true)
    setImportError(null)
    try {
      let payload: unknown
      try {
        payload = JSON.parse(await file.text())
      } catch {
        throw new Error('That file is not valid JSON.')
      }
      const detail = await api.importCourse(payload as CourseExport)
      navigate(`/courses/${detail.id}`)
    } catch (err) {
      setImportError(
        err instanceof ApiError || err instanceof Error ? err.message : 'Could not import that file.',
      )
      setImporting(false)
    }
  }

  // Ordering — home course, then pinned, then by name — is the query's job.
  // Re-sorting here would only reach the twenty-five rows already fetched, so
  // a home course on page three would still be on page three.
  const items = page?.items ?? []
  const total = page?.total ?? 0
  const showingEmpty = !loading && items.length === 0

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Courses</h1>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {total === 0 ? 'No courses yet' : `${total} course${total === 1 ? '' : 's'}`}
          </p>
        </div>
        <div className="ml-auto flex gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={(e) => void handleImportFile(e)}
          />
          <button
            type="button"
            className="btn-secondary"
            disabled={importing}
            onClick={() => fileInputRef.current?.click()}
          >
            {importing ? (
              <Spinner label="Importing" />
            ) : (
              <>
                <UploadIcon className="size-4" />
                Import
              </>
            )}
          </button>
          <Link to="/courses/new" className="btn-primary">
            <PlusIcon className="size-4" />
            Add course
          </Link>
        </div>
      </div>

      {importError && <Alert>{importError}</Alert>}

      <div className="relative">
        <SearchIcon className="pointer-events-none absolute top-1/2 left-3 size-5 -translate-y-1/2 text-slate-400" />
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search by name or place"
          aria-label="Search courses"
          className="input pl-10"
        />
      </div>

      {error && <Alert>{error}</Alert>}

      {loading && !page ? (
        <PageSpinner label="Loading courses" />
      ) : showingEmpty ? (
        search ? (
          <EmptyState
            title="No matches"
            description={`Nothing matched “${search}”. Try a different name, town, or state.`}
          />
        ) : (
          <EmptyState
            title="Add your first course"
            description="Set up a course with its tees and the par and yardage for each hole, then you can score rounds on it."
            action={
              <Link to="/courses/new" className="btn-primary mt-2">
                <PlusIcon className="size-4" />
                Add course
              </Link>
            }
          />
        )
      ) : (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {items.map((course) => (
            <li key={course.id}>
              <Link
                to={`/courses/${course.id}`}
                className="card flex h-full flex-col p-4 transition-colors hover:border-accent-400 hover:bg-accent-50/50 dark:hover:border-accent-400 dark:hover:bg-accent-950/30"
              >
                <div className="flex items-start gap-2">
                  <h2 className="font-semibold">{course.name}</h2>
                  <div className="ml-auto flex shrink-0 items-center gap-1.5">
                    {/* Home is where you play, so it wears the accent. Pinned is
                        a tag you applied rather than a state of the course, and
                        it wore green until green was needed for scoring - the
                        pin icon carries it. */}
                    {course.id === user?.home_course_id && (
                      <span className="flex items-center gap-1 rounded-md bg-accent-100 px-2 py-0.5 text-xs font-medium text-accent-800 dark:bg-accent-900 dark:text-accent-100">
                        <HomeIcon className="size-3" />
                        Home
                      </span>
                    )}
                    {course.pinned && (
                      <span className="flex items-center gap-1 rounded-md bg-slate-200 px-2 py-0.5 text-xs font-medium text-slate-700 dark:bg-slate-700 dark:text-slate-200">
                        <PinIcon className="size-3" />
                        Pinned
                      </span>
                    )}
                  </div>
                </div>
                {/* The town, not the street: on a card the question is which
                    course this is, and the street answers that no better than
                    the town while costing a line of width.

                    Always rendered, even for a course with no location, so
                    every card reserves the same line — otherwise cards without
                    one end up visibly shorter than cards with one. */}
                <p
                  className={cx(
                    'mt-1 text-sm text-slate-600 dark:text-slate-400',
                    !formatCourseLocation(course) && 'invisible',
                  )}
                >
                  {formatCourseLocation(course) || '—'}
                </p>
                <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">
                  {course.hole_count} hole{course.hole_count === 1 ? '' : 's'} ·{' '}
                  {course.tee_count} tee{course.tee_count === 1 ? '' : 's'}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}

      {total > PAGE_SIZE && (
        <div className="flex items-center justify-between gap-3">
          <button
            type="button"
            className="btn-secondary"
            disabled={offset === 0}
            onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
          >
            Previous
          </button>
          <span className="text-sm text-slate-600 dark:text-slate-400">
            {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
          </span>
          <button
            type="button"
            className="btn-secondary"
            disabled={offset + PAGE_SIZE >= total}
            onClick={() => setOffset(offset + PAGE_SIZE)}
          >
            Next
          </button>
        </div>
      )}
    </div>
  )
}
