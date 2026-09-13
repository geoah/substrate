/** A read over several kinds, done by the host: one list per kind with the
 * query rewritten onto that kind's temporal point, the pages merged and
 * sorted by the point, and `incomplete` when any page still had a cursor.
 * There is no merged cursor: a fan-out is one page per kind and says so.
 * When the trait records route takes `from`, `to` and `first` this collapses
 * to one request and no app changes. */

import type { ListParams } from "@/lib/api/records"
import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { splitKind } from "@/lib/definition"
import type { ListArguments, ListResult } from "./bridge/protocol"
import { boundPoint, pointOrder, rewriteForKind } from "./temporal"

export interface Read {
  kind: KindInfo
  params: ListParams
}

/** The list read for one kind, the query rewritten onto its point. */
export function readFor(query: ListArguments, kind: KindInfo): Read {
  const { authority, pkg, name } = splitKind(kind.identity)
  const rewritten = rewriteForKind(
    { filter: query.filter, orderBy: query.orderBy },
    kind
  )
  return {
    kind,
    params: {
      authority,
      package: pkg,
      name,
      first: query.first,
      after: query.after,
      filter: rewritten.filter,
      orderBy: rewritten.orderBy,
    },
  }
}

export function readsFor(query: ListArguments, kinds: KindInfo[]): Read[] {
  return kinds.map((kind) => readFor(query, kind))
}

function pointMs(record: SubstrateRecord, point: string): number {
  const t = Date.parse(String(record.properties[point] ?? ""))
  return Number.isNaN(t) ? Infinity : t
}

/** The pages of a fan-out as one result. A single read passes through with
 * its cursor; several merge, sort by each record's own point, and report
 * `incomplete` instead of a cursor. */
export function mergePages(
  reads: Read[],
  pages: (Page | ListResult | undefined)[],
  orderBy?: string
): ListResult {
  if (reads.length === 1) {
    const page = pages[0]
    return {
      records: page?.records ?? [],
      cursor: page?.cursor,
      head: page?.head,
    }
  }
  const records: SubstrateRecord[] = []
  let incomplete = false
  let head: number | undefined
  pages.forEach((page) => {
    if (!page) return
    records.push(...(page.records ?? []))
    if (page.cursor) incomplete = true
    if (page.head !== undefined) head = Math.max(head ?? 0, page.head)
  })
  const pointOf = new Map(
    reads.map((r) => [r.kind.identity, boundPoint(r.kind)])
  )
  const sign = pointOrder(orderBy) === "desc" ? -1 : 1
  records.sort(
    (a, b) =>
      sign *
      (pointMs(a, pointOf.get(a.kind) ?? "at") -
        pointMs(b, pointOf.get(b.kind) ?? "at"))
  )
  return incomplete ? { records, head, incomplete } : { records, head }
}

/** One read per kind, in parallel, merged. */
export async function runImplements(
  query: ListArguments,
  kinds: KindInfo[],
  list: (params: ListParams) => Promise<Page>
): Promise<ListResult> {
  const reads = readsFor(query, kinds)
  const pages = await Promise.all(reads.map((r) => list(r.params)))
  return mergePages(reads, pages, query.orderBy)
}
