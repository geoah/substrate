/** What a kind's declaration says about one property, and what that means for
 * a value. This is the ONE projection every editing surface reads: the YAML
 * lens (`record-yaml.ts`), the form lens (`record-form.ts`, and through it the
 * integrations dialog), and the controls both of them render.
 *
 * Two things live here and nowhere else:
 *
 * - `propSpecs(kind)` — the declared properties with everything an editor
 *   needs: datatype, required, repeated, an enum's admitted values, a
 *   reference's `to:` target, a state machine's states, `min`/`max`/`pattern`,
 *   a declared `default`, the `writer:` role and the one-liner.
 * - `checkValue(spec, value)` — the client's copy of the substrate's own
 *   coercion rules (`internal/engine/validate.go`), so a bad datetime, a
 *   string where an int is declared or an unadmitted enum value is named ON
 *   THE LINE instead of arriving as a 422 after the round trip. It is
 *   deliberately the SAME shape of check, never a stricter one: the server is
 *   still the authority, and a check the server would pass must pass here. */

import { parseEnumValues, type EnumValue, type KindInfo } from "@/lib/api/types"
import { temporalProperties } from "@/lib/definition"

/** The `to:` value a reference uses when it is pinned to no kind at all. */
export const TO_ANY = "any"

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
  /** Enum (and any property that narrows a string): the admitted values. */
  values?: EnumValue[]
  /** A declared `default:` — what a create seeds the property with. */
  default?: unknown
  /** `state`: the machine's states and the state a record is born into. */
  states?: string[]
  initial?: string
  /** `reference`: the kind this pointer is pinned to, or `any`. */
  to?: string
  /** `object`: the declared fields, each a property in its own right (a field
   * may narrow, range or enumerate exactly as a property does). One level
   * deep: a field is never itself an object, and never repeated. */
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
 * create; `json` covers `json` and `object`; `prose` is a textarea. */
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
  "decimal",
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
      specOf(name, typeof raw[name] === "string" ? { type: raw[name] } : (raw[name] as Record<string, unknown>) ?? {})
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
    values: parseEnumValues(def.values),
    default: def.default,
    states: stringList(def.states),
    initial: typeof def.initial === "string" ? def.initial : undefined,
    to: typeof def.to === "string" ? def.to : undefined,
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
 * `title` and `body` are always legal strings; `at`/`endsAt`/`dueAt` are legal
 * only where a temporal trait binds them. An editor must not call these
 * undeclared, because the substrate accepts them. */
export function systemSpecs(kind: KindInfo): PropSpec[] {
  const specs: PropSpec[] = [
    specOf("title", { type: "string", description: "the record's display title" }),
    specOf("body", { type: "text", description: "the record's body text" }),
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

export function controlFor(spec: PropSpec): Control {
  if (spec.kind === "secret") return "secret"
  if (spec.kind === "state") return "state"
  if (spec.kind === "reference") return "reference"
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
  return spec.repeated ? `${base}[]` : base
}

// ── examples ────────────────────────────────────────────────────────────────

/** A WORKED value for the datatype: what a filled-in property looks like, so a
 * blank create is not a blank page. Shown as a placeholder in the form lens and
 * as an `e.g.` in the template's trailing comment. Derived from the
 * declaration alone, never from a kind's name. */
export function exampleFor(spec: PropSpec): string | undefined {
  if (spec.values?.length) return spec.values[0].value
  if (spec.states?.length) return spec.initial ?? spec.states[0]
  switch (spec.kind) {
    case "datetime":
      return "2026-01-31T09:00:00Z"
    case "date":
      return "2026-01-31"
    case "duration":
      return "47m12s"
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
        ? `{kind: ${spec.to}, id: some-id}`
        : "{kind: <kind>, id: <id>}"
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
  if (spec.repeated) return []
  if (spec.kind === "state") return spec.initial ?? spec.states?.[0] ?? ""
  if (isBooleanKind(spec.kind)) return false
  if (isNumericKind(spec.kind)) return 0
  if (isObjectKind(spec.kind)) return {}
  return ""
}

// ── the value rules (the substrate's own, client-side) ───────────────────────

/** RFC 3339, and the two shorter forms the substrate's `parseTime` accepts. */
const DATETIME = /^\d{4}-\d{2}-\d{2}([T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?)?$/
const CIVIL_DATE = /^\d{4}-\d{2}-\d{2}$/
/** Go's duration grammar (`time.ParseDuration`): `47m12s`, `1.5h`, `-3ms`. */
const DURATION = /^[-+]?(\d+(\.\d*)?(ns|us|µs|μs|ms|s|m|h))+$/
const E164 = /^\+[1-9]\d{1,14}$/
const SHA256 = /^[0-9a-f]{64}$/
const BLOB_REF = /^blob-sha256-[0-9a-f]{64}$/

function admitted(spec: PropSpec): string {
  return (spec.values ?? []).map((v) => v.value).join(", ")
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
      const entries = Object.entries(value as Record<string, unknown>)
      for (const [name, field] of entries) {
        const declared = spec.fields.find((f) => f.name === name)
        if (!declared) return `\`${name}\` is not a declared field`
        const problem = checkValue(declared, field)
        if (problem) return `\`${name}\`: ${problem}`
      }
    }
    return undefined
  }

  if (spec.kind === "reference") {
    if (typeof value === "string") {
      if (!value.trim()) return "a reference needs an id"
      return spec.to && spec.to !== TO_ANY
        ? undefined
        : "a reference to any kind needs an explicit kind"
    }
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return "a reference is a {kind, id} object"
    }
    const ref = value as Record<string, unknown>
    if (typeof ref.id !== "string" || !ref.id.trim()) {
      return "a reference needs an id"
    }
    const hasKind = typeof ref.kind === "string" && ref.kind.trim().length > 0
    if (!hasKind && (!spec.to || spec.to === TO_ANY)) {
      return "a reference to any kind needs an explicit kind"
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
    if (spec.min !== undefined && value < spec.min) return `must be >= ${spec.min}`
    if (spec.max !== undefined && value > spec.max) return `must be <= ${spec.max}`
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
      if (!DURATION.test(s)) return "expected a duration like 47m12s"
      break
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
      if (!BLOB_REF.test(s)) return "expected a blob digest (blob-sha256-<64 hex>)"
      break
  }

  if (spec.values?.length && !spec.values.some((v) => v.value === s)) {
    return `expected one of ${admitted(spec)}`
  }
  if (spec.pattern) {
    try {
      if (!new RegExp(spec.pattern).test(s)) return `does not match ${spec.pattern}`
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
export function checkValue(spec: PropSpec, value: unknown): string | undefined {
  if (value === null || value === undefined) return undefined
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
      .map((v) => (typeof v === "object" && v !== null ? JSON.stringify(v) : String(v)))
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
