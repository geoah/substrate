/** A route's search value as text.
 *
 * The router JSON-parses every search value it reads, so `?q=2024` arrives as
 * the number 2024, `?q=true` as a boolean, `?q=null` as null and `?q=[1,2]`
 * as an array. A validator that keeps only strings drops those, and the
 * router then rewrites the address without them: a search for a year, a
 * digit-only id or a prompt written as JSON is gone after a reload. Every key
 * a page reads as text is taken through here instead.
 *
 * The text is the value's JSON spelling, which is not always what was typed:
 * a number not written in JSON's shortest form (`12.50`, `1.0`, `1e3`, or an
 * integer past 2^53) has already been parsed, so it comes back as `12.5`,
 * `1`, `1000` or the rounded integer. Only a router-wide parseSearch that
 * leaves values as text would keep those. */

import { scalarText } from "@/lib/format"

export function searchText(value: unknown): string | undefined {
  if (value === null) return "null"
  if (typeof value === "object") return JSON.stringify(value)
  return scalarText(value) || undefined
}
