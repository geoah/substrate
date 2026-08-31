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
  isMap,
  isNode,
  isScalar,
  isSeq,
  parseDocument,
  YAMLMap,
} from "yaml"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import { isDeclarationKind } from "@/lib/declarations"
import {
  checkValue,
  exampleFor,
  propSpecs,
  seedValue,
  systemSpecs,
  typeLabel,
  underField,
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
  // A MANAGED property is the engine's stamp, and it refuses a write that
  // disagrees with what it stamped, so a template that seeded one with a
  // typed zero would hand every create a value the substrate then refuses.
  const specs = propSpecs(kind).filter((spec) => !spec.managed)

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

/** Drop the properties the kind declares `managed:`, the engine's stamps
 * (the declaration version above all), which it refuses a write that
 * disagrees with, so neither the edit seed nor the put may carry one. The
 * same source of truth as `templateDoc` and the form lens
 * (`record-form.toFieldValue`). Without the kind the declaration is unknown,
 * so the properties pass through untouched: this lens edits every record,
 * and guessing would drop somebody's data. */
function withoutManaged(
  props: Record<string, unknown>,
  kind?: KindInfo
): Record<string, unknown> {
  if (!kind) return props
  const managed = new Set(
    propSpecs(kind)
      .filter((spec) => spec.managed)
      .map((spec) => spec.name)
  )
  if (!managed.size) return props
  const out: Record<string, unknown> = {}
  for (const [name, value] of Object.entries(props)) {
    if (!managed.has(name)) out[name] = value
  }
  return out
}

/** The EDIT seed: the record's apply-able envelope — the manifest view's shape
 * without the server-owned `status` block, and (when the kind is known)
 * without the managed properties a hand edit must not carry.
 * Order-preserving so the yaml serializes what the read served. Labels ride
 * in `metadata`; a pointer at another record is a `reference` property, so it
 * travels inside `data.properties` like every other value. */
export function applyManifestOf(
  record: SubstrateRecord,
  kind?: KindInfo
): Record<string, unknown> {
  const metadata: Record<string, unknown> = { id: record.id }
  if (Object.keys(record.labels ?? {}).length) metadata.labels = record.labels
  if (record.annotations && Object.keys(record.annotations).length) {
    metadata.annotations = record.annotations
  }

  const data: Record<string, unknown> = {}
  const properties = withoutManaged(record.properties ?? {}, kind)
  if (Object.keys(properties).length) {
    data.properties = properties
  }
  const doc: Record<string, unknown> = { kind: record.kind, metadata }
  if (Object.keys(data).length) doc.data = data
  return doc
}

export function applyManifestYAML(
  record: SubstrateRecord,
  kind?: KindInfo
): string {
  return new Document(applyManifestOf(record, kind)).toString(STRINGIFY)
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
  const re = new RegExp(
    `^\\s+${key.replace(/[.*+?^${}()|[\\]\\\\]/g, "\\$&")}\\s*:`
  )
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
  // A DECLARATION is addressed by its declared identity, and the substrate
  // mints none: `putSchemaRecord` refuses a write that carries no id, because
  // the id is the name the registry resolves. Only on a create: an edit's id
  // is the route's.
  if (
    !ctx.record &&
    isDeclarationKind(kind.identity) &&
    !(typeof authored === "string" && authored.trim())
  ) {
    problems.push({
      severity: "error",
      message: `\`metadata.id\` is required: a ${kind.name} is addressed by the identity it declares, and the substrate never mints one.`,
      path: "metadata.id",
      line: lineOfKey(text, "id"),
    })
  }
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
        // The property is the outermost level of the same dotted trail, so a
        // failure four fields down still reads as one path.
        message: `${underField(spec.name, problem)}.`,
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

  return problems
}

// ── surgery: the form lens editing the same document ────────────────────────

/** A path INTO the document: mapping keys as strings, sequence positions as
 * numbers. The form lens builds one down to the value that changed
 * (`record-form.narrowEdit`) so a write touches exactly that value. */
export type EditPath = (string | number)[]

/** The document's authored properties, or undefined when the text does not
 * parse to an envelope. The form lens reads its values through this, so the
 * YAML is the single source of truth for both lenses. */
export function propertiesOf(
  text: string
): Record<string, unknown> | undefined {
  const parsed = parseApplyDoc(text)
  if (parsed.error || !parsed.value) return undefined
  const props = parsed.value.data?.properties
  if (props === undefined) return {}
  if (typeof props !== "object" || Array.isArray(props)) return undefined
  return props as Record<string, unknown>
}

/** Whether the document holds anything at this path. What separates a
 * container somebody EMPTIED from one that was never there. */
export function hasIn(text: string, path: EditPath): boolean {
  const doc = parseDocument(text)
  if (doc.errors.length) return false
  return doc.hasIn(path)
}

/** Whether the document can take a write at this path without inventing what
 * lies between. Every step must be a collection of the right shape (or absent,
 * which `setIn` creates), and a numeric step must be an index the sequence
 * already holds or the one just past its end, because writing past that would
 * leave a hole in the list.
 *
 * A path that fails this is one the document has DRIFTED from: the form's rows
 * and the document's have stopped lining up. The caller falls back to writing
 * the whole property, which is always safe and only costs the comments inside
 * it. */
export function canSetIn(text: string, path: EditPath): boolean {
  const doc = parseDocument(text)
  if (doc.errors.length) return false
  let node: unknown = doc.contents
  for (let i = 0; i < path.length; i++) {
    const key = path[i]
    // Nothing here yet: `setIn` builds the rest of the path itself.
    if (node === null || node === undefined) return true
    if (!isCollection(node)) return false
    if (typeof key === "number") {
      if (!isSeq(node) || key > node.items.length) return false
    } else if (!isMap(node)) {
      return false
    }
    if (i === path.length - 1) return true
    node = node.get(key as never, true)
  }
  return true
}

/** Set one path in the document and give back the text, leaving every other
 * line exactly as authored: a form edit must not reflow a hand-written
 * document, and the template's own comments must survive being filled in.
 *
 * The path reaches as DEEP as the value that changed, which is what makes a
 * nested edit lossless: writing `reads.budgets.calls` touches that one scalar,
 * where writing the whole `reads` would re-serialize the subtree and take
 * every comment and every empty sibling with it.
 *
 * A text that does not parse, or a path the document cannot take, comes back
 * untouched. */
export function setIn(text: string, path: EditPath, value: unknown): string {
  // An empty document has no mapping to set a path in, so start one.
  const doc: Document = text.trim() ? parseDocument(text) : new Document({})
  if (doc.errors.length) return text
  // Collections standing EMPTY before the write render flow (`{}` inline with
  // their trailing comment, as the template writes them). One that gains
  // content has to go back to block, or a deep write would fold a whole
  // subtree onto one line.
  const wereEmpty = []
  for (let i = 1; i < path.length; i++) {
    const node = doc.getIn(path.slice(0, i), true)
    if (isCollection(node) && node.items.length === 0) wereEmpty.push(node)
  }
  const existing = doc.getIn(path, true)
  const comment = isNode(existing) ? existing.comment : undefined
  const commentBefore = isNode(existing) ? existing.commentBefore : undefined
  try {
    doc.setIn(path, value)
  } catch {
    // The path runs through something that is not a collection: the document
    // and the form disagree about the shape, and the form does not win.
    return text
  }
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
  for (const node of wereEmpty) {
    if (node.items.length > 0) node.flow = false
  }
  return doc.toString(STRINGIFY)
}

/** Remove one path from the document, leaving the rest as authored. The path
 * reaches as deep as `setIn`'s: clearing one field of an object takes that key
 * out and leaves its siblings, comments and all, where they were. */
export function deleteIn(text: string, path: EditPath): string {
  const doc = parseDocument(text)
  if (doc.errors.length) return text
  try {
    doc.deleteIn(path)
  } catch {
    return text
  }
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

export interface PutInput {
  id?: string
  properties?: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
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
 * `properties` under `data`, `labels`/`annotations` under `metadata`,
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
    // Managed properties never reach the wire from this lens: a hand-typed
    // version would disagree with the engine's stamp and the write refused.
    out.properties = pruneBlanks(
      withoutManaged(data.properties as Record<string, unknown>, kind),
      kind
    )
  }
  return out
}
