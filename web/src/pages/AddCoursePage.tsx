import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError, api } from '../lib/api'
import { formatPhone } from '../lib/phone'
import type { CourseDetail } from '../types'
import {
  TeeFields,
  emptyTeeForm,
  teeFormToPayload,
  type TeeFormValues,
} from '../components/TeeForm'
import { ScorecardGrid } from '../components/ScorecardGrid'
import {
  Alert,
  ChevronLeftIcon,
  Field,
  PlusIcon,
  Spinner,
  TeeChip,
  TrashIcon,
  cx,
} from '../components/ui'

type Step = 1 | 2 | 3

const STEPS: Array<{ step: Step; label: string }> = [
  { step: 1, label: 'Course' },
  { step: 2, label: 'Tees' },
  { step: 3, label: 'Scorecard' },
]

export default function AddCoursePage() {
  const navigate = useNavigate()

  const [step, setStep] = useState<Step>(1)
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [phone, setPhone] = useState('')
  const [holeCount, setHoleCount] = useState<9 | 18>(18)
  const [tees, setTees] = useState<TeeFormValues[]>([emptyTeeForm()])

  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  // Set once the course exists on the server, which is what step 3 edits.
  const [created, setCreated] = useState<CourseDetail | null>(null)

  function goToTees(event: React.FormEvent) {
    event.preventDefault()
    if (name.trim() === '') {
      setErrors({ name: 'This field is required.' })
      return
    }
    setErrors({})
    setStep(2)
  }

  /**
   * Creates the course, its holes, and its tees in one request, then moves to the
   * grid. Doing it as one call means an abandoned flow leaves nothing behind.
   */
  async function handleCreate() {
    setCreating(true)
    setErrors({})
    setFormError(null)

    try {
      const detail = await api.createCourse({
        name,
        address: address.trim() === '' ? null : address,
        phone: phone.trim() === '' ? null : formatPhone(phone),
        hole_count: holeCount,
        tees: tees.filter((tee) => tee.name.trim() !== '').map(teeFormToPayload),
      })
      setCreated(detail)
      setStep(3)
    } catch (error) {
      if (error instanceof ApiError && error.isValidation) {
        setErrors(error.fields)
        // Field paths come back as "tees.0.name"; anything on a tee belongs to
        // step 2, so send the user back there to fix it.
        const hasTeeError = Object.keys(error.fields).some((key) => key.startsWith('tees.'))
        setStep(hasTeeError ? 2 : 1)
      } else {
        setFormError(error instanceof ApiError ? error.message : 'Could not create the course.')
      }
    } finally {
      setCreating(false)
    }
  }

  async function reloadCreated() {
    if (!created) return
    setCreated(await api.getCourse(created.id))
  }

  function updateTee(index: number, values: TeeFormValues) {
    setTees((prev) => prev.map((tee, i) => (i === index ? values : tee)))
  }

  function removeTee(index: number) {
    setTees((prev) => prev.filter((_, i) => i !== index))
  }

  /** Pulls the "tees.N.field" errors out for one tee's fields. */
  function teeErrors(index: number): Record<string, string> {
    const prefix = `tees.${index}.`
    const scoped: Record<string, string> = {}
    for (const [key, message] of Object.entries(errors)) {
      if (key.startsWith(prefix)) scoped[key.slice(prefix.length)] = message
    }
    return scoped
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

      <div>
        <h1 className="text-2xl font-bold tracking-tight">Add a course</h1>
        <ol className="mt-4 flex items-center gap-2 text-sm">
          {STEPS.map(({ step: s, label }, index) => (
            <li key={s} className="flex items-center gap-2">
              <span
                className={cx(
                  'flex items-center gap-2 rounded-full px-3 py-1',
                  step === s
                    ? 'bg-brand-600 font-semibold text-white dark:bg-brand-500 dark:text-brand-950'
                    : step > s
                      ? 'bg-brand-100 text-brand-800 dark:bg-brand-950 dark:text-brand-200'
                      : 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400',
                )}
                aria-current={step === s ? 'step' : undefined}
              >
                <span className="tabular-nums">{s}</span>
                {label}
              </span>
              {index < STEPS.length - 1 && (
                <span aria-hidden="true" className="text-slate-300 dark:text-slate-600">
                  →
                </span>
              )}
            </li>
          ))}
        </ol>
      </div>

      {formError && <Alert>{formError}</Alert>}

      {step === 1 && (
        <form onSubmit={goToTees} className="card space-y-4 p-5">
          <Field
            label="Course name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            error={errors.name}
            required
            maxLength={120}
            placeholder="Pebble Ridge Golf Club"
            autoFocus
          />
          <Field
            label="Address"
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            error={errors.address}
            maxLength={240}
            placeholder="Optional"
          />
          <Field
            label="Phone number"
            type="tel"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            error={errors.phone}
            maxLength={30}
            placeholder="Optional"
          />
          <fieldset>
            <legend className="label">Holes</legend>
            <div className="flex gap-2">
              {([18, 9] as const).map((count) => (
                <button
                  key={count}
                  type="button"
                  onClick={() => setHoleCount(count)}
                  className={cx(
                    'btn flex-1',
                    holeCount === count
                      ? 'bg-brand-600 text-white dark:bg-brand-500 dark:text-brand-950'
                      : 'btn-secondary',
                  )}
                  aria-pressed={holeCount === count}
                >
                  {count} holes
                </button>
              ))}
            </div>
            {errors.hole_count && <p className="field-error">{errors.hole_count}</p>}
          </fieldset>
          <button type="submit" className="btn-primary w-full">
            Continue to tees
          </button>
        </form>
      )}

      {step === 2 && (
        <div className="space-y-4">
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Add each set of tees you play. Par and yardage are recorded per tee on the next step.
          </p>

          {tees.map((tee, index) => (
            <div key={index} className="card space-y-4 p-5">
              <div className="flex items-center gap-2">
                <h2 className="text-sm font-semibold text-slate-500 uppercase dark:text-slate-400">
                  Tee {index + 1}
                </h2>
                {tees.length > 1 && (
                  <button
                    type="button"
                    onClick={() => removeTee(index)}
                    className="btn-ghost ml-auto !min-h-0 !px-2 !py-1 text-red-600 dark:text-red-400"
                    aria-label={`Remove tee ${index + 1}`}
                  >
                    <TrashIcon className="size-4" />
                  </button>
                )}
              </div>
              <TeeFields
                values={tee}
                errors={teeErrors(index)}
                onChange={(values) => updateTee(index, values)}
                idPrefix={`new-tee-${index}`}
              />
            </div>
          ))}

          <button
            type="button"
            className="btn-secondary w-full"
            onClick={() => setTees((prev) => [...prev, emptyTeeForm()])}
          >
            <PlusIcon className="size-4" />
            Add another tee
          </button>

          <div className="flex gap-2">
            <button type="button" className="btn-secondary flex-1" onClick={() => setStep(1)}>
              Back
            </button>
            <button
              type="button"
              className="btn-primary flex-1"
              disabled={creating}
              onClick={() => void handleCreate()}
            >
              {creating ? <Spinner label="Creating course" /> : 'Create course'}
            </button>
          </div>
        </div>
      )}

      {step === 3 && created && (
        <div className="space-y-4">
          <Alert tone="success">
            “{created.name}” is saved. Fill in par and yardage below — each cell saves as you go.
          </Alert>

          {created.tees.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {created.tees.map((tee) => (
                <TeeChip key={tee.id} name={tee.name} color={tee.color} />
              ))}
            </div>
          )}

          <ScorecardGrid course={created} editable onCourseChanged={() => void reloadCreated()} />

          <div className="flex gap-2">
            <Link to={`/courses/${created.id}`} className="btn-secondary flex-1">
              Go to course
            </Link>
            <button
              type="button"
              className="btn-primary flex-1"
              onClick={() => navigate('/courses')}
            >
              Done
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
