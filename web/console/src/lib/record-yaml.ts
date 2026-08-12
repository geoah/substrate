/** The YAML editor's pure core (create + edit a record from its kind).
 *
 * Everything here is data-in/data-out so it can be unit-tested without a DOM:
 * - `templateYAML(kind)` builds the apply-able envelope a NEW record starts
 *   from, entirely from the kind's declared schema — every property seeded with
 *   its `default` (else a typed zero / placeholder), required properties first,
 *   a comment marking required vs optional, and an enum's admitted values in a
 *   comment. Schema-driven, never kind-special-cased.
 * - `applyManifestYAML(record)` is the EDIT seed — the same envelope
 *   (`kind`/`metadata`/`data`) the manifest view shows, MINUS the server-owned
 *   `status` block.
 * - `parseApplyDoc` / `validateApplyDoc` parse the editor's text and check it
 *   against the schema (YAML parses, required present, enum admitted, obvious
 *   kind mismatches), returning inline problems.
 * - `toPutInput` coerces a parsed doc into the create/patch write payload. */

import { Document, isCollection, isScalar, parseDocument, YAMLMap } from "yaml"

import type { SubstrateRecord, EnumValue, KindInfo } from "@/lib/api/types"
import { parseEnumValues } from "@/lib/api/types"

// ── the declared-property spec the editor reads ─────────────────────────────

/** One declared property, projected with everything the template + validation
 * need — kind, required/enum/default/repeated — straight off the verbatim
 * manifest block the substrate serves (`definition.properties.<name>`). */
export interface PropSpec {
  name: string
  kind: string
  required: boolean
  repeated: boolean
  /** Enum: the admitted values (`{value, label}`), declaration order. */
  values?: EnumValue[]
  /** A declared `default:` — the value a create seeds the field with. */
  default?: unknown
  /** `state`-kind: the machine's states and the state it is born into. */
  states?: string[]
  initial?: string
  description?: string
}

function rawProps(kind: KindInfo): Record<string, Record<string, unknown>> {
  const def = (kind.definition ?? {}) as Record<string, unknown>
  return (def.properties ?? {}) as Record<string, Record<string, unknown>>
}

function stringList(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined
  const out = v.filter((x): x is string => typeof x === "string")
  return out.length ? out : undefined
}

/** The declared properties, required first then optional, alphabetical within
 * each band (key order is lost to jsonb, so alphabetical is the honest order). */
export function propSpecs(kind: KindInfo): PropSpec[] {
  const raw = rawProps(kind)
  const specs: PropSpec[] = Object.entries(raw).map(([name, def]) => ({
    name,
    kind: typeof def.type === "string" ? def.type : "string",
    required: def.required === true,
    repeated: def.repeated === true,
    values: parseEnumValues(def.values),
    default: def.default,
    states: stringList(def.states),
    initial: typeof def.initial === "string" ? def.initial : undefined,
    description:
      typeof def.description === "string" ? def.description : undefined,
  }))
  return specs.sort(
    (a, b) =>
      Number(b.required) - Number(a.required) || a.name.localeCompare(b.name)
  )
}

// ── kind families ───────────────────────────────────────────────────────────

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

function isNumeric(kind: string): boolean {
  return NUMERIC.has(kind)
}

/** The seed a create template gives a property: its declared `default` when it
 * has one, else a typed zero / placeholder by kind. Repeated properties seed to
 * an empty list regardless of element kind. */
export function seedValue(spec: PropSpec): unknown {
  if (spec.default !== undefined && spec.default !== null) return spec.default
  if (spec.repeated) return []
  if (spec.kind === "state") return spec.initial ?? spec.states?.[0] ?? ""
  if (BOOLEAN.has(spec.kind)) return false
  if (isNumeric(spec.kind)) return 0
  if (OBJECT.has(spec.kind)) return {}
  // string / email / url / text / markdown / date / secret / enum …
  return ""
}

// ── the template envelope ────────────────────────────────────────────────────

/** A short trailing comment describing a property: required/optional, its kind,
 * and (for an enum) its admitted values. */
function specComment(spec: PropSpec): string {
  const parts = [spec.required ? "required" : "optional"]
  if (spec.values?.length) {
    const admitted = spec.values
      .map((v) => (v.label ? `${v.value} (${v.label})` : v.value))
      .join(" | ")
    parts.push(`enum: ${admitted}`)
  } else parts.push(spec.repeated ? `${spec.kind}[]` : spec.kind)
  return ` ${parts.join(", ")}`
}

/** The apply-able envelope a NEW record of `kind` starts from, as a yaml
 * Document with comments. `kind` is fixed to the kind reference; `metadata.id`
 * is blank (omit to let the substrate mint one); `data.properties` carries
 * every declared property, required first, each seeded and commented. */
export function templateDoc(kind: KindInfo): Document {
  const specs = propSpecs(kind)

  const properties: Record<string, unknown> = {}
  for (const spec of specs) properties[spec.name] = seedValue(spec)

  const doc = new Document({
    kind: kind.identity,
    metadata: { id: "" },
    data: { properties },
  })

  const idNode = doc.getIn(["metadata", "id"], true)
  if (isScalar(idNode)) {
    idNode.comment = " optional — omit to let the substrate mint one"
  }

  const propsMap = doc.getIn(["data", "properties"], true)
  if (propsMap instanceof YAMLMap) {
    const bySpec = new Map(specs.map((s) => [s.name, s]))
    for (const item of propsMap.items) {
      const key = String(item.key)
      const spec = bySpec.get(key)
      const value = item.value as { comment?: string; items?: unknown[] } | null
      if (spec && value && typeof value === "object") {
        // An empty seed collection (`[]` / `{}`) renders block-style, which
        // strands a trailing comment on its own line — force flow so the
        // comment sits inline beside `functions: []`.
        if (isCollection(item.value) && item.value.items.length === 0) {
          item.value.flow = true
        }
        value.comment = specComment(spec)
      }
    }
  }

  return doc
}

export function templateYAML(kind: KindInfo): string {
  return templateDoc(kind).toString({
    lineWidth: 0,
    defaultStringType: "PLAIN",
    defaultKeyType: "PLAIN",
  })
}

/** The EDIT seed: the record's apply-able envelope — the manifest view's shape
 * without the server-owned `status` block. Order-preserving so the yaml
 * serializes what the read served. Labels ride in `metadata`; edge references
 * travel whole as `{kind, id}`. */
export function applyManifestOf(record: SubstrateRecord): Record<string, unknown> {
  const metadata: Record<string, unknown> = { id: record.id }
  if (Object.keys(record.labels ?? {}).length) metadata.labels = record.labels
  if (record.annotations && Object.keys(record.annotations).length) {
    metadata.annotations = record.annotations
  }

  const data: Record<string, unknown> = {}
  if (Object.keys(record.properties ?? {}).length) {
    data.properties = record.properties
  }
  const edges = Object.entries(record.edges ?? {}).flatMap(([rel, targets]) =>
    (targets ?? []).map((t) => {
      const to = { kind: t.kind, id: t.id }
      return t.properties && Object.keys(t.properties).length
        ? { rel, properties: t.properties, to }
        : { rel, to }
    })
  )
  if (edges.length) data.edges = edges

  const doc: Record<string, unknown> = { kind: record.kind, metadata }
  if (Object.keys(data).length) doc.data = data
  return doc
}

export function applyManifestYAML(record: SubstrateRecord): string {
  return new Document(applyManifestOf(record)).toString({
    lineWidth: 0,
    defaultStringType: "PLAIN",
    defaultKeyType: "PLAIN",
  })
}

// ── parse + validate ─────────────────────────────────────────────────────────

/** The apply envelope, as the editor's text parses to. Everything is optional
 * on the wire of a parse — validation is what enforces the shape. */
export interface ApplyDoc {
  kind?: string
  metadata?: {
    id?: string
    labels?: Record<string, unknown>
    annotations?: Record<string, unknown>
  }
  data?: {
    properties?: Record<string, unknown>
    edges?: unknown[]
  }
}

export interface ParsedDoc {
  value?: ApplyDoc
  /** A YAML syntax error, with the 1-based line it sits on when known. */
  error?: { message: string; line?: number }
}

/** Parse the editor text to the apply envelope. A syntax error comes back as
 * `error` (with a line where the parser reports one); a document that is not a
 * mapping comes back as `error` too. */
export function parseApplyDoc(text: string): ParsedDoc {
  const doc = parseDocument(text)
  const fatal = doc.errors[0]
  if (fatal) {
    return {
      error: {
        message: fatal.message,
        line: fatal.linePos?.[0]?.line,
      },
    }
  }
  const value = doc.toJS() as unknown
  if (value === null || value === undefined) return { value: {} }
  if (typeof value !== "object" || Array.isArray(value)) {
    return { error: { message: "The document must be a YAML mapping." } }
  }
  return { value: value as ApplyDoc }
}

/** One issue the editor surfaces inline. `line` is 1-based when we could locate
 * the offending key; `path` names it for the problems list. */
export interface Problem {
  severity: "error" | "warning"
  message: string
  path?: string
  line?: number
}

function isEmptyValue(v: unknown): boolean {
  return (
    v === undefined ||
    v === null ||
    (typeof v === "string" && v.trim() === "") ||
    (Array.isArray(v) && v.length === 0)
  )
}

/** Best-effort 1-based line of a top-level `data.properties` key in the text,
 * so a problem can point a gutter at it. Matches the key at any indentation. */
function lineOfKey(text: string, key: string): number | undefined {
  const lines = text.split("\n")
  const re = new RegExp(`^\\s+${key.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\$&")}\\s*:`)
  for (let i = 0; i < lines.length; i++) {
    if (re.test(lines[i])) return i + 1
  }
  return undefined
}

/** A human phrase for the kind a value should be, for a kind-mismatch message. */
function kindNoun(spec: PropSpec): string {
  if (spec.repeated) return "a list"
  if (spec.values?.length) {
    return `one of: ${spec.values.map((v) => v.value).join(", ")}`
  }
  if (BOOLEAN.has(spec.kind)) return "a boolean"
  if (isNumeric(spec.kind)) return "a number"
  if (OBJECT.has(spec.kind)) return "an object"
  return "a string"
}

/** Whether a present value's JS type clashes with the declared kind. Lenient —
 * only flags an OBVIOUS mismatch (a list where a scalar is declared, a string
 * where a number is, an unknown enum value), never a coercible near-miss. */
function typeMismatch(spec: PropSpec, value: unknown): boolean {
  if (value === null) return false
  if (spec.repeated) return !Array.isArray(value)
  if (Array.isArray(value)) return true
  if (spec.values?.length) {
    return (
      typeof value !== "string" ||
      !spec.values.some((v) => v.value === value)
    )
  }
  if (BOOLEAN.has(spec.kind)) return typeof value !== "boolean"
  if (isNumeric(spec.kind)) return typeof value !== "number"
  if (OBJECT.has(spec.kind)) return typeof value !== "object"
  // string family — a scalar object/number/bool where prose is declared.
  return typeof value === "object"
}

/** Validate the editor text against the kind's schema. Returns every problem;
 * an empty list (or warnings only) means the client is willing to apply. A
 * single fatal syntax/shape error short-circuits the rest. */
export function validateApplyDoc(text: string, kind: KindInfo): Problem[] {
  const parsed = parseApplyDoc(text)
  if (parsed.error) {
    return [
      {
        severity: "error",
        message: parsed.error.message,
        line: parsed.error.line,
      },
    ]
  }
  const doc = parsed.value ?? {}
  const problems: Problem[] = []

  const data = doc.data
  if (data !== undefined && (typeof data !== "object" || Array.isArray(data))) {
    return [{ severity: "error", message: "`data` must be a mapping." }]
  }
  const props = data?.properties
  if (
    props !== undefined &&
    (typeof props !== "object" || Array.isArray(props))
  ) {
    return [
      { severity: "error", message: "`data.properties` must be a mapping." },
    ]
  }
  const values = (props ?? {}) as Record<string, unknown>

  const specs = propSpecs(kind)
  const byName = new Map(specs.map((s) => [s.name, s]))

  for (const spec of specs) {
    const present = Object.prototype.hasOwnProperty.call(values, spec.name)
    const value = values[spec.name]
    if (spec.required && (!present || isEmptyValue(value))) {
      problems.push({
        severity: "error",
        message: `\`${spec.name}\` is required.`,
        path: spec.name,
        line: lineOfKey(text, spec.name),
      })
      continue
    }
    if (present && !isEmptyValue(value) && typeMismatch(spec, value)) {
      problems.push({
        severity: "error",
        message: `\`${spec.name}\` must be ${kindNoun(spec)}.`,
        path: spec.name,
        line: lineOfKey(text, spec.name),
      })
    }
  }

  // Unknown properties are a warning, not a bar — the server is the authority,
  // but a typo'd key should not sail through silently.
  for (const key of Object.keys(values)) {
    if (!byName.has(key)) {
      problems.push({
        severity: "warning",
        message: `\`${key}\` is not a declared property of this kind.`,
        path: key,
        line: lineOfKey(text, key),
      })
    }
  }

  return problems
}

// ── the write payload ────────────────────────────────────────────────────────

/** One edge on a write, the envelope's own `{rel, to:{kind, id}}` shape. */
export interface EdgeWrite {
  rel: string
  to: { kind?: string; id: string }
  properties?: Record<string, unknown>
}

export interface PutInput {
  id?: string
  properties?: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  edges?: EdgeWrite[]
}

function edgeWrites(raw: unknown): EdgeWrite[] | undefined {
  if (!Array.isArray(raw)) return undefined
  const out: EdgeWrite[] = []
  for (const item of raw) {
    if (!item || typeof item !== "object") continue
    const e = item as Record<string, unknown>
    const rel = typeof e.rel === "string" ? e.rel : undefined
    const to = e.to as Record<string, unknown> | undefined
    const id = to && typeof to.id === "string" ? to.id : undefined
    if (!rel || !id) continue
    out.push({
      rel,
      to: {
        id,
        kind: typeof to?.kind === "string" ? to.kind : undefined,
      },
      properties:
        e.properties && typeof e.properties === "object"
          ? (e.properties as Record<string, unknown>)
          : undefined,
    })
  }
  return out.length ? out : undefined
}

/** Coerce a parsed apply doc into the create/upsert write body: the authored
 * `properties`/`edges` under `data`, `labels`/`annotations` under `metadata`,
 * and `metadata.id` as the write's own key (omitted when blank, so the
 * substrate mints one on create). */
export function toPutInput(doc: ApplyDoc): PutInput {
  const out: PutInput = {}
  const id = doc.metadata?.id
  if (typeof id === "string" && id.trim()) out.id = id.trim()
  const meta = doc.metadata ?? {}
  if (meta.labels && typeof meta.labels === "object") out.labels = meta.labels
  if (meta.annotations && typeof meta.annotations === "object") {
    out.annotations = meta.annotations
  }
  const data = doc.data ?? {}
  if (data.properties && typeof data.properties === "object") {
    out.properties = data.properties
  }
  const edges = edgeWrites(data.edges)
  if (edges) out.edges = edges
  return out
}
