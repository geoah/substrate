/** Record reads and writes: keyset-paged collection lists (server-side
 * filter/sort), a bounded size probe, single-record reads (the only wire
 * surface carrying `propertyMeta`), the record-57 incoming-edge pages, and the
 * change feed filtered to one record.
 *
 * A collection path IS the kind reference split into segments —
 * `/{authority}/{plural}` for a published kind, `/{plural}` for a
 * repository-local one (`collectionPath`). The PUT/POST body carries the
 * authored envelope only (`{properties, labels, annotations, edges}`); the kind
 * is settled by the path, so the body never repeats it.
 *
 * PAGINATION: the list `cursor` is an OPAQUE keyset
 * token — the client stores it and resends it VERBATIM as `after=`. There is
 * no offset, so a "load more" walks the server cursor and a size is a bounded
 * cursor walk. */

import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"

import { fetchChangesPage } from "./changes"
import { collectionPath, request, seg } from "./http"
import type {
  ChangeRow,
  EdgeRef,
  SubstrateRecord,
  RecordFilter,
  IncomingEdge,
  IncomingPage,
  Page,
} from "./types"

/** A record id as one URL path segment: `encodeURIComponent`, so a `/` inside
 * a writer-supplied id (or a declaration record's kind-reference id) travels
 * as `%2F` and the API decodes it exactly once (`pathParam`). */
export const recordIdSegment = (id: string): string => seg(id)

// ── list ────────────────────────────────────────────────────────────────────

export interface ListParams {
  /** The publishing authority, or "" for a repository-local kind. */
  authority: string
  plural: string
  first?: number
  /** The opaque keyset cursor a previous page returned, resent VERBATIM. */
  after?: string
  filter?: RecordFilter
  /** `"updatedAt:desc"` — the wire's compact orderBy spelling. */
  orderBy?: string
  withEdges?: boolean
}

function hasFilter(filter?: RecordFilter): filter is RecordFilter {
  if (!filter) return false
  return Boolean(
    filter.kinds?.length ||
    Object.keys(filter.properties ?? {}).length ||
    Object.keys(filter.labels ?? {}).length ||
    filter.edge
  )
}

export function listPath(p: ListParams): string {
  const q = new URLSearchParams()
  q.set("first", String(p.first ?? 50))
  if (p.after) q.set("after", p.after)
  if (hasFilter(p.filter)) q.set("filter", JSON.stringify(p.filter))
  if (p.orderBy) q.set("orderBy", p.orderBy)
  if (p.withEdges) q.set("withEdges", "1")
  return `${collectionPath(p.authority, p.plural)}?${q}`
}

export function recordsQueryOptions(p: ListParams) {
  return queryOptions({
    queryKey: [
      "records",
      p.authority,
      p.plural,
      {
        first: p.first ?? 50,
        after: p.after ?? null,
        filter: hasFilter(p.filter) ? p.filter : null,
        orderBy: p.orderBy ?? null,
        withEdges: Boolean(p.withEdges),
      },
    ],
    queryFn: ({ signal }) =>
      request<Page>("GET", listPath(p), undefined, { signal }),
    placeholderData: (prev) => prev,
  })
}

// ── size ──────────────────────────────────────────────────────────────────

/** A collection size. The server has no count and no offset, so a size is a
 * BOUNDED keyset walk: `value` is what was counted, and `capped` is true when
 * the collection outran the walk's ceiling (render it as `value+`). */
export interface RecordCount {
  value: number
  capped: boolean
}

/** One walk page (big — a size cares about throughput, not latency). */
const COUNT_PAGE = 500
/** The walk's ceiling: past this many rows a size answers `capped`. These
 * surfaces glance, they do not audit (api/overview.ts) — an exact size for
 * every realistic collection, an honest `N+` for the pathological one. */
const COUNT_MAX_PAGES = 20

/** Count a collection by walking the opaque cursor, bounded by the ceiling. */
export async function countRecords(
  authority: string,
  plural: string,
  filter: RecordFilter | undefined,
  signal?: AbortSignal
): Promise<RecordCount> {
  let value = 0
  let after: string | undefined
  for (let page = 0; page < COUNT_MAX_PAGES; page++) {
    const res = await request<Page>(
      "GET",
      listPath({ authority, plural, first: COUNT_PAGE, after, filter }),
      undefined,
      { signal }
    )
    value += res.records?.length ?? 0
    if (!res.cursor) return { value, capped: false }
    after = res.cursor
  }
  return { value, capped: true }
}

export function recordCountQueryOptions(
  authority: string,
  plural: string,
  filter?: RecordFilter
) {
  return queryOptions({
    queryKey: [
      "records-count",
      authority,
      plural,
      hasFilter(filter) ? filter : null,
    ],
    queryFn: ({ signal }) => countRecords(authority, plural, filter, signal),
    staleTime: 60_000,
  })
}

/** Render a `RecordCount` for a glance surface: the number, `+` when capped. */
export function formatCount(count: RecordCount): string {
  return `${count.value.toLocaleString()}${count.capped ? "+" : ""}`
}

// ── single record ───────────────────────────────────────────────────────────

export function recordQueryOptions(
  authority: string,
  plural: string,
  id: string
) {
  return queryOptions({
    queryKey: ["record", authority, plural, id],
    queryFn: ({ signal }) =>
      request<SubstrateRecord>(
        "GET",
        `${collectionPath(authority, plural)}/${seg(id)}`,
        undefined,
        { signal }
      ),
  })
}

// ── writes (bundle config + account records, integrations flow) ─────────────

/** One edge on a write: `{rel, to: {kind, id}}`, plus the edge's own
 * properties where it has any. */
export interface EdgeWrite {
  rel: string
  to: EdgeRef
  properties?: Record<string, unknown>
}

/** A create/upsert write body (`substrate.PutInput`): authored properties,
 * labels, annotations and edges, plus an optional id (omit to let the
 * substrate mint one). The kind is settled by the collection path. */
export interface RecordWrite {
  id?: string
  properties?: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  edges?: EdgeWrite[]
  ifVersion?: number
}

/** Create one record in a collection: `POST /{authority}/{plural}` (or
 * `POST /{plural}` for a repository-local kind). The kind is settled by the
 * URL; the body carries authored properties (and an optional id — omit it and
 * the substrate mints one). */
export function createRecord(
  authority: string,
  plural: string,
  input: RecordWrite
): Promise<SubstrateRecord> {
  return request<SubstrateRecord>(
    "POST",
    collectionPath(authority, plural),
    input
  )
}

/** Upsert one record by id: `PUT /{authority}/{plural}/{id}`. A full-document
 * apply — the write replaces the authored envelope wholesale (unlike PATCH's
 * key-wise merge), the natural semantic for the YAML editor's Edit flow. */
export function putRecord(
  authority: string,
  plural: string,
  id: string,
  input: RecordWrite
): Promise<SubstrateRecord> {
  return request<SubstrateRecord>(
    "PUT",
    `${collectionPath(authority, plural)}/${seg(id)}`,
    input
  )
}

/** Patch one record in place: `PATCH /{authority}/{plural}/{id}`. Maps merge
 * key-wise (a null value deletes the key); omitted fields are untouched — so a
 * blank secret input simply isn't sent and the sealed value stands. */
export function patchRecord(
  authority: string,
  plural: string,
  id: string,
  patch: {
    properties?: Record<string, unknown>
    labels?: Record<string, unknown>
    annotations?: Record<string, unknown>
  }
): Promise<SubstrateRecord> {
  return request<SubstrateRecord>(
    "PATCH",
    `${collectionPath(authority, plural)}/${seg(id)}`,
    patch
  )
}

export async function deleteRecord(
  authority: string,
  plural: string,
  id: string
): Promise<void> {
  await request<void>(
    "DELETE",
    `${collectionPath(authority, plural)}/${seg(id)}`
  )
}

// ── incoming edges (record 57) ──────────────────────────────────────────────

/** The fan-in of one record. `rel`/`fromKind` narrow it to ONE group, which
 * is how a drill-down expands a group without pulling every other pointer the
 * record has — and the page's total is then that group's, not the record's. */
export function incomingInfiniteOptions(
  authority: string,
  plural: string,
  id: string,
  first = 50,
  narrow: { rel?: string; fromKind?: string } = {}
) {
  return infiniteQueryOptions({
    queryKey: [
      "incoming",
      authority,
      plural,
      id,
      { rel: narrow.rel ?? null, fromKind: narrow.fromKind ?? null, first },
    ],
    queryFn: ({ pageParam, signal }) => {
      const q = new URLSearchParams({ first: String(first) })
      if (pageParam) q.set("after", pageParam)
      if (narrow.rel) q.set("rel", narrow.rel)
      if (narrow.fromKind) q.set("fromKind", narrow.fromKind)
      return request<IncomingPage>(
        "GET",
        `${collectionPath(authority, plural)}/${seg(id)}/incoming?${q}`,
        undefined,
        { signal }
      )
    },
    initialPageParam: "",
    getNextPageParam: (last) => last.cursor ?? undefined,
  })
}

/** One rel × source-kind bucket of the fan-in. The server orders by
 * (rel, kind, id), so buckets stay contiguous across pages and the grouping is
 * a stable fold, not a re-sort. */
export interface IncomingGroup {
  rel: string
  kind: string
  rows: IncomingEdge[]
}

export function groupIncoming(rows: IncomingEdge[]): IncomingGroup[] {
  const out: IncomingGroup[] = []
  for (const row of rows) {
    const last = out[out.length - 1]
    if (last && last.rel === row.rel && last.kind === row.from.kind) {
      last.rows.push(row)
    } else {
      out.push({ rel: row.rel, kind: row.from.kind, rows: [row] })
    }
  }
  return out
}

// ── the change feed, filtered to one record ─────────────────────────────────

/** The whole changelog slice for one exact record, walked by the server
 * cursor. An id is NOT unique, so the `recordId` facet
 * REQUIRES its `recordKind` companion. Under scope filtering a page can be
 * short without being the end, so the walk continues on the returned cursor,
 * not on a full page — bounded by a page ceiling. */
const FORMER_SLICE_PAGE = 200
const FORMER_SLICE_MAX_PAGES = 25

async function fetchRecordHistory(
  recordId: string,
  recordKind: string,
  signal?: AbortSignal
): Promise<ChangeRow[]> {
  const rows: ChangeRow[] = []
  let before: number | undefined
  for (let page = 0; page < FORMER_SLICE_MAX_PAGES; page++) {
    const res = await fetchChangesPage({
      before,
      first: FORMER_SLICE_PAGE,
      filter: { recordId, recordKind },
      signal,
    })
    rows.push(...res.changes)
    if (res.cursor === undefined) break
    before = res.cursor
  }
  return rows
}

/** The history a merge hid: the wire's `recordId` filter is an exact match,
 * so rows written before a merge live under the loser's FORMER id and never
 * answer a query for the canonical one. This reads each former id's slice
 * whole (former ids are retired — their slices no longer grow) so the activity
 * rail can stitch the full record. A merge joins records of ONE kind, so the
 * former ids share the canonical record's kind. */
export function formerIdChangesQueryOptions(
  formerIds: string[],
  recordKind: string
) {
  return queryOptions({
    queryKey: ["changes", "former", recordKind, [...formerIds].sort()],
    enabled: formerIds.length > 0 && Boolean(recordKind),
    staleTime: 60_000,
    queryFn: async ({ signal }) => {
      const slices = await Promise.all(
        formerIds.map((id) => fetchRecordHistory(id, recordKind, signal))
      )
      return slices.flat().sort((a, b) => b.seq - a.seq)
    },
  })
}

export function recordChangesInfiniteOptions(
  recordId: string,
  recordKind: string,
  first = 25
) {
  return infiniteQueryOptions({
    queryKey: ["changes", "record", recordKind, recordId],
    queryFn: ({ pageParam, signal }) =>
      fetchChangesPage({
        before: pageParam > 0 ? pageParam : undefined,
        first,
        filter: { recordId, recordKind },
        signal,
      }),
    initialPageParam: 0,
    // The server cursor is the continuation — it advances past scope-filtered
    // rows, so a short page is not the end. The walk ends when the cursor is
    // omitted (exhausted).
    getNextPageParam: (last) => last.cursor ?? undefined,
  })
}
