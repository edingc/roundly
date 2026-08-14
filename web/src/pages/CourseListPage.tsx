import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import type { CoursePage } from '../types'
import { Alert, EmptyState, PageSpinner, PlusIcon, SearchIcon } from '../components/ui'

const PAGE_SIZE = 25

export default function CourseListPage() {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState<CoursePage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)

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
        <Link to="/courses/new" className="btn-primary ml-auto">
          <PlusIcon className="size-4" />
          Add course
        </Link>
      </div>

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
                className="card block h-full p-4 transition-colors hover:border-brand-400 hover:bg-brand-50/50 dark:hover:border-brand-600 dark:hover:bg-brand-950/30"
              >
                <div className="flex items-start gap-2">
                  <h2 className="font-semibold">{course.name}</h2>
                  {!course.can_edit && (
                    <span className="ml-auto shrink-0 rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                      Read only
                    </span>
                  )}
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
