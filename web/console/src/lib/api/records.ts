/** Record reads and writes: keyset-paged lists off the one records route
 * (server-side filter/sort), a filtered set's size, the ranked read, single-
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
 * PAGINATION, two styles the wire keeps apart (decision 0084). The `cursor` a
 * page returns is an OPAQUE keyset token — the client stores it and resends it
 * VERBATIM as `after=` — and that is what a "load more" feed and the bounded
 * size walk use, because a keyset walk sees every row exactly once. `offset=`
 * is the other: a count of ordered rows to skip, so a numbered page is
 * reachable in one request. They are alternatives; sending both is refused. */

import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"

import { fetchChangesPage, type HistoryPosition } from "./changes"
import {
  CORE_PACKAGE,
  collectionPath,
  joinKind,
  request,
  rootPath,
  seg,
} from "./http"
import { ApiError } from "./types"
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
  /** Rows to skip before the page: the numbered-page continuation, and the
   * ALTERNATIVE to `after` — the wire refuses the two together. */
  offset?: number
  /** The filter's other arms; `kinds` here is the scope's, not the caller's. */
  filter?: RecordFilter
  /** `"updatedAt:desc"` — the wire's compact orderBy spelling. */
  orderBy?: string
  /** Reference properties whose referents the page carries in `included`,
   * one hop. Each must be declared by a kind the scope admits. */
  expand?: string[]
  /** Ask for the size of the whole filtered set beside the page
   * (`count=1`); the page answers it as `count`. */
  count?: boolean
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
  else if (p.offset) q.set("offset", String(p.offset))
  const filter = listFilter(p)
  if (filter) q.set("filter", JSON.stringify(filter))
  if (p.orderBy) q.set("orderBy", p.orderBy)
  if (p.expand?.length) q.set("expand", p.expand.join(","))
  if (p.count) q.set("count", "1")
  return `${RECORDS_PATH}?${q}`
}

/** One page of the list, with its expansion DEGRADING rather than failing.
 *
 * `expand` is a sidecar: the page is the rows, and the referents beside them
 * are how a reference reads as a name instead of an id. But the server
 * refuses the whole read (`422 validation`) when the expansion would load
 * more than its page cap of referents — a wide kind at 50 rows a page can
 * reach it — and a table that 404s its own rows because it asked for nicer
 * labels is worse than a table of ids. So a 422 on an expanding read is
 * answered by the same read without one: the rows arrive, `included` does
 * not, and every pill falls back to the id it always showed.
 *
 * Only an expanding read degrades, and only once: without `expand` there is
 * nothing to give up, and a second 422 is the filter's problem and belongs in
 * front of the reader. */
export async function fetchRecordsPage(
  p: ListParams,
  signal?: AbortSignal
): Promise<Page> {
  try {
    return await request<Page>("GET", listPath(p), undefined, { signal })
  } catch (err) {
    if (!p.expand?.length) throw err
    if (!(err instanceof ApiError) || err.status !== 422) throw err
    return request<Page>(
      "GET",
      listPath({ ...p, expand: undefined }),
      undefined,
      { signal }
    )
  }
}

export function recordsQueryOptions(p: ListParams) {
  return queryOptions({
    queryKey: [
      "records",
      scopeKinds(p),
      {
        first: p.first ?? 50,
        after: p.after ?? null,
        offset: p.offset ?? null,
        filter: listFilter(p) ?? null,
        orderBy: p.orderBy ?? null,
        expand: p.expand ?? null,
        count: p.count ?? false,
      },
    ],
    queryFn: ({ signal }) => fetchRecordsPage(p, signal),
    placeholderData: (prev) => prev,
  })
}

// ── reference titles: the batched read a single record needs ────────────────

/** How many referents one record's page of titles may ask for. The server's
 * own page cap; a record pointing past it reads the first `first` and the
 * rest keep their ids. */
const TITLE_BATCH = 500

/** The titles of a set of referents, in ONE list read.
 *
 * A single-record `GET` does not expand (docs/api.md), so the record page
 * learns what its pointers are called the only other way the wire offers: a
 * list narrowed by `filter.ids` to the ids those paths name, inside the kinds
 * they name. One request for the whole page, however many properties and
 * however many kinds, because `ids` narrows WITHIN the selected kinds and ids
 * are unique per kind.
 *
 * Caller-supplied `kinds` and `ids` are the already-grouped scope
 * (`titleReadScope`), so a kind the repository never declared — which would
 * `404` the whole read — never reaches here. */
export function referenceTitlesQueryOptions(scope: {
  kinds: string[]
  ids: string[]
}) {
  return queryOptions({
    queryKey: ["reference-titles", scope.kinds, scope.ids],
    enabled: scope.kinds.length > 0 && scope.ids.length > 0,
    staleTime: 60_000,
    queryFn: ({ signal }) =>
      request<Page>(
        "GET",
        listPath({
          kinds: scope.kinds,
          first: TITLE_BATCH,
          filter: { ids: scope.ids.slice(0, TITLE_BATCH) },
        }),
        undefined,
        { signal }
      ),
  })
}

// ── the declarations and history a record's provenance reads ────────────────

/** The recordmapping declarations a set of `linkedFrom` entries name, in ONE
 * list read: a mapping's record id IS its identity (`<authority>/<package>/
 * <name>`), so `filter.ids` inside the one kind answers every group's header
 * — its title, the kind it reads, the properties its `map` rules write. Read
 * once per record page and held for a minute: a declaration moves rarely, and
 * the vocabulary apply that moves it is not something this page watches. */
export function recordMappingsQueryOptions(ids: readonly string[]) {
  const sorted = [...new Set(ids)].sort()
  return queryOptions({
    queryKey: ["record-mappings", sorted],
    enabled: sorted.length > 0,
    staleTime: 60_000,
    queryFn: ({ signal }) =>
      request<Page>(
        "GET",
        listPath({
          kinds: [`${CORE_PACKAGE}/recordmapping`],
          first: TITLE_BATCH,
          filter: { ids: sorted.slice(0, TITLE_BATCH) },
        }),
        undefined,
        { signal }
      ),
  })
}

/** The merges a record WON — the `recordmerge` rows whose `winner` names it —
 * and the requests that proposed them, each a reverse read narrowed to the
 * one kind and the one property, so a record that never merged costs one
 * empty page each and a record with a former id gets its history whole. */
export function mergesIntoQueryOptions(ref: string, enabled = true) {
  return queryOptions({
    queryKey: ["merges-into", ref],
    enabled,
    staleTime: 60_000,
    queryFn: ({ signal }) =>
      request<Page>(
        "GET",
        listPath({
          kinds: [`${CORE_PACKAGE}/recordmerge`],
          first: 200,
          filter: { referencing: { ref, property: "winner" } },
        }),
        undefined,
        { signal }
      ),
  })
}

export function mergeRequestsForQueryOptions(ref: string, enabled = true) {
  return queryOptions({
    queryKey: ["merge-requests-for", ref],
    enabled,
    staleTime: 60_000,
    queryFn: ({ signal }) =>
      request<Page>(
        "GET",
        listPath({
          kinds: [`${CORE_PACKAGE}/recordmergerequest`],
          first: 200,
          filter: { referencing: { ref, property: "winner" } },
        }),
        undefined,
        { signal }
      ),
  })
}

// ── size ──────────────────────────────────────────────────────────────────

/** A collection size: `value` is what was counted, and `capped` is true
 * when the collection outran a fallback walk's ceiling (render it as
 * `value+`, and never as the last page of a numbered bar — it is a floor). A
 * server that answers `count` is never capped. */
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

/** The probe's ceiling: past this many rows a size answers `capped`. */
const PROBE_MAX = 1 << 17

/** Count a collection without reading it.
 *
 * The list answers the size of its filtered set when asked (`count=1`,
 * decision 0105): one read of one row, and the number is exact. A server
 * that predates the parameter refuses it by name (`400`: the list refuses
 * every parameter it does not know), and a window read refuses it too, since
 * computed occurrences are not rows; either way, and on a `200` that carries
 * no `count`, the size falls back to what the wire offered before. That is
 * detected from the answer, never from a version.
 *
 * The fallback asks for ONE row at an `offset`: a row there means the
 * collection is longer than the offset. Doubling offsets bracket the size,
 * and a bisection narrows it, so a 10,000-row collection costs ~30 one-row
 * reads. Offsets are refused on a window read (decision 0084), so a refusal
 * falls back again to the bounded keyset walk. A write landing mid-probe can
 * skew a glance by a row; these surfaces glance, they do not audit. */
export async function countRecords(
  authority: string,
  pkg: string,
  name: string,
  filter: RecordFilter | undefined,
  signal?: AbortSignal
): Promise<RecordCount> {
  const scope = { authority, package: pkg, name, filter }
  let first: Page | undefined
  try {
    first = await request<Page>(
      "GET",
      listPath({ ...scope, first: 1, count: true }),
      undefined,
      { signal }
    )
  } catch (err) {
    // A refusal of the request's shape is the older server (or the window
    // read) saying no to `count`; anything else is the read's own failure.
    if (
      !(err instanceof ApiError) ||
      (err.status !== 400 && err.status !== 422)
    )
      throw err
  }
  if (typeof first?.count === "number")
    return { value: first.count, capped: false }
  return probeCount(scope, first, signal)
}

/** Count by one-row offset probes. `atZero` is a first-row read already in
 * hand (the count request's own page), so it is not asked twice. */
async function probeCount(
  scope: {
    authority: string
    package: string
    name: string
    filter: RecordFilter | undefined
  },
  atZero: Page | undefined,
  signal?: AbortSignal
): Promise<RecordCount> {
  const has = async (offset: number): Promise<boolean> => {
    if (offset === 0 && atZero) return (atZero.records?.length ?? 0) > 0
    const res = await request<Page>(
      "GET",
      listPath({ ...scope, first: 1, offset }),
      undefined,
      { signal }
    )
    return (res.records?.length ?? 0) > 0
  }
  try {
    // One probe at a time: a caller may budget one connection per count
    // (api/overview.ts), so doubling runs in sequence, not in parallel.
    if (!(await has(0))) return { value: 0, capped: false }
    let last = 0
    let next = 1
    while (await has(next)) {
      if (next >= PROBE_MAX) return { value: PROBE_MAX, capped: true }
      last = next
      next *= 2
    }
    // A row exists at `last` and none at `next`: the size is the first empty
    // offset in (last, next].
    let lo = last + 1
    let hi = next
    while (lo < hi) {
      const mid = Math.floor((lo + hi) / 2)
      if (await has(mid)) lo = mid + 1
      else hi = mid
    }
    return { value: lo, capped: false }
  } catch (err) {
    if (signal?.aborted) throw err
    return walkCount(
      scope.authority,
      scope.package,
      scope.name,
      scope.filter,
      signal
    )
  }
}

/** Count a collection by walking the opaque cursor, bounded by the ceiling. */
async function walkCount(
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

// ── what a write makes stale ────────────────────────────────────────────────

/** Whether a cached read can show a record one write just changed: the
 * record itself and its history, any read that names its kind anywhere in its
 * key (a collection, a count, a trait's read, the Agents page's providers), a
 * list or a ranking over every kind, the batched title reads that name the
 * record, and the reverse reads, since a moved pointer moves who is pointed
 * at. Everything else keeps its cache: another kind's list is not this
 * record's business. */
export function recordWriteReaches(
  queryKey: readonly unknown[],
  kind: string,
  id: string
): boolean {
  const [head, second, third] = queryKey
  switch (head) {
    case "record":
      return (
        joinKind(String(second), String(third), String(queryKey[3])) === kind &&
        queryKey[4] === id
      )
    case "reference-titles":
      return (
        Array.isArray(second) &&
        second.includes(kind) &&
        Array.isArray(third) &&
        third.includes(id)
      )
    case "referencing":
      return true
    case "records":
      if (Array.isArray(second) && second.length === 0) return true
      break
    case "records-search":
      if ((third as { kinds?: unknown } | undefined)?.kinds == null) return true
      break
  }
  return mentions(queryKey, kind)
}

function mentions(value: unknown, kind: string): boolean {
  if (typeof value === "string")
    return value === kind || value.startsWith(`${kind}/`)
  if (Array.isArray(value)) return value.some((v) => mentions(v, kind))
  if (value && typeof value === "object")
    return Object.values(value).some((v) => mentions(v, kind))
  return false
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
