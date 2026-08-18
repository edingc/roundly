import { useState } from 'react'
import type { Tee, TeePayload } from '../types'
import { Alert, Field, Spinner, cx } from './ui'

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
  courseRatingMen: string
  slopeRatingMen: string
  courseRatingWomen: string
  slopeRatingWomen: string
  front9CourseRatingMen: string
  front9SlopeRatingMen: string
  back9CourseRatingMen: string
  back9SlopeRatingMen: string
  front9CourseRatingWomen: string
  front9SlopeRatingWomen: string
  back9CourseRatingWomen: string
  back9SlopeRatingWomen: string
}

export function emptyTeeForm(): TeeFormValues {
  return {
    name: '',
    color: '#2563EB',
    courseRatingMen: '',
    slopeRatingMen: '',
    courseRatingWomen: '',
    slopeRatingWomen: '',
    front9CourseRatingMen: '',
    front9SlopeRatingMen: '',
    back9CourseRatingMen: '',
    back9SlopeRatingMen: '',
    front9CourseRatingWomen: '',
    front9SlopeRatingWomen: '',
    back9CourseRatingWomen: '',
    back9SlopeRatingWomen: '',
  }
}

/** Renders a rating value for the form, leaving null as a blank string. */
function ratingToForm(value: number | null): string {
  return value === null ? '' : String(value)
}

export function teeToForm(tee: Tee): TeeFormValues {
  return {
    name: tee.name,
    color: tee.color,
    courseRatingMen: ratingToForm(tee.course_rating_men),
    slopeRatingMen: ratingToForm(tee.slope_rating_men),
    courseRatingWomen: ratingToForm(tee.course_rating_women),
    slopeRatingWomen: ratingToForm(tee.slope_rating_women),
    front9CourseRatingMen: ratingToForm(tee.front9_course_rating_men),
    front9SlopeRatingMen: ratingToForm(tee.front9_slope_rating_men),
    back9CourseRatingMen: ratingToForm(tee.back9_course_rating_men),
    back9SlopeRatingMen: ratingToForm(tee.back9_slope_rating_men),
    front9CourseRatingWomen: ratingToForm(tee.front9_course_rating_women),
    front9SlopeRatingWomen: ratingToForm(tee.front9_slope_rating_women),
    back9CourseRatingWomen: ratingToForm(tee.back9_course_rating_women),
    back9SlopeRatingWomen: ratingToForm(tee.back9_slope_rating_women),
  }
}

/** Parses a trimmed form value to the API payload shape, leaving blanks as null. */
function ratingToPayload(value: string): number | null {
  const trimmed = value.trim()
  return trimmed === '' ? null : Number(trimmed)
}

/**
 * Converts form strings to the API payload, leaving blanks as null.
 *
 * A 9-hole course has one rating, which TeeFields binds to the front9_*
 * fields (their 25-45 scale fits a single 9 — the main course_rating_men
 * field is validated 50-90, an 18-hole scale). So for a 9-hole course this
 * maps front9_* to the payload's front9_* slots as the course's one rating,
 * and nulls out the main and back9_* fields, which the form doesn't show and
 * whose state may hold stale values from before the hole count changed.
 */
export function teeFormToPayload(values: TeeFormValues, holeCount: number): TeePayload {
  const is9Holes = holeCount === 9
  return {
    name: values.name.trim(),
    color: values.color,
    course_rating_men: is9Holes ? null : ratingToPayload(values.courseRatingMen),
    slope_rating_men: is9Holes ? null : ratingToPayload(values.slopeRatingMen),
    course_rating_women: is9Holes ? null : ratingToPayload(values.courseRatingWomen),
    slope_rating_women: is9Holes ? null : ratingToPayload(values.slopeRatingWomen),
    front9_course_rating_men: ratingToPayload(values.front9CourseRatingMen),
    front9_slope_rating_men: ratingToPayload(values.front9SlopeRatingMen),
    back9_course_rating_men: is9Holes ? null : ratingToPayload(values.back9CourseRatingMen),
    back9_slope_rating_men: is9Holes ? null : ratingToPayload(values.back9SlopeRatingMen),
    front9_course_rating_women: ratingToPayload(values.front9CourseRatingWomen),
    front9_slope_rating_women: ratingToPayload(values.front9SlopeRatingWomen),
    back9_course_rating_women: is9Holes ? null : ratingToPayload(values.back9CourseRatingWomen),
    back9_slope_rating_women: is9Holes ? null : ratingToPayload(values.back9SlopeRatingWomen),
  }
}

/** One row of a rating table: a period label plus its course rating and slope inputs. */
function RatingRow({
  idPrefix,
  label,
  courseValue,
  courseError,
  courseMin,
  courseMax,
  coursePlaceholder,
  onCourseChange,
  slopeValue,
  slopeError,
  onSlopeChange,
  slopePlaceholder,
}: {
  idPrefix: string
  label: string
  courseValue: string
  courseError?: string
  courseMin: number
  courseMax: number
  coursePlaceholder: string
  onCourseChange: (value: string) => void
  slopeValue: string
  slopeError?: string
  onSlopeChange: (value: string) => void
  slopePlaceholder: string
}) {
  return (
    <div className="grid grid-cols-[3.5rem_1fr_1fr] items-start gap-2">
      <span className="pt-2.5 text-xs font-medium text-slate-500 dark:text-slate-400">
        {label}
      </span>
      <div>
        <input
          id={`${idPrefix}-rating`}
          type="number"
          inputMode="decimal"
          step="0.1"
          min={courseMin}
          max={courseMax}
          value={courseValue}
          onChange={(e) => onCourseChange(e.target.value)}
          className={cx('input', courseError && 'input-error')}
          aria-label={`${label} course rating`}
          placeholder={coursePlaceholder}
        />
        {courseError && <p className="field-error">{courseError}</p>}
      </div>
      <div>
        <input
          id={`${idPrefix}-slope`}
          type="number"
          inputMode="numeric"
          min={55}
          max={155}
          value={slopeValue}
          onChange={(e) => onSlopeChange(e.target.value)}
          className={cx('input', slopeError && 'input-error')}
          aria-label={`${label} slope rating`}
          placeholder={slopePlaceholder}
        />
        {slopeError && <p className="field-error">{slopeError}</p>}
      </div>
    </div>
  )
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
  holeCount,
}: {
  values: TeeFormValues
  errors: Record<string, string>
  onChange: (values: TeeFormValues) => void
  idPrefix: string
  /** A 9-hole course has one rating; an 18-hole course also splits front/back 9. */
  holeCount: number
}) {
  const is9Holes = holeCount === 9

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
            className="h-11 w-14 shrink-0 cursor-pointer rounded-lg border border-slate-300 bg-white p-1 dark:border-slate-700 dark:bg-slate-900"
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
              className="flex items-center gap-1.5 rounded-md border border-slate-200 px-2 py-1 text-xs text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
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

      <div>
        <p className="label">Men's rating</p>
        <div className="space-y-2">
          <div className="grid grid-cols-[3.5rem_1fr_1fr] gap-2 text-xs text-slate-500 dark:text-slate-400">
            <span />
            <span>Course rating</span>
            <span>Slope</span>
          </div>
          {is9Holes ? (
            <RatingRow
              idPrefix={`${idPrefix}-men-9`}
              label="Rating"
              courseValue={values.front9CourseRatingMen}
              courseError={errors.front9_course_rating_men}
              courseMin={25}
              courseMax={45}
              coursePlaceholder="35.8"
              onCourseChange={(v) => update('front9CourseRatingMen', v)}
              slopeValue={values.front9SlopeRatingMen}
              slopeError={errors.front9_slope_rating_men}
              onSlopeChange={(v) => update('front9SlopeRatingMen', v)}
              slopePlaceholder="133"
            />
          ) : (
            <>
              <RatingRow
                idPrefix={`${idPrefix}-men-18`}
                label="18 holes"
                courseValue={values.courseRatingMen}
                courseError={errors.course_rating_men}
                courseMin={50}
                courseMax={90}
                coursePlaceholder="72.4"
                onCourseChange={(v) => update('courseRatingMen', v)}
                slopeValue={values.slopeRatingMen}
                slopeError={errors.slope_rating_men}
                onSlopeChange={(v) => update('slopeRatingMen', v)}
                slopePlaceholder="135"
              />
              <RatingRow
                idPrefix={`${idPrefix}-men-front9`}
                label="Front 9"
                courseValue={values.front9CourseRatingMen}
                courseError={errors.front9_course_rating_men}
                courseMin={25}
                courseMax={45}
                coursePlaceholder="35.8"
                onCourseChange={(v) => update('front9CourseRatingMen', v)}
                slopeValue={values.front9SlopeRatingMen}
                slopeError={errors.front9_slope_rating_men}
                onSlopeChange={(v) => update('front9SlopeRatingMen', v)}
                slopePlaceholder="133"
              />
              <RatingRow
                idPrefix={`${idPrefix}-men-back9`}
                label="Back 9"
                courseValue={values.back9CourseRatingMen}
                courseError={errors.back9_course_rating_men}
                courseMin={25}
                courseMax={45}
                coursePlaceholder="36.6"
                onCourseChange={(v) => update('back9CourseRatingMen', v)}
                slopeValue={values.back9SlopeRatingMen}
                slopeError={errors.back9_slope_rating_men}
                onSlopeChange={(v) => update('back9SlopeRatingMen', v)}
                slopePlaceholder="137"
              />
            </>
          )}
        </div>
      </div>

      <div>
        <p className="label">Women's rating</p>
        <div className="space-y-2">
          <div className="grid grid-cols-[3.5rem_1fr_1fr] gap-2 text-xs text-slate-500 dark:text-slate-400">
            <span />
            <span>Course rating</span>
            <span>Slope</span>
          </div>
          {is9Holes ? (
            <RatingRow
              idPrefix={`${idPrefix}-women-9`}
              label="Rating"
              courseValue={values.front9CourseRatingWomen}
              courseError={errors.front9_course_rating_women}
              courseMin={25}
              courseMax={45}
              coursePlaceholder="37.0"
              onCourseChange={(v) => update('front9CourseRatingWomen', v)}
              slopeValue={values.front9SlopeRatingWomen}
              slopeError={errors.front9_slope_rating_women}
              onSlopeChange={(v) => update('front9SlopeRatingWomen', v)}
              slopePlaceholder="138"
            />
          ) : (
            <>
              <RatingRow
                idPrefix={`${idPrefix}-women-18`}
                label="18 holes"
                courseValue={values.courseRatingWomen}
                courseError={errors.course_rating_women}
                courseMin={50}
                courseMax={90}
                coursePlaceholder="74.8"
                onCourseChange={(v) => update('courseRatingWomen', v)}
                slopeValue={values.slopeRatingWomen}
                slopeError={errors.slope_rating_women}
                onSlopeChange={(v) => update('slopeRatingWomen', v)}
                slopePlaceholder="140"
              />
              <RatingRow
                idPrefix={`${idPrefix}-women-front9`}
                label="Front 9"
                courseValue={values.front9CourseRatingWomen}
                courseError={errors.front9_course_rating_women}
                courseMin={25}
                courseMax={45}
                coursePlaceholder="37.0"
                onCourseChange={(v) => update('front9CourseRatingWomen', v)}
                slopeValue={values.front9SlopeRatingWomen}
                slopeError={errors.front9_slope_rating_women}
                onSlopeChange={(v) => update('front9SlopeRatingWomen', v)}
                slopePlaceholder="138"
              />
              <RatingRow
                idPrefix={`${idPrefix}-women-back9`}
                label="Back 9"
                courseValue={values.back9CourseRatingWomen}
                courseError={errors.back9_course_rating_women}
                courseMin={25}
                courseMax={45}
                coursePlaceholder="37.8"
                onCourseChange={(v) => update('back9CourseRatingWomen', v)}
                slopeValue={values.back9SlopeRatingWomen}
                slopeError={errors.back9_slope_rating_women}
                onSlopeChange={(v) => update('back9SlopeRatingWomen', v)}
                slopePlaceholder="142"
              />
            </>
          )}
        </div>
      </div>
    </div>
  )
}

/** A modal for adding or editing a single tee on the course detail screen. */
export function TeeDialog({
  title,
  initial,
  submitLabel,
  holeCount,
  onCancel,
  onSubmit,
}: {
  title: string
  initial: TeeFormValues
  submitLabel: string
  holeCount: number
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
      await onSubmit(teeFormToPayload(values, holeCount))
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
          <TeeFields
            values={values}
            errors={errors}
            onChange={setValues}
            idPrefix="tee-dialog"
            holeCount={holeCount}
          />
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
