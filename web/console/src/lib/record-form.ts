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
import { recordPath, splitRecordPath } from "@/lib/record-path"
import {
  TO_ANY,
  controlFor,
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

/** A reference as the FORM holds it: the referent's kind beside its id,
 * because the control is a kind picker beside a record picker. The WRITE
 * carries neither half on its own — the two join into the record path
 * `<kind>/<id>`, which is the whole stored value (`toFieldValue`). */
export interface RefValue {
  kind: string
  id: string
}

/** One object's fields as the form holds them: the same text-per-control bag
 * every other value uses, keyed by field name. A repeated object holds a list
 * of these, one per row. */
export type FieldBag = Record<string, string | boolean | null>

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
 * reference carries its two halves (`RefValue`), and `null` marks a field the
 * person EXPLICITLY cleared (distinct from a merely-blank, untouched one). */
export type FormValue =
  string | boolean | null | RefValue | FieldBag | FieldBag[]
export type FormValues = Record<string, FormValue>

/** Which shape a value is in is the CONTROL's business, never a guess at the
 * value: a reference and an object row are both bags of strings. */
export function asRef(value: FormValue): RefValue {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as RefValue)
    : { kind: "", id: "" }
}

export function asBag(value: FormValue): FieldBag {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as FieldBag)
    : {}
}

export function asRows(value: FormValue): FieldBag[] {
  return Array.isArray(value) ? value : []
}

/** The fields of an object property, as form fields in their own right. */
export function objectFields(field: FormField): FormField[] {
  return (field.spec.fields ?? []).map(fieldOf)
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
    case "reference": {
      const pinned =
        field.spec.to && field.spec.to !== TO_ANY ? field.spec.to : ""
      if (typeof stored !== "string" || !stored) return { kind: pinned, id: "" }
      // A stored value is a full path; anything short of one is the authored
      // short form, which only the pin can complete.
      return splitRecordPath(stored) ?? { kind: pinned, id: stored }
    }
    case "object":
      return seedBag(field, stored)
    case "objectList":
      return (Array.isArray(stored) ? stored : []).map((row) =>
        seedBag(field, row)
      )
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

/** One object row's fields, seeded from the stored mapping. */
function seedBag(field: FormField, stored: unknown): FieldBag {
  const row = (stored ?? {}) as Record<string, unknown>
  const bag: FieldBag = {}
  for (const sub of objectFields(field)) {
    bag[sub.name] = seedField(sub, row[sub.name], false) as
      string | boolean | null
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
  if (field.control === "reference") {
    const ref = asRef(value)
    const id = ref.id.trim()
    if (!id) return {}
    // A whole path pasted into the record box is already the value: joining the
    // kind onto it would name a record nobody meant. The substrate reads the
    // two spellings the same way, and by the same grammar.
    if (splitRecordPath(id)) return { value: id }
    const kind = ref.kind.trim()
    if (!kind) {
      return {
        error: `a reference to any kind needs a full "<kind>/<id>" path, not the bare id ${JSON.stringify(id)}`,
      }
    }
    return { value: recordPath(kind, id) }
  }
  if (field.control === "list") {
    const items = parseList(typeof value === "string" ? value : "")
    if (!items.length) return {}
    return parseValue(field.spec, items.join("\n"))
  }
  return parseValue(field.spec, typeof value === "string" ? value : "")
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
    const submitted = toFieldValue(sub, bag[sub.name] ?? "")
    if (submitted.error) return { error: `${sub.name}: ${submitted.error}` }
    if (submitted.value === undefined || submitted.value === null) continue
    out[sub.name] = submitted.value
  }
  return { value: out }
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
 *   a json property as parsed JSON, a reference as its `<kind>/<id>` record
 *   path); a field the person EXPLICITLY cleared (`null`) sends `null` on
 *   PATCH to delete the key (a create has nothing to clear); a merely-blank
 *   field is omitted so it is left as it stands.
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
