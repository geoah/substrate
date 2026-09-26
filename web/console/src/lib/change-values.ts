/** What a change did to each property, in values: "Priority: High → Urgent",
 * "+ grace@example.com". The server derives each before and after
 * (`affected[].properties`, decision 0108) when a read asks with `values=1`;
 * a server that predates it sends names alone, and every helper here answers
 * `undefined` so the caller falls back to them. Pure. */

import type { ChangeRow, KindInfo, PropertyChange } from "@/lib/api/types"
import { changedProperties } from "@/lib/changelog"
import {
  ownerWritable,
  propSpecsByName,
  REDACTED,
  systemSpecs,
  type PropSpec,
} from "@/lib/record-schema"

/** One property's move across one or more rows. */
export interface ValueMove {
  name: string
  /** Absent: the record held no value before (unless `beforeUnknown`). */
  before?: unknown
  /** Absent: the change cleared it. */
  after?: unknown
  /** The server could not derive the before: say only where it landed. */
  beforeUnknown: boolean
  /** A list whose before is known: the items it gained and lost. Absent for
   * a single value, and for a list that only changed order. */
  added?: unknown[]
  removed?: unknown[]
  /** A sensitive value was written: both sides read sealed, so the words say
   * it was replaced rather than show two equal markers. */
  replaced?: boolean
  /** Across a run, the value moved and came back to where it began. */
  changedBack?: boolean
}

/** The values one row carries for one record, or undefined when the server
 * sent none for it (an older server, or a kind it no longer declares). */
export function rowValues(
  row: ChangeRow,
  id: string = row.recordId,
  kind: string = row.kind
): PropertyChange[] | undefined {
  return row.affected?.find((a) => a.id === id && a.kind === kind)?.properties
}

function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b)
}

/** Items of `from` with no equal in `other`, counted: a list that holds the
 * same value twice and loses one copy lost one. */
function without(from: unknown[], other: unknown[]): unknown[] {
  const pool = other.map((v) => JSON.stringify(v))
  const out: unknown[] = []
  for (const v of from) {
    const i = pool.indexOf(JSON.stringify(v))
    if (i >= 0) pool.splice(i, 1)
    else out.push(v)
  }
  return out
}

function listDiff(move: ValueMove): ValueMove {
  if (move.beforeUnknown) return move
  const { before, after } = move
  const isList = (v: unknown) => v === undefined || Array.isArray(v)
  if (!isList(before) || !isList(after)) return move
  if (!Array.isArray(before) && !Array.isArray(after)) return move
  const was = (before as unknown[] | undefined) ?? []
  const is = (after as unknown[] | undefined) ?? []
  const added = without(is, was)
  const removed = without(was, is)
  // Reordered only: say it as the list it became.
  if (!added.length && !removed.length) return move
  return { ...move, added, removed }
}

/** The net move of each property across a run of rows on one record, newest
 * first as the feed reads: the oldest row's before, the newest row's after. A
 * move whose two sides read the same is still a move the server named: a
 * sealed value was replaced, or the run moved it and moved it back. Undefined
 * when any row that names properties carries no values for the record, or
 * when rows that name properties leave nothing to tell, so the caller says
 * names, never a half-told story or none at all. */
export function netMoves(
  rows: readonly ChangeRow[],
  id?: string,
  kind?: string
): ValueMove[] | undefined {
  const moves = new Map<string, ValueMove>()
  const touched = new Map<string, number>()
  for (const row of [...rows].reverse()) {
    const values = rowValues(row, id ?? row.recordId, kind ?? row.kind)
    if (!values) {
      if (changedProperties(row).length) return undefined
      continue
    }
    for (const pc of values) {
      touched.set(pc.name, (touched.get(pc.name) ?? 0) + 1)
      const seen = moves.get(pc.name)
      if (seen) {
        seen.after = pc.after
        continue
      }
      moves.set(pc.name, {
        name: pc.name,
        before: pc.before,
        after: pc.after,
        beforeUnknown: pc.beforeUnknown === true,
      })
    }
  }
  const out = [...moves.values()].map((m): ValueMove => {
    if (m.beforeUnknown || !same(m.before, m.after)) return listDiff(m)
    // A sealed value reads the marker on both sides whatever was written; a
    // single row the server named moved something it cannot show.
    if (m.before === REDACTED || (touched.get(m.name) ?? 0) < 2) {
      return { ...m, replaced: true }
    }
    return { ...m, changedBack: true }
  })
  if (!out.length && rows.some((r) => changedProperties(r).length)) {
    return undefined
  }
  return out
}

/** Each property's spec for rendering its values: the declared ones, and the
 * built-in title and instants where the kind does not declare its own. */
export function valueSpecs(kind: KindInfo | undefined): Map<string, PropSpec> {
  const out = new Map<string, PropSpec>()
  if (!kind) return out
  for (const s of systemSpecs(kind)) out.set(s.name, s)
  for (const s of propSpecsByName(kind)) out.set(s.name, s)
  return out
}

/** Whether a property is the host's to write rather than a person's: one
 * the engine manages (a digest, a version) or one a connector or the OAuth
 * facility alone may set (a cursor, a sync status). */
export function hostWritten(spec: PropSpec | undefined): boolean {
  return spec !== undefined && (spec.managed || !ownerWritable(spec))
}

/** No value to show: absent, null, an empty string or an empty list. */
export function isBlank(value: unknown): boolean {
  return (
    value === undefined ||
    value === null ||
    value === "" ||
    (Array.isArray(value) && value.length === 0)
  )
}

/** The moves an everyday reader is told about: none the host wrote, and
 * none with nothing to say (an empty list or value set where there was
 * none, a list diff that gained and lost nothing). */
export function readerMoves(
  moves: readonly ValueMove[],
  specs: ReadonlyMap<string, PropSpec>
): ValueMove[] {
  return moves.filter((m) => {
    if (hostWritten(specs.get(m.name))) return false
    if (m.replaced || m.changedBack) return true
    if (m.added || m.removed) {
      return (m.added ?? []).length + (m.removed ?? []).length > 0
    }
    return !(isBlank(m.after) && !m.beforeUnknown && isBlank(m.before))
  })
}

/** Long text is cut to this many characters in a sentence; the whole value
 * rides the hover. */
export const VALUE_CHARS = 60

/** A value as one short line of text, and the whole of it for the hover. */
export function shortText(value: unknown): { text: string; full: string } {
  const full =
    typeof value === "string" ? value : (JSON.stringify(value) ?? String(value))
  const line = full.replace(/\s+/g, " ").trim()
  const text =
    line.length > VALUE_CHARS ? `${line.slice(0, VALUE_CHARS - 1)}…` : line
  return { text, full }
}
