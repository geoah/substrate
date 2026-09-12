/** Record reads and writes: keyset-paged lists off the one records route
 * (server-side filter/sort), a bounded size probe, the ranked read, single-
 * record reads (the only wire surface carrying `propertyMeta`), the reverse
 * read (what points at a record), and the change feed filtered to one record.
 *
 * EVERY LIST IS `GET /api/v1/records`. The kind is not in the path: it is
 * `filter.kinds`, one entry for a collection, several for a cross-kind read,
 * none for every kind. A record's own path is still the kind reference split
 * into segments plus the id — `/{authority}/{package}/{name}/{id}`
 * (`collectionPath`) — and GET/PUT/PATCH/DELETE address it there. A create
 * under a server-minted id is `POST /api/v1/records` with `kind` in the body.
 *
 * PAGINATION: the list `cursor` is an OPAQUE keyset token — the client stores
 * it and resends it VERBATIM as `after=`. There is no offset, so a "load more"
 * walks the server cursor and a size is a bounded cursor walk. */

import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"

import { fetchChangesPage, type HistoryPosition } from "./changes"
import { collectionPath, joinKind, request, rootPath, seg } from "./http"
import type {
  ChangeRow,
  Page,
  PutInput,
  RankedPage,
  RecordFilter,
  ReferenceSite,
  SubstrateRecord,
} from "./types"

/** A record id as one URL path segment: `encodeURIComponent`, so a `/` inside
 * a writer-supplied id (or a declaration record's kind-reference id) travels
 * as `%2F` and the API decodes it exactly once (`pathParam`). */
export const recordIdSegment = (id: string): string => seg(id)

/** The one list route. */
export const RECORDS_PATH = rootPath("records")

// ── list ────────────────────────────────────────────────────────────────────

/** The list read's parameters. Its SCOPE is one of two spellings: ONE kind
 * by its three parts (the collection a page routes by), or `kinds` — several
 * references, or an empty list for every kind in the repository. `kinds`
 * wins where both are given. */
export interface ListParams {
  /** The publishing authority; every kind carries one. */
  authority?: string
  /** The package's own word; every kind carries one. */
  package?: string
  name?: string
  /** Several kinds, or `[]` for every kind; the alternative to the three. */
  kinds?: string[]
  first?: number
  /** The opaque keyset cursor a previous page returned, resent VERBATIM. */
  after?: string
  /** The filter's other arms; `kinds` here is the scope's, not the caller's. */
  filter?: RecordFilter
  /** `"updatedAt:desc"` — the wire's compact orderBy spelling. */
  orderBy?: string
  /** Reference properties whose referents the page carries in `included`,
   * one hop. Each must be declared by a kind the scope admits. */
  expand?: string[]
}

/** The kinds a list's scope names, as the filter spells them. */
export function scopeKinds(p: ListParams): string[] {
  return p.kinds ?? [joinKind(p.authority ?? "", p.package ?? "", p.name ?? "")]
}

/** The filter a list sends: the scope's kinds plus the caller's other arms.
 * `undefined` when there is nothing to narrow by, so a bare read of every
 * kind sends no `filter=` at all. */
export function listFilter(p: ListParams): RecordFilter | undefined {
  const kinds = scopeKinds(p)
  const out: RecordFilter = { ...p.filter }
  // The scope owns `kinds`; a caller's own entry would widen a collection.
  delete out.kinds
  if (kinds.length) out.kinds = kinds
  for (const key of Object.keys(out) as (keyof RecordFilter)[]) {
    const v = out[key]
    if (
      v === undefined ||
      (Array.isArray(v) && v.length === 0) ||
      (typeof v === "object" &&
        v !== null &&
        !Array.isArray(v) &&
        Object.keys(v).length === 0)
    )
      delete out[key]
  }
  return Object.keys(out).length ? out : undefined
}

export function listPath(p: ListParams): string {
  const q = new URLSearchParams()
  q.set("first", String(p.first ?? 50))
  if (p.after) q.set("after", p.after)
  const filter = listFilter(p)
  if (filter) q.set("filter", JSON.stringify(filter))
  if (p.orderBy) q.set("orderBy", p.orderBy)
  if (p.expand?.length) q.set("expand", p.expand.join(","))
  return `${RECORDS_PATH}?${q}`
}

export function recordsQueryOptions(p: ListParams) {
  return queryOptions({
    queryKey: [
      "records",
      scopeKinds(p),
      {
        first: p.first ?? 50,
        after: p.after ?? null,
        filter: listFilter(p) ?? null,
        orderBy: p.orderBy ?? null,
        expand: p.expand ?? null,
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
  pkg: string,
  name: string,
  filter: RecordFilter | undefined,
  signal?: AbortSignal
): Promise<RecordCount> {
  let value = 0
  let after: string | undefined
  for (let page = 0; page < COUNT_MAX_PAGES; page++) {
    const res = await request<Page>(
      "GET",
      listPath({
        authority,
        package: pkg,
        name,
        first: COUNT_PAGE,
        after,
        filter,
      }),
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
  pkg: string,
  name: string,
  filter?: RecordFilter
) {
  return queryOptions({
    queryKey: [
      "records-count",
      authority,
      pkg,
      name,
      listFilter({ authority, package: pkg, name, filter }) ?? null,
    ],
    queryFn: ({ signal }) => countRecords(authority, pkg, name, filter, signal),
    staleTime: 60_000,
  })
}

/** Render a `RecordCount` for a glance surface: the number, `+` when capped. */
export function formatCount(count: RecordCount): string {
  return `${count.value.toLocaleString()}${count.capped ? "+" : ""}`
}

// ── the ranked read ─────────────────────────────────────────────────────────

export type SearchMode = "lexical" | "semantic" | "hybrid"

/** `GET /records?q=`: a ranking, not a filter. `kinds` narrows the candidates
 * BEFORE ranking (the only filter arm the ranked read admits); `first` is the
 * hit count. */
export function searchQueryOptions(
  q: string,
  opts: { mode?: SearchMode; kinds?: string[]; first?: number } = {}
) {
  return queryOptions({
    queryKey: [
      "records-search",
      q,
      {
        mode: opts.mode ?? null,
        kinds: opts.kinds ?? null,
        first: opts.first ?? null,
      },
    ],
    enabled: q.trim().length > 0,
    queryFn: ({ signal }) => {
      const params = new URLSearchParams({ q })
      if (opts.mode) params.set("mode", opts.mode)
      if (opts.kinds?.length)
        params.set("filter", JSON.stringify({ kinds: opts.kinds }))
      if (opts.first) params.set("first", String(opts.first))
      return request<RankedPage>(
        "GET",
        `${RECORDS_PATH}?${params}`,
        undefined,
        {
          signal,
        }
      )
    },
  })
}

// ── single record ───────────────────────────────────────────────────────────

export function recordQueryOptions(
  authority: string,
  pkg: string,
  name: string,
  id: string
) {
  return queryOptions({
    queryKey: ["record", authority, pkg, name, id],
    queryFn: ({ signal }) =>
      request<SubstrateRecord>(
        "GET",
        `${collectionPath(authority, pkg, name)}/${seg(id)}`,
        undefined,
        { signal }
      ),
  })
}

// ── writes (bundle config + account records, integrations flow) ─────────────

/** A create/upsert write body: `substrate.PutInput` without `kind`, which the
 * caller names beside it (`createRecord` writes it into the body, a PUT's URL
 * says it). Authored properties, labels and annotations, plus an optional id
 * (omit to let the substrate mint one); a pointer at another record is a
 * `reference` property like any other. Derived from the pinned shape, so the
 * golden holds it through PutInput. */
export type RecordWrite = Omit<PutInput, "kind">

/** Create one record: under a server-minted id it is `POST /api/v1/records`,
 * the body naming its `kind`; with a chosen id it is `putRecord`, which fixes
 * (kind, id) in the URL, because the POST refuses an id in the body. */
export function createRecord(
  authority: string,
  pkg: string,
  name: string,
  input: RecordWrite
): Promise<SubstrateRecord> {
  // A chosen id is a PUT at the record path: POST refuses an id in the body,
  // because minting under a supplied one would be a second door to one write.
  if (input.id) {
    const { id, ...rest } = input
    return putRecord(authority, pkg, name, id, rest)
  }
  const body: PutInput = { kind: joinKind(authority, pkg, name), ...input }
  return request<SubstrateRecord>("POST", RECORDS_PATH, body)
}

/** Upsert one record by id: `PUT /{authority}/{package}/{name}/{id}`. The
 * apply the YAML editor's Edit flow makes: the server MERGES the authored
 * envelope into the record and never prunes — a key the document leaves out
 * stands, and deletion is only ever the explicit delete verb. */
export function putRecord(
  authority: string,
  pkg: string,
  name: string,
  id: string,
  input: RecordWrite
): Promise<SubstrateRecord> {
  return request<SubstrateRecord>(
    "PUT",
    `${collectionPath(authority, pkg, name)}/${seg(id)}`,
    input
  )
}

/** A patch write body (`substrate.PatchInput`). `ifVersion` is the CAS
 * precondition: the version the writer READ, refused with `conflict` when the
 * record has moved since. Some transitions require it (deciding a change
 * request is one), so it is part of the shape rather than a second door. */
export interface RecordPatch {
  properties?: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  /** The finalizer arms. The console writes neither; they ride along so the
   * mirror is whole. */
  addFinalizers?: string[]
  removeFinalizers?: string[]
  ifVersion?: number
}

/** Patch one record in place: `PATCH /{authority}/{package}/{name}/{id}`. Maps
 * merge key-wise (a null value deletes the key); omitted fields are untouched —
 * so a blank secret input simply isn't sent and the sealed value stands. */
export function patchRecord(
  authority: string,
  pkg: string,
  name: string,
  id: string,
  patch: RecordPatch
): Promise<SubstrateRecord> {
  return request<SubstrateRecord>(
    "PATCH",
    `${collectionPath(authority, pkg, name)}/${seg(id)}`,
    patch
  )
}

// ── the reverse read: what points at a record ───────────────────────────────

/** A record path, `<kind>/<id>` — the key `matches` and `included` use and
 * the value `referencing.ref` takes. */
export function recordPath(kind: string, id: string): string {
  return `${kind}/${id}`
}

/** The fan-in of one record: `GET /records?filter={referencing: {ref}}`, a
 * page of the DISTINCT pointing records with `matches` saying from which
 * property (and nested path) each one points. `kind`/`property` narrow it to
 * ONE group — `filter.kinds` and `referencing.property` — which is how a
 * drill-down expands a group without pulling every other pointer the record
 * has. */
export function referencingInfiniteOptions(
  ref: string,
  first = 50,
  narrow: { property?: string; kind?: string } = {}
) {
  return infiniteQueryOptions({
    queryKey: [
      "referencing",
      ref,
      { property: narrow.property ?? null, kind: narrow.kind ?? null, first },
    ],
    queryFn: ({ pageParam, signal }) =>
      request<Page>(
        "GET",
        listPath({
          kinds: narrow.kind ? [narrow.kind] : [],
          first,
          after: pageParam || undefined,
          filter: {
            referencing: narrow.property
              ? { ref, property: narrow.property }
              : { ref },
          },
        }),
        undefined,
        { signal }
      ),
    initialPageParam: "",
    getNextPageParam: (last) => last.cursor ?? undefined,
  })
}

/** One (pointing record, site) pair of the fan-in: the page's row joined with
 * one entry of its `matches`. A record pointing from two properties yields two
 * rows. */
export interface ReferencingRow {
  record: SubstrateRecord
  property: string
  /** The dotted address of a NESTED site, absent for a kind's own property. */
  path?: string
}

/** The rows of one page: each record once per site `matches` lists for it. A
 * record the page carries with no match entry (a server that did not say)
 * still shows, under an unnamed property, rather than vanishing. */
export function referencingRows(page: Page): ReferencingRow[] {
  const out: ReferencingRow[] = []
  for (const record of page.records ?? []) {
    const sites: ReferenceSite[] = page.matches?.[
      recordPath(record.kind, record.id)
    ] ?? [{ property: "" }]
    for (const site of sites) {
      out.push({ record, property: site.property, path: site.path })
    }
  }
  return out
}

/** One property × source-kind bucket of the fan-in. The fold is keyed rather
 * than adjacent (a page is in the list's order, not grouped), and the buckets
 * come back in (kind, property) order so a page arriving later lands where a
 * reader already looked. */
export interface ReferencingGroup {
  property: string
  kind: string
  rows: ReferencingRow[]
}

export function groupReferencing(rows: ReferencingRow[]): ReferencingGroup[] {
  const byKey = new Map<string, ReferencingGroup>()
  for (const row of rows) {
    const key = `${row.record.kind}\u0000${row.property}`
    const group = byKey.get(key)
    if (group) group.rows.push(row)
    else
      byKey.set(key, {
        property: row.property,
        kind: row.record.kind,
        rows: [row],
      })
  }
  return [...byKey.values()].sort(
    (a, b) =>
      a.kind.localeCompare(b.kind) || a.property.localeCompare(b.property)
  )
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
  let generation: string | undefined
  for (let page = 0; page < FORMER_SLICE_MAX_PAGES; page++) {
    const res = await fetchChangesPage({
      before,
      generation,
      first: FORMER_SLICE_PAGE,
      filter: { recordId, recordKind },
      signal,
    })
    rows.push(...res.changes)
    if (res.cursor === undefined) break
    before = res.cursor
    generation = res.generation
  }
  return rows
}

/** The history a merge hid: the wire's `recordId` scope follows one id (plus
 * the merge and split entries naming it, never the winner's later writes), so
 * rows written before a merge live under the loser's FORMER id and never
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
        before: pageParam.before > 0 ? pageParam.before : undefined,
        generation: pageParam.generation,
        first,
        filter: { recordId, recordKind },
        signal,
      }),
    initialPageParam: { before: 0 } as HistoryPosition,
    // The server cursor is the continuation — it advances past scope-filtered
    // rows, so a short page is not the end. The walk ends when the cursor is
    // omitted (exhausted). The continuation resends the page's generation.
    getNextPageParam: (last): HistoryPosition | undefined =>
      last.cursor === undefined
        ? undefined
        : { before: last.cursor, generation: last.generation },
  })
}
