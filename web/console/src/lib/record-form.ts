/** The typed record form's pure core: which declared properties become editable
 * fields, how a record seeds them, how the values validate, and how they coerce
 * back into a properties payload. Two surfaces read it: the integrations
 * dialog (`RecordConfigForm`, one bundle config or account record) and the
 * record editor's form lens (any kind at all). The DECLARATION itself is read
 * through `record-schema.ts`, which the YAML lens reads too, so the two lenses
 * can never disagree about what a property is.
 *
 * Invariants that live here, not in the components:
 * - HOST-MANAGED properties are never offered for editing, driven off the
 *   declaration's `writer:` role and NOT a name blacklist. A property whose
 *   `writer` is a non-owner role (the OAuth facility's `oauth`, the connector
 *   runtime's `connector`) is excluded; an owner-writable property (an explicit
 *   `writer: owner`, or none at all) is offered. So tokenRef/tokenStatus/
 *   grantedScopes and the connector's sync state drop out because the schema
 *   marks them, not because the console knows their names.
 * - MANAGED properties (`managed: true`) are the ENGINE's stamp: it refuses a
 *   write that disagrees with what it stamped, so the form never submits one
 *   and never validates one. It still SHOWS them, read-only, because a
 *   declaration's version is worth reading where the declaration is edited.
 * - SECRET fields are write-only: their stored value never seeds the form (a
 *   read serves `<redacted>` anyway). A blank secret is OMITTED on patch (the
 *   sealed value stands) but REQUIRED on create, because a create with a blank
 *   secret can never work.
 * - A value is checked against its DATATYPE before it is submitted
 *   (`checkValue`), so a bad datetime or a string where an int is declared is
 *   named on the field instead of arriving as a 422.
 * - Ordinary fields can be EXPLICITLY CLEARED (value `null`), which on patch
 *   sends `null` to delete the key; a merely-blank field is left untouched. */

import type { SubstrateRecord, EnumValue, KindInfo } from "@/lib/api/types"
import { readReference } from "@/lib/api/types"
import { coerceReferencePath, splitRecordPath } from "@/lib/record-path"
import type { EditPath as DocumentPath } from "@/lib/record-yaml"
import {
  TO_ANY,
  checkKey,
  checkValue,
  controlFor,
  elementSpec,
  emptyContainer,
  exampleFor,
  formatValue,
  inputTypeFor,
  ownerWritable,
  parseValue,
  propSpecsByName,
  type Control,
  type PropSpec,
} from "@/lib/record-schema"

export { humanizeName } from "@/lib/record-schema"

/** Whether the form is minting a new record or patching an existing one: the
 * two modes differ on secret handling and on what "blank" means. */
export type FormMode = "create" | "patch"

/** One object's fields as the form holds them: the same per-control value every
 * top-level property uses, keyed by field name. A repeated object holds a list
 * of these, one per row. A field is a property in its own right, so a bag's
 * value is a whole `FormValue`: a nested object is another bag, a keyed field
 * another row list.
 *
 * An interface rather than a `Record<string, FormValue>` alias: the bag and the
 * value refer to each other, and a type alias cannot close that loop. */
export interface FieldBag {
  [field: string]: FormValue
}

/** One row of a KEYED map: the author's key beside the value it maps to,
 * whatever control that value earns. A row list rather than an object because
 * a half-typed key is still a row, and an object keyed by the draft would lose
 * the row's identity on every keystroke. */
export interface KeyedRow {
  key: string
  value: FormValue
}

/** One editable field, projected from a declared property. `spec` is the whole
 * declaration, so a control can reach for the states, the `to:` target or the
 * range without the projection having to flatten every one of them. */
export interface FormField {
  name: string
  /** The human label: the declaration's `displayName`, or the humanized
   * property id, never the raw camelCase name. */
  label: string
  /** The control this property earns (`record-schema.controlFor`). */
  control: Control
  /** The HTML input type for a `text` control (email/url get the right
   * keyboard and affordance). */
  inputType: "text" | "email" | "url"
  /** `select`-only: the admitted enum options, declaration order. An option
   * shows its authored `label` when it has one and submits the raw `value`. */
  options?: EnumValue[]
  /** A declared `default:` — seeds the field on CREATE; a patch seeds from the
   * record instead. */
  defaultValue?: string
  /** The declaration marked it `required`: the form refuses to submit empty. */
  required: boolean
  description?: string
  /** A worked value for the datatype, shown as the control's placeholder. */
  example?: string
  spec: PropSpec
}

function fieldOf(spec: PropSpec): FormField {
  const control = controlFor(spec)
  return {
    name: spec.name,
    label: spec.label,
    control,
    inputType: inputTypeFor(spec),
    options: spec.values,
    defaultValue:
      typeof spec.default === "string" && spec.default.length
        ? spec.default
        : undefined,
    required: spec.required,
    description: spec.description,
    example: exampleFor(spec),
    spec,
  }
}

/** The editable fields for a kind: every OWNER-WRITABLE declared property, in
 * the declaration's (alphabetical) order. */
export function buildFormFields(kind: KindInfo): FormField[] {
  return propSpecsByName(kind).filter(ownerWritable).map(fieldOf)
}

/** The same fields with the ones a person MUST fill first: an editor asks for
 * required properties before optional ones. */
export function requiredFirst(fields: FormField[]): FormField[] {
  return [...fields].sort(
    (a, b) =>
      Number(b.required) - Number(a.required) || a.name.localeCompare(b.name)
  )
}

/** The form's value bag. Text-ish controls carry a string (a list joins on
 * newlines, a json control carries its JSON text), a bool carries a boolean, a
 * pointer carries its two halves and a list of pointers a list of them, a
 * keyed map its rows, and `null` marks a field the person EXPLICITLY cleared
 * (distinct from a merely-blank, untouched one). */
export type FormValue =
  | string
  | boolean
  | null
  | RefValue
  | FieldBag
  | RefValue[]
  | FieldBag[]
  | KeyedRow[]
export type FormValues = Record<string, FormValue>

/** A pointer as the FORM holds it: two halves, one value. */
export interface RefValue {
  kind: string
  id: string
}

/** Which shape a value is in is the CONTROL's business, never a guess at the
 * value: a pointer, an object row and a keyed row are all objects. */
export function asRef(value: FormValue): RefValue {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as RefValue)
    : { kind: "", id: "" }
}

export function asRefs(value: FormValue): RefValue[] {
  return Array.isArray(value) ? (value as RefValue[]) : []
}

export function asBag(value: FormValue): FieldBag {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as FieldBag)
    : {}
}

export function asRows(value: FormValue): FieldBag[] {
  return Array.isArray(value) ? (value as FieldBag[]) : []
}

export function asKeyedRows(value: FormValue): KeyedRow[] {
  return Array.isArray(value) ? (value as KeyedRow[]) : []
}

/** The fields of an object property, as form fields in their own right. */
export function objectFields(field: FormField): FormField[] {
  return (field.spec.fields ?? []).map(fieldOf)
}

/** One ITEM of a container, as a field in its own right: what one row of a
 * keyed map is edited as. It inherits the container's label, which a caller
 * rendering many rows replaces: a row's name is its key. */
export function elementField(field: FormField): FormField {
  return fieldOf(elementSpec(field.spec))
}

/** Seed one field from a stored value (absent on a create). A secret NEVER
 * seeds (write-only); a reference keeps its pinned kind; everything else
 * renders through the schema's own formatting. */
export function seedField(
  field: FormField,
  stored: unknown,
  creating: boolean
): FormValue {
  switch (field.control) {
    case "secret":
      return ""
    case "bool":
      if (stored === undefined || stored === null) {
        return creating && field.spec.default === true
      }
      return stored === true
    case "reference":
      return seedRef(field, stored)
    case "referenceList":
      return (Array.isArray(stored) ? stored : []).map((item) =>
        seedRef(field, item)
      )
    case "object":
      return seedBag(field, stored)
    case "objectList":
      return (Array.isArray(stored) ? stored : []).map((row) =>
        seedBag(field, row)
      )
    case "keyedMap": {
      if (!stored || typeof stored !== "object" || Array.isArray(stored)) {
        return [] as KeyedRow[]
      }
      const item = elementField(field)
      return Object.entries(stored as Record<string, unknown>).map(
        ([key, value]) => ({ key, value: seedField(item, value, false) })
      )
    }
    case "state":
      if (typeof stored === "string" && stored) return stored
      return creating
        ? (field.spec.initial ?? field.spec.states?.[0] ?? "")
        : ""
    default:
      if (stored === null || stored === undefined) {
        return creating && field.defaultValue ? field.defaultValue : ""
      }
      return formatValue(field.spec, stored)
  }
}

/** One object row's fields, seeded from the stored mapping. A field is a
 * property in its own right, so this recurses through nested objects and keyed
 * fields exactly as the top level does.
 *
 * ONLY the fields the row actually holds are seeded. Seeding every declared
 * field would make absent and empty the same thing, and a write rebuilt from
 * that seed would add keys nobody set: a `reads.kinds: []` the author never
 * wrote is not a harmless blank, it is a load error. */
function seedBag(field: FormField, stored: unknown): FieldBag {
  const row = (stored ?? {}) as Record<string, unknown>
  const bag: FieldBag = {}
  for (const sub of objectFields(field)) {
    if (!Object.prototype.hasOwnProperty.call(row, sub.name)) continue
    bag[sub.name] = seedField(sub, row[sub.name], false)
  }
  return bag
}

/** Seed the whole form from a record (absent seeds a create). */
export function initialValues(
  fields: FormField[],
  record?: SubstrateRecord
): FormValues {
  const values: FormValues = {}
  for (const field of fields) {
    values[field.name] = seedField(
      field,
      record?.properties?.[field.name],
      !record
    )
  }
  return values
}

/** Split a list textarea into a clean string array: one entry per line or
 * comma, trimmed, blanks dropped. */
export function parseList(raw: string): string[] {
  return raw
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

/** One field's value as the write carries it. `{}` means the field says
 * nothing (blank, or a secret left untouched), `{value: null}` is an explicit
 * clear, and `error` is the datatype complaint to show on the control. */
export interface FieldValue {
  value?: unknown
  error?: string
}

export function toFieldValue(field: FormField, value: FormValue): FieldValue {
  // A MANAGED property is the engine's to stamp, and a write that disagrees
  // with the stamp is refused: the form says nothing about it at all.
  if (field.spec.managed) return {}
  if (value === null) return { value: null }
  if (field.control === "bool") return { value: value === true }
  if (field.control === "secret") {
    const s = typeof value === "string" ? value : ""
    return s.length ? { value: s } : {}
  }
  if (field.control === "object") {
    const row = bagValue(field, asBag(value))
    if (row.error) return row
    return row.value && Object.keys(row.value).length
      ? { value: row.value }
      : {}
  }
  if (field.control === "objectList") {
    const out: Record<string, unknown>[] = []
    for (const bag of asRows(value)) {
      const row = bagValue(field, bag)
      if (row.error) return row
      if (row.value && Object.keys(row.value).length) out.push(row.value)
    }
    return out.length ? { value: out } : {}
  }
  if (field.control === "keyedMap") {
    const out: Record<string, unknown> = {}
    const item = elementField(field)
    for (const row of asKeyedRows(value)) {
      const submitted = toFieldValue(item, row.value)
      const says = submitted.value !== undefined && submitted.value !== null
      // A row with neither a key nor a value is the empty row an "Add" just
      // made: it is not yet a claim about the map, so it is not an error.
      if (row.key === "" && !says) continue
      // The key is checked EXACTLY as typed, never trimmed: the substrate
      // stores what it is given, and " helper " is not "helper".
      const badKey = checkKey(field.spec, row.key)
      if (badKey) return { error: badKey }
      if (Object.prototype.hasOwnProperty.call(out, row.key)) {
        return { error: `\`${row.key}\` is named twice` }
      }
      if (submitted.error) return { error: `${row.key}: ${submitted.error}` }
      if (!says) {
        // A key that names an empty container still names it.
        const empty = emptyContainer(item.spec)
        if (empty !== undefined) out[row.key] = empty
        continue
      }
      out[row.key] = submitted.value
    }
    return Object.keys(out).length ? { value: out } : {}
  }
  if (field.control === "reference") {
    return refValue(asRef(value))
  }
  if (field.control === "referenceList") {
    const out: string[] = []
    for (const ref of asRefs(value)) {
      const submitted = refValue(ref)
      if (submitted.error) return submitted
      if (submitted.value === undefined) continue
      out.push(submitted.value as string)
    }
    return out.length ? { value: out } : {}
  }
  if (field.control === "list") {
    const items = parseList(typeof value === "string" ? value : "")
    if (!items.length) return {}
    return parseValue(field.spec, items.join("\n"))
  }
  return parseValue(field.spec, typeof value === "string" ? value : "")
}

/** One pointer, coerced to the flat record PATH the substrate stores. A blank
 * says nothing; a whole path typed into the record box is ALREADY the value,
 * because joining the kind onto it would name a record nobody meant; and a
 * pointer at no declared kind has only the path to go on. */
function refValue(ref: RefValue): FieldValue {
  const id = ref.id.trim()
  if (!id) return {}
  // The write path's own decision, mirrored once: whether this is a path, a
  // short form the pin completes, or a value that reads two ways and is
  // refused naming both.
  return coerceReferencePath(ref.kind.trim(), id)
}

/** One pointer, seeded from its stored value. A served reference is the object
 * `{ref: "<kind>/<id>", …}`, so the path is read through `readReference` rather
 * than off the value: reading only the string arm seeded the form BLANK from
 * every real record and wiped the pointer on the next save. A value short of a
 * full path is the authored short form, which only the pin can complete. */
function seedRef(field: FormField, stored: unknown): RefValue {
  const pinned = field.spec.to && field.spec.to !== TO_ANY ? field.spec.to : ""
  const held = readReference(stored)
  if (!held) return { kind: pinned, id: "" }
  return splitRecordPath(held.path) ?? { kind: pinned, id: held.path }
}

/** One object row, coerced field by field. A field that fails its datatype
 * carries the field's name into the message: the row alone would not say
 * which control is wrong. */
function bagValue(
  field: FormField,
  bag: FieldBag
): { value?: Record<string, unknown>; error?: string } {
  const out: Record<string, unknown> = {}
  for (const sub of objectFields(field)) {
    // A field the row does not HOLD is not a field the row is silent about:
    // it is one nobody has written, and rebuilding must not invent it.
    if (!Object.prototype.hasOwnProperty.call(bag, sub.name)) continue
    const submitted = toFieldValue(sub, bag[sub.name])
    if (submitted.error) return { error: `${sub.name}: ${submitted.error}` }
    if (submitted.value === undefined || submitted.value === null) {
      // A CONTAINER the row holds is EMPTY on purpose. Dropping it here would
      // rewrite `{options: [], count: 1}` as `{count: 1}` the moment anything
      // else in the row moved, which is absent standing in for empty.
      const empty = emptyContainer(sub.spec)
      if (empty !== undefined) out[sub.name] = empty
      continue
    }
    out[sub.name] = submitted.value
  }
  return { value: out }
}

// ── the narrowest write ─────────────────────────────────────────────────────

/** Where a value sits BELOW its property: declared field names, keyed-map keys,
 * and list indices. It is a document path (`record-yaml.EditPath`), because
 * that is what it becomes. */
export type EditPath = DocumentPath

/** One narrow write: the path from the property down to the single value that
 * changed, and the declaration that value answers to. */
export interface FieldEdit {
  path: EditPath
  field: FormField
  value: FormValue
}

/** Structural equality of two form values. Both sides are built by this module
 * from the same shapes, so a JSON round trip settles it; two bags that differ
 * only in key ORDER read as different, which costs a narrow write and never
 * a wrong one. */
function sameValue(
  a: FormValue | undefined,
  b: FormValue | undefined
): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null)
}

function under(segment: string | number, edit: FieldEdit): FieldEdit {
  return { ...edit, path: [segment, ...edit.path] }
}

/** The NARROWEST write that turns one property value into another: the path to
 * the single leaf that moved, and what it moved to. `undefined` when more than
 * one thing moved, or when the move is structural (a row added or removed, a
 * key renamed), because those restructure the value and are written whole.
 *
 * This is what keeps the document-backed lens lossless. A write at the leaf
 * touches ONE key, so every neighbouring line (a sibling's comment, a nested
 * empty list, a hand-authored blank line) is left exactly as it was, where a
 * write of the whole property re-serializes the subtree and takes all of that
 * with it. Pure, so the rule is testable without a DOM. */
export function narrowEdit(
  field: FormField,
  before: FormValue,
  after: FormValue
): FieldEdit | undefined {
  if (sameValue(before, after)) return undefined

  if (field.control === "object") {
    const from = asBag(before)
    const to = asBag(after)
    let found: FieldEdit | undefined
    for (const sub of objectFields(field)) {
      if (sameValue(from[sub.name], to[sub.name])) continue
      if (found) return undefined
      const value = to[sub.name] ?? ""
      const inner = narrowEdit(sub, from[sub.name] ?? "", value)
      // A field that RESTRUCTURED is still narrower written at its own key than
      // the whole object is: every other field, and its comments, stay put.
      found = inner
        ? under(sub.name, inner)
        : { path: [sub.name], field: sub, value }
    }
    return found
  }

  if (field.control === "keyedMap") {
    const from = asKeyedRows(before)
    const to = asKeyedRows(after)
    // The KEY is the path, so a key that moved is not a value that moved: an
    // added, removed or renamed entry restructures the map.
    if (from.length !== to.length) return undefined
    const item = elementField(field)
    let found: FieldEdit | undefined
    for (let i = 0; i < to.length; i++) {
      if (from[i].key !== to[i].key) return undefined
      if (sameValue(from[i].value, to[i].value)) continue
      if (found) return undefined
      // A key the substrate would refuse names no path to write at.
      if (checkKey(field.spec, to[i].key)) return undefined
      const inner = narrowEdit(item, from[i].value, to[i].value)
      found = inner
        ? under(to[i].key, inner)
        : { path: [to[i].key], field: item, value: to[i].value }
    }
    return found
  }

  if (field.control === "objectList" || field.control === "referenceList") {
    const from = Array.isArray(before) ? (before as FormValue[]) : []
    const to = Array.isArray(after) ? (after as FormValue[]) : []
    // A length change renumbers everything after it: the list is written whole.
    if (from.length !== to.length) return undefined
    const item = elementField(field)
    let found: FieldEdit | undefined
    for (let i = 0; i < to.length; i++) {
      if (sameValue(from[i], to[i])) continue
      if (found) return undefined
      const inner = narrowEdit(item, from[i], to[i])
      found = inner ? under(i, inner) : { path: [i], field: item, value: to[i] }
    }
    return found
  }

  return { path: [], field, value: after }
}

/** One validation failure, keyed to the field that produced it. */
export interface FieldError {
  name: string
  message: string
}

/** Validate the form before a write: a secret is required on CREATE, a
 * `required` field must be non-empty in its submitted state (including when
 * the person tries to clear it), and every value must satisfy its datatype.
 * Returns every failure; an empty array means the form may submit. */
export function validate(
  fields: FormField[],
  values: FormValues,
  mode: FormMode
): FieldError[] {
  const errors: FieldError[] = []
  for (const field of fields) {
    const value = values[field.name]
    // The engine stamps a managed property, so the form neither sends it nor
    // has anything to be right or wrong about.
    if (field.spec.managed) continue
    if (field.control === "secret") {
      const filled = typeof value === "string" && value.length > 0
      if (mode === "create" && !filled) {
        errors.push({ name: field.name, message: `${field.name} is required.` })
      }
      continue
    }
    const submitted = toFieldValue(field, value)
    if (submitted.error) {
      errors.push({
        name: field.name,
        message: `${field.name}: ${submitted.error}.`,
      })
      continue
    }
    // The assembled value against its whole declaration, which is where a
    // REQUIRED field four levels down is finally asked for: coercion builds
    // what the controls hold, and only the declaration knows what is missing.
    const deep = checkValue(field.spec, submitted.value)
    if (deep) {
      errors.push({ name: field.name, message: `${field.name}: ${deep}.` })
      continue
    }
    if (!field.required || field.control === "bool") continue
    if (submitted.value === undefined || submitted.value === null) {
      errors.push({ name: field.name, message: `${field.name} is required.` })
    }
  }
  return errors
}

/** Coerce the form's values into a properties payload.
 *
 * - `secret`: sent ONLY when non-empty; a blank keeps the sealed value.
 * - `bool`: always sent (a toggle is an explicit choice).
 * - everything else: a value is sent in its DECLARED type (an int as a number,
 *   a json property as parsed JSON, a pointer as its `<kind>/<id>` record
 *   path); a field the
 *   person EXPLICITLY cleared (`null`) sends `null` on PATCH to delete the key
 *   (a create has nothing to clear); a merely-blank field is omitted so it is
 *   left as it stands.
 *
 * Patch merges key-wise, so an omitted field is untouched and a `null` deletes
 * it. Values that fail their datatype are dropped here; `validate` is what bars
 * the submit, and it runs first. */
export function toProperties(
  fields: FormField[],
  values: FormValues,
  mode: FormMode = "create"
): Record<string, unknown> {
  const props: Record<string, unknown> = {}
  for (const field of fields) {
    const submitted = toFieldValue(field, values[field.name])
    if (submitted.error || submitted.value === undefined) continue
    if (submitted.value === null) {
      if (mode === "patch") props[field.name] = null
      continue
    }
    props[field.name] = submitted.value
  }
  return props
}
