// What the runner does with a record a provider's plan writes that is
// already there (docs/plans/healthspan.md, Importers: Writing a provider's
// plan): the scripts' `decide`, by the record's revision, for the records
// the importer maintains; and the merge an organization takes instead.
// Pure, so each branch is tested alone.
//
// Content is compared as the scripts compare it: references and documents
// by their target and digest, quantities by number and unit (through the
// repository's unit graph), instants as instants, and a property neither
// side holds as equal.

import type { SubstrateRecord } from "@substrate/client"
import type { PlannedRecord } from "./importers/types"
import { refPath, unitName, type UnitKey } from "./records"
import { unitSpellingKey } from "../labs/units"

/** A record's properties as the runner writes them: every planned
 * reference resolved to its path and every document to its digest. */
export type Resolved = Record<string, unknown>

export type Decision =
  /** As the plan writes it, at a revision no lower: nothing written. */
  | { kind: "unchanged" }
  /** A newer export already wrote it: nothing written. */
  | { kind: "stale" }
  /** The same content at a lower revision: the revision alone raised. */
  | { kind: "raise"; props: Resolved }
  /** Other content at a revision no higher: every property the importer
   * maintains written, null where the plan no longer gives one. */
  | { kind: "update"; props: Resolved }

/** Properties the decision never compares or rewrites: the import that
 * created the record stays its `origin`, and the revision is decided
 * apart. */
const OUTSIDE = new Set(["origin", "revision"])

const INSTANT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/

function held(v: unknown): boolean {
  return v !== undefined && v !== null && !(Array.isArray(v) && !v.length)
}

/** A value in the form two sides compare in. */
function norm(v: unknown, unitKey: UnitKey): unknown {
  if (!held(v)) return undefined
  if (typeof v === "string") {
    if (INSTANT.test(v)) {
      const t = Date.parse(v)
      if (!Number.isNaN(t)) return `\u0000t${t}`
    }
    return v
  }
  if (Array.isArray(v)) return v.map((x) => norm(x, unitKey))
  if (typeof v === "object") {
    const o = v as Record<string, unknown>
    if (typeof o.ref === "string") return o.ref
    if (typeof o.digest === "string" && !("value" in o)) return o.digest
    if ("value" in o && "unit" in o && o.unit !== undefined) {
      const unit = unitName(o.unit)
      const n = Number(o.value)
      return `\u0000q${Number.isNaN(n) ? String(o.value) : n}|${
        unit === undefined ? "" : unitKey(unit)
      }|${typeof o.comparator === "string" ? o.comparator : ""}`
    }
    const out: Record<string, unknown> = {}
    for (const [k, x] of Object.entries(o)) {
      const n = norm(x, unitKey)
      if (n !== undefined) out[k] = n
    }
    return Object.keys(out).length ? out : undefined
  }
  return v
}

function equal(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b))
    return a.length === b.length && a.every((x, i) => equal(x, b[i]))
  if (
    a &&
    b &&
    typeof a === "object" &&
    typeof b === "object" &&
    !Array.isArray(a) &&
    !Array.isArray(b)
  ) {
    const x = a as Record<string, unknown>
    const y = b as Record<string, unknown>
    const keys = new Set([...Object.keys(x), ...Object.keys(y)])
    for (const k of keys) if (!equal(x[k], y[k])) return false
    return true
  }
  return false
}

/** Whether a planned value and a stored one are the same content. */
export function sameValue(
  planned: unknown,
  stored: unknown,
  unitKey: UnitKey = unitSpellingKey
): boolean {
  return equal(norm(planned, unitKey), norm(stored, unitKey))
}

/** The properties an importer maintains on each kind of its plan: every
 * one a record of the kind names, a null included. An importer names a
 * property it maintains that the provider no longer gives as null, so a
 * value an earlier export wrote is cleared; a property no record of the
 * kind names is not the importer's, and is left as it is. */
export function maintainedByKind(
  records: readonly PlannedRecord[]
): Map<string, Set<string>> {
  const out = new Map<string, Set<string>>()
  for (const r of records) {
    if (r.mode !== "maintain") continue
    let set = out.get(r.kind)
    if (!set) out.set(r.kind, (set = new Set()))
    for (const k of Object.keys(r.properties)) if (!OUTSIDE.has(k)) set.add(k)
  }
  return out
}

function revisionOf(r: SubstrateRecord): number | undefined {
  const v = r.properties.revision
  return typeof v === "number" && Number.isFinite(v) ? v : undefined
}

/** The decision on a record the plan maintains that is already there. */
export function decideMaintained(
  planned: {
    props: Resolved
    revision?: number
    createOnly?: readonly string[]
  },
  maintained: ReadonlySet<string>,
  found: SubstrateRecord,
  unitKey: UnitKey = unitSpellingKey
): Decision {
  const skip = new Set(planned.createOnly ?? [])
  const keys = [...new Set([...maintained, ...Object.keys(planned.props)])]
    .filter((k) => !OUTSIDE.has(k) && !skip.has(k))
    .sort()
  const same = keys.every((k) =>
    sameValue(planned.props[k], found.properties[k], unitKey)
  )
  const stored = revisionOf(found)
  const rev = planned.revision
  if (same) {
    if (rev === undefined || (stored !== undefined && stored >= rev))
      return { kind: "unchanged" }
    return { kind: "raise", props: { revision: rev } }
  }
  if (rev !== undefined && stored !== undefined && stored > rev)
    return { kind: "stale" }
  const props: Resolved = {}
  for (const k of keys) {
    const v = planned.props[k]
    if (held(v)) props[k] = v
    else if (held(found.properties[k])) props[k] = null
  }
  if (rev !== undefined) props.revision = rev
  return { kind: "update", props }
}

export type Merge = { kind: "unchanged" } | { kind: "update"; props: Resolved }

/** An identifier's identity: its system and value. */
function identifierKey(v: unknown): string | undefined {
  if (!v || typeof v !== "object") return undefined
  const o = v as Record<string, unknown>
  if (typeof o.value !== "string") return undefined
  return `${typeof o.system === "string" ? o.system : ""}\u0000${o.value}`
}

/** An organization merged, never replaced (docs/plans/healthspan.md,
 * Sources and organizations): its identifiers the union of both sides' by
 * system and value, every other field written only where the stored record
 * has none, whatever either side's revision, and the revision raised only
 * upward and otherwise left out. Anything added, or the revision raised,
 * is an update; else it is unchanged. Never stale. */
export function mergeInto(
  planned: { props: Resolved; revision?: number },
  found: SubstrateRecord
): Merge {
  const out: Resolved = {}
  for (const [k, v] of Object.entries(planned.props)) {
    if (OUTSIDE.has(k) || !held(v)) continue
    const have = found.properties[k]
    if (k === "identifiers" && Array.isArray(v)) {
      const stored: unknown[] = Array.isArray(have) ? (have as unknown[]) : []
      const keys = new Set(stored.map(identifierKey).filter(Boolean))
      const added = (v as unknown[]).filter((x) => {
        const key = identifierKey(x)
        if (!key || keys.has(key)) return false
        keys.add(key)
        return true
      })
      if (added.length) out.identifiers = [...stored, ...added]
      continue
    }
    if (!held(have) || have === "") out[k] = v
  }
  const stored = revisionOf(found)
  if (
    planned.revision !== undefined &&
    (stored === undefined || planned.revision > stored)
  )
    out.revision = planned.revision
  return Object.keys(out).length
    ? { kind: "update", props: out }
    : { kind: "unchanged" }
}

/** Whether a record's `origin` is the import at `path`. */
export function originIs(r: SubstrateRecord, path: string): boolean {
  return refPath(r.properties.origin) === path
}
