/** The property sheet's pure decisions: how each row is edited, what one
 * edit writes, which icon a property wears, and which values take a block. */

import {
  AlignLeftIcon,
  AtSignIcon,
  BracesIcon,
  CalendarIcon,
  CheckSquareIcon,
  CircleDotIcon,
  ClockIcon,
  GlobeIcon,
  HashIcon,
  LinkIcon,
  ListIcon,
  LockIcon,
  PhoneIcon,
  RepeatIcon,
  TagIcon,
  TypeIcon,
  type LucideIcon,
} from "lucide-react"

import { kindGlyph } from "@/lib/kind-glyph"
import { toFieldValue, type FormField, type FormValue } from "@/lib/record-form"
import {
  TO_ANY,
  elementSpec,
  emptyContainer,
  parseValue,
  type PropSpec,
} from "@/lib/record-schema"

/** How a row is edited: on its own line, from a list that drops under it,
 * or in a panel that takes the row's full width. */
export function editStyle(field: FormField): "line" | "pop" | "panel" {
  switch (field.control) {
    case "text":
    case "number":
    case "datetime":
    case "secret":
    case "prose":
      return "line"
    case "select":
    case "state":
      return "pop"
    case "reference":
      return field.spec.to && field.spec.to !== TO_ANY ? "line" : "panel"
    default:
      return "panel"
  }
}

const same = (a: unknown, b: unknown) =>
  JSON.stringify(a ?? null) === JSON.stringify(b ?? null)

/** The one write, from a control's value: the property and nothing else, or
 * `null` to empty it. Answers `undefined` when there is nothing to send. */
export function propertyWrite(
  field: FormField,
  stored: unknown,
  next: FormValue
): { properties?: Record<string, unknown>; error?: string } {
  const submitted = toFieldValue(field, next)
  if (submitted.error) return { error: submitted.error }
  if (submitted.value === undefined || submitted.value === null) {
    if (stored === undefined || stored === null) return {}
    if (field.control === "secret") return {}
    // An emptied container is a claim ("none"), not an absence: null would
    // delete the property and let its sources refill it. Releasing to the
    // sources is the ownership chip's explicit act, never a side effect.
    const empty = emptyContainer(field.spec)
    if (empty !== undefined) {
      return same(stored, empty) ? {} : { properties: { [field.name]: empty } }
    }
    return { properties: { [field.name]: null } }
  }
  if (field.control !== "secret" && same(normalize(stored), submitted.value)) {
    return {}
  }
  return { properties: { [field.name]: submitted.value } }
}

/** The one write a list editor makes: the whole list, each item checked
 * against the element's datatype, blank items dropped. An emptied list is a
 * claim ("none") and writes `[]`, as `propertyWrite` does; an absent list left
 * empty writes nothing. Items are taken as typed, never split on commas: a
 * phone number or a name may carry one. */
export function listWrite(
  field: FormField,
  stored: unknown,
  items: readonly string[]
): { properties?: Record<string, unknown>; error?: string } {
  const kept = items.map((s) => s.trim()).filter(Boolean)
  const values: unknown[] = []
  for (const [i, item] of kept.entries()) {
    const parsed = parseValue(field.spec, item)
    if (parsed.error) return { error: `Item ${i + 1}: ${parsed.error}` }
    values.push(...((parsed.value as unknown[] | undefined) ?? []))
  }
  if (!values.length && (stored === undefined || stored === null)) return {}
  if (same(stored, values)) return {}
  return { properties: { [field.name]: values } }
}

/** A stored list's items as the strings a list editor holds. */
export function listItems(stored: unknown): string[] {
  if (!Array.isArray(stored)) return []
  return stored.map((v) =>
    typeof v === "object" && v !== null ? JSON.stringify(v) : String(v)
  )
}

/** A stored reference is served as `{ref}` and written as the path; compare
 * like with like. */
function normalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalize)
  if (
    value &&
    typeof value === "object" &&
    "ref" in value &&
    Object.keys(value).length === 1
  ) {
    return (value as { ref: unknown }).ref
  }
  return value
}

const TYPE_ICONS: Record<string, LucideIcon> = {
  state: CircleDotIcon,
  datetime: CalendarIcon,
  date: CalendarIcon,
  duration: ClockIcon,
  email: AtSignIcon,
  url: LinkIcon,
  phone: PhoneIcon,
  timezone: GlobeIcon,
  recurrence: RepeatIcon,
  markdown: AlignLeftIcon,
  text: AlignLeftIcon,
  json: BracesIcon,
  object: BracesIcon,
  secret: LockIcon,
  bool: CheckSquareIcon,
  boolean: CheckSquareIcon,
  int: HashIcon,
  integer: HashIcon,
  number: HashIcon,
  float: HashIcon,
  decimal: HashIcon,
}

/** A property's icon: a reference wears its target kind's own glyph icon, an
 * enum a tag, everything else its datatype's. */
export function propertyIcon(spec: PropSpec): { icon: LucideIcon } {
  if (spec.kind === "reference") {
    return {
      icon: spec.to && spec.to !== TO_ANY ? kindGlyph(spec.to).icon : LinkIcon,
    }
  }
  if (spec.values?.length) return { icon: TagIcon }
  if (spec.keyed) return { icon: BracesIcon }
  return {
    icon: TYPE_ICONS[spec.kind] ?? (spec.repeated ? ListIcon : TypeIcon),
  }
}

/** How a list's items read: short tokens (addresses, numbers, enum values,
 * tags) as chips that wrap inside the value's column, and anything a reader
 * reads rather than scans (records, links, prose, longer strings) one per
 * line. */
export function repeatedLayout(
  item: PropSpec,
  values: readonly unknown[]
): "chips" | "lines" {
  if (item.kind === "reference" || item.kind === "url") return "lines"
  if (item.kind === "markdown" || item.kind === "text") return "lines"
  if (item.values?.length) return "chips"
  if (item.kind === "email" || item.kind === "phone") return "chips"
  return values.every(
    (v) =>
      (typeof v === "string" && v.length <= CHIP_MAX && !v.includes("\n")) ||
      typeof v === "number" ||
      typeof v === "boolean"
  )
    ? "chips"
    : "lines"
}

/** The longest string that still reads as a chip. */
const CHIP_MAX = 32

/** Whether a value renders as a block under its row rather than on the
 * row's line. */
export function isBlockValue(spec: PropSpec, value: unknown): boolean {
  if (value === undefined || value === null) return false
  if (spec.kind === "reference" && !spec.repeated) return false
  if (spec.keyed) return true
  if (spec.kind === "object" || spec.kind === "json") return true
  if (spec.repeated) {
    const item = elementSpec(spec)
    if (item.kind === "object" || item.kind === "json") return true
    return (
      Array.isArray(value) &&
      value.length > 1 &&
      repeatedLayout(item, value) === "lines"
    )
  }
  return false
}
