import type { CourseLocation } from '../types'

/** Trims, drops the parts that are not set, and joins what is left. */
function join(parts: Array<string | null | undefined>, separator = ', '): string {
  return parts
    .map((part) => part?.trim() ?? '')
    .filter((part) => part !== '')
    .join(separator)
}

/**
 * "City, State, Country" — how a course is identified in a list.
 *
 * Street and postal code are deliberately left out. This runs beside a course
 * name in search results and on directory cards, where the question being
 * answered is "which Pine Valley is this?", and a street number answers that
 * no better than the town does while costing a line of width.
 */
export function formatCourseLocation(course: CourseLocation): string {
  return join([course.city, course.region, course.country])
}

/**
 * The full postal address on one line, the way it would be written on an
 * envelope: "1831 Johnson St., Marne, MI 49435, USA".
 *
 * The region and postal code join with a space rather than a comma because
 * that is the convention in every country that uses both, and it is also what
 * Google Maps parses most reliably — this string is the map query.
 */
export function formatCourseAddress(course: CourseLocation): string {
  const regionLine = join([course.region, course.postal_code], ' ')
  return join([course.street, course.city, regionLine, course.country])
}

/** Whether a course has any address at all, worth rendering a line for. */
export function hasLocation(course: CourseLocation): boolean {
  return formatCourseAddress(course) !== ''
}
