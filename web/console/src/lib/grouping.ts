/** Grouping a collection by one property: an enum, a state or a single
 * reference. The server does the work that paging needs: the list is ordered
 * by the grouped property first, so every page is contiguous groups and a
 * group that runs past the page's end carries on at the top of the next. The
 * page draws a head wherever the value changes, and each head's count is the
 * whole group's, from a bounded count over the view's filter narrowed to that
 * value, so it does not depend on where the page boundaries fall.
 *
 * The order of the groups is the server's: an enum and a state by their
 * stored value, a reference by the path it stores, the records with no value
 * last. */

import {
  readReference,
  type KindInfo,
  type RecordFilter,
} from "@/lib/api/types"
import { filterableProperties, type DeclaredProperty } from "@/lib/definition"
import { enumLabel } from "@/lib/grid-values"
import { lowerFirst, untitled } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"
import type { ReferenceTitles } from "@/lib/reference-titles"
import { stateWord } from "@/lib/state-words"

/** The properties a collection may be grouped by: every enum and state, and
 * every single, pinned or unpinned reference. A repeated value or a keyed map
 * puts one record in many groups, which a list ordered by it cannot page. */
export function groupableProperties(kind: KindInfo): DeclaredProperty[] {
  return filterableProperties(kind).filter(
    (p) =>
      !p.repeated &&
      !p.keyed &&
      (p.kind === "enum" || p.kind === "state" || p.kind === "reference")
  )
}

/** The group a record is in: the stored value, a reference's record path, or
 * "" for a record with no value. */
export function groupKeyOf(
  properties: Record<string, unknown>,
  property: DeclaredProperty
): string {
  const value = properties[property.name]
  if (property.kind === "reference") return readReference(value)?.path ?? ""
  return typeof value === "string" ? value : ""
}

/** The wire order for a grouped view: the grouped property first, the view's
 * own order within each group. Sorted by the grouped property itself, the
 * view's direction is the groups' direction. */
export function groupedOrderBy(
  sort: string,
  group: string | undefined
): string {
  if (!group) return sort
  const [property] = sort.split(":")
  if (property === group) return sort
  return `${group}:asc,${sort}`
}

/** The view's filter narrowed to one group, for that group's count. */
export function groupFilter(
  filter: RecordFilter | undefined,
  property: DeclaredProperty,
  key: string
): RecordFilter {
  const cond = !key
    ? { exists: false }
    : property.kind === "reference"
      ? { in: [key] }
      : { eq: key }
  return {
    ...filter,
    properties: { ...filter?.properties, [property.name]: cond },
  }
}

export interface GroupSegment<T> {
  key: string
  rows: T[]
}

/** The rows cut wherever the group changes, in order. */
export function groupSegments<T>(
  rows: readonly T[],
  keyOf: (row: T) => string
): GroupSegment<T>[] {
  const out: GroupSegment<T>[] = []
  for (const row of rows) {
    const key = keyOf(row)
    const last = out[out.length - 1]
    if (last && last.key === key) last.rows.push(row)
    else out.push({ key, rows: [row] })
  }
  return out
}

/** Under a tree, a row is in its top-level row's group, whatever it holds
 * itself, so a subtree is never split across heads. `depthOf` is the tree's
 * depth for a row id (0 or absent is the top level). */
export function treeGroupKeys<T extends { id: string }>(
  rows: readonly T[],
  depthOf: (id: string) => number | undefined,
  keyOf: (row: T) => string
): Map<string, string> {
  const out = new Map<string, string>()
  let current = ""
  for (const row of rows) {
    if (!depthOf(row.id)) current = keyOf(row)
    out.set(row.id, current)
  }
  return out
}

/** A group's value in words, for the fold control's name ("Priority: High"). */
export function groupWords(
  prop: DeclaredProperty,
  key: string,
  label: string,
  titles?: ReferenceTitles
): string {
  if (!key) return `No ${lowerFirst(label)}`
  if (prop.kind === "enum") return `${label}: ${enumLabel(prop, key)}`
  if (prop.kind === "state") return `${label}: ${stateWord(key)}`
  const target = splitRecordPath(key)
  const title =
    titles?.get(key) ?? (target ? untitled(target.kind) : undefined) ?? key
  return `${label}: ${title}`
}

/** Where a flat page's run of a group sits in the whole group, in words: the
 * page's first run began on an earlier page, its last runs on to the next.
 * Said only where the run holds fewer rows than the group's exact count, so a
 * whole group on one page says nothing. A run that fills a middle page may
 * have begun on it or ended on it, so there only the number of the group's
 * rows on other pages is certain. */
export function groupRunNote(
  at: { first: boolean; last: boolean; rows: number },
  count: { value: number; capped: boolean } | undefined,
  page: number,
  hasNext: boolean
): string | undefined {
  if (!count || count.capped || at.rows >= count.value) return undefined
  const before = at.first && page > 1
  const after = at.last && hasNext
  if (before && after)
    return `${(count.value - at.rows).toLocaleString()} more on other pages`
  if (before) return "continued from the previous page"
  if (after) return "continues on the next page"
  return undefined
}
