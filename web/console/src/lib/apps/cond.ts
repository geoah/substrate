/** Two matchers over one record. `matchesWhen` is the `when` gate on an
 * action: one property, an `in` list coerced to the property's datatype, or
 * presence, optionally inverted. `matchesFilter` is a BEST-EFFORT reading of
 * the wire filter grammar, used only to guess whether a record still belongs
 * in a cached page after an optimistic write; the server's answer, refetched
 * on the next watch row, heals a wrong guess. A reference compares by its
 * path, a datetime by its instant, a number numerically. */

import { readReference, type RecordFilter, type Cond } from "@/lib/api/types"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { propSpecs, systemSpecs, type PropSpec } from "@/lib/record-schema"
import type { WhenSpec } from "./spec"

const NUMERIC = new Set([
  "int",
  "integer",
  "int64",
  "float",
  "number",
  "decimal",
  "double",
])
const BOOLEAN = new Set(["bool", "boolean"])
const TEMPORAL = new Set(["datetime", "date", "timestamp"])

/** Every property a record of this kind may carry: the declared ones and the
 * hot columns its traits bind. */
export function allSpecs(kind: KindInfo): PropSpec[] {
  return [...propSpecs(kind), ...systemSpecs(kind)]
}

export function specOf(
  kind: KindInfo | undefined,
  name: string
): PropSpec | undefined {
  return kind ? allSpecs(kind).find((s) => s.name === name) : undefined
}

/** A declaration's string coerced to the property's datatype, so `in: ["3"]`
 * on an int meets the record's `3`. */
export function coerceLike(spec: PropSpec | undefined, raw: string): unknown {
  if (!spec) return raw
  if (NUMERIC.has(spec.kind)) {
    const n = Number(raw)
    return Number.isFinite(n) ? n : raw
  }
  if (BOOLEAN.has(spec.kind)) return raw === "true"
  return raw
}

/** One stored value read for comparison: a reference as its path, a repeated
 * reference as its paths, everything else as itself. */
export function comparable(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(comparable)
  const held = readReference(value)
  return held ? held.path : value
}

function same(a: unknown, b: unknown, spec?: PropSpec): boolean {
  if (spec && TEMPORAL.has(spec.kind)) {
    const ta = Date.parse(String(a))
    const tb = Date.parse(String(b))
    if (!Number.isNaN(ta) && !Number.isNaN(tb)) return ta === tb
  }
  if (typeof a === "number" || typeof b === "number") {
    return Number(a) === Number(b)
  }
  return String(a) === String(b)
}

function present(value: unknown): boolean {
  if (value === undefined || value === null || value === "") return false
  return !(Array.isArray(value) && value.length === 0)
}

export function matchesWhen(
  record: SubstrateRecord,
  when: WhenSpec,
  kind?: KindInfo
): boolean {
  const spec = specOf(kind, when.property)
  const value = comparable(record.properties[when.property])
  let hit: boolean
  if (when.exists) {
    hit = present(value)
  } else if (when.in?.length) {
    const wanted = when.in.map((raw) => coerceLike(spec, raw))
    hit = Array.isArray(value)
      ? value.some((v) => wanted.some((w) => same(v, w, spec)))
      : wanted.some((w) => same(value, w, spec))
  } else {
    hit = present(value)
  }
  return when.not ? !hit : hit
}

function ordered(a: unknown, b: unknown, spec?: PropSpec): number | undefined {
  if (spec && TEMPORAL.has(spec.kind)) {
    const ta = Date.parse(String(a))
    const tb = Date.parse(String(b))
    if (Number.isNaN(ta) || Number.isNaN(tb)) return undefined
    return ta - tb
  }
  if (typeof a === "number" && typeof b === "number") return a - b
  const na = Number(a)
  const nb = Number(b)
  if (Number.isFinite(na) && Number.isFinite(nb) && a !== "" && b !== "") {
    return na - nb
  }
  return String(a).localeCompare(String(b))
}

function matchesCond(value: unknown, cond: Cond, spec?: PropSpec): boolean {
  if (cond.exists !== undefined && present(value) !== cond.exists) return false
  if (cond.eq !== undefined) {
    const hit = Array.isArray(value)
      ? value.some((v) => same(v, comparable(cond.eq), spec))
      : same(value, comparable(cond.eq), spec)
    if (!hit) return false
  }
  if (cond.in) {
    const wanted = cond.in.map(comparable)
    const hit = Array.isArray(value)
      ? value.some((v) => wanted.some((w) => same(v, w, spec)))
      : wanted.some((w) => same(value, w, spec))
    if (!hit) return false
  }
  if (cond.prefix !== undefined) {
    if (typeof value !== "string" || !value.startsWith(cond.prefix)) {
      return false
    }
  }
  if (cond.contains !== undefined) {
    const wanted = comparable(cond.contains)
    if (!Array.isArray(value) || !value.some((v) => same(v, wanted, spec))) {
      return false
    }
  }
  for (const [op, test] of [
    ["gt", (d: number) => d > 0],
    ["gte", (d: number) => d >= 0],
    ["lt", (d: number) => d < 0],
    ["lte", (d: number) => d <= 0],
  ] as const) {
    const bound = cond[op]
    if (bound === undefined) continue
    if (!present(value)) return false
    const d = ordered(value, comparable(bound), spec)
    if (d === undefined || !test(d)) return false
  }
  return true
}

/** Whether a record satisfies a filter, as far as one page can tell. `ids`,
 * `properties` and `labels` are checked; `kinds` and `implements` are the
 * collection's business and `deleted` is never set by a view. */
export function matchesFilter(
  record: SubstrateRecord,
  filter: RecordFilter | undefined,
  kind?: KindInfo
): boolean {
  if (!filter) return true
  if (filter.ids && !filter.ids.includes(record.id)) return false
  for (const [name, cond] of Object.entries(filter.properties ?? {})) {
    const spec = specOf(kind, name)
    if (!matchesCond(comparable(record.properties[name]), cond, spec)) {
      return false
    }
  }
  for (const [name, cond] of Object.entries(filter.labels ?? {})) {
    if (!matchesCond(record.labels?.[name], cond)) return false
  }
  return true
}
