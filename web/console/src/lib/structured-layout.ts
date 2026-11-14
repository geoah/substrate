/** How a declared object, or a list of them, reads wherever a value is shown:
 * which field names an item, the order its fields read in, which two fields
 * read as one range, and whether a list reads as a table or as stacked
 * items. The dialect has no key for any of this and gains none (decision
 * 0020), so every answer is inferred from the declaration and the values,
 * the same way every time, and a reader can predict it. */

import { elementSpec, personWritable, type PropSpec } from "@/lib/record-schema"

/** Whether a property reads as a structure of its own fields: a declared
 * object, alone or in a list. A keyed map keeps its own rendering. */
export function isStructured(spec: PropSpec): boolean {
  return spec.kind === "object" && Boolean(spec.fields?.length) && !spec.keyed
}

/** Whether a value says anything: absent, null, "", [] and {} do not. Zero
 * and false do. */
export function held(value: unknown): boolean {
  if (value === undefined || value === null || value === "") return false
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === "object") return Object.keys(value).length > 0
  return true
}

/** One item's fields as stored; anything that is not an object holds none. */
export function fieldsOf(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

/** A field that holds a structure of its own and reads as a section under
 * its item: a nested object, list of objects, keyed map or JSON. */
export function isNestedField(spec: PropSpec): boolean {
  if (isStructured(spec) || spec.keyed) return true
  return spec.kind === "json" || spec.kind === "object" || spec.kind === "map"
}

/** A field read as running text, which takes its item's full width. */
export function isProseField(spec: PropSpec): boolean {
  return (spec.kind === "text" || spec.kind === "markdown") && !spec.repeated
}

/** A field short enough to sit in a table cell. */
function isCompactField(spec: PropSpec): boolean {
  return !isNestedField(spec) && !isProseField(spec)
}

/** A field whose value is a short piece of text a reader names an item by. */
function isShortText(spec: PropSpec): boolean {
  if (spec.repeated || spec.keyed) return false
  return spec.kind === "string" || Boolean(spec.values?.length)
}

/** Fields a reader names an item by, in the order they are tried. */
const NAME_FIELDS = [
  "name",
  "title",
  "label",
  "displayName",
  "display",
  "subject",
  "summary",
]
/** Fields an item is known by when it carries no name. */
const HANDLE_FIELDS = ["key", "id", "code", "slug"]

/** The field that names each item of a list, or undefined when none does:
 * a name written on some item; else the one record every item is about (its
 * sole required reference); else its handle (`key`, `id`, `code`); else its
 * sole required short field. The same field for every item of the list, so
 * a reader learns where to look once. */
export function identityField(
  spec: PropSpec,
  items: readonly unknown[] = []
): PropSpec | undefined {
  const fields = spec.fields ?? []
  const byName = (name: string) => fields.find((f) => f.name === name)
  for (const name of NAME_FIELDS) {
    const field = byName(name)
    if (
      field &&
      isShortText(field) &&
      items.some((item) => held(fieldsOf(item)[name]))
    ) {
      return field
    }
  }
  const references = fields.filter(
    (f) => f.kind === "reference" && !f.repeated && !f.keyed && f.required
  )
  if (references.length === 1) return references[0]
  for (const name of HANDLE_FIELDS) {
    const field = byName(name)
    if (field && isShortText(field)) return field
  }
  const required = fields.filter((f) => f.required && isShortText(f))
  return required.length === 1 ? required[0] : undefined
}

/** Two fields read as one range: `repsMin` and `repsMax`, `durationMin` and
 * `durationMax`. Each keeps its own value and its own write. */
export interface RangePair {
  /** The stem the two names share: `reps`. */
  stem: string
  /** The range's label: the two labels' shared words ("Reps"). */
  label: string
  min: PropSpec
  max: PropSpec
}

const RANGE_END = /^(.+)(Min|Max)$/

/** The range pairs among an object's fields: a field `<stem>Min` beside a
 * field `<stem>Max` of the same datatype, neither a container. */
export function rangePairs(fields: readonly PropSpec[]): RangePair[] {
  const named = new Map(fields.map((f) => [f.name, f]))
  const out: RangePair[] = []
  for (const min of fields) {
    const end = RANGE_END.exec(min.name)
    if (!end || end[2] !== "Min") continue
    const max = named.get(`${end[1]}Max`)
    if (!max || max.kind !== min.kind) continue
    if (min.repeated || min.keyed || max.repeated || max.keyed) continue
    if (isNestedField(min) || isProseField(min)) continue
    out.push({ stem: end[1], label: rangeLabel(min, max), min, max })
  }
  return out
}

/** "Reps min" and "Reps max" read as "Reps"; labels an author wrote that do
 * not share their words keep the stem's own spelling. */
function rangeLabel(min: PropSpec, max: PropSpec): string {
  const a = /^(.*\S)\s+min(imum)?$/i.exec(min.label)
  const b = /^(.*\S)\s+max(imum)?$/i.exec(max.label)
  if (a && b && a[1] === b[1]) return a[1]
  const stem = RANGE_END.exec(min.name)?.[1] ?? min.name
  return stem.charAt(0).toUpperCase() + stem.slice(1)
}

/** The band a field reads in, the property sheet's own order: what an item
 * is about (references), how it is sorted (enums), when, then the plain
 * values, the prose, and the structures last. */
function band(spec: PropSpec): number {
  if (spec.kind === "state") return 0
  if (spec.kind === "reference") return spec.repeated ? 2 : 1
  if (spec.values?.length && !spec.repeated) return 3
  if (spec.kind === "datetime" || spec.kind === "date") return 4
  if (isNestedField(spec)) return 7
  if (isProseField(spec)) return 6
  return 5
}

/** A name that sorts a range's two ends together, the low end first:
 * alphabetical order alone reads `repsMax` before `repsMin`. */
function sortName(spec: PropSpec): string {
  const end = RANGE_END.exec(spec.name)
  if (!end) return spec.name
  return `${end[1]} ${end[2] === "Min" ? 0 : 1}`
}

/** An object's fields in reading order: the field that names it, then the
 * fields it must hold, then the rest by band, alphabetical within a band
 * with a range's ends together. Declaration order is lost to jsonb, so this
 * is the order every surface reads. */
export function orderedFields(spec: PropSpec, identity?: PropSpec): PropSpec[] {
  return [...(spec.fields ?? [])].sort((a, b) => {
    if (a.name === identity?.name) return -1
    if (b.name === identity?.name) return 1
    return (
      Number(b.required) - Number(a.required) ||
      band(a) - band(b) ||
      sortName(a).localeCompare(sortName(b))
    )
  })
}

/** One place an item shows a value: a field, a range's two fields, or a
 * key the item holds that its declaration does not name, read as it is
 * stored and never edited here. */
export type Cell =
  | { kind: "field"; field: PropSpec; label: string }
  | { kind: "range"; pair: RangePair; label: string }
  | { kind: "loose"; key: string; label: string }

/** The declared fields a cell writes; a loose key writes none. */
export function cellFields(cell: Cell): PropSpec[] {
  if (cell.kind === "loose") return []
  return cell.kind === "field" ? [cell.field] : [cell.pair.min, cell.pair.max]
}

/** A stable name for a cell: the field's name, a range's stem, or the
 * undeclared key. */
export function cellName(cell: Cell): string {
  if (cell.kind === "loose") return `${cell.key}~loose`
  return cell.kind === "field" ? cell.field.name : `${cell.pair.stem}~range`
}

/** The keys `items` hold that `fields` do not declare, in name order. A
 * write keeps them as they were read, so a read shows them. */
export function looseKeys(
  fields: readonly PropSpec[],
  items: readonly unknown[]
): string[] {
  const named = new Set(fields.map((f) => f.name))
  const keys = new Set<string>()
  for (const item of items) {
    for (const [key, value] of Object.entries(fieldsOf(item))) {
      if (!named.has(key) && held(value)) keys.add(key)
    }
  }
  return [...keys].sort((a, b) => a.localeCompare(b))
}

function looseCells(keys: readonly string[]): Cell[] {
  return keys.map((key) => ({ kind: "loose", key, label: key }))
}

/** Whether a cell's value fits a table cell: a short declared value, or an
 * undeclared one that holds no structure. */
function isCompactCell(cell: Cell, items: readonly unknown[]): boolean {
  if (cell.kind !== "loose") return cellFields(cell).every(isCompactField)
  return items.every((item) => {
    const value = fieldsOf(item)[cell.key]
    return value === null || typeof value !== "object"
  })
}

/** The cells for `fields`, in order, a range's two ends as one cell where
 * both are shown. */
export function cellsFor(
  ordered: readonly PropSpec[],
  pairs: readonly RangePair[]
): Cell[] {
  const shown = new Set(ordered.map((f) => f.name))
  const pairOf = new Map<string, RangePair>()
  for (const pair of pairs) {
    if (!shown.has(pair.min.name) || !shown.has(pair.max.name)) continue
    pairOf.set(pair.min.name, pair)
    pairOf.set(pair.max.name, pair)
  }
  const out: Cell[] = []
  const done = new Set<string>()
  for (const field of ordered) {
    const pair = pairOf.get(field.name)
    if (!pair) {
      out.push({ kind: "field", field, label: field.label })
      continue
    }
    if (done.has(pair.stem)) continue
    done.add(pair.stem)
    out.push({ kind: "range", pair, label: pair.label })
  }
  return out
}

/** How a list of objects lays out its items, decided once per list. */
export interface ListLayout {
  /** A table when every shown field fits a cell and there are few enough
   * of them; stacked items otherwise. */
  mode: "table" | "stack"
  /** The field that names each item, if any. */
  identity?: PropSpec
  /** Every field in reading order. */
  fields: PropSpec[]
  /** The cells the list shows: its required fields and every field some
   * item holds, plus `extra`. A table's columns, a stacked item's facts. */
  cells: Cell[]
  pairs: RangePair[]
}

/** The most columns a list reads well as a table, a range counting as one. */
export const TABLE_COLUMNS = 6

/** How `items` of the object list `spec` lay out. `extra` names fields to
 * show although no item holds them yet (one being set right now); they
 * never change the mode, so a list does not reflow under an open editor. */
export function listLayout(
  spec: PropSpec,
  items: readonly unknown[],
  extra: readonly string[] = []
): ListLayout {
  const item = elementSpec(spec)
  const identity = identityField(item, items)
  const fields = orderedFields(item, identity)
  const pairs = rangePairs(fields)
  const shownBy = (more: readonly string[]) =>
    fields.filter(
      (f) =>
        f.required ||
        more.includes(f.name) ||
        items.some((one) => held(fieldsOf(one)[f.name]))
    )
  const loose = looseCells(looseKeys(fields, items))
  const stored = [...cellsFor(shownBy([]), pairs), ...loose]
  const fits =
    stored.length <= TABLE_COLUMNS &&
    stored.every((cell) => isCompactCell(cell, items))
  return {
    mode: fits ? "table" : "stack",
    identity,
    fields,
    cells: extra.length
      ? [...cellsFor(shownBy(extra), pairs), ...loose]
      : stored,
    pairs,
  }
}

/** The cells one object shows: the fields it must hold and the fields it
 * holds, plus `extra`, in reading order. */
export function objectCells(
  spec: PropSpec,
  value: unknown,
  extra: readonly string[] = [],
  identity?: PropSpec
): Cell[] {
  const fields = orderedFields(elementSpec(spec), identity)
  const bag = fieldsOf(value)
  return [
    ...cellsFor(
      fields.filter(
        (f) => f.required || extra.includes(f.name) || held(bag[f.name])
      ),
      rangePairs(fields)
    ),
    ...looseCells(looseKeys(fields, [value])),
  ]
}

/** The optional fields an item could still be given: everything it does not
 * hold and the owner may write, in reading order. A structure is added from
 * its own section once the item exists, so only fields that take one value
 * are offered. */
export function fieldsToAdd(
  fields: readonly PropSpec[],
  value: unknown
): PropSpec[] {
  const bag = fieldsOf(value)
  return fields.filter(
    (f) =>
      !held(bag[f.name]) && !f.managed && personWritable(f) && !isNestedField(f)
  )
}

/** What a collapsed item says beside its name: up to two of its own values
 * and how many entries each nested list holds. */
export function itemSummary(
  layout: ListLayout,
  value: unknown,
  limit = 2
): Array<{ label: string; cell?: Cell; count?: number }> {
  const bag = fieldsOf(value)
  const out: Array<{ label: string; cell?: Cell; count?: number }> = []
  for (const cell of layout.cells) {
    if (out.length >= limit) break
    if (cell.kind === "loose") continue
    if (cell.kind === "field" && cell.field.name === layout.identity?.name) {
      continue
    }
    const fields = cellFields(cell)
    if (fields.some(isNestedField) || fields.some(isProseField)) continue
    if (!fields.some((f) => held(bag[f.name]))) continue
    out.push({ label: cell.label, cell })
  }
  for (const field of layout.fields) {
    if (!isStructured(field) || !field.repeated) continue
    const list = bag[field.name]
    if (Array.isArray(list) && list.length) {
      out.push({ label: field.label, count: list.length })
    }
  }
  return out
}

/** How a range's two ends read: one value where they agree, "12–15" where
 * they differ, and the end that is set where only one is. `text` turns one
 * end into its words. */
export function rangeText(
  min: unknown,
  max: unknown,
  text: (value: unknown) => string
): string {
  const lo = held(min) ? text(min) : ""
  const hi = held(max) ? text(max) : ""
  if (lo && hi) return lo === hi ? lo : `${lo}–${hi}`
  if (lo) return `≥ ${lo}`
  if (hi) return `≤ ${hi}`
  return ""
}
