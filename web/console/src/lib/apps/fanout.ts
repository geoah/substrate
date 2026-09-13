/** A read over several kinds, done by the host: one list per kind with the
 * query rewritten onto that kind's temporal point where it names `at`, the
 * pages merged under the order the query asked for, and `incomplete` when any
 * page still had a cursor. There is no merged cursor: a fan-out is one page
 * per kind and says so. When the trait records route takes `from`, `to` and
 * `first` this collapses to one request and no app changes.
 *
 * The merge is the engine's order, done again across kinds: the keys of
 * `orderBy` in turn (`createdAt` newest-first when there are none), a null
 * after every value in either direction, and the `(kind, id)` tiebreak in
 * the leading key's direction, so the merged page is a strict total order
 * and two records the server would separate are separated here. What a key
 * compares as comes from the declarations, the way `orderExpr` reads them:
 * an `int`/`float`/`decimal` as a number, a `datetime`/`date` and every hot
 * column as an instant, a reference by the path it points at, anything else
 * as text. A key the kinds read differently has no one order, and is
 * refused before a request is made. */

import type { ListParams } from "@/lib/api/records"
import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { declaredProperties, splitKind } from "@/lib/definition"
import {
  ERROR,
  RpcError,
  type ListArguments,
  type ListResult,
} from "./bridge/protocol"
import {
  AT,
  boundPoint,
  parseOrderBy,
  rewriteForKind,
  type OrderKey,
} from "./temporal"

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

/** One read per kind. A fan-out of more than one is ordered by the host, so
 * the order is resolved here, once, against every kind, and a key with no
 * cross-kind order is refused before a single request is made. */
export function readsFor(query: ListArguments, kinds: KindInfo[]): Read[] {
  if (kinds.length > 1) resolveOrder(query.orderBy, kinds)
  return kinds.map((kind) => readFor(query, kind))
}

// ── the cross-kind order ────────────────────────────────────────────────────

type ValueClass = "instant" | "number" | "text" | "reference"

/** The engine's default: newest first. */
const DEFAULT_ORDER: OrderKey = { property: "createdAt", desc: true }

/** The envelope's own fields, which are not under `properties` on the wire. */
const RECORD_FIELDS: Record<string, ValueClass> = {
  createdAt: "instant",
  updatedAt: "instant",
  deletedAt: "instant",
  id: "text",
  version: "number",
}

/** The hot columns; every kind reads them back under `properties`. */
const HOT_COLUMNS = new Set(["at", "endsAt", "dueAt"])
const NUMERIC = new Set(["int", "float", "decimal"])
const INSTANT = new Set(["datetime", "date"])

function invalid(message: string): never {
  throw new RpcError(ERROR.invalidParams, message)
}

/** The property an order key reads on one kind: the bound point for `at`,
 * the key itself otherwise. */
function propertyOn(kind: KindInfo, property: string): string {
  return property === AT ? boundPoint(kind) : property
}

/** How one kind compares under one key, from its declaration. An undeclared
 * name orders as the server orders it (`props->>`, text, null when absent). */
function classOn(kind: KindInfo, property: string): ValueClass {
  if (property in RECORD_FIELDS) return RECORD_FIELDS[property]
  const name = propertyOn(kind, property)
  if (HOT_COLUMNS.has(name)) return "instant"
  const declared = declaredProperties(kind).find((p) => p.name === name)
  if (!declared) return "text"
  if (declared.repeated) {
    invalid(
      `orderBy: ${name} is repeated on ${kind.identity} and cannot be ordered by`
    )
  }
  if (declared.kind === "secret") {
    invalid(
      `orderBy: ${name} is sensitive on ${kind.identity} and cannot be ordered by`
    )
  }
  if (NUMERIC.has(declared.kind)) return "number"
  if (INSTANT.has(declared.kind)) return "instant"
  if (declared.kind === "reference") return "reference"
  return "text"
}

interface ResolvedKey {
  desc: boolean
  cls: ValueClass
  /** The key's value on a record, by the record's kind. */
  read: Map<string, (r: SubstrateRecord) => unknown>
}

/** The order keys against every kind of the fan-out; `createdAt` newest
 * first when the query names none. */
function resolveOrder(orderBy: string | undefined, kinds: KindInfo[]) {
  const keys = parseOrderBy(orderBy)
  return (keys.length ? keys : [DEFAULT_ORDER]).map(
    ({ property, desc }): ResolvedKey => {
      let cls: ValueClass | undefined
      let first: KindInfo | undefined
      const read = new Map<string, (r: SubstrateRecord) => unknown>()
      for (const kind of kinds) {
        const c = classOn(kind, property)
        if (cls && first && c !== cls) {
          invalid(
            `orderBy: ${property} is ${describe(cls)} on ${first.identity} and ${describe(c)} on ${kind.identity}; a read over both cannot order by it`
          )
        }
        cls = c
        first ??= kind
        const name = propertyOn(kind, property)
        read.set(
          kind.identity,
          property in RECORD_FIELDS
            ? (r) => r[property as keyof SubstrateRecord]
            : (r) => r.properties[name]
        )
      }
      return { desc, cls: cls ?? "text", read }
    }
  )
}

function describe(cls: ValueClass): string {
  return cls === "instant"
    ? "an instant"
    : cls === "number"
      ? "a number"
      : cls === "reference"
        ? "a reference"
        : "text"
}

/** A value as its class compares it, or null for what the server would sort
 * as NULL: an absent value, and one that does not read as the class. */
function normalize(cls: ValueClass, v: unknown): number | string | null {
  if (v === undefined || v === null) return null
  switch (cls) {
    case "instant": {
      const t = Date.parse(String(v))
      return Number.isNaN(t) ? null : t
    }
    case "number": {
      const n = typeof v === "number" ? v : Number(v)
      return Number.isNaN(n) ? null : n
    }
    case "reference": {
      if (typeof v === "string") return v
      const ref = (v as { ref?: unknown }).ref
      return typeof ref === "string" ? ref : null
    }
    default:
      return typeof v === "string" ? v : JSON.stringify(v)
  }
}

function compare(a: number | string, b: number | string): number {
  if (typeof a === "number" && typeof b === "number") return a - b
  return String(a).localeCompare(String(b))
}

/** The comparator a fan-out of `reads` merges under. */
export function orderComparator(
  reads: Read[],
  orderBy?: string
): (a: SubstrateRecord, b: SubstrateRecord) => number {
  const keys = resolveOrder(
    orderBy,
    reads.map((r) => r.kind)
  )
  const tie = keys[0].desc ? -1 : 1
  return (a, b) => {
    for (const key of keys) {
      const av = normalize(key.cls, key.read.get(a.kind)?.(a))
      const bv = normalize(key.cls, key.read.get(b.kind)?.(b))
      if (av === null && bv === null) continue
      if (av === null) return 1
      if (bv === null) return -1
      const c = compare(av, bv)
      if (c !== 0) return key.desc ? -c : c
    }
    return tie * (a.kind.localeCompare(b.kind) || a.id.localeCompare(b.id))
  }
}

// ── the merge ───────────────────────────────────────────────────────────────

/** The pages of a fan-out as one result. A single read passes through with
 * its cursor; several merge under the query's order and report `incomplete`
 * instead of a cursor. */
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
  records.sort(orderComparator(reads, orderBy))
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
