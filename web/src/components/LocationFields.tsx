import type { CourseLocation } from '../types'
import { countrySuggestions, regionSuggestions } from '../lib/places'
import { useAuth } from '../lib/auth'
import { Field } from './ui'

/**
 * The address plus the point it resolves to. Coordinates live here rather than
 * beside them on each page because they are the same fact in machine-readable
 * form, and because the "leave these blank to have them filled in" rule needs
 * to be stated once, next to the thing it is about.
 */
export interface CoursePlace extends CourseLocation {
  latitude: number | null
  longitude: number | null
}

/**
 * The address half of a course form, shared by "Add a course" and the detail
 * page's edit form so the two cannot drift apart.
 *
 * Values are held as strings rather than `string | null` for the same reason
 * TeeFormValues are: a controlled input's value is a string, and mapping null
 * in and out at every keystroke buys nothing. The conversion happens once, in
 * locationFormToPayload.
 */
export interface LocationFormValues {
  street: string
  city: string
  region: string
  postalCode: string
  country: string
  latitude: string
  longitude: string
}

export function emptyLocationForm(): LocationFormValues {
  return { street: '', city: '', region: '', postalCode: '', country: '', latitude: '', longitude: '' }
}

export function locationToForm(course: CoursePlace): LocationFormValues {
  return {
    street: course.street ?? '',
    city: course.city ?? '',
    region: course.region ?? '',
    postalCode: course.postal_code ?? '',
    country: course.country ?? '',
    latitude: course.latitude != null ? String(course.latitude) : '',
    longitude: course.longitude != null ? String(course.longitude) : '',
  }
}

/** Blank means "not set", which the API stores as null rather than "". */
function orNull(value: string): string | null {
  const trimmed = value.trim()
  return trimmed === '' ? null : trimmed
}

/**
 * Blank coordinates are sent as null rather than omitted, and that is load
 * bearing: null is what tells the server to place this course from its address.
 */
function numberOrNull(value: string): number | null {
  const trimmed = value.trim()
  return trimmed === '' ? null : Number(trimmed)
}

export function locationFormToPayload(values: LocationFormValues): CoursePlace {
  return {
    street: orNull(values.street),
    city: orNull(values.city),
    region: orNull(values.region),
    postal_code: orNull(values.postalCode),
    country: orNull(values.country),
    latitude: numberOrNull(values.latitude),
    longitude: numberOrNull(values.longitude),
  }
}

/**
 * Street gets its own full-width row because it is the longest line by far;
 * the four short parts pair up beneath it. Every one of them is optional — a
 * course whose town is all anyone knows is still worth having in the
 * directory, so nothing here is marked required.
 */
export function LocationFields({
  values,
  errors,
  onChange,
  idPrefix,
}: {
  values: LocationFormValues
  errors: Record<string, string>
  onChange: (values: LocationFormValues) => void
  idPrefix: string
}) {
  const { geocodingEnabled } = useAuth()

  function set<K extends keyof LocationFormValues>(key: K, value: string) {
    onChange({ ...values, [key]: value })
  }

  return (
    <>
      <Field
        id={`${idPrefix}-street`}
        label="Street address"
        value={values.street}
        onChange={(e) => set('street', e.target.value)}
        error={errors.street}
        maxLength={240}
        placeholder="Optional"
        autoComplete="off"
      />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field
          id={`${idPrefix}-city`}
          label="City"
          value={values.city}
          onChange={(e) => set('city', e.target.value)}
          error={errors.city}
          maxLength={80}
          placeholder="Optional"
          autoComplete="off"
        />
        <Field
          id={`${idPrefix}-region`}
          label="State or province"
          value={values.region}
          onChange={(e) => set('region', e.target.value)}
          error={errors.region}
          // Suggestions follow whatever country is typed beside this, and go
          // away entirely for a country whose subdivisions are not bundled —
          // at which point this is the plain text box it has always been.
          options={regionSuggestions(values.country)}
          maxLength={80}
          placeholder="Optional"
          autoComplete="off"
        />
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Field
          id={`${idPrefix}-postal-code`}
          label="ZIP or postal code"
          value={values.postalCode}
          onChange={(e) => set('postalCode', e.target.value)}
          error={errors.postal_code}
          maxLength={20}
          placeholder="Optional"
          autoComplete="off"
        />
        <Field
          id={`${idPrefix}-country`}
          label="Country"
          value={values.country}
          onChange={(e) => set('country', e.target.value)}
          error={errors.country}
          options={countrySuggestions()}
          maxLength={80}
          placeholder="Optional"
          autoComplete="off"
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <Field
          id={`${idPrefix}-latitude`}
          label="Latitude"
          type="number"
          value={values.latitude}
          onChange={(e) => set('latitude', e.target.value)}
          error={errors.latitude}
          placeholder={geocodingEnabled ? 'From the address' : 'Optional'}
          step="any"
          min={-90}
          max={90}
        />
        <Field
          id={`${idPrefix}-longitude`}
          label="Longitude"
          type="number"
          value={values.longitude}
          onChange={(e) => set('longitude', e.target.value)}
          error={errors.longitude}
          placeholder={geocodingEnabled ? 'From the address' : 'Optional'}
          step="any"
          min={-180}
          max={180}
        />
      </div>

      {/* Only shown where it is true. An instance with no geocoder configured
          leaves these two as the hand-typed fields they have always been —
          and owes OpenStreetMap no attribution, having used none of its data. */}
      {geocodingEnabled && (
        <p className="text-xs text-slate-500 dark:text-slate-400">
          Left blank, these are filled in from the address when the course is saved. Clear them
          and save again to re-place a course that has moved. Geocoding by{' '}
          <a
            href="https://www.openstreetmap.org/copyright"
            target="_blank"
            rel="noreferrer"
            className="underline hover:text-accent-700 dark:hover:text-accent-300"
          >
            OpenStreetMap
          </a>{' '}
          contributors.
        </p>
      )}
    </>
  )
}
