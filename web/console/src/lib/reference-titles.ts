/** What a pointer is CALLED: a record path (`<kind>/<id>`) → the referent's
 * display title.
 *
 * A stored reference is `{ref: "<kind>/<id>"}` and nothing more — the id is
 * everything the pointing record holds — so a pill rendered from the value
 * alone prints an id where a name belongs (owner report, 2026-09-18: a task's
 * `assignee` read as `kq3v9x2m41pf`). The title lives on the REFERENT, so
 * learning it always costs a second read, and the wire offers exactly two
 * shapes for it (docs/api.md):
 *
 *   - the list's forward hop, `expand=<properties>` → `included`, which costs
 *     the collection page nothing extra; and
 *   - a batched list read over the paths one record holds, because a
 *     single-record `GET` does not expand.
 *
 * Both land here, as one map, so `RecordPill` has one thing to be handed and
 * a surface that knows no titles simply hands nothing and falls back to the
 * id, which is what every surface did before. */

import { queryOptions } from "@tanstack/react-query"

import { request } from "@/lib/api/http"
import { listPath } from "@/lib/api/records"
import { readReference, type Page, type SubstrateRecord } from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { splitRecordPath } from "@/lib/record-path"

/** Record path → the referent's title. Read-only: a resolver is handed down
 * through the render, never written to by it. */
export type ReferenceTitles = ReadonlyMap<string, string>

/** The resolver a surface with no second read hands down. */
export const NO_TITLES: ReferenceTitles = new Map<string, string>()

/** The titles a list page's `included` sidecar carries, keyed the way the
 * pointing rows wrote the path. A referent with no title of its own is left
 * out rather than mapped to its id: absence IS the id fallback, and mapping
 * it would make a titleless referent indistinguishable from an unresolved
 * one. */
export function titlesFromIncluded(
  included: Record<string, SubstrateRecord> | undefined
): ReferenceTitles {
  const out = new Map<string, string>()
  for (const [path, record] of Object.entries(included ?? {})) {
    const title = recordTitle(record.properties)
    if (title) out.set(path, title)
  }
  return out
}

/** The titles of a batch of records, keyed by their own canonical paths. The
 * batched read answers records, not the paths that were asked for; a pointer
 * written under a former id therefore resolves through `formerIds`, exactly
 * as the server's own expansion does. */
export function titlesFromRecords(
  records: readonly SubstrateRecord[]
): ReferenceTitles {
  const out = new Map<string, string>()
  for (const record of records) {
    const title = recordTitle(record.properties)
    if (!title) continue
    out.set(`${record.kind}/${record.id}`, title)
    for (const former of record.formerIds ?? []) {
      out.set(`${record.kind}/${former}`, title)
    }
  }
  return out
}

/** How deep a value is walked for pointers. A declared object may nest, and
 * the dialect allows four levels; the bound is here so a cyclic or
 * pathological `json` value cannot turn one record into an unbounded walk. */
const MAX_DEPTH = 6

function walk(value: unknown, depth: number, out: Set<string>): void {
  if (depth > MAX_DEPTH || value === null || value === undefined) return
  const held = readReference(value)
  if (held) {
    // A value is a pointer only if it names a record path; `{ref: "x"}` on a
    // json blob that means something else is not one.
    if (splitRecordPath(held.path)) out.add(held.path)
    // Link data hangs off the same object and may itself carry pointers.
    for (const one of Object.values(held.properties)) walk(one, depth + 1, out)
    return
  }
  if (Array.isArray(value)) {
    for (const one of value) walk(one, depth + 1, out)
    return
  }
  if (typeof value === "object") {
    for (const one of Object.values(value)) walk(one, depth + 1, out)
  }
}

/** Every record path one record points at, in no particular order: its own
 * reference properties, the elements of a repeated one, the values of a
 * `keyed:` map of pointers, and the pointers nested inside a declared object.
 *
 * It reads the VALUES rather than the declaration on purpose. The Properties
 * tab renders a pointer wherever it finds the served `{ref}` shape — a
 * declared `reference`, and a `json` value carrying one — so resolving by
 * declaration alone would leave exactly the pills the tab draws without a
 * name. A STRING is taken as a pointer when it parses as a record path,
 * because that is the authored shorthand the same pill already renders; a
 * prose value that happens to parse costs one id in the batch and answers
 * nothing. */
export function referencePathsOf(record: SubstrateRecord): string[] {
  const out = new Set<string>()
  for (const value of Object.values(record.properties)) walk(value, 0, out)
  return [...out]
}

/** The list-read scope that answers a set of paths: the kinds they name and
 * the ids within them.
 *
 * Ids are unique per kind, so ONE read carrying every kind and every id
 * answers the whole set — `filter.ids` narrows within the kinds already
 * selected (docs/api.md). Two kinds sharing an id means the page also carries
 * a record nobody asked about, which is keyed by its own path and ignored.
 *
 * A path whose kind is not in `known` is dropped: naming a kind the
 * repository never declared is `404` for the WHOLE read, and such a reference
 * renders as inert text anyway. */
export function titleReadScope(
  paths: readonly string[],
  known: ReadonlySet<string>
): { kinds: string[]; ids: string[] } {
  const kinds = new Set<string>()
  const ids = new Set<string>()
  for (const path of paths) {
    const target = splitRecordPath(path)
    if (!target || !known.has(target.kind)) continue
    kinds.add(target.kind)
    ids.add(target.id)
  }
  return { kinds: [...kinds].sort(), ids: [...ids].sort() }
}

// ── one record's title, read in batches ─────────────────────────────────────

/** How many referents one batched read asks for. */
const BATCH = 200
/** How long a batch waits for more asks: long enough for one render pass. */
const BATCH_WINDOW_MS = 10

interface Waiter {
  kind: string
  id: string
  settle: Array<{
    resolve: (title: string | null) => void
    reject: (error: unknown) => void
  }>
}

const waiting = new Map<string, Waiter>()
let flushTimer: ReturnType<typeof setTimeout> | undefined

async function flush(): Promise<void> {
  flushTimer = undefined
  const all = [...waiting.values()]
  waiting.clear()
  for (let at = 0; at < all.length; at += BATCH) {
    const batch = all.slice(at, at + BATCH)
    const kinds = [...new Set(batch.map((w) => w.kind))].sort()
    const ids = [...new Set(batch.map((w) => w.id))].sort()
    try {
      const page = await request<Page>(
        "GET",
        listPath({ kinds, first: BATCH * 2, filter: { ids } })
      )
      const titles = titlesFromRecords(page.records ?? [])
      for (const w of batch) {
        const title = titles.get(`${w.kind}/${w.id}`) ?? null
        for (const s of w.settle) s.resolve(title)
      }
    } catch (error) {
      for (const w of batch) for (const s of w.settle) s.reject(error)
    }
  }
}

/** One referent's title, collected with every other title asked for in the
 * same render into ONE list read. `kind` must be a kind the repository
 * declares: naming an unknown kind refuses the whole batch. */
export function batchedRecordTitle(
  kind: string,
  id: string
): Promise<string | null> {
  return new Promise((resolve, reject) => {
    const path = `${kind}/${id}`
    let waiter = waiting.get(path)
    if (!waiter) {
      waiter = { kind, id, settle: [] }
      waiting.set(path, waiter)
    }
    waiter.settle.push({ resolve, reject })
    flushTimer ??= setTimeout(() => void flush(), BATCH_WINDOW_MS)
  })
}

/** A single record's display title (null: it has none), batched. */
export function recordTitleQueryOptions(kind: string, id: string) {
  return queryOptions({
    queryKey: ["record-title", kind, id],
    queryFn: () => batchedRecordTitle(kind, id),
    staleTime: 60_000,
  })
}
