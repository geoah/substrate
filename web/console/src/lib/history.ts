/** History in sentences: the change feed's rows folded into what a person
 * would say happened ("You added 14 tasks", "Google Contacts sync changed
 * Grace Hopper"), grouped under the day they happened. Pure, so the feed
 * component only renders. */

import { actorIdentity, PROVIDERS_AUTHORITY } from "@/lib/actor-identity"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import { changedProperties } from "@/lib/changelog"
import { kindPurpose } from "@/lib/definition"

/** Runs of the same actor doing the same thing to the same kind fold into one
 * sentence while each row lands within this long of the one before it. */
export const FOLD_WINDOW_MS = 10 * 60_000

/** What a row did, as the verb of a sentence. */
export function historyVerb(row: ChangeRow): string {
  switch (row.op) {
    case "put":
      if (row.payload?.restored === true) return "restored"
      return row.payload?.created === true ? "added" : "changed"
    case "patch":
      return "changed"
    case "delete":
      return "deleted"
    case "merge":
      return "merged"
    case "split":
      return "split"
    case "gc":
      return "cleaned up"
    default:
      return row.op
  }
}

export interface HistoryEntry {
  /** The newest row's seq: stable while the entry grows at its old end. */
  key: string
  actor: string
  verb: string
  kind: string
  /** Newest first. */
  rows: ChangeRow[]
  /** The distinct records the rows touched, in first-seen order. */
  records: string[]
  /** The newest row's time. */
  ts: string
  /** Every property the rows named, first-seen order. */
  properties: string[]
}

/** Fold a newest-first feed into sentences. Only neighbours fold: a run is
 * broken by any row from another actor, verb or kind, so the order of what
 * happened is never rearranged. */
export function foldHistory(
  rows: readonly ChangeRow[],
  windowMs = FOLD_WINDOW_MS
): HistoryEntry[] {
  const out: HistoryEntry[] = []
  let last: HistoryEntry | undefined
  let lastTs = 0
  for (const row of rows) {
    const verb = historyVerb(row)
    const ts = Date.parse(row.ts)
    const fits =
      last &&
      last.actor === row.actor &&
      last.verb === verb &&
      last.kind === row.kind &&
      Math.abs(lastTs - ts) <= windowMs
    if (fits && last) {
      last.rows.push(row)
      if (!last.records.includes(row.recordId)) last.records.push(row.recordId)
      for (const p of changedProperties(row))
        if (!last.properties.includes(p)) last.properties.push(p)
    } else {
      last = {
        key: String(row.seq),
        actor: row.actor,
        verb,
        kind: row.kind,
        rows: [row],
        records: [row.recordId],
        ts: row.ts,
        properties: [...changedProperties(row)],
      }
      out.push(last)
    }
    lastTs = ts
  }
  return out
}

export interface HistoryDay {
  /** "Today", "Yesterday", or the date. */
  label: string
  /** The local calendar day, `YYYY-MM-DD`. */
  key: string
  entries: HistoryEntry[]
}

function localDay(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, "0")
  const d = String(date.getDate()).padStart(2, "0")
  return `${y}-${m}-${d}`
}

/** The heading a day's changes sit under, in the reader's own time zone. */
export function dayLabel(iso: string, now = Date.now()): string {
  const at = new Date(iso)
  const today = new Date(now)
  if (localDay(at) === localDay(today)) return "Today"
  const yesterday = new Date(now)
  yesterday.setDate(yesterday.getDate() - 1)
  if (localDay(at) === localDay(yesterday)) return "Yesterday"
  return at.toLocaleDateString(undefined, {
    weekday: "long",
    day: "numeric",
    month: "long",
    ...(at.getFullYear() !== today.getFullYear() && { year: "numeric" }),
  })
}

export function groupByDay(
  entries: readonly HistoryEntry[],
  now = Date.now()
): HistoryDay[] {
  const out: HistoryDay[] = []
  for (const entry of entries) {
    const key = localDay(new Date(entry.ts))
    const day = out[out.length - 1]
    if (day && day.key === key) day.entries.push(entry)
    else out.push({ key, label: dayLabel(entry.ts, now), entries: [entry] })
  }
  return out
}

/** A property key as a label: `dueAt` → "Due at", `display_name` →
 * "Display name". */
export function propertyLabel(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
    .toLowerCase()
  return words ? words[0].toUpperCase() + words.slice(1) : key
}

/** The views the History page offers, each a set of actors the change feed
 * filters on server-side. */
export type HistoryView = "everything" | "you" | "agents" | "providers"

export const HISTORY_VIEWS: readonly { value: HistoryView; label: string }[] = [
  { value: "everything", label: "Everything" },
  { value: "you", label: "By you" },
  { value: "agents", label: "By agents" },
  { value: "providers", label: "By providers" },
]

/** What the views filter on: the actor names the repository knows, and the
 * declaration ids (`<authority>/<package>/<name>`, a bundle's
 * `<authority>/<package>`) the agent, function and bundle actors are
 * derived from (decision 0025). */
export interface ViewActorSources {
  actors: readonly string[]
  agents: readonly string[]
  functions: readonly string[]
  bundles: readonly string[]
}

/** The doors a person writes through, always "you" whether or not the actor
 * mirror lists them yet. */
const PERSON_DOORS = ["console", "substratectl", "api"]

function colons(id: string): string {
  return id.split("/").join(":")
}

/** The actors one view reads: undefined for everything, an empty list when the
 * repository has no actor of that sort (the feed is then empty without a
 * read, since an empty filter would mean every actor). */
export function viewActors(
  view: HistoryView,
  sources: ViewActorSources
): string[] | undefined {
  switch (view) {
    case "everything":
      return undefined
    case "you":
      return [
        ...new Set([
          ...PERSON_DOORS,
          ...sources.actors.filter((a) => actorIdentity(a).cls === "you"),
        ]),
      ].sort()
    case "agents":
      return sources.agents.map((id) => `agent:${colons(id)}`).sort()
    case "providers":
      return [
        ...sources.functions
          .filter((id) => id.startsWith(`${PROVIDERS_AUTHORITY}/`))
          .map((id) => `function:${colons(id)}`),
        ...sources.bundles
          .filter((id) => id.startsWith(`${PROVIDERS_AUTHORITY}/`))
          .map((id) => `bundle:${colons(id)}`),
      ].sort()
  }
}

/** A change to machinery rather than to data a person keeps: a write to a
 * kind the console lists as internal (every kind the substrate's own
 * authority publishes among them) — trigger runs, tokens, preferences. A
 * row whose kind the registry no longer carries is judged by its reference
 * alone. */
export function isSystemChange(
  row: ChangeRow,
  kinds: ReadonlyMap<string, KindInfo>
): boolean {
  return kindPurpose(kinds.get(row.kind) ?? row.kind) === "internal"
}

/** The registry keyed by full reference, the lookup `isSystemChange` reads. */
export function kindsByReference(
  kinds: readonly KindInfo[] | undefined
): Map<string, KindInfo> {
  return new Map((kinds ?? []).map((k) => [k.identity, k]))
}
