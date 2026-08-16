/**
 * Playing a round, one hole at a time, on a phone.
 *
 * Two things shape this screen. It is used outdoors, one-handed, between shots,
 * so score and putts are large targets and everything else is behind a toggle.
 * And it is used where there is no signal, so every entry is written locally
 * first and pushed when it can be - see lib/roundQueue.ts.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import {
  ACCURACY_LABELS,
  PENALTY_LABELS,
  PENALTY_TYPES,
  formatToPar,
  targetLabel,
  toHolePayload,
} from '../lib/rounds'
import {
  cacheRound,
  cachedRound,
  enqueueHole,
  flushQueue,
  forgetRound,
  isOnline,
  pendingCount,
} from '../lib/roundQueue'
import type { Bag, Club, Round, RoundHolePayload, TeeAccuracy } from '../types'
import { Alert, PageSpinner, Spinner, cx } from '../components/ui'

export default function LiveRoundPage() {
  const { roundID = '' } = useParams()
  const navigate = useNavigate()

  const [round, setRound] = useState<Round | null>(() => cachedRound(roundID))
  const [rows, setRows] = useState<Map<number, RoundHolePayload>>(new Map())
  const [clubs, setClubs] = useState<Club[]>([])
  const [index, setIndex] = useState(0)
  const [showDetail, setShowDetail] = useState(false)
  const [pending, setPending] = useState(() => pendingCount(roundID))
  const [loading, setLoading] = useState(!cachedRound(roundID))
  const [error, setError] = useState<string | null>(null)
  const [finishing, setFinishing] = useState(false)

  // Load from the server when it can be reached, and fall back to the cached
  // copy when it cannot. A round already in progress has to open in a tunnel.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const [loaded, bag] = await Promise.all([
          api.getRound(roundID),
          api.getBag().catch(() => ({
            active: [],
            benched: [],
            retired: [],
            active_count: 0,
            club_limit: 0,
            over_limit: false,
          }) as Bag),
        ])
        if (cancelled) return
        adopt(loaded)
        setClubs([...bag.active, ...bag.benched, ...bag.retired])
      } catch (err) {
        if (cancelled) return
        if (!cachedRound(roundID)) {
          setError(err instanceof ApiError ? err.message : 'Could not open that round.')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roundID])

  // Seed the editable rows from whichever copy arrived first.
  useEffect(() => {
    if (!round || rows.size > 0) return
    setRows(new Map(round.holes.map((h) => [h.hole_number, toHolePayload(h)])))
    // Open on the first hole with no score, which is where play is.
    const next = round.holes.findIndex((h) => h.strokes === null)
    setIndex(next === -1 ? 0 : next)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [round])

  function adopt(loaded: Round) {
    setRound(loaded)
    cacheRound(loaded)
  }

  // Push whatever is queued whenever the connection comes back, and on a timer
  // while the screen is open. A round finished in a dead zone syncs the moment
  // the car reaches the road.
  const flush = useCallback(async () => {
    if (!isOnline()) return
    const updated = await flushQueue(roundID)
    if (updated) adopt(updated)
    setPending(pendingCount(roundID))
  }, [roundID])

  useEffect(() => {
    void flush()
    const onOnline = () => void flush()
    window.addEventListener('online', onOnline)
    const timer = window.setInterval(() => void flush(), 20_000)
    return () => {
      window.removeEventListener('online', onOnline)
      window.clearInterval(timer)
    }
  }, [flush])

  useWakeLock()

  const holeNumbers = useMemo(
    () => (round ? round.holes.map((h) => h.hole_number) : []),
    [round],
  )
  const holeNumber = holeNumbers[index]
  const row = holeNumber !== undefined ? rows.get(holeNumber) : undefined
  const snapshot = round?.holes.find((h) => h.hole_number === holeNumber)

  /** Writes a hole locally, queues it, and tries to push. */
  function save(changes: Partial<RoundHolePayload>) {
    if (!row) return
    const next = { ...row, ...changes }
    setRows((current) => new Map(current).set(next.hole_number, next))
    enqueueHole(roundID, next)
    setPending(pendingCount(roundID))
    void flush()
  }

  // The running total is computed from local rows, not from the server's
  // summary: the point of local-first is that the number moves the instant a
  // score is tapped, signal or no signal.
  const running = useMemo(() => {
    let strokes = 0
    let par = 0
    let played = 0
    for (const r of rows.values()) {
      if (r.strokes === null) continue
      played++
      strokes += r.strokes
      const p = round?.holes.find((h) => h.hole_number === r.hole_number)?.par
      if (p) par += p
    }
    return { strokes, toPar: strokes - par, played }
  }, [rows, round])

  async function handleFinish() {
    setFinishing(true)
    setError(null)
    try {
      // Everything has to be on the server before the round is declared done,
      // or a hole could be left behind in a queue nobody visits again.
      await flushQueue(roundID)
      if (pendingCount(roundID) > 0) {
        setError('Some holes have not been saved yet. Reconnect and try again.')
        setFinishing(false)
        return
      }
      await api.completeRound(roundID)
      forgetRound(roundID)
      navigate(`/rounds/${roundID}`, { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not finish that round.')
      setFinishing(false)
    }
  }

  if (loading) return <PageSpinner label="Opening round" />
  if (!round || !row || holeNumber === undefined) {
    return <Alert>{error ?? 'That round could not be opened.'}</Alert>
  }

  const par = snapshot?.par ?? null
  const onLast = index === holeNumbers.length - 1

  return (
    <div className="mx-auto max-w-md space-y-4 pb-24">
      {/* Sticky, because the running total is the thing you glance at. */}
      <header className="sticky top-0 z-10 -mx-4 border-b border-slate-200 bg-white/95 px-4 py-3 backdrop-blur dark:border-slate-700 dark:bg-slate-900/95">
        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-lg font-bold">Hole {holeNumber}</p>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              {par ? `Par ${par}` : 'Par not recorded'}
              {snapshot?.yardage ? ` · ${snapshot.yardage} yds` : ''}
              {snapshot?.stroke_index ? ` · SI ${snapshot.stroke_index}` : ''}
            </p>
          </div>
          <div className="text-right">
            <p className="text-lg font-bold tabular-nums">{formatToPar(running.toPar)}</p>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              {running.strokes} thru {running.played}
            </p>
          </div>
        </div>
      </header>

      {error && <Alert>{error}</Alert>}

      <section className="card space-y-4 p-4">
        <div className="space-y-2">
          <span className="label">Score</span>
          <Stepper
            label={`Score for hole ${holeNumber}`}
            value={row.strokes}
            placeholder={par}
            min={1}
            max={20}
            onChange={(v) => save({ strokes: v, putts: v === null ? null : row.putts })}
          />
        </div>

        <div className="space-y-2">
          <span className="label">Putts</span>
          <div className="flex gap-2">
            {[0, 1, 2, 3, 4, 5].map((n) => (
              <button
                key={n}
                type="button"
                aria-pressed={row.putts === n}
                // More putts than strokes is not a thing, and the server
                // refuses it; disabling here says so before the tap.
                disabled={row.strokes !== null && n > row.strokes}
                onClick={() => save({ putts: row.putts === n ? null : n })}
                className={cx(
                  'flex-1 rounded-lg border py-3 text-base font-semibold tabular-nums transition',
                  row.putts === n
                    ? 'border-brand-600 bg-brand-600 text-white'
                    : 'border-slate-300 dark:border-slate-600',
                  'disabled:opacity-30',
                )}
              >
                {n}
              </button>
            ))}
          </div>
        </div>

        {/* Above the fold, beside the score and the putts, because a fairway is
            the third thing recorded on a hole and the first thing every screen
            in this app reports. It used to sit behind "Add detail", which made
            a headline statistic something you had to go looking for. */}
        <div className="space-y-2">
          <span className="label">Tee shot</span>
          <AccuracyPad
            par={par}
            value={row.tee_accuracy}
            onChange={(v) => save({ tee_accuracy: v })}
          />
        </div>

        <button
          type="button"
          className="btn-secondary w-full !min-h-0 !py-2 !text-sm"
          onClick={() => setShowDetail((v) => !v)}
          aria-expanded={showDetail}
        >
          {showDetail ? 'Hide detail' : 'Add detail'}
        </button>

        {showDetail && (
          <div className="space-y-4 border-t border-slate-100 pt-4 dark:border-slate-800">
            <div className="space-y-2">
              <span className="label">Tee club</span>
              <select
                aria-label="Tee club"
                className="input"
                value={row.tee_club_id ?? ''}
                onChange={(e) => save({ tee_club_id: e.target.value || null })}
              >
                <option value="">Not recorded</option>
                {clubs.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.label}
                  </option>
                ))}
              </select>
            </div>

            <div className="grid grid-cols-2 gap-2">
              <Toggle
                label="Fairway bunker"
                checked={row.fairway_bunker}
                onChange={(v) => save({ fairway_bunker: v })}
              />
              <Toggle
                label="Greenside bunker"
                checked={row.greenside_bunker}
                onChange={(v) => save({ greenside_bunker: v })}
              />
            </div>

            <div className="space-y-2">
              <span className="label">First putt (feet)</span>
              <Stepper
                label="First putt distance in feet"
                value={row.first_putt_feet}
                min={0}
                max={200}
                step={1}
                onChange={(v) => save({ first_putt_feet: v })}
              />
            </div>

            <div className="space-y-2">
              <span className="label">Penalty strokes</span>
              <Stepper
                label="Penalty strokes"
                value={row.penalties === 0 ? null : row.penalties}
                min={0}
                max={10}
                onChange={(v) =>
                  save({ penalties: v ?? 0, penalty_type: v ? row.penalty_type : null })
                }
              />
              {row.penalties > 0 && (
                <select
                  aria-label="Penalty reason"
                  className="input"
                  value={row.penalty_type ?? ''}
                  onChange={(e) => save({ penalty_type: (e.target.value || null) as never })}
                >
                  <option value="">Reason not recorded</option>
                  {PENALTY_TYPES.map((p) => (
                    <option key={p} value={p}>
                      {PENALTY_LABELS[p]}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>
        )}
      </section>

      <div className="flex items-center gap-2">
        <button
          type="button"
          className="btn-secondary flex-1"
          disabled={index === 0}
          onClick={() => setIndex((i) => Math.max(0, i - 1))}
        >
          Previous
        </button>
        {onLast ? (
          <button
            type="button"
            className="btn-primary flex-1"
            disabled={finishing}
            onClick={() => void handleFinish()}
          >
            {finishing ? <Spinner label="Finishing" /> : 'Finish round'}
          </button>
        ) : (
          <button
            type="button"
            className="btn-primary flex-1"
            onClick={() => setIndex((i) => Math.min(holeNumbers.length - 1, i + 1))}
          >
            Next hole
          </button>
        )}
      </div>

      <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400">
        <Link to={`/rounds/${roundID}`} className="underline">
          Full scorecard
        </Link>
        {pending > 0 ? (
          <span className="text-amber-700 dark:text-amber-300">
            {pending} {pending === 1 ? 'hole' : 'holes'} not yet saved
          </span>
        ) : (
          <span>All holes saved</span>
        )}
      </div>
    </div>
  )
}

/**
 * Keeps the screen awake while a round is open.
 *
 * Without it the phone sleeps between every shot, and a round becomes eighteen
 * unlocks. Best-effort: the API is unavailable on some browsers and the lock is
 * dropped whenever the tab is hidden, so it is re-taken on return.
 */
function useWakeLock() {
  const lock = useRef<WakeLockSentinel | null>(null)

  useEffect(() => {
    let released = false

    async function take() {
      if (!('wakeLock' in navigator) || document.visibilityState !== 'visible') return
      try {
        lock.current = await navigator.wakeLock.request('screen')
      } catch {
        // Denied, or the battery is too low. Not worth telling anybody about.
      }
    }

    void take()
    const onVisible = () => void take()
    document.addEventListener('visibilitychange', onVisible)

    return () => {
      released = true
      document.removeEventListener('visibilitychange', onVisible)
      void lock.current?.release().catch(() => {})
      lock.current = null
      void released
    }
  }, [])
}

/** A big plus/minus for a number that may legitimately be absent. */
function Stepper({
  label,
  value,
  placeholder,
  min,
  max,
  step = 1,
  onChange,
}: {
  label: string
  value: number | null
  placeholder?: number | null
  min: number
  max: number
  step?: number
  onChange: (value: number | null) => void
}) {
  // An empty box starts from the placeholder - par, usually - because the
  // commonest score to enter is the one written on the card beside it.
  const base = value ?? placeholder ?? min

  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        aria-label={`Decrease ${label}`}
        className="btn-secondary !min-h-0 size-12 !px-0 text-xl"
        onClick={() => onChange(Math.max(min, (value ?? base) - step))}
      >
        −
      </button>
      <div className="flex-1 text-center">
        <span
          aria-label={label}
          role="status"
          className={cx(
            'text-3xl font-bold tabular-nums',
            value === null && 'text-slate-300 dark:text-slate-600',
          )}
        >
          {value ?? placeholder ?? '—'}
        </span>
      </div>
      <button
        type="button"
        aria-label={`Increase ${label}`}
        className="btn-secondary !min-h-0 size-12 !px-0 text-xl"
        onClick={() => onChange(Math.min(max, (value ?? base - step) + step))}
      >
        +
      </button>
      {value !== null && (
        <button
          type="button"
          className="btn-ghost !min-h-0 !px-2 !py-1 text-xs"
          onClick={() => onChange(null)}
        >
          Clear
        </button>
      )}
    </div>
  )
}

/**
 * The dispersion pad: where the tee shot finished, laid out the way it looked.
 *
 * A grid rather than a dropdown because it is one tap, and because the spatial
 * arrangement is the information - far left is further from the middle than
 * left, and the eye reads that without a label.
 */
function AccuracyPad({
  par,
  value,
  onChange,
}: {
  par: number | null
  value: TeeAccuracy | null
  onChange: (value: TeeAccuracy | null) => void
}) {
  const cell = (key: TeeAccuracy, label: string, className?: string) => (
    <button
      key={key}
      type="button"
      aria-pressed={value === key}
      onClick={() => onChange(value === key ? null : key)}
      className={cx(
        'rounded-lg border py-2 text-xs font-medium transition',
        value === key
          ? 'border-brand-600 bg-brand-600 text-white'
          : 'border-slate-300 dark:border-slate-600',
        className,
      )}
    >
      {label}
    </button>
  )

  return (
    <div className="space-y-2">
      <div className="grid grid-cols-5 gap-1">
        <div />
        <div className="col-span-3">{cell('long', 'Long', 'w-full')}</div>
        <div />
      </div>
      <div className="grid grid-cols-5 gap-1">
        {cell('far_left', 'Far L')}
        {cell('left', 'Left')}
        {/* The stored value is always `hit`; only the word changes, because a
            par 3 has no fairway. */}
        {cell('hit', targetLabel(par))}
        {cell('right', 'Right')}
        {cell('far_right', 'Far R')}
      </div>
      <div className="grid grid-cols-5 gap-1">
        <div />
        <div className="col-span-3">{cell('short', 'Short', 'w-full')}</div>
        <div />
      </div>
      <div className="grid grid-cols-5 gap-1">
        <div />
        <div className="col-span-3">{cell('mishit', ACCURACY_LABELS.mishit, 'w-full')}</div>
        <div />
      </div>
    </div>
  )
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string
  checked: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={checked}
      onClick={() => onChange(!checked)}
      className={cx(
        'rounded-lg border px-3 py-3 text-sm font-medium transition',
        checked
          ? 'border-brand-600 bg-brand-600 text-white'
          : 'border-slate-300 dark:border-slate-600',
      )}
    >
      {label}
    </button>
  )
}
