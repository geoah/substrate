/** The changelog's brain, kept pure: the flat row's verb and summary voice (the
 * changelog is a flat table now), the human reading of the records a write
 * moved (see "the affected records" below), the live tail's buffer
 * (pause-on-scroll holds rows aside), and the mapping from the toolbar's facet
 * rows to the wire's `ChangeFeedFilter` — including the two facets the wire
 * lacks: `authority` expands to its kinds (still server-side), time becomes a
 * seq seek plus a paging floor (see api/changes.ts). */

import type { ChangeFeedFilter } from "@/lib/api/changes"
import type { AffectedRecord, ChangeRow, KindInfo } from "@/lib/api/types"
import { CORE_PACKAGE } from "@/lib/api/http"
import type { ActiveFilter } from "@/lib/filters"
import type { DeclaredProperty } from "@/lib/definition"

// ── schema-change rows ──────────────────────────────────────────────────────

/** Registry vocabulary: a change to one of these kinds is the schema itself
 * moving (a bundle install, a mapping update), rendered set apart in the
 * changelog. The v1 meta-kinds: kind, propertytype, trait, recordmapping,
 * function, authority, package — plus the actor registrations that ride the
 * same install motions. All published by the core package. */
const SCHEMA_KIND_NAMES = [
  "kind",
  "propertytype",
  "trait",
  "recordmapping",
  "function",
  "authority",
  "package",
  "actor",
] as const

const SCHEMA_KINDS = new Set<string>(
  SCHEMA_KIND_NAMES.map((n) => `${CORE_PACKAGE}/${n}`)
)

export function isVocabularyChange(row: ChangeRow): boolean {
  return SCHEMA_KINDS.has(row.kind)
}

// ── the summary voice ───────────────────────────────────────────────────────

export function verbOf(row: ChangeRow): string {
  switch (row.op) {
    case "put":
      return row.payload?.created === true ? "created" : "updated"
    case "patch":
      return "updated"
    case "delete":
      return "deleted"
    case "merge":
      return "merged"
    case "split":
      return "split"
    case "gc":
      return "collected"
    default:
      return row.op
  }
}

/** The properties a change row touched, by name — the `properties` key carries
 * no values (states/managers ride their own keys; a value is read off the
 * record itself). */
export function changedProperties(row: ChangeRow): string[] {
  const properties = row.payload?.properties
  return Array.isArray(properties) ? properties.map(String) : []
}

/** A trigger or callable identity's plain first label:
 * `on-githubwriteback.providers.substrate.reamde.dev/github` → `on-githubwriteback` — the
 * summary column speaks it with a plain verb. */
function shortIdentity(name: string): string {
  return name.split(".")[0] || name
}

/** One flat row's summary: what changed, said plainly. Empty when the payload
 * says nothing. */
export function changeSummary(row: ChangeRow): string {
  const parts: string[] = []
  const props = changedProperties(row)
  if (props.length) {
    parts.push(
      props.length === 1
        ? `property: ${props[0]}`
        : `${props.length} properties: ${props.join(", ")}`
    )
  }
  if (row.payload?.restored === true) parts.push("restored")
  for (const tr of row.triggers ?? []) {
    const stance = `${shortIdentity(tr.trigger)} ${tr.state}`
    parts.push(tr.error ? `${stance}: ${tr.error}` : stance)
  }
  return parts.join(", ")
}

// ── the affected records ────────────────────────────────────────────────────
//
// A write's public event names every record it moved with the version each
// reached (decision 0061): the addressed record, a merge's tombstoned loser,
// a collection's purges. The write's stored replay effects never reach the
// wire, so this list is the whole of what the console knows about them, and a
// reader who wants a record's values opens the record.

/** One affected record, said in English: what became of it and which record
 * it is (`<kind>/<id>`). */
interface AffectedLine {
  /** `deleted`, `version N`, or `changed` when the entry predates versions. */
  verb: string
  target: string
}

function affectedVerb(a: AffectedRecord): string {
  if (a.deleted) return "deleted"
  if (typeof a.version === "number") return `version ${a.version}`
  return "changed"
}

/** The records a row moved, one line each, in the order the entry touched
 * them. Empty for a row that names none, and never throws on junk. */
export function affectedLines(row: ChangeRow): AffectedLine[] {
  const raw: unknown = row.affected
  if (!Array.isArray(raw)) return []
  return raw
    .filter(
      (a): a is AffectedRecord =>
        typeof a === "object" &&
        a !== null &&
        typeof (a as AffectedRecord).kind === "string" &&
        typeof (a as AffectedRecord).id === "string"
    )
    .map((a) => ({ verb: affectedVerb(a), target: `${a.kind}/${a.id}` }))
}

/** The payload keys the detail surface renders by hand; the remainder is shown
 * as JSON so nothing the wire said goes missing. */
export const NAMED_PAYLOAD_KEYS = new Set([
  "created",
  "restored",
  "properties",
  "states",
  "managers",
])

// ── the live buffer ─────────────────────────────────────────────────────────

/** What the watch has delivered: `rows` are showing, `pending` are held back
 * because the reader scrolled away from the top (pause-on-scroll). */
export interface LiveFeed {
  rows: ChangeRow[]
  pending: ChangeRow[]
}

export const EMPTY_LIVE_FEED: LiveFeed = { rows: [], pending: [] }

/** Bound the tail's memory; history stays a query concern. */
const LIVE_CAP = 2000

export function pushLive(
  feed: LiveFeed,
  row: ChangeRow,
  paused: boolean,
  cap = LIVE_CAP
): LiveFeed {
  if (
    feed.rows.some((r) => r.seq === row.seq) ||
    feed.pending.some((r) => r.seq === row.seq)
  ) {
    return feed
  }
  if (paused) return { ...feed, pending: [row, ...feed.pending].slice(0, cap) }
  return {
    rows: [row, ...feed.pending, ...feed.rows].slice(0, cap),
    pending: [],
  }
}

export function flushLive(feed: LiveFeed): LiveFeed {
  if (!feed.pending.length) return feed
  return { rows: [...feed.pending, ...feed.rows], pending: [] }
}

/** Live rows over history pages, one newest-first feed, exactly once per seq —
 * the watch resumes from the head the history query already read, so the seam
 * overlaps by design. */
export function mergeFeed(
  live: ChangeRow[],
  history: ChangeRow[]
): ChangeRow[] {
  const seen = new Set<number>()
  const out: ChangeRow[] = []
  for (const row of [...live, ...history]) {
    if (seen.has(row.seq)) continue
    seen.add(row.seq)
    out.push(row)
  }
  return out.sort((a, b) => b.seq - a.seq)
}

// ── facets → wire ───────────────────────────────────────────────────────────

export const CHANGE_OPS = ["put", "patch", "delete", "merge", "split", "gc"]

/** `Date.parse`-able input; "2026-08-05 22:00" style included. */
export function parseTimeInput(raw: string): number | undefined {
  const t = Date.parse(raw.trim())
  return Number.isNaN(t) ? undefined : t
}

/** An impossible kind: the honest answer to `kind ∧ authority = ∅` — the server
 * filters it to nothing rather than the console silently widening the AND. */
const NO_KIND = "∅"

interface ChangelogQuery {
  filter: ChangeFeedFilter
  /** Client floor for the time range (wire has no param; recorded). */
  sinceMs?: number
  /** Ceiling, served via the seq seek. */
  untilMs?: number
}

function values(filters: ActiveFilter[], field: string): string[] {
  return filters
    .filter((f) => f.field === field)
    .flatMap((f) => f.value.split(","))
    .map((s) => s.trim())
    .filter(Boolean)
}

/** Fold the toolbar's rows into the wire filter. `authority` expands to its
 * registry kinds (server-side via `kinds`); explicit kinds AND authorities
 * intersect. `since`/`until` come back as instants for the seek/floor. */
export function toChangelogQuery(
  filters: ActiveFilter[],
  kinds: KindInfo[]
): ChangelogQuery {
  const explicit = values(filters, "kind")
  const authorities = values(filters, "authority")
  let kindList = explicit
  if (authorities.length) {
    const inAuthorities = kinds
      .filter((k) => authorities.includes(k.authority))
      .map((k) => k.identity)
    kindList = explicit.length
      ? explicit.filter((k) => inAuthorities.includes(k))
      : inAuthorities
    if (!kindList.length) kindList = [NO_KIND]
  }
  const actors = values(filters, "actor")
  const ops = values(filters, "op")
  const q = filters.find((f) => f.field === "q")?.value.trim()
  const sinceRaw = filters.find((f) => f.field === "since")?.value
  const untilRaw = filters.find((f) => f.field === "until")?.value
  return {
    filter: {
      kinds: kindList.length ? kindList : undefined,
      actors: actors.length ? actors : undefined,
      ops: ops.length ? ops : undefined,
      q: q || undefined,
    },
    sinceMs: sinceRaw ? parseTimeInput(sinceRaw) : undefined,
    untilMs: untilRaw ? parseTimeInput(untilRaw) : undefined,
  }
}

/** The facet vocabulary as the shared filter toolbar's field shape — state
 * facets from known values, time facets take an instant as text. */
export function changelogFacetFields(opts: {
  kinds: KindInfo[]
  actors: string[]
  /** The actor view fixes the actor — its toolbar drops that facet. */
  fixedActor?: boolean
}): DeclaredProperty[] {
  const fields: DeclaredProperty[] = [
    {
      name: "kind",
      kind: "state",
      repeated: false,
      states: opts.kinds.map((k) => k.identity).sort(),
      description: "Changes to records of these kinds",
    },
    {
      name: "authority",
      kind: "state",
      repeated: false,
      states: [...new Set(opts.kinds.map((k) => k.authority))].sort(),
      description: "Changes anywhere under these authorities",
    },
  ]
  if (!opts.fixedActor) {
    fields.push({
      name: "actor",
      kind: "state",
      repeated: false,
      states: opts.actors,
      description: "Who made the change",
    })
  }
  fields.push(
    {
      name: "op",
      kind: "state",
      repeated: false,
      states: CHANGE_OPS,
      description: "What the change did",
    },
    {
      name: "since",
      kind: "time",
      repeated: false,
      description: "Changes at or after this time, such as 2026-08-05 14:00",
    },
    {
      name: "until",
      kind: "time",
      repeated: false,
      description: "Changes at or before this time",
    },
    {
      name: "q",
      kind: "string",
      repeated: false,
      description: "Text to look for in the kind, actor, record id or payload",
    }
  )
  return fields
}
