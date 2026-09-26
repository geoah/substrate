/** History in sentences: the change feed's rows folded into what a person
 * would say happened ("You added 14 tasks", "Google Contacts sync changed
 * Grace Hopper"), grouped under the day they happened. Pure, so the feed
 * component only renders. */

import {
  actorIdentity,
  agentName,
  providerInfo,
  PROVIDERS_AUTHORITY,
} from "@/lib/actor-identity"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME, splitKind } from "@/lib/api/http"
import type { ChangeRow, KindInfo } from "@/lib/api/types"
import type { ValueMove } from "@/lib/change-values"
import { changedProperties } from "@/lib/changelog"
import { kindPurpose } from "@/lib/definition"
import { packageDisplayName } from "@/lib/kind-names"

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

const TRIGGER_RUN = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/triggerrun`

/** Housekeeping rather than a change anybody made: collecting what a delete
 * left behind, and pruning a trigger's finished runs. */
export function isHousekeeping(row: ChangeRow): boolean {
  return row.op === "gc" || (row.op === "delete" && row.kind === TRIGGER_RUN)
}

/** The sentences a feed shows, housekeeping only with technical details on,
 * folded. */
export function historyEntries(
  rows: readonly ChangeRow[],
  technical: boolean
): HistoryEntry[] {
  return foldHistory(technical ? rows : rows.filter((r) => !isHousekeeping(r)))
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

/** The two ways a technical reader can lay the feed out. */
export const HISTORY_LAYOUTS: readonly {
  value: "sentences" | "table"
  label: string
}[] = [
  { value: "sentences", label: "Sentences" },
  { value: "table", label: "Table view" },
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

// ── the substrate's own records, said as what they are to a person ────────

/** The rest of a sentence about one of the substrate's own records, after the
 * actor: "updated its package to version 35", "updated the [Gmail threads]
 * collection", "changed your console layout (sidebar closed)". */
export interface SystemPhrase {
  words: string
  /** A collection the sentence names, drawn as a KindRef after `words`. */
  collection?: string
  /** Words after the collection. */
  tail?: string
  /** The phrase says all an everyday reader needs: the value moves stay
   * for technical details. */
  complete: boolean
}

const VERB_WORDS: Record<string, string> = {
  added: "added",
  changed: "updated",
  deleted: "removed",
  restored: "restored",
}

function counted(n: number, one: string, many: string): string {
  return n === 1 ? one : `${n.toLocaleString()} ${many}`
}

/** A package's name as a person reads it: the provider's own name, else its
 * own word ("Notes"). */
function packageName(identity: string): string {
  const [owner, word = ""] = identity.split("/")
  return owner === PROVIDERS_AUTHORITY
    ? providerInfo(word).name
    : packageDisplayName(word)
}

function newVersion(moves: readonly ValueMove[] | undefined): unknown {
  return moves?.find((m) => m.name === "version")?.after
}

/** What a trigger run ran, by name: its `callableRef` names the function
 * or the agent as a core record. */
function runCallable(
  moves: readonly ValueMove[] | undefined
): string | undefined {
  const ref = moves?.find((m) => m.name === "callableRef")?.after
  const path =
    ref && typeof ref === "object" && "ref" in ref ? String(ref.ref) : ""
  const core = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/`
  if (!path.startsWith(core)) return undefined
  const rest = path.slice(core.length)
  const slash = rest.indexOf("/")
  const kind = rest.slice(0, slash)
  const id = rest.slice(slash + 1)
  if (kind === "function")
    return actorIdentity(`function:${id.split("/").join(":")}`).name
  if (kind === "agent") return `the agent ${agentName(id)}`
  return undefined
}

/** How each console layout setting reads inside the parenthesis: the
 * words the Settings page labels it with. */
const LAYOUT_WORDS: Record<string, string> = {
  density: "row height",
  recordWidth: "record page width",
  tableWidth: "table width",
  theme: "appearance",
  collapsed: "folded sections",
  favorites: "favorites",
  technicalDetails: "technical details",
  sidebarOpen: "sidebar",
}

function capitalised(value: unknown): string {
  const text = String(value)
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

/** "sidebar closed, table width Full", from the values when the server sent
 * them and from the names when it did not. */
export function layoutSummary(
  names: readonly string[],
  moves: readonly ValueMove[] | undefined
): string {
  if (!moves) {
    return names
      .map((n) => LAYOUT_WORDS[n] ?? propertyLabel(n).toLowerCase())
      .join(", ")
  }
  const parts: string[] = []
  for (const m of moves) {
    const word = LAYOUT_WORDS[m.name] ?? propertyLabel(m.name).toLowerCase()
    const after = m.after
    if (m.name === "sidebarOpen" && typeof after === "boolean") {
      parts.push(after ? "sidebar open" : "sidebar closed")
    } else if (typeof after === "boolean") {
      parts.push(`${word} ${after ? "on" : "off"}`)
    } else if (Array.isArray(after)) {
      const moved = m.beforeUnknown
        ? after.length > 0
        : (m.added ?? []).length + (m.removed ?? []).length > 0
      if (moved) parts.push(word)
    } else if (after !== undefined && after !== null && after !== "") {
      parts.push(`${word} ${capitalised(after)}`)
    }
  }
  return parts.join(", ")
}

/** The sentence for a change to one of the substrate's own records: its
 * packages, collections, tools, agents, sign-ins, runs and the console's own
 * layout. Undefined for anything else, which reads as an ordinary record.
 * `moves` are the entry's net value moves when it touched one record and the
 * server sent them. */
export function systemPhrase(
  entry: HistoryEntry,
  moves?: readonly ValueMove[]
): SystemPhrase | undefined {
  const { authority, pkg, name } = splitKind(entry.kind)
  if (authority !== CORE_AUTHORITY || pkg !== CORE_PACKAGE_NAME)
    return undefined
  const verb = VERB_WORDS[entry.verb]
  if (!verb) return undefined
  const n = entry.records.length
  const one = n === 1 ? entry.records[0] : undefined
  const own = (identity: string) =>
    entry.actor === `bundle:${identity.split("/").join(":")}`
  switch (name) {
    case "bundle":
    case "package": {
      if (!one) return undefined
      const self = own(one)
      const called = packageName(one)
      const version = newVersion(moves)
      if (name === "bundle") {
        if (entry.verb === "added")
          return {
            words: self ? "was added" : `added ${called}`,
            complete: true,
          }
        if (entry.verb === "deleted")
          return {
            words: self ? "was removed" : `removed ${called}`,
            complete: true,
          }
        if (entry.verb !== "changed") return undefined
        if (version !== undefined)
          return {
            words: self
              ? `updated to version ${version}`
              : `updated ${called} to version ${version}`,
            complete: true,
          }
        return {
          words: self ? "changed its setup" : `changed how ${called} is set up`,
          complete: false,
        }
      }
      const object = self ? "its package" : `the ${called} package`
      const to =
        entry.verb === "changed" && version !== undefined
          ? ` to version ${version}`
          : ""
      return { words: `${verb} ${object}${to}`, complete: true }
    }
    case "authority":
      return {
        words: `${verb} ${counted(n, "its publisher name", "publisher names")}`,
        complete: true,
      }
    case "kind":
      return one
        ? {
            words: `${verb} the`,
            collection: one,
            tail: "collection",
            complete: true,
          }
        : { words: `${verb} ${n.toLocaleString()} collections`, complete: true }
    case "function":
      return {
        words: one
          ? `${verb} the tool ${actorIdentity(`function:${one.split("/").join(":")}`).name}`
          : `${verb} ${n.toLocaleString()} tools`,
        complete: true,
      }
    case "agent":
      return {
        words: one
          ? `${verb} the agent ${agentName(one)}`
          : `${verb} ${n.toLocaleString()} agents`,
        complete: true,
      }
    case "trigger":
      return {
        words: `${verb} ${counted(n, "a trigger", "triggers")}`,
        complete: true,
      }
    case "recordmapping":
      return {
        words: `${verb} ${counted(n, "a mapping", "mappings")}`,
        complete: true,
      }
    case "triggerrun": {
      if (entry.verb === "added") {
        const callable = one ? runCallable(moves) : undefined
        return {
          words: callable
            ? `ran ${callable}`
            : n === 1
              ? "ran a trigger"
              : `ran triggers ${n.toLocaleString()} times`,
          complete: true,
        }
      }
      const runs = counted(n, "a trigger run", "trigger runs")
      return {
        words:
          entry.verb === "deleted"
            ? `cleared ${counted(n, "a finished run", "finished runs")}`
            : `${verb} ${runs}`,
        complete: true,
      }
    }
    case "token": {
      if (entry.verb !== "added" && entry.verb !== "deleted") return undefined
      const person = actorIdentity(entry.actor).cls === "you"
      const inOut = entry.verb === "added" ? "in" : "out"
      const times = n > 1 ? ` ${n.toLocaleString()} times` : ""
      return {
        words: person
          ? `signed ${inOut}${times}`
          : `signed you ${inOut}${times}`,
        complete: true,
      }
    }
    case "consolepreference": {
      const summary = layoutSummary(entry.properties, moves)
      const doing = entry.verb === "added" ? "set up" : "changed"
      if (entry.verb !== "added" && entry.verb !== "changed") return undefined
      return {
        words: `${doing} your console layout${summary ? ` (${summary})` : ""}`,
        complete: true,
      }
    }
    default:
      return undefined
  }
}
