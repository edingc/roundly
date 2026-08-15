import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import type { CourseExport, CoursePage } from '../types'
import {
  Alert,
  EmptyState,
  PageSpinner,
  PinIcon,
  PlusIcon,
  SearchIcon,
  Spinner,
  UploadIcon,
} from '../components/ui'

const PAGE_SIZE = 25

export default function CourseListPage() {
  const navigate = useNavigate()
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

  const items = [...(page?.items ?? [])].sort((a, b) => {
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
    return 0 // preserve server name ordering within each group
  })
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
          placeholder="Search by name or address"
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
            description={`Nothing matched “${search}”. Try a different name or address.`}
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
        <ul className="grid gap-3 sm:grid-cols-2">
          {items.map((course) => (
            <li key={course.id}>
              <Link
                to={`/courses/${course.id}`}
                className="card block h-full p-4 transition-colors hover:border-brand-400 hover:bg-brand-50/50 dark:hover:border-brand-400 dark:hover:bg-brand-950/30"
              >
                <div className="flex items-start gap-2">
                  <h2 className="font-semibold">{course.name}</h2>
                  <div className="ml-auto flex shrink-0 items-center gap-1.5">
                    {course.pinned && (
                      <span className="flex items-center gap-1 rounded-full bg-brand-100 px-2 py-0.5 text-xs font-medium text-brand-800 dark:bg-brand-900 dark:text-brand-100">
                        <PinIcon className="size-3" />
                        Pinned
                      </span>
                    )}
                    {!course.can_edit && (
                      <span className="rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                        Read only
                      </span>
                    )}
                  </div>
                </div>
                {course.address && (
                  <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">{course.address}</p>
                )}
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
