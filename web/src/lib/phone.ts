import { parsePhoneNumberWithError } from 'libphonenumber-js/min'

/**
 * Golf courses in this directory are assumed domestic unless the user types a
 * leading "+country code", which parsePhoneNumberWithError honors regardless
 * of this default.
 */
const DEFAULT_COUNTRY = 'US'

/**
 * Formats a phone number for display: national format for the default
 * country ("(555) 123-4567"), international format otherwise ("+44 20 7946
 * 0958"). Falls back to the trimmed input as typed when it cannot be parsed
 * as a valid number, so a partial or foreign format is never mangled.
 */
export function formatPhone(phone: string): string {
  const trimmed = phone.trim()
  if (trimmed === '') return trimmed

  try {
    const parsed = parsePhoneNumberWithError(trimmed, DEFAULT_COUNTRY)
    if (!parsed.isValid()) return trimmed
    return parsed.country === DEFAULT_COUNTRY ? parsed.formatNational() : parsed.formatInternational()
  } catch {
    return trimmed
  }
}

/** A tel: URI built from the canonical E.164 number, so the dialer gets an unambiguous number. */
export function phoneHref(phone: string): string {
  const trimmed = phone.trim()

  try {
    const parsed = parsePhoneNumberWithError(trimmed, DEFAULT_COUNTRY)
    if (parsed.isValid()) return `tel:${parsed.number}`
  } catch {
    // Falls through to the best-effort strip below.
  }
  // Unparseable input (e.g. an extension) still needs a href; strip everything
  // tel: doesn't tolerate but keep a leading "+" for a country code.
  return `tel:${trimmed.replace(/(?!^\+)[^0-9]/g, '')}`
}
