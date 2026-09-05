/** What a kind's declaration says about one property, and what that means for
 * a value. This is the ONE projection every editing surface reads: the YAML
 * lens (`record-yaml.ts`), the form lens (`record-form.ts`, and through it the
 * integrations dialog), and the controls both of them render.
 *
 * Two things live here and nowhere else:
 *
 * - `propSpecs(kind)` — the declared properties with everything an editor
 *   needs: datatype, the container (`repeated` or `keyed`, with the keys'
 *   `keyPattern`), an enum's admitted values, a reference's `to:` target, what
 *   whether the engine `managed:` it, a state machine's
 *   states, `min`/`max`/`pattern`, a declared `default`, the `writer:` role,
 *   an object's declared `fields` (which nest), and the one-liner.
 * - `checkValue(spec, value)` — the client's copy of the substrate's own
 *   coercion rules (`internal/engine/validate.go`), so a bad datetime, a
 *   string where an int is declared or an unadmitted enum value is named ON
 *   THE LINE instead of arriving as a 422 after the round trip. It is
 *   deliberately the SAME shape of check, never a stricter one: the server is
 *   still the authority, and a check the server would pass must pass here. */

import {
  parseEnumValues,
  readReference,
  REFERENCE_KEY,
  type EnumValue,
  type KindInfo,
} from "@/lib/api/types"
import { temporalProperties } from "@/lib/definition"
import { coerceReferencePath, recordPath } from "@/lib/record-path"

/** The `kind:` pin a reference wears when it is pinned to no kind at all. */
export const TO_ANY = "any"

/** The contracts a keyed map's KEYS hold to, as the substrate spells them
 * (`internal/vocabulary/types.go`, KeyPatternRegexp). A pattern this map does
 * not know leaves the key unchecked here; the server is still the authority.
 *
 * These literals are the ONLY copy of a Go grammar in the console that no
 * golden file covers, so a Go test reads them back out of this file and holds
 * them to `KeyPatternRegexp`: `TestConsoleKeyPatternsMatchTheLoader` in
 * `internal/vocabulary`. Edit a pattern here and that test fails; the block's
 * shape is part of what it parses, so keep the `name: /regex/,` entries. */
const KEY_PATTERNS: Record<string, RegExp> = {
  camel: /^[a-z][a-zA-Z0-9]*$/,
  kindRef:
    /^([a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+\/[a-z][a-z0-9]*\/)?[a-z][a-z0-9]*$/,
}

/** What each contract says when it refuses, in the author's terms rather than
 * the regexp's (`vocabulary.keyPatternRule`). */
const KEY_RULES: Record<string, string> = {
  camel: "camelCase ([a-z][a-zA-Z0-9]*)",
  kindRef: "a kind reference (`task` or `tasks.example.com/task`)",
}

/** What a secret-typed property reads back as, everywhere (engine.Redacted).
 * Writing it back is a round trip, not an assignment, so an edit that leaves
 * the sentinel alone leaves the sealed value alone. */
export const REDACTED = "<redacted>"

/** One declared property, projected with everything the editors need, straight
 * off the verbatim declaration block the substrate serves
 * (`definition.properties.<name>`). */
export interface PropSpec {
  name: string
  /** The human label: the declaration's `displayName`, else the humanized id. */
  label: string
  /** The declared datatype (`string`, `datetime`, `state`, ...). An
   * authority-local datatype passes through and is treated as a string. */
  kind: string
  required: boolean
  repeated: boolean
  /** `keyed: true`: the value is a MAP from author-chosen keys to this
   * property's datatype. Keyed and repeated are the two containers, and a
   * declaration is one or the other, never both. */
  keyed: boolean
  /** The contract a KEYED map's keys hold to (`camel`, `kindRef`). */
  keyPattern?: string
  /** The ENGINE stamps this property, and refuses a write that disagrees with
   * what it stamped, so no editing surface offers an input for it. */
  managed: boolean
  /** Enum (and any property that narrows a string): the admitted values. */
  values?: EnumValue[]
  /** A declared `default:` — what a create seeds the property with. */
  default?: unknown
  /** `state`: the machine's states and the state a record is born into. */
  states?: string[]
  initial?: string
  /** `reference`: the kind this pointer is pinned to, or `any`. */
  to?: string
  /** `reference`: the LINK DATA the declaration hangs off the pointer, each a
   * property in its own right. EVERY reference stores and serves the object
   * `{ref, <prop>: <val>}`; these say which keys may sit beside `ref`, and an
   * empty list means none may. */
  linkFields?: PropSpec[]
  /** `object`: the declared fields, each a property in its own right (a field
   * may narrow, range or enumerate exactly as a property does). Fields NEST:
   * a field may itself be an object with fields, and may be repeated or keyed,
   * to the dialect's four levels, a kind's own property being level one. */
  fields?: PropSpec[]
  min?: number
  max?: number
  pattern?: string
  /** The declared `writer:` role. Anything but the owner is host-managed and
   * never offered for editing. */
  writer?: string
  description?: string
}

/** The control a property earns. `select` covers an enum and any property that
 * narrows its values; `state` is a select the server will only accept on a
 * create; `json` covers `json` and an object nobody declared fields for;
 * `prose` is a textarea. `reference`/`referenceList` are the POINTER controls:
 * a dropdown over the records the declaration pins to. `keyedMap` is the
 * add-remove list of key/value rows a `keyed:` map earns, whatever its element
 * datatype. */
export type Control =
  | "text"
  | "prose"
  | "secret"
  | "bool"
  | "select"
  | "state"
  | "number"
  | "datetime"
  | "json"
  | "object"
  | "objectList"
  | "list"
  | "reference"
  | "referenceList"
  | "keyedMap"

// `decimal` is NOT here: its value is a string of exact digits, and a number
// control would coerce it through the float64 rounding the datatype refuses.
const NUMERIC = new Set([
  "int",
  "integer",
  "int32",
  "int64",
  "uint",
  "number",
  "float",
  "float64",
  "double",
])
const BOOLEAN = new Set(["bool", "boolean"])
const OBJECT = new Set(["json", "object", "map"])
const PROSE = new Set(["text", "markdown"])

export function isNumericKind(kind: string): boolean {
  return NUMERIC.has(kind)
}

export function isBooleanKind(kind: string): boolean {
  return BOOLEAN.has(kind)
}

export function isObjectKind(kind: string): boolean {
  return OBJECT.has(kind)
}

/** Whether a declared `writer:` role leaves the property the owner's to set.
 * An unrestricted property is the owner's, an explicit `writer: owner` is too;
 * any other role (`oauth`, `connector`) is host- or connector-managed and no
 * editing surface offers it. */
export function ownerWritable(spec: PropSpec): boolean {
  return !spec.writer || spec.writer === "owner"
}

/** Humanize a camelCase property id for a label when the declaration carries no
 * `displayName`: `backfillDepth` becomes "Backfill depth". An ALL-CAPS run is
 * an acronym the author wrote, and keeps its case — `baseURL` is "Base URL",
 * never "Base url". */
export function humanizeName(name: string): string {
  const spaced = name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
  if (!spaced) return name
  const words = spaced
    .split(/\s+/)
    .map((word) => (/^[A-Z0-9]{2,}$/.test(word) ? word : word.toLowerCase()))
  const first = words[0]
  words[0] = first.charAt(0).toUpperCase() + first.slice(1)
  return words.join(" ")
}

function stringList(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined
  const out = v.filter((x): x is string => typeof x === "string")
  return out.length ? out : undefined
}

function numberOr(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) ? v : undefined
}

/** An object's declared fields, in declaration-name order. A field is written
 * either as a block (`{type: string, description: …}`) or as the bare datatype
 * (`value: string`), which the loader accepts and so does this. */
function fieldSpecs(v: unknown): PropSpec[] | undefined {
  if (!v || typeof v !== "object" || Array.isArray(v)) return undefined
  const raw = v as Record<string, unknown>
  const out = Object.keys(raw)
    .sort((a, b) => a.localeCompare(b))
    .map((name) =>
      specOf(
        name,
        typeof raw[name] === "string"
          ? { type: raw[name] }
          : ((raw[name] as Record<string, unknown>) ?? {})
      )
    )
  return out.length ? out : undefined
}

function specOf(name: string, def: Record<string, unknown>): PropSpec {
  const displayName =
    typeof def.displayName === "string" && def.displayName.trim()
      ? def.displayName.trim()
      : undefined
  return {
    name,
    label: displayName ?? humanizeName(name),
    kind: typeof def.type === "string" ? def.type : "string",
    required: def.required === true,
    repeated: def.repeated === true,
    keyed: def.keyed === true,
    keyPattern: typeof def.keyPattern === "string" ? def.keyPattern : undefined,
    managed: def.managed === true,
    values: parseEnumValues(def.values),
    default: def.default,
    states: stringList(def.states),
    initial: typeof def.initial === "string" ? def.initial : undefined,
    // THE PIN. A reference property names the kind its value points at under
    // `kind:`, the one spelling the loader accepts.
    to: typeof def.kind === "string" ? def.kind : undefined,
    linkFields:
      def.type === "reference" ? fieldSpecs(def.properties) : undefined,
    fields: fieldSpecs(def.fields),
    min: numberOr(def.min),
    max: numberOr(def.max),
    pattern: typeof def.pattern === "string" ? def.pattern : undefined,
    writer: typeof def.writer === "string" ? def.writer : undefined,
    description:
      typeof def.description === "string" ? def.description : undefined,
  }
}

function rawProps(kind: KindInfo): Record<string, Record<string, unknown>> {
  const def = (kind.definition ?? {}) as Record<string, unknown>
  return (def.properties ?? {}) as Record<string, Record<string, unknown>>
}

/** The declared properties, required first then optional, alphabetical within
 * each band (key order is lost to jsonb, so alphabetical is the honest order,
 * and required-first is what an editor should ask for first). */
export function propSpecs(kind: KindInfo): PropSpec[] {
  return Object.entries(rawProps(kind))
    .map(([name, def]) => specOf(name, def ?? {}))
    .sort(
      (a, b) =>
        Number(b.required) - Number(a.required) || a.name.localeCompare(b.name)
    )
}

/** The properties every kind carries whether or not it declares them: the hot
 * columns the write path splits out before validation (`engine/write.go`).
 * `title` is always a legal string; `at`/`endsAt`/`dueAt` are legal only where
 * a temporal trait binds them. `body` is NOT here: since #68 a kind carries a
 * body only when it declares one, so body comes from the declared properties,
 * and offering it everywhere would suggest a write the substrate now refuses.
 * An editor must not call these undeclared, because the substrate accepts them.
 */
export function systemSpecs(kind: KindInfo): PropSpec[] {
  const specs: PropSpec[] = [
    specOf("title", {
      type: "string",
      description: "the record's display title",
    }),
  ]
  for (const name of temporalProperties(kind)) {
    specs.push(
      specOf(name, {
        type: "datetime",
        description: "a temporal trait binds this hot column",
      })
    )
  }
  return specs
}

/** The declared properties in name order, as the read surfaces list them. */
export function propSpecsByName(kind: KindInfo): PropSpec[] {
  return Object.entries(rawProps(kind))
    .map(([name, def]) => specOf(name, def ?? {}))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** One ITEM of a container, as a declaration in its own right: the same spec
 * with its container marker dropped, so a row of a keyed map and an element of
 * a repeated property check and render exactly like the scalar they are. */
export function elementSpec(spec: PropSpec): PropSpec {
  return { ...spec, repeated: false, keyed: false }
}

/** What a CONTAINER holds when it holds nothing: `{}` for a keyed map or a
 * declared object, `[]` for a repeated property. Not a container: undefined.
 *
 * An empty container is a claim and an absent one is not, so a surface that
 * rebuilds a value has to be able to write "empty" rather than dropping the
 * key. `json` is deliberately absent: its emptiness is whatever its text
 * says, and nobody but the author owns that shape. */
export function emptyContainer(spec: PropSpec): unknown | undefined {
  if (spec.keyed) return {}
  if (spec.repeated) return []
  if (spec.kind === "object" && spec.fields?.length) return {}
  return undefined
}

export function controlFor(spec: PropSpec): Control {
  if (spec.kind === "secret") return "secret"
  if (spec.kind === "state") return "state"
  // A CONTAINER is decided before its element datatype: a repeated reference
  // is a list of pickers, not one picker, and the write carries an array.
  if (spec.keyed) return "keyedMap"
  // A POINTER is picked, never remembered. A repeated one is a LIST of them,
  // which is what the write carries and what the single control never was.
  if (spec.kind === "reference") {
    return spec.repeated ? "referenceList" : "reference"
  }
  // A DECLARED object is a set of fields and is edited as one; `json` is the
  // shape nobody owns, and stays a text box.
  if (spec.kind === "object" && spec.fields?.length) {
    return spec.repeated ? "objectList" : "object"
  }
  if (isObjectKind(spec.kind)) return "json"
  if (spec.repeated) return "list"
  if (spec.values?.length) return "select"
  if (isBooleanKind(spec.kind)) return "bool"
  if (isNumericKind(spec.kind)) return "number"
  if (spec.kind === "datetime" || spec.kind === "date") return "datetime"
  if (PROSE.has(spec.kind)) return "prose"
  return "text"
}

/** The HTML input type a `text` control wears, so email and url get the right
 * keyboard and the browser's own affordance. */
export function inputTypeFor(spec: PropSpec): "text" | "email" | "url" {
  if (spec.kind === "email") return "email"
  if (spec.kind === "url") return "url"
  return "text"
}

/** How a property's datatype reads wherever the schema is shown: the declared
 * spelling, `[]`-suffixed when repeated, a pointer naming what it points at. */
export function typeLabel(spec: PropSpec): string {
  const base = spec.to ? `${spec.kind} → ${spec.to}` : spec.kind
  if (spec.keyed) return `{string: ${base}}`
  return spec.repeated ? `${base}[]` : base
}

// ── examples ────────────────────────────────────────────────────────────────

/** A WORKED value for the datatype: what a filled-in property looks like, so a
 * blank create is not a blank page. Shown as a placeholder in the form lens and
 * as an `e.g.` in the template's trailing comment. Derived from the
 * declaration alone, never from a kind's name. */
export function exampleFor(spec: PropSpec): string | undefined {
  if (spec.keyed) return "{}"
  if (spec.values?.length) return spec.values[0].value
  if (spec.states?.length) return spec.initial ?? spec.states[0]
  switch (spec.kind) {
    case "datetime":
      return "2026-01-31T09:00:00Z"
    case "date":
      return "2026-01-31"
    case "duration":
      return "PT47M12S"
    case "decimal":
      return "19.99"
    case "email":
      return "someone@example.com"
    case "url":
      return "https://example.com"
    case "phone":
      return "+441234567890"
    case "timezone":
      return "Europe/London"
    case "recurrence":
      return "RRULE:FREQ=WEEKLY;BYDAY=MO"
    case "digest":
      return "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    case "blobref":
      return "blob-sha256-e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    case "secret":
      return "the value to seal"
    case "reference":
      return spec.to && spec.to !== TO_ANY
        ? recordPath(spec.to, "some-id")
        : "<kind>/<id>"
    case "json":
    case "object":
    case "map":
      return "{}"
    case "int":
    case "float":
    case "number":
      return "0"
    case "bool":
      return "false"
    default:
      return undefined
  }
}

/** The seed a create template gives a property: its declared `default` when it
 * has one, else a typed zero. Repeated properties seed to an empty list
 * whatever their element datatype. */
export function seedValue(spec: PropSpec): unknown {
  if (spec.default !== undefined && spec.default !== null) return spec.default
  if (spec.keyed) return {}
  if (spec.repeated) return []
  if (spec.kind === "state") return spec.initial ?? spec.states?.[0] ?? ""
  if (isBooleanKind(spec.kind)) return false
  if (isNumericKind(spec.kind)) return 0
  // A decimal's zero is spelled, not numbered: the value is a string of digits.
  if (spec.kind === "decimal") return "0"
  if (isObjectKind(spec.kind)) return {}
  return ""
}

// ── the value rules (the substrate's own, client-side) ───────────────────────

/** RFC 3339, and the two shorter forms the substrate's `parseTime` accepts. */
const DATETIME =
  /^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$/
const CIVIL_DATE = /^\d{4}-\d{2}-\d{2}$/
/** ISO 8601's duration, THE one grammar, minus years and months (neither has
 * a fixed length): `PT47M12S`, `P2DT3H`, `P1W`. Go's own syntax (`47m12s`) is
 * refused; the server stores a canonical ISO decomposition. */
const ISO_DURATION =
  /^-?P(?=.)(\d+W)?(\d+D)?(T(?=.)(\d+(\.\d+)?H)?(\d+(\.\d+)?M)?(\d+(\.\d+)?S)?)?$/
/** An exact decimal: an optional sign, digits, an optional fraction. A string,
 * never a JSON number, because a number rides float64 and may already be
 * rounded. */
const DECIMAL = /^[+-]?\d+(\.\d+)?$/
const E164 = /^\+[1-9]\d{1,14}$/
const SHA256 = /^[0-9a-f]{64}$/
const BLOB_REF = /^blob-sha256-[0-9a-f]{64}$/

function admitted(spec: PropSpec): string {
  return (spec.values ?? []).map((v) => v.value).join(", ")
}

/** Whether a value says nothing: absent, blank or an empty container. `false`
 * and `0` are values somebody meant, so they are not blank. */
function isBlank(value: unknown): boolean {
  if (value === undefined || value === null) return true
  if (typeof value === "string") return value.trim() === ""
  if (Array.isArray(value)) return value.length === 0
  return false
}

/** Prefix a nested problem with the field it came from, so a failure four
 * levels down reads as one dotted trail (`reads.budgets.calls is required`)
 * rather than one name per level. A message that does not open with a path
 * is a plain reason, and gets the field named in front of it.
 *
 * Exported because the PROPERTY is the outermost level of the same trail: a
 * caller naming the property joins it the same way, or the path would break
 * at the top. */
export function underField(name: string, problem: string): string {
  const pathed = /^`([^`]+)`(.*)$/.exec(problem)
  return pathed
    ? `\`${name}.${pathed[1]}\`${pathed[2]}`
    : `\`${name}\`: ${problem}`
}

function validTimezone(name: string): boolean {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: name })
    return true
  } catch {
    return false
  }
}

/** Check ONE value against the declaration's element rules, ignoring whether
 * the property is repeated: a list's item and a scalar property answer to the
 * same datatype. */
export function checkItem(spec: PropSpec, value: unknown): string | undefined {
  if (isObjectKind(spec.kind)) {
    if (spec.kind === "json") {
      return value === undefined ? "expected a value" : undefined
    }
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return "expected an object"
    }
    if (spec.fields) {
      const held = value as Record<string, unknown>
      for (const [name, field] of Object.entries(held)) {
        const declared = spec.fields.find((f) => f.name === name)
        if (!declared) return `\`${name}\` is not a declared field`
        const problem = checkValue(declared, field)
        if (problem) return underField(name, problem)
      }
      // A field the declaration marks REQUIRED has to be there. Checking only
      // the fields a value happens to carry lets `{}` through an object whose
      // every field is required, which the loader then refuses.
      for (const declared of spec.fields) {
        if (!declared.required) continue
        if (isBlank(held[declared.name]))
          return `\`${declared.name}\` is required`
      }
    }
    return undefined
  }

  // The substrate's own refusals, through the one mirror of its coercion
  // (`record-path.coerceReferencePath`): a reference is the referent's record
  // PATH, a bare id is the authored short form only where the declaration pins
  // the kind that completes it, and a value that reads two ways is refused
  // naming both rather than resolved by precedence.
  if (spec.kind === "reference") {
    const pin = spec.to && spec.to !== TO_ANY ? spec.to : ""
    if (typeof value === "string") {
      return coerceReferencePath(pin, value.trim()).error
    }
    // A reference is STORED and SERVED as the object: the pointer under the one
    // reserved key, any declared link properties beside it. The bare string
    // above is write-time shorthand the server normalizes, which is why that
    // arm runs first: an authored document is checked here before it is sent.
    const held = readReference(value)
    if (!held) {
      return `a reference is a {${REFERENCE_KEY}: "<kind>/<id>"} object, or the path string alone`
    }
    const bad = coerceReferencePath(pin, held.path.trim()).error
    if (bad) return bad
    for (const [name, held_] of Object.entries(held.properties)) {
      const declared = spec.linkFields?.find((f) => f.name === name)
      if (!declared) return `\`${name}\` is not a declared link property`
      const problem = checkValue(declared, held_)
      if (problem) return underField(name, problem)
    }
    for (const declared of spec.linkFields ?? []) {
      if (!declared.required) continue
      if (isBlank(held.properties[declared.name])) {
        return `\`${declared.name}\` is required`
      }
    }
    return undefined
  }

  if (isBooleanKind(spec.kind)) {
    return typeof value === "boolean" ? undefined : "expected a boolean"
  }

  if (isNumericKind(spec.kind)) {
    if (typeof value !== "number" || !Number.isFinite(value)) {
      return "expected a number"
    }
    if (
      (spec.kind === "int" ||
        spec.kind === "integer" ||
        spec.kind === "int32" ||
        spec.kind === "int64" ||
        spec.kind === "uint") &&
      !Number.isInteger(value)
    ) {
      return "expected an integer"
    }
    if (spec.min !== undefined && value < spec.min)
      return `must be >= ${spec.min}`
    if (spec.max !== undefined && value > spec.max)
      return `must be <= ${spec.max}`
    return undefined
  }

  if (spec.kind === "state") {
    if (typeof value !== "string") return "expected a state name"
    if (spec.states?.length && !spec.states.includes(value)) {
      return `expected one of ${spec.states.join(", ")}`
    }
    return undefined
  }

  if (typeof value !== "string") return "expected a string"
  const s = value

  // A secret is opaque, and the redaction sentinel a read hands back is a
  // no-op the substrate itself recognises: any string is admissible.
  if (spec.kind === "secret") return undefined

  switch (spec.kind) {
    case "datetime":
      if (!DATETIME.test(s) || Number.isNaN(Date.parse(s))) {
        return "expected a timestamp (2026-01-31T09:00:00Z)"
      }
      break
    case "date":
      if (!CIVIL_DATE.test(s) || Number.isNaN(Date.parse(s))) {
        return "expected a civil date (2026-01-31)"
      }
      break
    case "duration":
      if (!ISO_DURATION.test(s)) {
        return "expected an ISO 8601 duration like PT47M12S"
      }
      break
    case "decimal": {
      if (!DECIMAL.test(s)) {
        return 'expected a decimal ("19.99"): an optional sign, digits, an optional fraction'
      }
      // The bound check may round through a float; the value itself never
      // does, and the server holds the exact line.
      const n = Number(s)
      if (spec.min !== undefined && n < spec.min)
        return `must be >= ${spec.min}`
      if (spec.max !== undefined && n > spec.max)
        return `must be <= ${spec.max}`
      break
    }
    case "email":
      if (!/^[^@\s]+@[^@\s]+$/.test(s.replace(/^.*<|>$/g, ""))) {
        return "expected a mailbox (someone@example.com)"
      }
      break
    case "url":
      if (!/^[a-z][a-z0-9+.-]*:/i.test(s)) return "expected an absolute URL"
      break
    case "phone":
      if (!E164.test(s)) return "expected an E.164 phone number (+441234567890)"
      break
    case "timezone":
      if (!validTimezone(s)) return "expected an IANA time zone name"
      break
    case "recurrence":
      if (!s.replace(/^RRULE:/, "").includes("FREQ=")) {
        return "expected an RFC 5545 RRULE string"
      }
      break
    case "digest":
      if (!SHA256.test(s)) return "expected a SHA-256 digest (64 lowercase hex)"
      break
    case "blobref":
      if (!BLOB_REF.test(s))
        return "expected a blob digest (blob-sha256-<64 hex>)"
      break
  }

  if (spec.values?.length && !spec.values.some((v) => v.value === s)) {
    return `expected one of ${admitted(spec)}`
  }
  if (spec.pattern) {
    try {
      if (!new RegExp(spec.pattern).test(s))
        return `does not match ${spec.pattern}`
    } catch {
      // A Go pattern JavaScript cannot compile is the server's to enforce.
    }
  }
  return undefined
}

/** Check one value against its declaration, the way the substrate will. The
 * message is the reason alone (`expected a number`), so a caller can prefix it
 * with wherever the value sits. `undefined` means the value is admissible;
 * `null` is a delete marker and always is. */
/** Check ONE key of a keyed map, the way the substrate checks it
 * (`vocabulary.Property.CheckKey`): an empty key is refused, a declared
 * `keyPattern` is held to, and absent one ANY non-empty string is a key,
 * because that is what a map is for.
 *
 * The key is checked EXACTLY as it was typed. Nothing here trims: ` helper `
 * and `helper` are two different keys, and quietly storing the second when
 * somebody named the first is a rename nobody asked for. A key with spaces
 * that no pattern refuses is a key. */
export function checkKey(spec: PropSpec, key: string): string | undefined {
  if (key === "") return "a keyed map's key is never empty"
  const grammar = spec.keyPattern ? KEY_PATTERNS[spec.keyPattern] : undefined
  if (grammar && !grammar.test(key)) {
    return `key "${key}" must be ${KEY_RULES[spec.keyPattern ?? ""] ?? spec.keyPattern}`
  }
  return undefined
}

export function checkValue(spec: PropSpec, value: unknown): string | undefined {
  if (value === null || value === undefined) return undefined
  if (spec.keyed) {
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return `expected a map of ${spec.kind}`
    }
    const item = elementSpec(spec)
    for (const [key, entry] of Object.entries(
      value as Record<string, unknown>
    )) {
      const badKey = checkKey(spec, key)
      if (badKey) return badKey
      const problem = checkItem(item, entry)
      if (problem) return underField(key, problem)
    }
    return undefined
  }
  if (spec.repeated) {
    if (!Array.isArray(value)) return `expected a list of ${spec.kind}`
    for (let i = 0; i < value.length; i++) {
      const problem = checkItem(spec, value[i])
      if (problem) return `[${i}]: ${problem}`
    }
    return undefined
  }
  if (Array.isArray(value)) return `expected a single ${spec.kind}`
  return checkItem(spec, value)
}

// ── text ↔ value ────────────────────────────────────────────────────────────

/** A property value as one line of text, for a form control. Objects and lists
 * render as JSON / one-per-line, everything else stringifies. A secret NEVER
 * renders: the stored value is sealed and the read is redacted. */
export function formatValue(spec: PropSpec, value: unknown): string {
  if (value === null || value === undefined) return ""
  if (controlFor(spec) === "secret") return ""
  if (Array.isArray(value)) {
    return value
      .map((v) =>
        typeof v === "object" && v !== null ? JSON.stringify(v) : String(v)
      )
      .join("\n")
  }
  if (typeof value === "object") return JSON.stringify(value, null, 2)
  return String(value)
}

export interface ParsedValue {
  value?: unknown
  error?: string
}

/** Read a control's text back into the typed value the write carries. Blank is
 * "no value" (`{}` with neither key), which a caller reads as absent. */
export function parseValue(spec: PropSpec, text: string): ParsedValue {
  const control = controlFor(spec)
  if (control === "list") {
    const items = text
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean)
      .map((s) => parseScalarText(spec, s))
    const bad = items.find((p) => p.error)
    if (bad?.error) return { error: bad.error }
    return items.length ? { value: items.map((p) => p.value) } : {}
  }
  const trimmed = control === "prose" || control === "json" ? text : text.trim()
  if (!trimmed) return {}
  return parseScalarText(spec, trimmed)
}

function parseScalarText(spec: PropSpec, text: string): ParsedValue {
  if (isObjectKind(spec.kind)) {
    try {
      return { value: JSON.parse(text) }
    } catch (e) {
      return { error: `not valid JSON (${(e as Error).message})` }
    }
  }
  if (isNumericKind(spec.kind)) {
    const n = Number(text)
    if (!Number.isFinite(n)) return { error: "expected a number" }
    const problem = checkItem(spec, n)
    return problem ? { error: problem } : { value: n }
  }
  if (isBooleanKind(spec.kind)) return { value: text === "true" }
  const problem = checkItem(spec, text)
  return problem ? { error: problem } : { value: text }
}
