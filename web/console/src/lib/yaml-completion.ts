/** What the editor can offer at a point in an apply document, read off the
 * kind's declaration. Pure line/indent analysis so the whole completion
 * contract is unit-testable without an editor, a DOM or a language server.
 *
 * The document has a fixed shape (`kind` / `metadata` / `data`), so knowing
 * WHERE the cursor is means knowing what may be written there: the envelope's
 * own keys at the top, the declared properties under `data.properties`, an
 * enum's admitted values after its key, a state machine's states, the declared
 * edge rels after a `rel:`. Everything offered is something the substrate will
 * accept, which is the point: the editor stops being a guess. */

import type { KindInfo } from "@/lib/api/types"
import {
  controlFor,
  exampleFor,
  propSpecs,
  systemSpecs,
  typeLabel,
  type PropSpec,
} from "@/lib/record-schema"

/** One thing the editor can offer. `apply` is what lands in the document when
 * it differs from the label (a key completes with its colon and a space). */
export interface Suggestion {
  label: string
  /** The datatype, shown beside the label. */
  detail?: string
  /** The declaration's one-liner, shown under it. */
  info?: string
  apply?: string
  /** What kind of thing this is, for the editor's own icon/sorting. */
  type: "property" | "value" | "key"
}

/** Where a completion goes: the suggestions, and the COLUMN on the line the
 * replacement starts at (0-based), so the editor replaces the partial word
 * rather than appending to it. */
export interface Completions {
  from: number
  options: Suggestion[]
}

/** The envelope's own keys, which are not the kind's to declare. */
const ENVELOPE: Record<string, { keys: Suggestion[] }> = {
  "": {
    keys: [
      { label: "kind", type: "key", info: "the record's kind reference" },
      { label: "metadata", type: "key", info: "id, labels and annotations" },
      { label: "data", type: "key", info: "properties and edges" },
    ],
  },
  metadata: {
    keys: [
      { label: "id", type: "key", info: "omit to let the substrate mint one" },
      { label: "labels", type: "key", info: "writer-set labels" },
      { label: "annotations", type: "key", info: "writer-set annotations" },
    ],
  },
  data: {
    keys: [
      { label: "properties", type: "key", info: "the declared properties" },
      { label: "edges", type: "key", info: "a list of {rel, to: {kind, id}}" },
    ],
  },
}

const indentOf = (line: string): number => /^\s*/.exec(line)?.[0].length ?? 0

/** Where a line's CONTENT starts. A list item's `- ` is part of the indent as
 * far as nesting goes: `- model: x` and the `inputPer1M:` under it are
 * siblings inside the same item, not parent and child. */
function contentIndent(line: string): number {
  const m = /^(\s*)(- +)?/.exec(line)
  return (m?.[1].length ?? 0) + (m?.[2]?.length ?? 0)
}

/** The mapping keys the cursor line sits under, outermost first: the line
 * `    title: x` inside `data:` → `properties:` answers `["data",
 * "properties"]`. A list item (`- rel: x`) belongs to the key that opened the
 * list. */
export function pathAt(lines: string[], line: number): string[] {
  const path: string[] = []
  let want = contentIndent(lines[line] ?? "")
  for (let i = line - 1; i >= 0; i--) {
    const text = lines[i]
    if (!text.trim() || text.trim().startsWith("#")) continue
    const indent = contentIndent(text)
    if (indent >= want) continue
    const key = /^\s*(?:- +)?([\w.]+):/.exec(text)?.[1]
    if (key) path.unshift(key)
    want = indentOf(text)
    if (want === 0) break
  }
  return path
}

/** Whether this path addresses the declared properties block. */
function inProperties(path: string[]): boolean {
  return path.length === 2 && path[0] === "data" && path[1] === "properties"
}

/** The OBJECT property whose block this path sits inside, if any: the fields
 * of `data.properties.<name>` are completable exactly as properties are. */
function objectAt(
  path: string[],
  byName: Map<string, PropSpec>
): PropSpec | undefined {
  if (path.length !== 3 || path[0] !== "data" || path[1] !== "properties") {
    return undefined
  }
  const spec = byName.get(path[2])
  return spec?.fields?.length ? spec : undefined
}

function suggestValues(spec: PropSpec): Suggestion[] {
  if (spec.values?.length) {
    return spec.values.map((v) => ({
      label: v.value,
      detail: v.label || undefined,
      type: "value" as const,
    }))
  }
  if (spec.states?.length) {
    return spec.states.map((s) => ({
      label: s,
      detail: s === spec.initial ? "initial" : undefined,
      type: "value" as const,
    }))
  }
  if (controlFor(spec) === "bool") {
    return [
      { label: "true", type: "value" },
      { label: "false", type: "value" },
    ]
  }
  const example = exampleFor(spec)
  return example ? [{ label: example, detail: "example", type: "value" }] : []
}

function propertySuggestion(
  spec: PropSpec,
  taken: Set<string>
): Suggestion | null {
  if (taken.has(spec.name)) return null
  return {
    label: spec.name,
    detail: `${typeLabel(spec)}${spec.required ? ", required" : ""}`,
    info: spec.description,
    apply: `${spec.name}: `,
    type: "property",
  }
}

/** The property names already written in the document, so a completion never
 * offers a key twice. Read off the `data.properties` block by indentation, not
 * by parsing: the document is being typed and may not parse at all. */
export function writtenProperties(lines: string[]): Set<string> {
  const taken = new Set<string>()
  for (let i = 0; i < lines.length; i++) {
    if (!inProperties(pathAt(lines, i))) continue
    const key = /^\s*([\w.]+):/.exec(lines[i])?.[1]
    if (key) taken.add(key)
  }
  return taken
}

/** The keys the object ROW around `line` already writes. A repeated object is
 * a list of rows, so the row is bounded by its own `- ` item, not by the
 * property block. */
export function writtenFields(lines: string[], line: number): Set<string> {
  const taken = new Set<string>()
  const want = contentIndent(lines[line] ?? "")
  const bounds = (i: number) => {
    const text = lines[i]
    if (!text.trim() || text.trim().startsWith("#")) return "skip"
    if (contentIndent(text) < want) return "stop"
    if (contentIndent(text) > want) return "skip"
    // A new list item at this depth starts a new row.
    return /^\s*- +/.test(text) ? "edge" : "take"
  }
  const read = (i: number) => {
    const key = /^\s*(?:- +)?([\w.]+):/.exec(lines[i])?.[1]
    if (key) taken.add(key)
  }
  // A cursor on a `- ` line IS the start of its row: nothing above it belongs
  // to this row, so the walk only goes forward.
  const startsRow = /^\s*- +/.test(lines[line] ?? "")
  for (let i = line - 1; !startsRow && i >= 0; i--) {
    const at = bounds(i)
    if (at === "skip") continue
    if (at === "stop") break
    read(i)
    if (at === "edge") break
  }
  for (let i = line + 1; i < lines.length; i++) {
    const at = bounds(i)
    if (at === "skip") continue
    if (at === "stop" || at === "edge") break
    read(i)
  }
  return taken
}

/** What may be written at (line, column), or null where nothing useful can be
 * said. `column` is 0-based and the text before it on the line is what decides:
 * a partial key completes to a key, a partial value after `name:` completes to
 * that property's admitted values. */
export function completionsAt(
  text: string,
  line: number,
  column: number,
  kind: KindInfo
): Completions | null {
  const lines = text.split("\n")
  const current = lines[line] ?? ""
  const before = current.slice(0, column)

  const specs = [...propSpecs(kind), ...systemSpecs(kind)]
  const byName = new Map(specs.map((s) => [s.name, s]))
  const path = pathAt(lines, line)

  // A value position: `key: <partial>` (the key may itself be a list item).
  const value = /^(\s*)(?:- )?([\w.]+):[ \t]+(\S*)$/.exec(before)
  if (value) {
    const [, indent, key, partial] = value
    const from = column - partial.length
    if (key === "kind" && indent.length === 0) {
      return {
        from,
        options: [
          {
            label: kind.identity,
            detail: "this collection's kind",
            type: "value",
          },
        ],
      }
    }
    if (key === "rel") {
      const edges = (kind.definition ?? {}) as Record<string, unknown>
      const declared = (edges.edges ?? {}) as Record<
        string,
        { to?: string; description?: string }
      >
      const options = Object.entries(declared).map(([rel, def]) => ({
        label: rel,
        detail: def?.to ? `→ ${def.to}` : undefined,
        info: def?.description,
        type: "value" as const,
      }))
      return options.length ? { from, options } : null
    }
    const object = objectAt(path, byName)
    const spec = inProperties(path)
      ? byName.get(key)
      : object?.fields?.find((f) => f.name === key)
    if (!spec) return null
    const options = suggestValues(spec)
    return options.length ? { from, options } : null
  }

  // A key position: whitespace, then a partial word, and nothing else.
  const key = /^(\s*)(?:- )?([\w.]*)$/.exec(before)
  if (!key) return null
  const [, indent, partial] = key
  const from = column - partial.length

  if (inProperties(path)) {
    const taken = writtenProperties(lines)
    // The key being typed is not "already written" from its own point of view.
    taken.delete(partial)
    const options = specs
      .map((spec) => propertySuggestion(spec, taken))
      .filter((s): s is Suggestion => s !== null)
    return options.length ? { from, options } : null
  }

  // Inside an object property: its declared fields, minus the ones this row
  // already writes.
  const object = objectAt(path, byName)
  if (object?.fields) {
    const taken = writtenFields(lines, line)
    taken.delete(partial)
    const options = object.fields
      .map((spec) => propertySuggestion(spec, taken))
      .filter((s): s is Suggestion => s !== null)
    return options.length ? { from, options } : null
  }

  const envelope = ENVELOPE[path.join(".")]
  if (!envelope) return null
  // An envelope key is only offered where one belongs: at the depth of the
  // block it opens.
  if (path.length === 0 && indent.length > 0) return null
  return {
    from,
    options: envelope.keys.map((k) => ({ ...k, apply: `${k.label}: ` })),
  }
}
