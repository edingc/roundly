import { useState } from 'react'
import type { Tee, TeePayload } from '../types'
import { Alert, Field, Spinner } from './ui'

/** Presets cover the common cases; the color input allows anything else. */
const COLOR_PRESETS = [
  { name: 'Black', color: '#111827' },
  { name: 'Gold', color: '#FFD700' },
  { name: 'Blue', color: '#2563EB' },
  { name: 'White', color: '#F8FAFC' },
  { name: 'Green', color: '#16A34A' },
  { name: 'Red', color: '#DC2626' },
]

export interface TeeFormValues {
  name: string
  color: string
  courseRating: string
  slopeRating: string
}

export function emptyTeeForm(): TeeFormValues {
  return { name: '', color: '#2563EB', courseRating: '', slopeRating: '' }
}

export function teeToForm(tee: Tee): TeeFormValues {
  return {
    name: tee.name,
    color: tee.color,
    courseRating: tee.course_rating === null ? '' : String(tee.course_rating),
    slopeRating: tee.slope_rating === null ? '' : String(tee.slope_rating),
  }
}

/** Converts form strings to the API payload, leaving blanks as null. */
export function teeFormToPayload(values: TeeFormValues): TeePayload {
  const rating = values.courseRating.trim()
  const slope = values.slopeRating.trim()
  return {
    name: values.name.trim(),
    color: values.color,
    course_rating: rating === '' ? null : Number(rating),
    slope_rating: slope === '' ? null : Number(slope),
  }
}

/**
 * Fields for one tee. Used inline by the add-course flow and inside the edit
 * dialog on the course detail screen, so it owns no submit button of its own.
 */
export function TeeFields({
  values,
  errors,
  onChange,
  idPrefix,
}: {
  values: TeeFormValues
  errors: Record<string, string>
  onChange: (values: TeeFormValues) => void
  idPrefix: string
}) {
  function update<K extends keyof TeeFormValues>(key: K, value: TeeFormValues[K]) {
    onChange({ ...values, [key]: value })
  }

  return (
    <div className="space-y-4">
      <Field
        id={`${idPrefix}-name`}
        label="Tee name"
        value={values.name}
        onChange={(e) => update('name', e.target.value)}
        error={errors.name}
        placeholder="Championship"
        required
        maxLength={60}
      />

      <div>
        <label htmlFor={`${idPrefix}-color`} className="label">
          Color
        </label>
        <div className="flex items-center gap-2">
          <input
            id={`${idPrefix}-color`}
            type="color"
            value={values.color}
            onChange={(e) => update('color', e.target.value.toUpperCase())}
            className="h-11 w-14 shrink-0 cursor-pointer rounded-lg border border-slate-300 bg-white p-1 dark:border-slate-700 dark:bg-slate-950"
            aria-label="Tee color"
          />
          <input
            type="text"
            value={values.color}
            onChange={(e) => update('color', e.target.value.toUpperCase())}
            className="input font-mono"
            aria-label="Tee color hex value"
            maxLength={7}
          />
        </div>
        <div className="mt-2 flex flex-wrap gap-1.5">
          {COLOR_PRESETS.map((preset) => (
            <button
              key={preset.color}
              type="button"
              onClick={() => onChange({ ...values, color: preset.color, name: values.name || preset.name })}
              className="flex items-center gap-1.5 rounded-full border border-slate-200 px-2 py-1 text-xs text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
            >
              <span
                aria-hidden="true"
                className="size-2.5 rounded-full ring-1 ring-black/15 dark:ring-white/25"
                style={{ backgroundColor: preset.color }}
              />
              {preset.name}
            </button>
          ))}
        </div>
        {errors.color && <p className="field-error">{errors.color}</p>}
      </div>

      <div className="grid grid-cols-2 gap-3">
        <Field
          id={`${idPrefix}-rating`}
          label="Course rating"
          type="number"
          inputMode="decimal"
          step="0.1"
          min="50"
          max="90"
          value={values.courseRating}
          onChange={(e) => update('courseRating', e.target.value)}
          error={errors.course_rating}
          placeholder="72.4"
        />
        <Field
          id={`${idPrefix}-slope`}
          label="Slope"
          type="number"
          inputMode="numeric"
          min="55"
          max="155"
          value={values.slopeRating}
          onChange={(e) => update('slopeRating', e.target.value)}
          error={errors.slope_rating}
          placeholder="135"
        />
      </div>
    </div>
  )
}

/** A modal for adding or editing a single tee on the course detail screen. */
export function TeeDialog({
  title,
  initial,
  submitLabel,
  onCancel,
  onSubmit,
}: {
  title: string
  initial: TeeFormValues
  submitLabel: string
  onCancel: () => void
  onSubmit: (payload: TeePayload) => Promise<void>
}) {
  const [values, setValues] = useState<TeeFormValues>(initial)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setErrors({})
    setFormError(null)
    try {
      await onSubmit(teeFormToPayload(values))
    } catch (error) {
      const apiError = error as { fields?: Record<string, string>; message?: string }
      if (apiError.fields && Object.keys(apiError.fields).length > 0) setErrors(apiError.fields)
      else setFormError(apiError.message ?? 'Could not save the tee.')
      setSaving(false)
    }
  }

  return (
    <div
      className="fixed inset-0 z-30 flex items-end justify-center bg-black/50 p-0 sm:items-center sm:p-4"
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div className="card max-h-[90vh] w-full max-w-md overflow-y-auto rounded-b-none p-6 sm:rounded-xl">
        <h2 className="mb-4 text-lg font-semibold">{title}</h2>
        {formError && (
          <div className="mb-4">
            <Alert>{formError}</Alert>
          </div>
        )}
        <form onSubmit={handleSubmit} className="space-y-4">
          <TeeFields values={values} errors={errors} onChange={setValues} idPrefix="tee-dialog" />
          <div className="flex gap-2 pt-2">
            <button type="button" onClick={onCancel} className="btn-secondary flex-1">
              Cancel
            </button>
            <button type="submit" disabled={saving} className="btn-primary flex-1">
              {saving ? <Spinner label="Saving" /> : submitLabel}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
