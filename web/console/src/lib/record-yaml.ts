/** The YAML lens's pure core (create + edit a record from its kind). The
 * DECLARATION is read through `record-schema.ts`, which the form lens reads
 * too, so both lenses agree on what a property is and what a value may be.
 *
 * Everything here is data-in/data-out so it can be unit-tested without a DOM:
 * - `templateYAML(kind)` builds the apply-able envelope a NEW record starts
 *   from, entirely from the declared schema: every property seeded with its
 *   `default` (else a typed zero), required properties first, and a trailing
 *   comment carrying required/optional, the datatype, the one-liner and a
 *   worked example. Schema-driven, never kind-special-cased.
 * - `applyManifestYAML(record)` is the EDIT seed: the same envelope
 *   (`kind`/`metadata`/`data`) the manifest view shows, MINUS the server-owned
 *   `status` block.
 * - `parseApplyDoc` / `validateApplyDoc` parse the editor's text and check it
 *   against the declaration the way the substrate will (`checkValue`), plus the
 *   two rules that are the WRITE's, not the value's: a put may not move a
 *   state, and the route's id is the one the write lands on.
 * - `setIn` / `propertiesOf` are the form lens's surgery on the same text: one
 *   key changes, every other line (comments included) stays exactly as authored.
 * - `toPutInput` coerces a parsed doc into the create/upsert write payload. */

import {
  Document,
  isCollection,
  isNode,
  isScalar,
  parseDocument,
  YAMLMap,
} from "yaml"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  checkValue,
  exampleFor,
  propSpecs,
  seedValue,
  systemSpecs,
  typeLabel,
  type PropSpec,
} from "@/lib/record-schema"

export type { PropSpec }

/** One serialization, so a form edit and a hand edit produce the same shape. */
const STRINGIFY = {
  lineWidth: 0,
  defaultStringType: "PLAIN",
  defaultKeyType: "PLAIN",
} as const

// ── the template envelope ────────────────────────────────────────────────────

/** How long a declaration's one-liner may run inside a trailing comment before
 * it is cut: a comment is a hint, not the documentation. */
const COMMENT_DESCRIPTION_MAX = 96

function shorten(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim()
  return flat.length > COMMENT_DESCRIPTION_MAX
    ? `${flat.slice(0, COMMENT_DESCRIPTION_MAX - 1)}…`
    : flat
}

/** The trailing comment a template's property carries: required or optional,
 * its datatype (an enum lists what it admits), the declaration's one-liner, and
 * a worked example of the datatype. Everything a person needs to fill the line
 * in, read off the declaration alone. */
export function specComment(spec: PropSpec): string {
  const parts = [spec.required ? "required" : "optional"]
  if (spec.values?.length) {
    parts.push(
      `enum: ${spec.values
        .map((v) => (v.label ? `${v.value} (${v.label})` : v.value))
        .join(" | ")}`
    )
  } else if (spec.states?.length) {
    parts.push(`state: ${spec.states.join(" | ")}`)
  } else {
    parts.push(typeLabel(spec))
  }
  let out = parts.join(", ")
  if (spec.description) out += `: ${shorten(spec.description)}`
  const example = exampleFor(spec)
  if (example && !spec.values?.length && !spec.states?.length) {
    out += `; e.g. ${example}`
  }
  return ` ${out}`
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
  return templateDoc(kind).toString(STRINGIFY)
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
  return new Document(applyManifestOf(record)).toString(STRINGIFY)
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

/** The same, for an envelope key at the document's own level (`kind:`). */
function lineOfTopKey(text: string, key: string): number | undefined {
  const lines = text.split("\n")
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].startsWith(`${key}:`)) return i + 1
  }
  return undefined
}

/** What the editor knows about the write it is preparing, beyond the text: the
 * record being edited (a put may not move a state, and the id in the document
 * is not what the write lands on) — absent on a create. */
export interface ApplyContext {
  record?: SubstrateRecord
}

function edgeProblems(raw: unknown, kind: KindInfo, text: string): Problem[] {
  if (raw === undefined) return []
  if (!Array.isArray(raw)) {
    return [
      {
        severity: "error",
        message: "`data.edges` must be a list of `{rel, to: {kind, id}}`.",
        line: lineOfKey(text, "edges"),
      },
    ]
  }
  const declared = new Set(
    Object.keys(
      ((kind.definition ?? {}) as Record<string, unknown>).edges ??
        ({} as Record<string, unknown>)
    )
  )
  const problems: Problem[] = []
  raw.forEach((item, i) => {
    const at = `edges[${i}]`
    if (!item || typeof item !== "object" || Array.isArray(item)) {
      problems.push({
        severity: "error",
        message: `\`${at}\` must be a \`{rel, to: {kind, id}}\` mapping.`,
        path: at,
      })
      return
    }
    const e = item as Record<string, unknown>
    const rel = typeof e.rel === "string" ? e.rel : ""
    const to = (e.to ?? {}) as Record<string, unknown>
    if (!rel) {
      problems.push({
        severity: "error",
        message: `\`${at}\` needs a \`rel\`.`,
        path: at,
      })
    } else if (declared.size && !declared.has(rel)) {
      problems.push({
        severity: "warning",
        message: `\`${rel}\` is not a declared edge of this kind.`,
        path: at,
        line: lineOfKey(text, "rel"),
      })
    }
    if (typeof to.id !== "string" || !to.id.trim()) {
      problems.push({
        severity: "error",
        message: `\`${at}\` needs a \`to.id\`.`,
        path: at,
      })
    }
  })
  return problems
}

/** Validate the editor text against the kind's declaration. Returns every
 * problem; an empty list (or warnings only) means the client is willing to
 * apply. A single fatal syntax/shape error short-circuits the rest. */
export function validateApplyDoc(
  text: string,
  kind: KindInfo,
  ctx: ApplyContext = {}
): Problem[] {
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

  if (typeof doc.kind === "string" && doc.kind && doc.kind !== kind.identity) {
    problems.push({
      severity: "error",
      message: `\`kind\` is ${doc.kind}, but this collection is ${kind.identity}.`,
      path: "kind",
      line: lineOfTopKey(text, "kind"),
    })
  }
  const authored = doc.metadata?.id
  if (
    ctx.record &&
    typeof authored === "string" &&
    authored.trim() &&
    authored.trim() !== ctx.record.id
  ) {
    problems.push({
      severity: "warning",
      message: `\`metadata.id\` does not rename a record — this write lands on ${ctx.record.id}.`,
      path: "metadata.id",
      line: lineOfKey(text, "id"),
    })
  }

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

  // The system columns ride in `properties` on the wire and the write path
  // splits them out, so they are checked like any other value and never
  // reported as undeclared. A kind that declares one of them wins: its own
  // declaration is the stricter truth.
  const byName = new Map(
    [...systemSpecs(kind), ...propSpecs(kind)].map((s) => [s.name, s])
  )

  for (const spec of byName.values()) {
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
    if (!present || isEmptyValue(value)) continue

    const problem = checkValue(spec, value)
    if (problem) {
      problems.push({
        severity: "error",
        message: `\`${spec.name}\`: ${problem}.`,
        path: spec.name,
        line: lineOfKey(text, spec.name),
      })
      continue
    }
    // A put may not move a state (engine/write.go): the transition is a patch,
    // and the console drives it from the record page. Catch it here rather than
    // through a guard rejection after the round trip.
    if (
      spec.kind === "state" &&
      ctx.record &&
      typeof value === "string" &&
      value !== ctx.record.properties?.[spec.name]
    ) {
      problems.push({
        severity: "error",
        message: `\`${spec.name}\` is a state: it moves by transition, not by editing. Leave it at \`${String(
          ctx.record.properties?.[spec.name] ?? ""
        )}\`.`,
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

  problems.push(...edgeProblems(data?.edges, kind, text))

  return problems
}

// ── surgery: the form lens editing the same document ────────────────────────

/** The document's authored properties, or undefined when the text does not
 * parse to an envelope. The form lens reads its values through this, so the
 * YAML is the single source of truth for both lenses. */
export function propertiesOf(text: string): Record<string, unknown> | undefined {
  const parsed = parseApplyDoc(text)
  if (parsed.error || !parsed.value) return undefined
  const props = parsed.value.data?.properties
  if (props === undefined) return {}
  if (typeof props !== "object" || Array.isArray(props)) return undefined
  return props as Record<string, unknown>
}

/** Set one path in the document and give back the text, leaving every other
 * line exactly as authored: a form edit must not reflow a hand-written
 * document, and the template's own comments must survive being filled in. A
 * text that does not parse comes back untouched. */
export function setIn(text: string, path: string[], value: unknown): string {
  // An empty document has no mapping to set a path in, so start one.
  const doc: Document = text.trim() ? parseDocument(text) : new Document({})
  if (doc.errors.length) return text
  const existing = doc.getIn(path, true)
  const comment = isNode(existing) ? existing.comment : undefined
  const commentBefore = isNode(existing) ? existing.commentBefore : undefined
  doc.setIn(path, value)
  const next = doc.getIn(path, true)
  if (isNode(next)) {
    if (comment) next.comment = comment
    if (commentBefore) next.commentBefore = commentBefore
    // Setting a key reuses the node that stood there, quoting style included:
    // a value written over the template's `""` would inherit its quotes. Drop
    // the inherited style so the one serialization decides, and a string that
    // still needs quotes still gets them.
    if (isScalar(next)) next.type = undefined
    // An empty collection keeps its trailing comment on the same line only in
    // flow style, exactly as the template writes it.
    if (isCollection(next) && next.items.length === 0) next.flow = true
  }
  return doc.toString(STRINGIFY)
}

/** Remove one path from the document, leaving the rest as authored. */
export function deleteIn(text: string, path: string[]): string {
  const doc = parseDocument(text)
  if (doc.errors.length) return text
  doc.deleteIn(path)
  return doc.toString(STRINGIFY)
}

/** Reformat the document: the parse re-emitted under the editor's one
 * serialization, so a hand-edited file settles back into the shape the
 * template and the read both produce. Comments survive; a document that does
 * not parse comes back with the parser's complaint and no change. */
export function formatYAML(text: string): { text: string; error?: string } {
  const doc = parseDocument(text)
  const fatal = doc.errors[0]
  if (fatal) return { text, error: fatal.message }
  return { text: doc.toString(STRINGIFY) }
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

/** Datatypes where an empty string is a value somebody could mean. Everywhere
 * else a blank line is "not set", and sending it would be refused: `url: ""` is
 * not an absolute URL, `datetime: ""` is not a timestamp, and `secret: ""`
 * would seal an empty credential over a live one. */
const EMPTY_IS_A_VALUE = new Set(["string", "text", "markdown"])

/** Drop the properties a blank template line left behind, so a create that
 * filled in three of eleven properties does not carry eight empty strings into
 * a 422. An explicit `null` survives: that is the delete marker. */
function pruneBlanks(
  props: Record<string, unknown>,
  kind?: KindInfo
): Record<string, unknown> {
  if (!kind) return props
  const specs = new Map(propSpecs(kind).map((s) => [s.name, s]))
  const out: Record<string, unknown> = {}
  for (const [name, value] of Object.entries(props)) {
    const spec = specs.get(name)
    if (value === "" && spec && !EMPTY_IS_A_VALUE.has(spec.kind)) continue
    out[name] = value
  }
  return out
}

/** Coerce a parsed apply doc into the create/upsert write body: the authored
 * `properties`/`edges` under `data`, `labels`/`annotations` under `metadata`,
 * and `metadata.id` as the write's own key (omitted when blank, so the
 * substrate mints one on create). Pass the kind and blank lines the author
 * never filled in are left out of the write. */
export function toPutInput(doc: ApplyDoc, kind?: KindInfo): PutInput {
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
    out.properties = pruneBlanks(
      data.properties as Record<string, unknown>,
      kind
    )
  }
  const edges = edgeWrites(data.edges)
  if (edges) out.edges = edges
  return out
}
