/** The changelog's brain, kept pure: the flat row's verb and summary voice (the
 * changelog is a flat table now), the human reading of a write's EFFECTS (see
 * "the effects" below), the live tail's buffer (pause-on-scroll holds rows
 * aside), and the mapping from the toolbar's facet rows to the wire's
 * `ChangeFeedFilter` — including the two facets the wire lacks: `authority`
 * expands to its kinds (still server-side), time becomes a seq seek plus a
 * paging floor (see api/changes.ts). */

import type { ChangeFeedFilter } from "@/lib/api/changes"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
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
    case "gc":
      return "collected"
    default:
      return row.op
  }
}

/** The properties a change row touched, by name — the `properties` key carries
 * no values (states/managers ride their own keys, and the write's recorded
 * effects carry the rest; see `changeEffects`). */
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

// ── the effects ─────────────────────────────────────────────────────────────
//
// Every committed write records, in order, the EFFECTS it applied — record
// deltas with their values, tombstones, manager rows — because
// the log is the truth and the records table is a fold of it, and a rebuild
// replays exactly these (engine/fold.go).
//
// That machinery has an internal name the reader must never meet: the payload
// carries the list under `fold`, an implementation word for an implementation
// concern. This section is the translation — one honest English line per
// effect, and an unknown effect kind names itself rather than disappearing, so
// a substrate that grows a new one degrades to "did something we don't render"
// instead of lying by omission. The raw JSON stays one disclosure away.

/** Where a write's effects live in the changelog payload. INTERNAL — this is
 * the only place in the console that may know the key, and nothing renders it. */
const EFFECTS_PAYLOAD_KEY = "fold"

/** One effect as the wire encodes it (engine/fold.go `foldOp`). Every field is
 * optional: the encoding is one flat union and each effect kind reads its own. */
interface ChangeEffect {
  kind?: string
  /** The kind reference of the record the effect lands on. */
  ref?: string
  id?: string
  delta?: {
    created?: boolean
    restored?: boolean
    force?: boolean
    set?: Record<string, unknown>
    del?: string[]
    title?: string
    body?: string
    at?: string
    endsAt?: string
    dueAt?: string
    states?: Record<string, string>
    labels?: Record<string, unknown>
    finalizers?: string[]
  }
  finalizer?: string
  key?: string
  value?: unknown
  property?: string
  actor?: string
  tier?: string
  formerId?: string
  scope?: { kind?: string; id?: string }[]
  rows?: {
    annotations?: unknown[]
    managers?: unknown[]
    formerIds?: unknown[]
  }
}

/** One effect, said in English. `detail` is the second, quieter half — the
 * property names that moved, the counts a resync restated — and is empty when
 * the effect has nothing more to say. `actor` is the row's, not the effect's:
 * the log attributes a whole write, not each effect inside it. */
export interface EffectLine {
  /** What happened, e.g. `updated` / `restored` / `deleted`. */
  verb: string
  /** The record it happened to, `<kind>/<id>`, or "" when the effect names no
   * single record (a resync names a scope). */
  target: string
  detail: string
}

function refOf(effect: ChangeEffect): string {
  const kind = effect.ref ?? ""
  const id = effect.id ?? ""
  if (kind && id) return `${kind}/${id}`
  return kind || id
}

function names(list: unknown): string[] {
  return Array.isArray(list) ? list.map(String) : []
}

/** The record delta's detail: which properties took a value, which were
 * cleared, and which of the record's own columns moved. Values themselves are
 * NOT spelled here — the delta carries them, and the raw disclosure is where a
 * reader who wants them looks; a summary that inlined every value would be the
 * payload again, just slower to read. */
function deltaDetail(delta: NonNullable<ChangeEffect["delta"]>): string {
  const parts: string[] = []
  const set = Object.keys(delta.set ?? {})
  const del = names(delta.del)
  if (set.length) parts.push(`set ${set.join(", ")}`)
  if (del.length) parts.push(`cleared ${del.join(", ")}`)
  const columns: string[] = []
  if (delta.title !== undefined) columns.push("title")
  if (delta.body !== undefined) columns.push("body")
  if (delta.at !== undefined) columns.push("at")
  if (delta.endsAt !== undefined) columns.push("endsAt")
  if (delta.dueAt !== undefined) columns.push("dueAt")
  if (delta.states !== undefined) columns.push("states")
  if (delta.labels !== undefined) columns.push("labels")
  if (delta.finalizers !== undefined) columns.push("finalizers")
  if (columns.length) parts.push(`moved ${columns.join(", ")}`)
  return parts.join("; ")
}

function resyncDetail(effect: ChangeEffect): string {
  const scope = effect.scope ?? []
  const rows = effect.rows ?? {}
  const counted: string[] = []
  const count = (label: string, list: unknown[] | undefined) => {
    const n = list?.length ?? 0
    if (n) counted.push(`${n} ${label}${n === 1 ? "" : "s"}`)
  }
  count("annotation", rows.annotations)
  count("manager row", rows.managers)
  count("former id", rows.formerIds)
  const where = scope
    .map((s) => (s.kind && s.id ? `${s.kind}/${s.id}` : (s.id ?? s.kind ?? "")))
    .filter(Boolean)
    .join(", ")
  const what = counted.length ? counted.join(", ") : "nothing left"
  return where ? `${what} — on ${where}` : what
}

/** One effect → one line. An effect kind this console does not know still
 * renders: it names itself and its target rather than being dropped. */
export function effectLine(effect: ChangeEffect): EffectLine {
  const target = refOf(effect)
  switch (effect.kind) {
    case "record": {
      const delta = effect.delta ?? {}
      const verb = delta.created
        ? "created"
        : delta.restored
          ? "restored"
          : "updated"
      return { verb, target, detail: deltaDetail(delta) }
    }
    case "tombstone":
      return {
        verb: "deleted",
        target,
        detail: effect.finalizer ? `held by ${effect.finalizer}` : "",
      }
    case "purge":
      return { verb: "purged", target, detail: "and everything hanging off it" }
    case "bump":
      return { verb: "touched", target, detail: "version only" }
    case "annotation":
      // The engine carries the value as a POINTER precisely so `false`, `0`
      // and `""` stay values: only an ABSENT value is the deletion. JSON
      // cannot tell absent from null, so both read as the deletion here.
      return {
        verb:
          effect.value === undefined || effect.value === null
            ? "un-annotated"
            : "annotated",
        target,
        detail: effect.key ?? "",
      }
    case "manager":
      return effect.actor
        ? {
            verb: "reassigned",
            target,
            detail: `${effect.property ?? "?"} → ${effect.actor}${effect.tier ? ` (${effect.tier})` : ""}`,
          }
        : {
            verb: "released",
            target,
            detail: `${effect.property ?? "?"} has no manager`,
          }
    case "former":
      return {
        verb: "aliased",
        target: effect.ref ? `${effect.ref}/${effect.formerId ?? ""}` : "",
        detail: effect.id ? `now resolves to ${effect.id}` : "",
      }
    case "resync":
      return { verb: "restated", target: "", detail: resyncDetail(effect) }
    default:
      // An effect kind this console has not learned. Name it plainly — the
      // reader can open the raw payload, and the line is a signal that this
      // console is older than the substrate it is talking to.
      return {
        verb: effect.kind ? `${effect.kind} (unrecognized)` : "unrecognized",
        target,
        detail: "",
      }
  }
}

/** The write's effects, in the order it applied them, each said in English.
 * Empty for a row whose payload records none (an older entry, or an operation
 * that moved nothing). */
export function changeEffects(row: ChangeRow): EffectLine[] {
  const raw = row.payload?.[EFFECTS_PAYLOAD_KEY]
  if (!Array.isArray(raw)) return []
  return raw
    .filter((e): e is ChangeEffect => typeof e === "object" && e !== null)
    .map(effectLine)
}

/** The payload keys the detail surface renders by hand; the remainder is shown
 * as JSON so nothing the wire said goes missing. `fold` is here because its
 * effects render as `changeEffects` above — it must never reach a reader raw. */
export const NAMED_PAYLOAD_KEYS = new Set([
  "created",
  "restored",
  "properties",
  "states",
  "managers",
  EFFECTS_PAYLOAD_KEY,
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

export interface ChangelogQuery {
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
      description: "Who committed the change",
    })
  }
  fields.push(
    {
      name: "op",
      kind: "state",
      repeated: false,
      states: CHANGE_OPS,
      description: "The commit's operation",
    },
    {
      name: "since",
      kind: "time",
      repeated: false,
      description: "Events at or after this instant (e.g. 2026-08-05 14:00)",
    },
    {
      name: "until",
      kind: "time",
      repeated: false,
      description: "Events at or before this instant",
    },
    {
      name: "q",
      kind: "string",
      repeated: false,
      description: "Substring over kind, actor, record id and payload",
    }
  )
  return fields
}
