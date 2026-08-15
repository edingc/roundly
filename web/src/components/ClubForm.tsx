import { useState } from 'react'
import type { Club, ClubPayload } from '../types'
import { useDistanceUnit } from '../lib/auth'
import {
  boundFromYards,
  fromYards,
  toYards,
  unitLabel,
  unitSuffix,
  type DistanceUnit,
} from '../lib/units'
import { Alert, Field, Spinner, cx } from './ui'

/** Carry and dispersion bounds, in the yards the server validates against. */
const CARRY_MIN_YARDS = 1
const CARRY_MAX_YARDS = 400
const DISPERSION_MIN_YARDS = 0
const DISPERSION_MAX_YARDS = 150

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
  'x-stiff': 'Extra Stiff (X)',
  wedge: 'Wedge (W)',
}

/**
 * Whether a club type gets the full spec set: loft, flex, expected carry, and
 * average dispersion. Only a putter does not.
 *
 * Two different reasons are bundled here, and they differ in how hard the rule
 * is. Carry and dispersion describe a full shot, which a putter never hits, so
 * the *server* rejects them on one. Loft and flex are real on a putter — 3.5°
 * is a genuine spec — but not worth the form space, so they are merely hidden
 * here and stay perfectly valid over the API.
 *
 * Kept as one helper so the form and the payload cannot disagree about which
 * fields a putter shows.
 */
export function hasFullSpecs(clubType: string): boolean {
  return clubType !== 'putter'
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
  expectedCarry: string
  averageDispersion: string
}

export function emptyClubForm(clubType = 'iron'): ClubFormValues {
  return {
    clubType,
    label: '',
    loft: '',
    brand: '',
    model: '',
    shaft: '',
    flex: '',
    notes: '',
    expectedCarry: '',
    averageDispersion: '',
  }
}

export function clubToForm(club: Club, unit: DistanceUnit): ClubFormValues {
  return {
    clubType: club.club_type,
    label: club.label,
    loft: club.loft === null ? '' : String(club.loft),
    brand: club.brand ?? '',
    model: club.model ?? '',
    shaft: club.shaft ?? '',
    flex: club.flex ?? '',
    notes: club.notes ?? '',
    // Stored in yards; shown in whatever the user reads.
    expectedCarry:
      club.expected_carry === null ? '' : String(fromYards(club.expected_carry, unit)),
    averageDispersion:
      club.average_dispersion === null ? '' : String(fromYards(club.average_dispersion, unit)),
  }
}

/** Blank optional strings become null, which is how the server clears a field. */
function optional(value: string): string | null {
  const trimmed = value.trim()
  return trimmed === '' ? null : trimmed
}

/** Blank numeric fields become null; anything else is sent as a number. */
function optionalNumber(value: string): number | null {
  const trimmed = value.trim()
  return trimmed === '' ? null : Number(trimmed)
}

export function clubFormToPayload(values: ClubFormValues, unit: DistanceUnit): ClubPayload {
  // A putter sends null for every hidden field regardless of what the inputs
  // still hold. For carry and dispersion this is required — the server rejects
  // them on a putter, so re-typing a club would otherwise fail validation on
  // values the form is no longer showing. For loft and flex it is a choice: a
  // club converted to a putter should not keep an invisible 10.5° and a stiff
  // shaft that resurface if it is converted back.
  const full = hasFullSpecs(values.clubType)
  return {
    club_type: values.clubType,
    label: values.label.trim(),
    brand: optional(values.brand),
    model: optional(values.model),
    loft: full ? optionalNumber(values.loft) : null,
    shaft: optional(values.shaft),
    flex: full ? optional(values.flex) : null,
    notes: optional(values.notes),
    // Converted back to the yards the API stores.
    expected_carry: full ? toStoredYards(values.expectedCarry, unit) : null,
    average_dispersion: full ? toStoredYards(values.averageDispersion, unit) : null,
  }
}

/** A blank distance field is null; anything else converts to stored yards. */
function toStoredYards(value: string, unit: DistanceUnit): number | null {
  const parsed = optionalNumber(value)
  return parsed === null ? null : toYards(parsed, unit)
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
  const unit = useDistanceUnit()
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
      await onSubmit(clubFormToPayload(values, unit))
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

          {hasFullSpecs(values.clubType) && (
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
          )}

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

          {hasFullSpecs(values.clubType) && (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field
                id="club-carry"
                label="Expected carry"
                type="number"
                inputMode="numeric"
                min={boundFromYards(CARRY_MIN_YARDS, unit, 'min')}
                max={boundFromYards(CARRY_MAX_YARDS, unit, 'max')}
                value={values.expectedCarry}
                onChange={(e) => update('expectedCarry', e.target.value)}
                error={errors.expected_carry}
                placeholder={String(fromYards(158, unit))}
                suffix={unitSuffix(unit)}
                aria-label={`Expected carry in ${unitLabel(unit).toLowerCase()}`}
              />
              <Field
                id="club-dispersion"
                label="Average dispersion"
                type="number"
                inputMode="numeric"
                min={boundFromYards(DISPERSION_MIN_YARDS, unit, 'min')}
                max={boundFromYards(DISPERSION_MAX_YARDS, unit, 'max')}
                value={values.averageDispersion}
                onChange={(e) => update('averageDispersion', e.target.value)}
                error={errors.average_dispersion}
                placeholder={String(fromYards(12, unit))}
                suffix={unitSuffix(unit)}
                aria-label={`Average dispersion in ${unitLabel(unit).toLowerCase()}`}
              />
            </div>
          )}

          <Field
            id="club-shaft"
            label="Shaft"
            value={values.shaft}
            onChange={(e) => update('shaft', e.target.value)}
            error={errors.shaft}
            placeholder="Project X 6.0"
            maxLength={120}
          />

          {hasFullSpecs(values.clubType) && (
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
          )}

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
