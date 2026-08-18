import { useEffect, useId, useRef, useState } from 'react'
import { api } from '../lib/api'
import { formatCourseLocation } from '../lib/location'
import { SearchIcon, Spinner, cx } from './ui'

/**
 * A course as this field reports it: enough to render the choice back without
 * refetching, and no more.
 *
 * `location` is a formatted "City, State, Country" rather than the parts,
 * because that is the only thing anybody here does with them — and the profile
 * seeds this field from `user.home_course_location`, which the server has
 * already formatted the same way.
 */
export interface SelectedCourse {
  id: string
  name: string
  location: string
}

const RESULT_LIMIT = 8

/**
 * Type-to-search picker for one course out of the directory.
 *
 * It replaced a `<select>` of every course. A dropdown is fine at a dozen
 * courses and unusable at a thousand, and it could only ever show a name —
 * which is the one thing that does not distinguish two clubs both called Pine
 * Valley. Searching hits the directory's own endpoint, so a term matches a
 * name, a street, a town, a state, or a postal code, and every result carries
 * the town it is in.
 */
export function CourseSearchField({
  value,
  onChange,
  error,
  label,
  hint,
  optional,
}: {
  value: SelectedCourse | null
  onChange: (course: SelectedCourse | null) => void
  error?: string
  label: string
  hint?: string
  /**
   * Says so in the placeholder rather than in the hint, matching Field's
   * `optional`. This box already has a placeholder worth keeping, so the word
   * joins it instead of replacing it — and it is dropped once a course is
   * picked, when the question is no longer whether to answer but whether to
   * change the answer.
   */
  optional?: boolean
}) {
  const inputId = useId()
  const listboxId = `${inputId}-listbox`
  const errorId = `${inputId}-error`
  const hintId = `${inputId}-hint`

  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SelectedCourse[]>([])
  const [searching, setSearching] = useState(false)
  const [open, setOpen] = useState(false)
  // -1 means "nothing highlighted": Enter should do nothing rather than pick
  // whatever happens to be first.
  const [activeIndex, setActiveIndex] = useState(-1)
  const [searchFailed, setSearchFailed] = useState(false)

  const inputRef = useRef<HTMLInputElement>(null)

  // Debounced so typing does not fire a request per keystroke, and aborted on
  // the next keystroke so a slow early response cannot overwrite a later one.
  useEffect(() => {
    const term = query.trim()
    if (term === '') {
      setResults([])
      setSearching(false)
      setSearchFailed(false)
      return
    }

    let cancelled = false
    setSearching(true)
    const timer = setTimeout(() => {
      api
        .listCourses({ q: term, limit: RESULT_LIMIT })
        .then((page) => {
          if (cancelled) return
          setResults(
            page.items.map((course) => ({
              id: course.id,
              name: course.name,
              location: formatCourseLocation(course),
            })),
          )
          setSearchFailed(false)
        })
        .catch(() => {
          if (cancelled) return
          setResults([])
          setSearchFailed(true)
        })
        .finally(() => {
          if (!cancelled) setSearching(false)
        })
    }, 250)

    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [query])

  // A fresh result set invalidates whichever row was highlighted.
  useEffect(() => {
    setActiveIndex(-1)
  }, [results])

  function select(course: SelectedCourse) {
    onChange(course)
    setQuery('')
    setResults([])
    setOpen(false)
  }

  function clear() {
    onChange(null)
    setQuery('')
    setResults([])
    // Focus goes back to the input: clearing is almost always the first half of
    // changing the answer, not the end of the interaction.
    requestAnimationFrame(() => inputRef.current?.focus())
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      if (results.length === 0) return
      event.preventDefault()
      setOpen(true)
      setActiveIndex((current) => {
        const step = event.key === 'ArrowDown' ? 1 : -1
        const next = current + step
        // Wraps at both ends, so holding one arrow key cycles the list.
        if (next < 0) return results.length - 1
        if (next >= results.length) return 0
        return next
      })
      return
    }
    if (event.key === 'Enter' && activeIndex >= 0 && results[activeIndex]) {
      // Only swallow Enter when it is picking a result; otherwise it should
      // still submit the surrounding form.
      event.preventDefault()
      select(results[activeIndex])
      return
    }
    if (event.key === 'Escape' && open) {
      event.preventDefault()
      setOpen(false)
    }
  }

  const term = query.trim()
  const showNoMatches = term !== '' && !searching && !searchFailed && results.length === 0
  // The panel opens only when it has something in it. Without this it flashes
  // as an empty bordered box for the length of the debounce on every keystroke.
  const showList = open && term !== '' && (results.length > 0 || showNoMatches || searchFailed)

  return (
    <div>
      <label htmlFor={inputId} className="label">
        {label}
      </label>

      {value && (
        <div className="mb-2 flex items-center gap-3 rounded-lg border border-accent-200 bg-accent-50 px-3 py-2 dark:border-accent-900 dark:bg-accent-950/50">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{value.name}</p>
            {value.location && (
              <p className="truncate text-xs text-slate-600 dark:text-slate-400">
                {value.location}
              </p>
            )}
          </div>
          <button
            type="button"
            className="btn-ghost ml-auto !min-h-0 shrink-0 !px-2 !py-1 text-sm"
            onClick={clear}
          >
            Clear
          </button>
        </div>
      )}

      <div className="relative">
        <SearchIcon className="pointer-events-none absolute top-1/2 left-3 size-5 -translate-y-1/2 text-slate-400" />
        <input
          id={inputId}
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded={showList}
          aria-controls={listboxId}
          aria-autocomplete="list"
          aria-activedescendant={activeIndex >= 0 ? `${listboxId}-${activeIndex}` : undefined}
          aria-invalid={error ? true : undefined}
          aria-describedby={cx(error && errorId, hint && hintId) || undefined}
          // Browsers happily autofill a street address into anything that looks
          // like a search box, and this one is not asking for the user's own.
          autoComplete="off"
          className={cx('input pl-10', error && 'input-error')}
          value={query}
          placeholder={
            value
              ? 'Search for a different course'
              : optional
                ? 'Optional — search by name or town'
                : 'Search by name or town'
          }
          onChange={(e) => {
            setQuery(e.target.value)
            setOpen(true)
          }}
          onFocus={() => setOpen(true)}
          // A click on a result fires after blur, so the list has to survive
          // long enough for it to land. onMouseDown on the option handles that;
          // this only closes the list for a real focus change.
          onBlur={() => setOpen(false)}
          onKeyDown={handleKeyDown}
        />
        {searching && (
          <span className="absolute top-1/2 right-3 -translate-y-1/2">
            <Spinner label="Searching" />
          </span>
        )}

        {showList && (
          // A listbox may only contain options, so the two empty states are
          // rendered as a sibling panel rather than as rows inside the list.
          <div className="absolute z-20 mt-1 w-full overflow-hidden rounded-xl border border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-slate-800">
            {results.length > 0 ? (
              <ul
                id={listboxId}
                role="listbox"
                aria-label="Matching courses"
                className="max-h-72 overflow-y-auto py-1"
              >
                {results.map((course, index) => (
                  <li
                    key={course.id}
                    id={`${listboxId}-${index}`}
                    role="option"
                    aria-selected={index === activeIndex}
                    // preventDefault keeps the input focused, so the blur that
                    // would close this list before the click never happens.
                    onMouseDown={(e) => e.preventDefault()}
                    onMouseEnter={() => setActiveIndex(index)}
                    onClick={() => select(course)}
                    className={cx(
                      'cursor-pointer px-3 py-2',
                      index === activeIndex && 'bg-accent-50 dark:bg-accent-950/60',
                    )}
                  >
                    <span className="block truncate text-sm font-medium">{course.name}</span>
                    <span className="block truncate text-xs text-slate-500 dark:text-slate-400">
                      {course.location || 'No location recorded'}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="px-3 py-2 text-sm text-slate-500 dark:text-slate-400">
                {searchFailed
                  ? 'Could not search the directory just now.'
                  : `No courses matched “${term}”.`}
              </p>
            )}
          </div>
        )}
      </div>

      {error ? (
        <p id={errorId} className="field-error">
          {error}
        </p>
      ) : (
        hint && (
          <p id={hintId} className="mt-1.5 text-sm text-slate-500 dark:text-slate-400">
            {hint}
          </p>
        )
      )}
    </div>
  )
}
