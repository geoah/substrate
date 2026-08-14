/** Which spans of a rendered manifest line earn a schema hover, and which earn
 * a link (record refs, kinds, actors). Pure line analysis, shared by the YAML
 * view and its tests — plus the projection that turns a kind's declaration
 * into the docs those hovers read. */

import type { KindInfo } from "@/lib/api/types"
import {
  declaredEdges,
  declaredProperties,
  propertyTypeLabel,
  edgeTypeLabel,
} from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"
import type { PropSpec } from "@/lib/record-schema"

/** What a hover says about one declared key: its DATATYPE and the record-56
 * one-liner. Either alone is worth a hover — a key whose kind declares only a
 * type still answers "what is this?" (owner: "I cannot hover to see the
 * description of one of the properties"). */
export interface KeyDoc {
  /** The declared datatype as the schema spells it, `[]`-suffixed when the
   * property is repeated (`email[]`); an edge reads `→ organization`. */
  type?: string
  description?: string
}

export interface KeyDocs {
  /** Property name → its doc. */
  properties: Record<string, KeyDoc>
  /** Edge rel → its doc (edges render as `- rel: <name>` rows). */
  edges: Record<string, KeyDoc>
}

export const NO_DOCS: KeyDocs = { properties: {}, edges: {} }

/** The kind's declaration as the hover vocabulary: every declared property and
 * edge that says anything at all. Built from the SAME registry query the page
 * already holds — a hover never fetches (rule: one kinds query, reused). */
export function keyDocsOf(kind: KindInfo | undefined): KeyDocs {
  if (!kind) return NO_DOCS
  const docs: KeyDocs = { properties: {}, edges: {} }
  for (const prop of declaredProperties(kind)) {
    docs.properties[prop.name] = {
      type: propertyTypeLabel(prop),
      description: prop.description,
    }
  }
  for (const edge of declaredEdges(kind)) {
    docs.edges[edge.rel] = {
      type: edgeTypeLabel(edge),
      description: edge.description,
    }
  }
  return docs
}

/** What in this line deserves a schema hover: the key of an indented `name:`
 * row when the kind declares that property, or the value of a `rel:` row when
 * it declares that edge. Top-level envelope keys (kind, metadata, data…) never
 * match — properties sit at depth ≥ 2. */
export function describableSpan(
  line: string,
  docs: KeyDocs
): { text: string; doc: KeyDoc } | null {
  const m = line.match(/^(\s*)(?:- )?([\w.]+):(.*)$/)
  if (!m) return null
  const [, indent, key, rest] = m
  if (key === "rel") {
    const rel = rest.trim()
    const doc = docs.edges[rel]
    return doc ? { text: rel, doc } : null
  }
  if (indent.length < 4) return null
  const doc = docs.properties[key]
  return doc ? { text: key, doc } : null
}

/** Which DECLARED PROPERTY a line of an apply document is writing, or nothing.
 * The editor hovers a line with the whole declaration (datatype, one-liner,
 * required, admitted values, a worked example), so it needs the spec itself and
 * not just the two strings a manifest hover shows. Same depth rule as
 * `describableSpan`: envelope keys sit shallower than a property. */
export function specOnLine(
  line: string,
  specs: Map<string, PropSpec>
): PropSpec | undefined {
  const m = line.match(/^(\s*)(?:- )?([\w.]+):/)
  if (!m) return undefined
  const [, indent, key] = m
  if (indent.length < 4) return undefined
  return specs.get(key)
}

/** The known reference strings a manifest can carry, each mapped to its
 * console address. Built from the record and the registry — never guessed
 * over arbitrary text (LinkedYaml's discipline): only exact values in the
 * right key positions become links. */
export interface YamlLinkTargets {
  /** Exact reference ids (edge targets, canonicalId, formerIds) and reference
   * property values (the referent's whole `<kind>/<id>` path) → href. An id is
   * matched as the value of `id:`/`canonicalId:` rows and bare `- id` list
   * items — an id inside a title is a word, not a link; a PATH is matched
   * under whatever key the declaration gave the property. */
  ids: Record<string, string>
  /** Kind reference (`<authority>/<name>`) → that kind's browse page. Matched
   * only as the value of a `kind:` key — the top-level envelope `kind:` and an
   * edge target's `to.kind:`; a `calendar` in prose is not the kind. */
  kinds: Record<string, string>
}

export const NO_TARGETS: YamlLinkTargets = { ids: {}, kinds: {} }

/** What in this line is a reference the console can follow: the value of a
 * `kind:` / `id:` / `canonicalId:` row when the targets know it, a bare list
 * item that is a known id (formerIds), the value of a `manager:` / `actor:`
 * row (actor names always link — the actor view renders a stub over a real
 * changelog even for unregistered names), or a reference property's value,
 * which is a record PATH and therefore links under the property's own key. */
export function linkableSpan(
  line: string,
  targets: YamlLinkTargets
): { text: string; href: string } | null {
  const m = line.match(/^(\s*)(?:- )?([\w.]+):(.*)$/)
  if (m) {
    const [, indent, key, rest] = m
    const value = rest.trim()
    if (!value) return null
    const href =
      key === "kind"
        ? targets.kinds[value]
        : key === "id" || key === "canonicalId"
          ? targets.ids[value]
          : // manager/actor rows live in `status.properties` (depth ≥ 3);
            // the guard keeps a data property named `actor` from linking.
            (key === "manager" || key === "actor") && indent.length >= 6
            ? `/actors/${value}`
            : // A reference property is keyed by its declared name, so the key
              // cannot say what the value is: the record PATH does, and only a
              // path the targets already know links.
              splitRecordPath(value)
              ? targets.ids[value]
              : undefined
    return href ? { text: value, href } : null
  }
  // A bare list item (`- <id>`): the formerIds rows.
  const item = line.match(/^\s*- (\S+)\s*$/)
  if (item) {
    const href = targets.ids[item[1]]
    return href ? { text: item[1], href } : null
  }
  return null
}

/** Characters that make a neighbour part of the SAME word — a mark surrounded
 * by any of them is a coincidence inside a longer token, not the span. */
const WORDISH = /[\w.@/+-]/

/** The first standalone occurrence of `mark` in `text`, or -1. */
function markIndexOf(text: string, mark: string): number {
  for (let from = 0; from <= text.length - mark.length;) {
    const at = text.indexOf(mark, from)
    if (at < 0) return -1
    const before = text[at - 1]
    const after = text[at + mark.length]
    if (!(before && WORDISH.test(before)) && !(after && WORDISH.test(after))) {
      return at
    }
    from = at + 1
  }
  return -1
}

/** The raw line cut into runs around the annotated spans, each mark matched
 * ONCE and never re-cut. This is what lets an UNTINTED render (shiki still in
 * flight, or its chunk gone after a redeploy) wrap exactly the spans a tinted
 * one wraps: the hovers and links are the schema's, never the highlighter's. */
export function splitAround(
  line: string,
  marks: (string | undefined)[]
): string[] {
  const runs: { text: string; marked: boolean }[] = [
    { text: line, marked: false },
  ]
  for (const mark of marks) {
    if (!mark) continue
    for (let i = 0; i < runs.length; i++) {
      const run = runs[i]
      if (run.marked) continue
      const at = markIndexOf(run.text, mark)
      if (at < 0) continue
      const parts: { text: string; marked: boolean }[] = []
      if (at > 0) parts.push({ text: run.text.slice(0, at), marked: false })
      parts.push({ text: mark, marked: true })
      const rest = run.text.slice(at + mark.length)
      if (rest) parts.push({ text: rest, marked: false })
      runs.splice(i, 1, ...parts)
      break
    }
  }
  return runs.map((r) => r.text)
}
