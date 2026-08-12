/** The generic config/account record form's pure core (GUIDE §8, the
 * integrations flow). It derives the editable field set from a trait-typed
 * config record's declared schema, seeds the form from an existing record,
 * validates the values, and coerces them back into a properties payload for
 * create/patch.
 *
 * Invariants that live here, not in the component:
 * - HOST-MANAGED properties are never offered for editing, and this is driven
 *   off the schema's `writer:` role — NOT a name blacklist. A property whose
 *   `writer` is a non-owner role (the OAuth facility's `oauth`, the connector
 *   runtime's `connector`) is excluded; an owner-writable property (an explicit
 *   `writer: owner`, or none at all — an unrestricted property is owner's to
 *   set) is offered. So the OAuth facility's tokenRef/tokenStatus/grantedScopes
 *   and the connector's sync state (syncToken/lastSyncedAt/syncStatus) drop out
 *   because the schema marks them, not because the console knows their names.
 * - SECRET fields are write-only — their stored value never seeds the form. A
 *   blank secret is OMITTED on patch (the sealed value stands) but REQUIRED on
 *   create (a create with a blank secret can never work — there is nothing
 *   sealed to fall back to, so it must be caught before it flips the
 *   integration to "configured").
 * - ENUM properties render as a select, so an unknown free-text value can never
 *   silently coerce to a default (e.g. a `syncFrequency` typo).
 * - Ordinary text/list fields can be EXPLICITLY CLEARED (value `null`), which on
 *   patch sends `null` to delete the key; a merely-blank field is left
 *   untouched. */

import type { SubstrateRecord, EnumValue, KindInfo } from "@/lib/api/types"
import { parseEnumValues } from "@/lib/api/types"
import { declaredProperties } from "@/lib/definition"

/** Whether a declared `writer:` role leaves the property owner-writable. An
 * unrestricted property (no writer) is the owner's to set, and an explicit
 * `writer: owner` is too; any other role (`oauth`, `connector`) is host- or
 * connector-managed and never offered on the form. This is the boundary the
 * old name blacklist stood in for — now read straight off the schema. */
function ownerWritable(writer: unknown): boolean {
  return typeof writer !== "string" || writer === "" || writer === "owner"
}

/** Humanize a camelCase property id for a label when the schema declares no
 * `displayName`: split on case boundaries and sentence-case the result, so
 * `backfillDepth` → "Backfill depth" and `enabledCalendar` → "Enabled
 * calendar" — never the raw id. */
export function humanizeName(name: string): string {
  const spaced = name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
  if (!spaced) return name
  const lower = spaced.toLowerCase()
  return lower.charAt(0).toUpperCase() + lower.slice(1)
}

/** The field's human label: the schema's `displayName` when it declares one,
 * else the humanized property id. Never the raw camelCase id. */
function labelFor(name: string, def?: Record<string, unknown>): string {
  const declared = def?.displayName
  if (typeof declared === "string" && declared.trim()) return declared.trim()
  return humanizeName(name)
}

/** A declared string `default:` — the value a create seeds the field with (an
 * enum's default option, mostly). Absent or non-string means no seed. */
function defaultOf(def?: Record<string, unknown>): string | undefined {
  const d = def?.default
  return typeof d === "string" && d.length ? d : undefined
}

/** Whether the form is minting a new record or patching an existing one — the
 * two modes differ on secret handling and on what "blank" means. */
export type FormMode = "create" | "patch"

/** One editable field, projected from a declared property. */
export interface FormField {
  name: string
  /** The human label the field renders: the schema's `displayName`, or the
   * humanized property id — never the raw camelCase `name`. */
  label: string
  /** The control the field renders: a text input, a write-only secret input, a
   * boolean toggle, a single-choice select (an enum), or a repeated-string list
   * (one per line). */
  control: "text" | "secret" | "bool" | "list" | "select"
  /** The HTML input type for `text` controls (email/url get the right keyboard
   * and validation affordance). */
  inputType: "text" | "email" | "url"
  /** `select`-only: the admitted enum options, declaration order. Each carries
   * the raw `value` a write submits and its authored `label` — an option shows
   * its `label` when non-empty ("Last 30 days"), else a humanized value, while
   * still submitting the raw `value` ("last30d"). */
  options?: EnumValue[]
  /** A declared `default:` value — seeds the field on CREATE (an enum's default
   * option, chiefly); a patch seeds from the record, not this. */
  defaultValue?: string
  /** The schema declared this property `required: true` — the form refuses to
   * submit it empty. (A secret is additionally required on create regardless.) */
  required: boolean
  description?: string
}

/** The raw declared property block (`definition.properties.<name>`), which the
 * substrate serves verbatim from the manifest — so `values` (enum) and
 * `required` ride along even where the compiled schema does not model them. */
function rawProps(kind: KindInfo): Record<string, Record<string, unknown>> {
  const def = (kind.definition ?? {}) as Record<string, unknown>
  return (def.properties ?? {}) as Record<string, Record<string, unknown>>
}

/** The admitted enum options for a property, projected from its raw `values`
 * (the `[{value, label}]` wire shape). Absent or empty means it is not an enum
 * — no select. */
function optionsOf(raw: Record<string, unknown> | undefined): EnumValue[] | undefined {
  return parseEnumValues(raw?.values)
}

function controlFor(
  kind: string,
  repeated: boolean,
  options?: EnumValue[]
): FormField["control"] {
  if (kind === "secret") return "secret"
  if (kind === "bool") return "bool"
  if (options) return "select"
  if (repeated) return "list"
  return "text"
}

function inputTypeFor(kind: string): FormField["inputType"] {
  if (kind === "email") return "email"
  if (kind === "url") return "url"
  return "text"
}

/** The editable fields for a config/account type: every OWNER-WRITABLE declared
 * property (see `ownerWritable` — driven off the schema's `writer:` role, not a
 * name list), in the schema's own (alphabetical) order. Each field's label is
 * the schema's `displayName` or the humanized id; enum properties (a declared
 * `values` list) become selects; a declared `required: true` marks the field
 * required; a declared `default` seeds a create. */
export function buildFormFields(kind: KindInfo): FormField[] {
  const raw = rawProps(kind)
  return declaredProperties(kind)
    .filter((p) => ownerWritable(raw[p.name]?.writer))
    .map((p) => {
      const def = raw[p.name]
      const options = optionsOf(def)
      return {
        name: p.name,
        label: labelFor(p.name, def),
        control: controlFor(p.kind, p.repeated, options),
        inputType: inputTypeFor(p.kind),
        options,
        defaultValue: defaultOf(def),
        required: def?.required === true,
        description: p.description,
      }
    })
}

/** The form's value bag: text/secret/list/select carry strings (a list is
 * newline joined), bool carries a boolean, and `null` marks a field the person
 * EXPLICITLY cleared (distinct from a merely-blank, untouched field). */
export type FormValues = Record<string, string | boolean | null>

/** Seed the form. A secret NEVER seeds (write-only, GUIDE rule); a list joins
 * on newlines; a select and everything else stringifies its stored value.
 * Absent a record (CREATE), a field seeds from its declared `default` when it
 * has one (an enum's default option), else empty/false. */
export function initialValues(fields: FormField[], record?: SubstrateRecord): FormValues {
  const values: FormValues = {}
  for (const field of fields) {
    const stored = record?.properties?.[field.name]
    switch (field.control) {
      case "secret":
        values[field.name] = ""
        break
      case "bool":
        values[field.name] = stored === true
        break
      case "list":
        values[field.name] = Array.isArray(stored)
          ? stored.map((v) => String(v)).join("\n")
          : ""
        break
      default:
        if (stored === null || stored === undefined) {
          // On create (no record), a declared default seeds the field; a patch
          // seeds from the stored value only (an absent value stays empty).
          values[field.name] = !record && field.defaultValue ? field.defaultValue : ""
        } else {
          values[field.name] = String(stored)
        }
    }
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

/** One validation failure, keyed to the field that produced it. */
export interface FieldError {
  name: string
  message: string
}

/** Validate the form before a write.
 *
 * - A secret is required on CREATE (a blank secret can never seed a working
 *   integration); on patch a blank secret is fine (it preserves the sealed
 *   value).
 * - A `required` field must be non-empty in its submitted state — including
 *   when the person tries to clear it (value `null`) — on both create and
 *   patch. Bools are always a definite value, so they never fail required.
 *
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
    if (!field.required || field.control === "bool") continue
    const empty =
      value === null ||
      (field.control === "list"
        ? parseList(typeof value === "string" ? value : "").length === 0
        : typeof value !== "string" || value.trim().length === 0)
    if (empty) {
      errors.push({ name: field.name, message: `${field.name} is required.` })
    }
  }
  return errors
}

/** Coerce the form's values into a properties payload.
 *
 * - `secret`: sent ONLY when non-empty — a blank keeps the sealed value.
 * - `bool`: always sent (a toggle is an explicit choice).
 * - `list` / text / `select`: a non-empty value is sent; a value the person
 *   EXPLICITLY cleared (`null`) sends `null` on PATCH to delete the key (create
 *   has nothing to clear, so it is omitted); a merely-blank value is omitted so
 *   an untouched field is left as it stands.
 *
 * Patch merges key-wise, so an omitted field is left untouched and a `null`
 * deletes it. */
export function toProperties(
  fields: FormField[],
  values: FormValues,
  mode: FormMode = "create"
): Record<string, unknown> {
  const props: Record<string, unknown> = {}
  for (const field of fields) {
    const value = values[field.name]
    switch (field.control) {
      case "bool":
        props[field.name] = value === true
        break
      case "secret": {
        const s = typeof value === "string" ? value : ""
        if (s.length) props[field.name] = s
        break
      }
      case "list": {
        if (value === null) {
          if (mode === "patch") props[field.name] = null
          break
        }
        const list = parseList(typeof value === "string" ? value : "")
        if (list.length) props[field.name] = list
        break
      }
      default: {
        if (value === null) {
          if (mode === "patch") props[field.name] = null
          break
        }
        const s = typeof value === "string" ? value.trim() : ""
        if (s.length) props[field.name] = s
      }
    }
  }
  return props
}
