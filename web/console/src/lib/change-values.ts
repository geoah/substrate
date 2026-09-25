/** What a change did to each property, in values: "Priority: High → Urgent",
 * "+ grace@example.com". The server derives each before and after
 * (`affected[].properties`, decision 0106) when a read asks with `values=1`;
 * a server that predates it sends names alone, and every helper here answers
 * `undefined` so the caller falls back to them. Pure. */

import type { ChangeRow, KindInfo, PropertyChange } from "@/lib/api/types"
import { changedProperties } from "@/lib/changelog"
import {
  propSpecsByName,
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
 * property that came back to where it started drops out. Undefined when any
 * row that names properties carries no values for the record, so the caller
 * says names, never a half-told story. */
export function netMoves(
  rows: readonly ChangeRow[],
  id?: string,
  kind?: string
): ValueMove[] | undefined {
  const moves = new Map<string, ValueMove>()
  for (const row of [...rows].reverse()) {
    const values = rowValues(row, id ?? row.recordId, kind ?? row.kind)
    if (!values) {
      if (changedProperties(row).length) return undefined
      continue
    }
    for (const pc of values) {
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
  return [...moves.values()]
    .filter((m) => m.beforeUnknown || !same(m.before, m.after))
    .map(listDiff)
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
