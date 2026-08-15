import { useState } from 'react'
import type { Club, ClubPayload } from '../types'
import { Alert, Field, Spinner, cx } from './ui'

/**
 * Display names for the club types the server knows about. Anything the server
 * adds that is missing here still renders — see clubTypeLabel — so this map is
 * a nicety rather than a second source of truth.
 */
const TYPE_LABELS: Record<string, string> = {
  driver: 'Driver',
  wood: 'Fairway wood',
  hybrid: 'Hybrid',
  iron: 'Iron',
  wedge: 'Wedge',
  putter: 'Putter',
}

const FLEX_LABELS: Record<string, string> = {
  ladies: 'Ladies (L)',
  senior: 'Senior (A)',
  regular: 'Regular (R)',
  stiff: 'Stiff (S)',
  'x-stiff': 'Extra stiff (X)',
}

/** Falls back to title-casing so an unknown value from the server still reads well. */
export function clubTypeLabel(value: string): string {
  return TYPE_LABELS[value] ?? value.charAt(0).toUpperCase() + value.slice(1)
}

export function flexLabel(value: string): string {
  return FLEX_LABELS[value] ?? value.charAt(0).toUpperCase() + value.slice(1)
}

/** An example label per type, to show the shape of a good one. */
const LABEL_PLACEHOLDERS: Record<string, string> = {
  driver: 'Driver',
  wood: '3 wood',
  hybrid: '4 hybrid',
  iron: '7 iron',
  wedge: '56° sand wedge',
  putter: 'Putter',
}

export interface ClubFormValues {
  clubType: string
  label: string
  loft: string
  brand: string
  model: string
  shaft: string
  flex: string
  notes: string
}

export function emptyClubForm(clubType = 'iron'): ClubFormValues {
  return { clubType, label: '', loft: '', brand: '', model: '', shaft: '', flex: '', notes: '' }
}

export function clubToForm(club: Club): ClubFormValues {
  return {
    clubType: club.club_type,
    label: club.label,
    loft: club.loft === null ? '' : String(club.loft),
    brand: club.brand ?? '',
    model: club.model ?? '',
    shaft: club.shaft ?? '',
    flex: club.flex ?? '',
    notes: club.notes ?? '',
  }
}

/** Blank optional strings become null, which is how the server clears a field. */
function optional(value: string): string | null {
  const trimmed = value.trim()
  return trimmed === '' ? null : trimmed
}

export function clubFormToPayload(values: ClubFormValues): ClubPayload {
  const loft = values.loft.trim()
  return {
    club_type: values.clubType,
    label: values.label.trim(),
    brand: optional(values.brand),
    model: optional(values.model),
    loft: loft === '' ? null : Number(loft),
    shaft: optional(values.shaft),
    flex: optional(values.flex),
    notes: optional(values.notes),
  }
}

/** A modal for adding or editing one club. */
export function ClubDialog({
  title,
  initial,
  submitLabel,
  clubTypes,
  flexes,
  onCancel,
  onSubmit,
}: {
  title: string
  initial: ClubFormValues
  submitLabel: string
  clubTypes: string[]
  flexes: string[]
  onCancel: () => void
  onSubmit: (payload: ClubPayload) => Promise<void>
}) {
  const [values, setValues] = useState<ClubFormValues>(initial)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  function update<K extends keyof ClubFormValues>(key: K, value: ClubFormValues[K]) {
    setValues((current) => ({ ...current, [key]: value }))
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setErrors({})
    setFormError(null)
    try {
      await onSubmit(clubFormToPayload(values))
    } catch (error) {
      const apiError = error as { fields?: Record<string, string>; message?: string }
      if (apiError.fields && Object.keys(apiError.fields).length > 0) setErrors(apiError.fields)
      else setFormError(apiError.message ?? 'Could not save the club.')
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
          <div>
            <label htmlFor="club-type" className="label">
              Type
            </label>
            <select
              id="club-type"
              value={values.clubType}
              onChange={(e) => update('clubType', e.target.value)}
              className={cx('input', errors.club_type && 'input-error')}
            >
              {clubTypes.map((type) => (
                <option key={type} value={type}>
                  {clubTypeLabel(type)}
                </option>
              ))}
            </select>
            {errors.club_type && <p className="field-error">{errors.club_type}</p>}
          </div>

          <Field
            id="club-label"
            label="Label"
            value={values.label}
            onChange={(e) => update('label', e.target.value)}
            error={errors.label}
            placeholder={LABEL_PLACEHOLDERS[values.clubType] ?? '7 iron'}
            hint="How this club shows up when you record a shot."
            required
            maxLength={60}
          />

          <Field
            id="club-loft"
            label="Loft (degrees)"
            type="number"
            inputMode="decimal"
            step="0.5"
            min={0}
            max={75}
            value={values.loft}
            onChange={(e) => update('loft', e.target.value)}
            error={errors.loft}
            placeholder="56"
          />

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field
              id="club-brand"
              label="Brand"
              value={values.brand}
              onChange={(e) => update('brand', e.target.value)}
              error={errors.brand}
              placeholder="Titleist"
              maxLength={60}
            />
            <Field
              id="club-model"
              label="Model"
              value={values.model}
              onChange={(e) => update('model', e.target.value)}
              error={errors.model}
              placeholder="Vokey SM9"
              maxLength={60}
            />
          </div>

          <Field
            id="club-shaft"
            label="Shaft"
            value={values.shaft}
            onChange={(e) => update('shaft', e.target.value)}
            error={errors.shaft}
            placeholder="Project X 6.0"
            maxLength={120}
          />

          <div>
            <label htmlFor="club-flex" className="label">
              Flex
            </label>
            <select
              id="club-flex"
              value={values.flex}
              onChange={(e) => update('flex', e.target.value)}
              className={cx('input', errors.flex && 'input-error')}
            >
              <option value="">Not set</option>
              {flexes.map((flex) => (
                <option key={flex} value={flex}>
                  {flexLabel(flex)}
                </option>
              ))}
            </select>
            {errors.flex && <p className="field-error">{errors.flex}</p>}
          </div>

          <div>
            <label htmlFor="club-notes" className="label">
              Notes
            </label>
            <textarea
              id="club-notes"
              value={values.notes}
              onChange={(e) => update('notes', e.target.value)}
              className={cx('input', errors.notes && 'input-error')}
              rows={2}
              maxLength={2000}
              placeholder="Regripped spring 2026"
            />
            {errors.notes && <p className="field-error">{errors.notes}</p>}
          </div>

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
