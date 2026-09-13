/** The temporal point a kind binds, and a query rewritten onto it. An
 * `implements` query names `at` because the trait does; a task binds the
 * point to `dueAt` (`temporal(point: dueAt)`), an event keeps `at`
 * (`temporal(range)`), and the engine resolves each kind's own column, so the
 * fan-out rewrites `at` in the filter and the order to each kind's bound
 * point before it asks. */

import type { RecordFilter } from "@/lib/api/types"
import type { KindInfo } from "@/lib/api/types"
import { temporalProperties } from "@/lib/definition"

/** The trait's own name for the point, which a query names. */
export const AT = "at"

/** The property the kind's temporal trait binds the point to: `dueAt` for a
 * remapped point, `at` for a plain point and for a range (whose start is
 * `at`), and `at` again for a kind that binds nothing, so the rewrite is the
 * identity there. */
export function boundPoint(kind: KindInfo): string {
  return temporalProperties(kind)[0] ?? AT
}

export interface TemporalQuery {
  filter?: RecordFilter
  orderBy?: string
}

/** `at` in `filter.properties` and in `orderBy` moved onto the kind's bound
 * point; everything else verbatim. */
export function rewriteForKind<Q extends TemporalQuery>(
  query: Q,
  kind: KindInfo
): Q {
  const point = boundPoint(kind)
  if (point === AT) return query
  let filter = query.filter
  if (filter?.properties && AT in filter.properties) {
    const { [AT]: cond, ...rest } = filter.properties
    filter = { ...filter, properties: { ...rest, [point]: cond } }
  }
  let orderBy = query.orderBy
  if (orderBy) {
    const [key, dir] = orderBy.split(":")
    if (key === AT) orderBy = dir ? `${point}:${dir}` : point
  }
  return { ...query, filter, orderBy }
}

/** The order an `implements` merge sorts by: the direction of `orderBy` when
 * it names the point, ascending otherwise. */
export function pointOrder(orderBy?: string): "asc" | "desc" {
  const [key, dir] = (orderBy ?? "").split(":")
  return key === AT && dir === "desc" ? "desc" : "asc"
}
