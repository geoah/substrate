/** The temporal point a kind binds, a query rewritten onto it, and the order
 * grammar the rewrite has to read. An `implements` query names `at` because
 * the trait does; a task binds the point to `dueAt` (`temporal(point:
 * dueAt)`), an event keeps `at` (`temporal(range)`), and the engine resolves
 * each kind's own column, so the fan-out rewrites `at` in the filter and the
 * order to each kind's bound point before it asks. A query that never names
 * `at` is not a temporal one and passes through verbatim. */

import type { RecordFilter } from "@/lib/api/types"
import type { KindInfo } from "@/lib/api/types"
import { temporalProperties } from "@/lib/definition"
import { ERROR, RpcError } from "./bridge/protocol"

/** The trait's own name for the point, which a query names. */
export const AT = "at"

/** The property the kind's temporal trait binds the point to: `dueAt` for a
 * remapped point, `at` for a plain point and for a range (whose start is
 * `at`), and `at` again for a kind that binds nothing, so the rewrite is the
 * identity there. */
export function boundPoint(kind: KindInfo): string {
  return temporalProperties(kind)[0] ?? AT
}

/** One sort key of the wire's `orderBy`. */
export interface OrderKey {
  property: string
  desc: boolean
}

/** `orderBy` in the wire's compact spelling (`dueAt:asc,name:desc`; a bare
 * name is ascending), parsed. A direction that is neither is refused here
 * with the server's own words, before a fan-out asks N collections and
 * collects N refusals. */
export function parseOrderBy(orderBy?: string): OrderKey[] {
  const keys: OrderKey[] = []
  for (const part of (orderBy ?? "").split(",")) {
    const term = part.trim()
    if (!term) continue
    const colon = term.indexOf(":")
    const property = (colon < 0 ? term : term.slice(0, colon)).trim()
    const dir = (colon < 0 ? "" : term.slice(colon + 1)).trim().toLowerCase()
    if (!property) {
      throw new RpcError(ERROR.invalidParams, "orderBy: a key names a property")
    }
    if (dir !== "" && dir !== "asc" && dir !== "desc") {
      throw new RpcError(
        ERROR.invalidParams,
        "orderBy: direction must be asc or desc"
      )
    }
    keys.push({ property, desc: dir === "desc" })
  }
  return keys
}

export function renderOrderBy(keys: OrderKey[]): string {
  return keys.map((k) => `${k.property}:${k.desc ? "desc" : "asc"}`).join(",")
}

export interface TemporalQuery {
  filter?: RecordFilter
  orderBy?: string
}

/** `at` in `filter.properties` and in `orderBy` moved onto the kind's bound
 * point; everything else verbatim, the order's spelling included when no key
 * moved, so a query key is not rewritten for nothing. */
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
    const keys = parseOrderBy(orderBy)
    if (keys.some((k) => k.property === AT)) {
      orderBy = renderOrderBy(
        keys.map((k) => (k.property === AT ? { ...k, property: point } : k))
      )
    }
  }
  return { ...query, filter, orderBy }
}
