/** How a collection's grid reads its values: the labels its headers carry,
 * the friendly dates its temporal cells show, which columns open hidden and
 * what a nested row's subtask badge counts. Pure, so the page and its tests
 * share one answer. */

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"
import { displayPlural, lowerFirst } from "@/lib/kind-names"
import { stateTone } from "@/lib/state-words"

/** The mark an absent value reads as, in the grid and on the sheet alike. */
export const EMPTY_VALUE = "—"

/** Words a label keeps in capitals. */
const ACRONYMS: Record<string, string> = {
  url: "URL",
  id: "ID",
  ids: "IDs",
  api: "API",
  iana: "IANA",
}

/** A property key in everyday words: `memberOf` → "Member of", `dueAt` →
 * "Due", `updatedAt` → "Updated". A trailing "at" on a stamp says nothing a
 * date does not; a lone `at` is the moment itself. The key is still what a
 * filter, a sort and technical mode use. */
export function propertyLabel(name: string): string {
  if (name === "at") return "When"
  const words = name
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .trim()
    .split(/\s+/)
    .map((w) => w.toLowerCase())
  if (words.length > 1 && words[words.length - 1] === "at") words.pop()
  const out = words.map((w) => ACRONYMS[w] ?? w)
  const first = out[0] ?? name
  out[0] = first === first.toLowerCase() ? capitalise(first) : first
  return out.join(" ")
}

function capitalise(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

/** An enum value's words: its authored label, else the value split like a
 * key ("publicfigure" stays one word; `inProgress` → "In progress"). */
export function enumLabel(
  prop: Pick<DeclaredProperty, "values">,
  value: string
): string {
  const authored = prop.values?.find((v) => v.value === value)?.label
  return authored || propertyLabel(value)
}

const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
]
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]

function startOfDay(t: number): number {
  const d = new Date(t)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** A stamp as a person says it, in the reader's own zone: "Today",
 * "Tomorrow", "Yesterday", a weekday within the coming week, else "3 Oct"
 * ("3 Oct 2025" outside this year). `time` appends the clock ("Today,
 * 14:00"). A value that is not a date comes back as it was. */
export function friendlyDate(
  iso: string,
  now = Date.now(),
  opts: { time?: boolean } = {}
): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  const d = new Date(t)
  const days = Math.round((startOfDay(t) - startOfDay(now)) / 86_400_000)
  let day: string
  if (days === 0) day = "Today"
  else if (days === 1) day = "Tomorrow"
  else if (days === -1) day = "Yesterday"
  else if (days > 1 && days < 7) day = WEEKDAYS[d.getDay()]
  else {
    day = `${d.getDate()} ${MONTHS[d.getMonth()]}`
    if (d.getFullYear() !== new Date(now).getFullYear()) {
      day += ` ${d.getFullYear()}`
    }
  }
  if (!opts.time) return day
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${day}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** A bare `date` (`2026-10-03`) is a calendar day, not an instant: read in
 * UTC it would slip a day west of Greenwich. */
export function friendlyDay(value: string, now = Date.now()): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (!m) return friendlyDate(value, now)
  const local = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return friendlyDate(local.toISOString(), now)
}

/** A column whose stamp is a deadline, so its cells colour by how close it
 * is. */
export function isDueColumn(name: string): boolean {
  return /due|deadline/i.test(name)
}

export type DueTone = "done" | "overdue" | "soon"

/** How a deadline reads: muted once the record is done, red when it has
 * passed, amber inside the coming day, plain otherwise. */
export function dueTone(
  iso: string,
  now = Date.now(),
  done = false
): DueTone | undefined {
  if (done) return "done"
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return undefined
  if (t < now) return "overdue"
  if (t - now <= 86_400_000) return "soon"
  return undefined
}

/** Whether a stored state reads as finished ("done", "completed", …). */
export function isDoneState(value: unknown, initial?: string): boolean {
  return typeof value === "string" && stateTone(value, initial) === "ok"
}

/** The kind's state property whose machine has a finished state, which a
 * subtask badge and a due cell read. */
export function doneStateProperty(
  props: readonly DeclaredProperty[]
): DeclaredProperty | undefined {
  return props.find(
    (p) =>
      p.kind === "state" &&
      (p.states ?? []).some((s) => isDoneState(s, p.initial))
  )
}

/** A value with nothing in it: absent, blank, or an empty list or map. */
export function isEmptyValue(value: unknown): boolean {
  if (value === undefined || value === null) return true
  if (typeof value === "string") return value.trim() === ""
  if (Array.isArray(value)) return value.every(isEmptyValue)
  if (typeof value === "object") return Object.keys(value).length === 0
  return false
}

/** The property names a kind's `displayTemplate` titles its records from
 * (`{displayName|name}` → displayName, name). The title column already shows
 * them, so their own columns open hidden. */
export function titleProperties(kind: KindInfo): string[] {
  const template = kind.definition?.displayTemplate
  if (typeof template !== "string") return []
  const out = new Set<string>()
  for (const [, inner] of template.matchAll(/\{([^}]*)\}/g)) {
    for (const part of inner.split("|")) {
      const name = part.trim().split(/[\s:]/)[0]
      if (name && name !== "title") out.add(name)
    }
  }
  return [...out]
}

/** The one property a kind's title IS, when its `displayTemplate` is a
 * single slot whose first choice is a plain property (`{name}`,
 * `{displayName|localName}` → the first). The title column is that property,
 * so it earns no column and no sort of its own. A composed title
 * (`{state} #{localName}`) is made of properties, and is none of them. */
export function titleBacking(kind: KindInfo): string | undefined {
  const template = kind.definition?.displayTemplate
  if (typeof template !== "string") return undefined
  const slot = /^\s*\{([^{}]*)\}\s*$/.exec(template)
  const first = slot?.[1].split("|")[0].trim()
  if (!first || first === "title" || !/^[A-Za-z][A-Za-z0-9_]*$/.test(first)) {
    return undefined
  }
  return first
}

/** The columns with nothing in them on the loaded rows, among `ids`. */
export function emptyColumnIds(
  ids: readonly string[],
  rows: readonly SubstrateRecord[],
  valueOf: (row: SubstrateRecord, id: string) => unknown
): string[] {
  if (!rows.length) return []
  return ids.filter((id) => rows.every((row) => isEmptyValue(valueOf(row, id))))
}

/** What a nested row's badge says about its children: how many there are and,
 * where the kind has a finished state, how many are finished. */
export function subtaskCounts(
  children: readonly SubstrateRecord[],
  state: DeclaredProperty | undefined
): { total: number; done?: number } {
  if (!state) return { total: children.length }
  const done = children.filter((c) =>
    isDoneState(c.properties[state.name], state.initial)
  ).length
  return { total: children.length, done }
}

/** What an everyday list of collections leaves out, said once under it: how
 * many, two of them by name, and where they are. Empty when nothing is. */
export function hiddenKindsNote(hidden: readonly KindInfo[]): string {
  if (!hidden.length) return ""
  const like = hidden
    .slice(0, 2)
    .map((k) => lowerFirst(displayPlural(k)))
    .join(" and ")
  const n = hidden.length
  return `${n} more ${n === 1 ? "holds" : "hold"} supporting details (like ${like}). You see them from the records they belong to, or here with Technical details on.`
}
