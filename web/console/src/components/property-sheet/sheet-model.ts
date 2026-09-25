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

/** Whether a value renders as a block under its row rather than on the
 * row's line. */
export function isBlockValue(spec: PropSpec, value: unknown): boolean {
  if (value === undefined || value === null) return false
  if (spec.kind === "reference") return false
  if (spec.keyed) return true
  if (spec.kind === "object" || spec.kind === "json") return true
  if (spec.repeated) {
    const item = elementSpec(spec)
    return item.kind === "object" || item.kind === "json"
  }
  return false
}
