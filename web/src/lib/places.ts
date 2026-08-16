/**
 * Suggestion lists for the country and state/province fields.
 *
 * These back a `<datalist>`, not a `<select>`, and the distinction is the whole
 * point. Migration 00010 chose free text for exactly one reason: every
 * structured alternative either ships a country list that goes stale or refuses
 * an address someone actually lives at. A datalist keeps that promise — anything
 * can still be typed — while removing the tedium and the four spellings of
 * "USA" that free text on its own invites.
 *
 * So the stored value stays a plain name or abbreviation, and nothing in the
 * schema, the API, or `formatCourseLocation` had to change to add this.
 */

/** One suggestion. `value` is inserted; `label` is the browser's hint beside it. */
export interface PlaceOption {
  value: string
  label?: string
}

/**
 * ISO 3166-1 alpha-2. Only the codes are listed — the names come from
 * `Intl.DisplayNames`, so they arrive in the reader's own language and stay
 * current with the browser rather than with this file.
 */
const COUNTRY_CODES = [
  'AD', 'AE', 'AF', 'AG', 'AI', 'AL', 'AM', 'AO', 'AQ', 'AR', 'AS', 'AT', 'AU', 'AW', 'AX', 'AZ',
  'BA', 'BB', 'BD', 'BE', 'BF', 'BG', 'BH', 'BI', 'BJ', 'BL', 'BM', 'BN', 'BO', 'BQ', 'BR', 'BS',
  'BT', 'BV', 'BW', 'BY', 'BZ', 'CA', 'CC', 'CD', 'CF', 'CG', 'CH', 'CI', 'CK', 'CL', 'CM', 'CN',
  'CO', 'CR', 'CU', 'CV', 'CW', 'CX', 'CY', 'CZ', 'DE', 'DJ', 'DK', 'DM', 'DO', 'DZ', 'EC', 'EE',
  'EG', 'EH', 'ER', 'ES', 'ET', 'FI', 'FJ', 'FK', 'FM', 'FO', 'FR', 'GA', 'GB', 'GD', 'GE', 'GF',
  'GG', 'GH', 'GI', 'GL', 'GM', 'GN', 'GP', 'GQ', 'GR', 'GS', 'GT', 'GU', 'GW', 'GY', 'HK', 'HM',
  'HN', 'HR', 'HT', 'HU', 'ID', 'IE', 'IL', 'IM', 'IN', 'IO', 'IQ', 'IR', 'IS', 'IT', 'JE', 'JM',
  'JO', 'JP', 'KE', 'KG', 'KH', 'KI', 'KM', 'KN', 'KP', 'KR', 'KW', 'KY', 'KZ', 'LA', 'LB', 'LC',
  'LI', 'LK', 'LR', 'LS', 'LT', 'LU', 'LV', 'LY', 'MA', 'MC', 'MD', 'ME', 'MF', 'MG', 'MH', 'MK',
  'ML', 'MM', 'MN', 'MO', 'MP', 'MQ', 'MR', 'MS', 'MT', 'MU', 'MV', 'MW', 'MX', 'MY', 'MZ', 'NA',
  'NC', 'NE', 'NF', 'NG', 'NI', 'NL', 'NO', 'NP', 'NR', 'NU', 'NZ', 'OM', 'PA', 'PE', 'PF', 'PG',
  'PH', 'PK', 'PL', 'PM', 'PN', 'PR', 'PS', 'PT', 'PW', 'PY', 'QA', 'RE', 'RO', 'RS', 'RU', 'RW',
  'SA', 'SB', 'SC', 'SD', 'SE', 'SG', 'SH', 'SI', 'SJ', 'SK', 'SL', 'SM', 'SN', 'SO', 'SR', 'SS',
  'ST', 'SV', 'SX', 'SY', 'SZ', 'TC', 'TD', 'TF', 'TG', 'TH', 'TJ', 'TK', 'TL', 'TM', 'TN', 'TO',
  'TR', 'TT', 'TV', 'TW', 'TZ', 'UA', 'UG', 'UM', 'US', 'UY', 'UZ', 'VA', 'VC', 'VE', 'VG', 'VI',
  'VN', 'VU', 'WF', 'WS', 'YE', 'YT', 'ZA', 'ZM', 'ZW',
]

/**
 * Falls back to the raw code on a browser without `Intl.DisplayNames`, or for a
 * code it does not recognise. A list of codes is worse than a list of names and
 * better than a crash.
 */
function countryName(code: string): string {
  try {
    return new Intl.DisplayNames(undefined, { type: 'region' }).of(code) ?? code
  } catch {
    return code
  }
}

/**
 * Countries as suggestions, sorted the way the reader's locale sorts them.
 *
 * Built once on first use rather than at module load: it is ~250 `Intl` calls,
 * and most screens never render a country field.
 */
let countryOptions: PlaceOption[] | null = null

export function countrySuggestions(): PlaceOption[] {
  if (!countryOptions) {
    countryOptions = COUNTRY_CODES.map((code) => ({ value: countryName(code) })).sort((a, b) =>
      a.value.localeCompare(b.value),
    )
  }
  return countryOptions
}

/** USPS abbreviations: the states, DC, and the inhabited territories. */
const US_REGIONS: PlaceOption[] = [
  { value: 'AL', label: 'Alabama' }, { value: 'AK', label: 'Alaska' },
  { value: 'AZ', label: 'Arizona' }, { value: 'AR', label: 'Arkansas' },
  { value: 'CA', label: 'California' }, { value: 'CO', label: 'Colorado' },
  { value: 'CT', label: 'Connecticut' }, { value: 'DE', label: 'Delaware' },
  { value: 'DC', label: 'District of Columbia' }, { value: 'FL', label: 'Florida' },
  { value: 'GA', label: 'Georgia' }, { value: 'HI', label: 'Hawaii' },
  { value: 'ID', label: 'Idaho' }, { value: 'IL', label: 'Illinois' },
  { value: 'IN', label: 'Indiana' }, { value: 'IA', label: 'Iowa' },
  { value: 'KS', label: 'Kansas' }, { value: 'KY', label: 'Kentucky' },
  { value: 'LA', label: 'Louisiana' }, { value: 'ME', label: 'Maine' },
  { value: 'MD', label: 'Maryland' }, { value: 'MA', label: 'Massachusetts' },
  { value: 'MI', label: 'Michigan' }, { value: 'MN', label: 'Minnesota' },
  { value: 'MS', label: 'Mississippi' }, { value: 'MO', label: 'Missouri' },
  { value: 'MT', label: 'Montana' }, { value: 'NE', label: 'Nebraska' },
  { value: 'NV', label: 'Nevada' }, { value: 'NH', label: 'New Hampshire' },
  { value: 'NJ', label: 'New Jersey' }, { value: 'NM', label: 'New Mexico' },
  { value: 'NY', label: 'New York' }, { value: 'NC', label: 'North Carolina' },
  { value: 'ND', label: 'North Dakota' }, { value: 'OH', label: 'Ohio' },
  { value: 'OK', label: 'Oklahoma' }, { value: 'OR', label: 'Oregon' },
  { value: 'PA', label: 'Pennsylvania' }, { value: 'RI', label: 'Rhode Island' },
  { value: 'SC', label: 'South Carolina' }, { value: 'SD', label: 'South Dakota' },
  { value: 'TN', label: 'Tennessee' }, { value: 'TX', label: 'Texas' },
  { value: 'UT', label: 'Utah' }, { value: 'VT', label: 'Vermont' },
  { value: 'VA', label: 'Virginia' }, { value: 'WA', label: 'Washington' },
  { value: 'WV', label: 'West Virginia' }, { value: 'WI', label: 'Wisconsin' },
  { value: 'WY', label: 'Wyoming' },
  { value: 'AS', label: 'American Samoa' }, { value: 'GU', label: 'Guam' },
  { value: 'MP', label: 'Northern Mariana Islands' }, { value: 'PR', label: 'Puerto Rico' },
  { value: 'VI', label: 'U.S. Virgin Islands' },
]

/** Canada Post abbreviations: ten provinces and three territories. */
const CA_REGIONS: PlaceOption[] = [
  { value: 'AB', label: 'Alberta' }, { value: 'BC', label: 'British Columbia' },
  { value: 'MB', label: 'Manitoba' }, { value: 'NB', label: 'New Brunswick' },
  { value: 'NL', label: 'Newfoundland and Labrador' }, { value: 'NS', label: 'Nova Scotia' },
  { value: 'NT', label: 'Northwest Territories' }, { value: 'NU', label: 'Nunavut' },
  { value: 'ON', label: 'Ontario' }, { value: 'PE', label: 'Prince Edward Island' },
  { value: 'QC', label: 'Quebec' }, { value: 'SK', label: 'Saskatchewan' },
  { value: 'YT', label: 'Yukon' },
]

/**
 * Spellings that mean the same country. The canonical `Intl` name is checked
 * separately, so this only has to cover what a person types instead of it.
 */
const COUNTRY_ALIASES: Record<string, string[]> = {
  US: ['us', 'usa', 'u.s.', 'u.s.a.', 'united states', 'united states of america', 'america'],
  CA: ['ca', 'can', 'canada'],
}

function matchesCountry(typed: string, code: string): boolean {
  const normalized = typed.trim().toLowerCase()
  if (normalized === '') return false
  return (
    normalized === countryName(code).toLowerCase() ||
    (COUNTRY_ALIASES[code]?.includes(normalized) ?? false)
  )
}

/**
 * Suggestions for the state/province field, given whatever is in the country
 * field beside it.
 *
 * A blank country falls back to the US list, matching `lib/phone.ts` — this
 * app assumes domestic until told otherwise, and every course already in the
 * directory is American. Any other country returns undefined, which renders a
 * plain text input: the alternative is shipping the world's subdivisions and
 * keeping them current, which is the staleness migration 00010 refused.
 */
export function regionSuggestions(country: string): PlaceOption[] | undefined {
  if (country.trim() === '' || matchesCountry(country, 'US')) return US_REGIONS
  if (matchesCountry(country, 'CA')) return CA_REGIONS
  return undefined
}
