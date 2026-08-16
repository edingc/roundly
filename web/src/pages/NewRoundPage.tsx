/**
 * Starting a round: which course, which tee, how many holes, and which way it
 * is going to be entered.
 *
 * The mode is asked here rather than inferred because it decides where the
 * player lands next - the hole-by-hole screen or the scorecard grid - and
 * because a round entered from memory should not claim a start time it never
 * had.
 */
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { useAuth } from '../lib/auth'
import { todayISO } from '../lib/rounds'
import type { CourseDetail, EntryMode, Nine } from '../types'
import { CourseSearchField, type SelectedCourse } from '../components/CourseSearchField'
import { Alert, Field, SegmentedControl, Spinner, TeeChip, cx } from '../components/ui'

const MODE_OPTIONS: Array<{ value: EntryMode; label: string }> = [
  { value: 'live', label: 'Play now' },
  { value: 'manual', label: 'Enter a card' },
]

export default function NewRoundPage() {
  const navigate = useNavigate()
  const { user } = useAuth()

  const [course, setCourse] = useState<SelectedCourse | null>(null)
  const [detail, setDetail] = useState<CourseDetail | null>(null)
  const [loadingCourse, setLoadingCourse] = useState(false)
  const [teeID, setTeeID] = useState('')
  const [holes, setHoles] = useState(18)
  const [nine, setNine] = useState<Nine>('front')
  const [playedOn, setPlayedOn] = useState(todayISO())
  const [mode, setMode] = useState<EntryMode>('live')
  const [starting, setStarting] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)

  // Seed from the home course, which is the one somebody is most often
  // standing on.
  useEffect(() => {
    if (course || !user?.home_course_id) return
    setCourse({
      id: user.home_course_id,
      name: user.home_course_name ?? '',
      location: user.home_course_location ?? '',
    })
  }, [user, course])

  // The tees and the scorecard live on the course, so the form cannot finish
  // being drawn until it loads.
  useEffect(() => {
    if (!course) {
      setDetail(null)
      return
    }
    let cancelled = false
    setLoadingCourse(true)
    api
      .getCourse(course.id)
      .then((loaded) => {
        if (cancelled) return
        setDetail(loaded)
        setTeeID((current) => current || loaded.tees[0]?.id || '')
        if (loaded.hole_count === 9) setHoles(9)
      })
      .catch(() => {
        if (!cancelled) setFormError('Could not load that course.')
      })
      .finally(() => {
        if (!cancelled) setLoadingCourse(false)
      })
    return () => {
      cancelled = true
    }
  }, [course])

  const tee = detail?.tees.find((t) => t.id === teeID)
  const incomplete = detail ? countMissingPars(detail, teeID) : 0

  async function handleStart(event: React.FormEvent) {
    event.preventDefault()
    setStarting(true)
    setErrors({})
    setFormError(null)

    try {
      const round = await api.startRound({
        // The client mints the id so a round can begin with no signal and still
        // have something for its holes to attach to.
        id: crypto.randomUUID(),
        course_id: course!.id,
        tee_id: teeID,
        played_on: playedOn,
        holes,
        nine: holes === 9 ? nine : '',
        entry_mode: mode,
      })
      navigate(mode === 'live' ? `/rounds/${round.id}/play` : `/rounds/${round.id}`, {
        replace: true,
      })
    } catch (err) {
      if (err instanceof ApiError && err.isValidation) setErrors(err.fields)
      else setFormError(err instanceof ApiError ? err.message : 'Could not start that round.')
      setStarting(false)
    }
  }

  return (
    <div className="mx-auto max-w-xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">New round</h1>
        <p className="text-sm text-slate-600 dark:text-slate-400">
          Score as you play, or type in a card you already have.
        </p>
      </div>

      {formError && <Alert>{formError}</Alert>}

      <form onSubmit={handleStart} className="card space-y-5 p-5">
        <CourseSearchField
          label="Course"
          value={course}
          onChange={(next) => {
            setCourse(next)
            setTeeID('')
          }}
          error={errors.course_id}
          hint="Search by name, town, or state."
        />

        {loadingCourse && <Spinner label="Loading course" />}

        {detail && (
          <>
            <div className="space-y-2">
              <span className="label">Tees</span>
              {detail.tees.length === 0 ? (
                <Alert tone="warning">
                  This course has no tees yet. Add one on the course page before starting a round -
                  the tee is where par and yardage come from.
                </Alert>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {detail.tees.map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => setTeeID(t.id)}
                      className={cx(
                        'rounded-full border p-0.5 transition',
                        t.id === teeID
                          ? 'border-brand-600 ring-2 ring-brand-500/30'
                          : 'border-transparent',
                      )}
                      aria-pressed={t.id === teeID}
                    >
                      <TeeChip name={t.name} color={t.color} />
                    </button>
                  ))}
                </div>
              )}
              {errors.tee_id && <p className="field-error">{errors.tee_id}</p>}
            </div>

            {detail.hole_count !== 9 && (
              <div className="space-y-2">
                <span className="label">Holes</span>
                <SegmentedControl
                  label="Holes"
                  value={String(holes)}
                  options={[
                    { value: '18', label: '18' },
                    { value: '9', label: '9' },
                  ]}
                  onChange={(v) => setHoles(Number(v))}
                />
              </div>
            )}

            {holes === 9 && detail.hole_count !== 9 && (
              <div className="space-y-2">
                <span className="label">Which nine</span>
                <SegmentedControl
                  label="Which nine"
                  value={nine}
                  options={[
                    { value: 'front', label: 'Front (1-9)' },
                    { value: 'back', label: 'Back (10-18)' },
                  ]}
                  onChange={setNine}
                />
              </div>
            )}

            {/* A course with gaps in its card can still be played; the holes
                without a par simply drop out of the statistics that need one,
                and the par can be filled in as you go. */}
            {incomplete > 0 && tee && (
              <Alert tone="warning">
                {incomplete} {incomplete === 1 ? 'hole has' : 'holes have'} no par recorded for the{' '}
                {tee.name} tees. You can still play - those holes will not count toward greens in
                regulation until you fill the par in.
              </Alert>
            )}
          </>
        )}

        <Field
          id="played-on"
          label="Date"
          type="date"
          value={playedOn}
          onChange={(e) => setPlayedOn(e.target.value)}
          error={errors.played_on}
          required
        />

        <div className="space-y-2">
          <span className="label">How</span>
          <SegmentedControl label="How" value={mode} options={MODE_OPTIONS} onChange={setMode} />
          <p className="text-sm text-slate-500 dark:text-slate-400">
            {mode === 'live'
              ? 'One hole at a time, built for a phone. Holes are saved as you go and kept on this device if you lose signal.'
              : 'A full scorecard grid. Better on a desktop, and the right choice for a round you have already played.'}
          </p>
        </div>

        <button
          type="submit"
          className="btn-primary w-full"
          disabled={starting || !course || !teeID}
        >
          {starting ? <Spinner label="Starting" /> : mode === 'live' ? 'Start playing' : 'Enter card'}
        </button>
      </form>
    </div>
  )
}

/** How many holes have no par for the chosen tee. */
function countMissingPars(detail: CourseDetail, teeID: string): number {
  if (!teeID) return 0
  return detail.holes.filter((h) => !h.tee_details.some((d) => d.tee_id === teeID)).length
}
