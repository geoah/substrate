/** The property sheet's rows, decided without React: which properties a record
 * page lists, in what order, which of them a person may edit in place, and
 * which fold away as empty. */

import type { KindInfo, PropertyMeta, SubstrateRecord } from "@/lib/api/types"
import { fieldOf, type FormField } from "@/lib/record-form"
import {
  bodyProperty,
  ownerWritable,
  propSpecsByName,
  systemSpecs,
  titleProperty,
  type PropSpec,
} from "@/lib/record-schema"

/** Why a row is not editable in place. */
export type RowLock =
  /** The engine stamps it and refuses a write that disagrees. */
  | "managed"
  /** A declared `writer:` other than the owner: the host keeps it. */
  | "host"
  /** A provider's own copy: the whole record is read-only here. */
  | "provider"
  /** The record carries it and the kind never declared it. */
  | "undeclared"

export interface SheetRow {
  name: string
  spec: PropSpec
  /** The control an edit opens, when the row is editable. */
  field?: FormField
  value: unknown
  filled: boolean
  lock?: RowLock
  meta?: PropertyMeta
}

/** Whether a stored value says anything: absent, null, "", [] and {} do
 * not. */
export function isFilled(value: unknown): boolean {
  if (value === undefined || value === null || value === "") return false
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === "object") return Object.keys(value).length > 0
  return true
}

/** Reading order: what a record is doing (its states), who and what it
 * belongs to (references), how it is sorted (enums), when (datetimes), then
 * the plain values, the lists, and the shapes last. Alphabetical within a
 * band, because the declaration's own order is lost to jsonb. */
function rank(spec: PropSpec): number {
  if (spec.kind === "state") return 0
  if (spec.kind === "reference") return spec.repeated ? 2 : 1
  if (spec.values?.length) return 3
  if (spec.kind === "datetime" || spec.kind === "date") return 4
  if (spec.kind === "object" || spec.kind === "json" || spec.keyed) return 7
  if (spec.kind === "markdown" || spec.kind === "text") return 6
  if (spec.repeated) return 5
  return 5
}

/** A spec for a value no declaration names, read off its shape. */
function looseSpec(name: string, value: unknown): PropSpec {
  return {
    name,
    label: name,
    kind: typeof value === "object" && value !== null ? "json" : "string",
    required: false,
    repeated: false,
    keyed: false,
    managed: false,
  }
}

export interface SheetRows {
  /** Every row, in reading order. */
  all: SheetRow[]
  filled: SheetRow[]
  empty: SheetRow[]
  /** The property the page title shows, never a row. */
  title: string
  /** The prose property shown as the body under the sheet, never a row. */
  body?: PropSpec
}

/** Every row a record page shows for `record`. `readOnly` locks the whole
 * sheet (a provider's copy). */
export function sheetRows(
  record: SubstrateRecord,
  kind: KindInfo | undefined,
  readOnly = false
): SheetRows {
  const title = titleProperty(kind)
  const body = bodyProperty(kind)
  const skip = new Set(["title", title])
  if (body) skip.add(body.name)
  const specs: PropSpec[] = []
  const named = new Set<string>()
  if (kind) {
    for (const spec of propSpecsByName(kind)) {
      named.add(spec.name)
      specs.push(spec)
    }
    // A temporal trait binds a hot column the kind may not declare; it is
    // legal on the record and belongs on the sheet.
    for (const spec of systemSpecs(kind)) {
      if (named.has(spec.name) || spec.name === "title") continue
      named.add(spec.name)
      specs.push({ ...spec, description: undefined })
    }
  }
  const rows: SheetRow[] = []
  for (const spec of specs) {
    if (skip.has(spec.name)) continue
    const value = record.properties[spec.name]
    const lock: RowLock | undefined = readOnly
      ? "provider"
      : spec.managed
        ? "managed"
        : !ownerWritable(spec)
          ? "host"
          : undefined
    rows.push({
      name: spec.name,
      spec,
      field: lock ? undefined : fieldOf(spec),
      value,
      filled: isFilled(value),
      lock,
      meta: record.propertyMeta?.[spec.name],
    })
  }
  for (const name of Object.keys(record.properties).sort()) {
    if (named.has(name) || skip.has(name)) continue
    const value = record.properties[name]
    if (!isFilled(value)) continue
    rows.push({
      name,
      spec: looseSpec(name, value),
      value,
      filled: true,
      lock: readOnly ? "provider" : "undeclared",
      meta: record.propertyMeta?.[name],
    })
  }
  rows.sort(
    (a, b) => rank(a.spec) - rank(b.spec) || a.name.localeCompare(b.name)
  )
  return {
    all: rows,
    filled: rows.filter((r) => r.filled),
    empty: rows.filter((r) => !r.filled),
    title,
    body,
  }
}
