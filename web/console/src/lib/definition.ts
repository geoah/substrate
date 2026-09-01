/** What the console derives from a kind's reconciled `definition`:
 * columns for the generic table, filterable fields, temporal (hot) columns
 * bound by traits, state machines, and the record-56 description one-liners
 * that feed every hover. Key order in `definition` is lost to jsonb, so
 * declared names sort alphabetically — stable and honest. */

import { splitKind } from "@/lib/api/http"
import { parseEnumValues, type EnumValue, type KindInfo } from "@/lib/api/types"

/** A kind reference split into `{authority, name}`: `people.substrate.reamde.dev/person`
 * → `{authority: "people.substrate.reamde.dev", name: "person"}`; a bare `task` →
 * `{authority: "", name: "task"}`. Re-exported from the addressing layer so a
 * page has one import for the grammar. */
export { splitKind }

export interface DeclaredProperty {
  name: string
  /** The declared datatype (`string`, `email`, `state`, …); an
   * authority-local datatype name passes through and is treated as a string. */
  kind: string
  description?: string
  repeated: boolean
  /** `state`-datatype only: the machine's states, declaration order. */
  states?: string[]
  initial?: string
  /** The declaration marked the property `required` — a presentational hint
   * the read surfaces consume verbatim (load.go's propKeys). Optional on the
   * interface so the hand-built field sets (merge requests, the changelog
   * filters) stay literal. */
  required?: boolean
  /** An enum's admitted set, declaration order — absent on every other kind. */
  values?: EnumValue[]
  /** `reference`-datatype only: the referent kind this reference is pinned to.
   * Absent on an UNPINNED reference, whose value carries the kind. */
  to?: string
  /** `reference`: the target must exist at write time. */
  mustExist?: boolean
  /** `reference`: `cascade` collects this record when the target dies. Absent
   * means detach — the value stays and dangles after a purge. */
  onDelete?: string
  /** `reference`: this property is a record mapping's SUBJECT. */
  subject?: boolean
  /** `reference`: the LINK DATA the declaration hangs off the pointer, by
   * property name. A reference declaring these stores `{ref, <prop>: <val>}`
   * instead of the flat path string. */
  linkProperties?: string[]
  /** What this pointer is called from the OTHER side, where the declaration
   * named it — `thread` on a message is `messages` on the thread. A label the
   * graph reads, never an identifier. */
  inverse?: string
  inverseDescription?: string
}

/** How a property's datatype reads wherever the schema is shown — a hover, the
 * definition table: the declared spelling, `[]`-suffixed when repeated, and a
 * typed pointer naming what it points at (`reference → person`). */
export function propertyTypeLabel(p: DeclaredProperty): string {
  const base = p.to ? `${p.kind} → ${p.to}` : p.kind
  return p.repeated ? `${base}[]` : base
}

type Definition = Record<string, unknown>

function definitionOf(k: KindInfo): Definition {
  return (k.definition ?? {}) as Definition
}

export function declaredProperties(k: KindInfo): DeclaredProperty[] {
  const props = (definitionOf(k).properties ?? {}) as Record<
    string,
    Record<string, unknown>
  >
  return Object.entries(props)
    .map(([name, def]) => ({
      name,
      kind: typeof def.type === "string" ? def.type : "string",
      description:
        typeof def.description === "string" ? def.description : undefined,
      repeated: def.repeated === true,
      states: Array.isArray(def.states)
        ? def.states.filter((s): s is string => typeof s === "string")
        : undefined,
      initial: typeof def.initial === "string" ? def.initial : undefined,
      required: def.required === true,
      values: parseEnumValues(def.values),
      // A reference pins its target under `kind:`, the one spelling the loader
      // accepts.
      to: typeof def.kind === "string" ? def.kind : undefined,
      mustExist: def.mustExist === true,
      onDelete: typeof def.onDelete === "string" ? def.onDelete : undefined,
      subject: def.subject === true,
      linkProperties: linkPropertyNames(def.properties),
      inverse: typeof def.inverse === "string" ? def.inverse : undefined,
      inverseDescription:
        typeof def.inverseDescription === "string"
          ? def.inverseDescription
          : undefined,
    }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** The LINK DATA a reference declaration hangs off the pointer, by name. The
 * block is flat and single-valued by grammar, so the names are all a read
 * surface needs to say what rides along. */
function linkPropertyNames(raw: unknown): string[] | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined
  const names = Object.keys(raw as Record<string, unknown>).sort()
  return names.length ? names : undefined
}

/** The hot columns a kind's traits bind: `temporal(point)` → `at`,
 * `temporal(range)` → `at` + `endsAt`, and a remap like
 * `temporal(point: dueAt)` moves the point onto `dueAt`. */
export function temporalProperties(k: KindInfo): string[] {
  const traits = definitionOf(k).traits
  if (!Array.isArray(traits)) return []
  const out: string[] = []
  for (const trait of traits) {
    if (typeof trait !== "string") continue
    const m = trait.match(/^temporal\(\s*(point|range)(?:\s*:\s*(\w+))?\s*\)$/)
    if (!m) continue
    if (m[1] === "range") out.push("at", "endsAt")
    else out.push(m[2] ?? "at")
  }
  return [...new Set(out)]
}

export function stateProperties(k: KindInfo): DeclaredProperty[] {
  return declaredProperties(k).filter((p) => p.kind === "state")
}

/** Datatypes that never earn a table column or a filter control: opaque blobs
 * and values the server refuses to compare. */
const OPAQUE_KINDS = new Set(["json", "secret", "digest", "object"])

/** Datatypes whose values are paragraphs, not cells. They stay filterable
 * (contains/eq work server-side) but make bad columns. */
const LONG_KINDS = new Set(["text", "markdown"])

/** The declared properties that become table columns, schema casing intact. */
export function columnProperties(k: KindInfo): DeclaredProperty[] {
  const temporal = new Set(temporalProperties(k))
  return declaredProperties(k).filter(
    (p) =>
      !OPAQUE_KINDS.has(p.kind) &&
      !LONG_KINDS.has(p.kind) &&
      // title is a system column already; hot columns render as temporal.
      p.name !== "title" &&
      !temporal.has(p.name)
  )
}

/** The fields the filter builder offers — ONLY declared properties the server
 * will actually filter (secret refuses, json/object have no comparison). */
export function filterableProperties(k: KindInfo): DeclaredProperty[] {
  return declaredProperties(k).filter((p) => !OPAQUE_KINDS.has(p.kind))
}

/** The record-56 one-liner for a declared property, feeding every hover:
 * column headers, YAML keys, filter builders. */
export function describeKey(k: KindInfo, key: string): string | undefined {
  return declaredProperties(k).find((p) => p.name === key)?.description
}

export function kindByIdentity(
  kinds: KindInfo[],
  identity: string
): KindInfo | undefined {
  return kinds.find((k) => k.identity === identity)
}

export function kindByCollection(
  kinds: KindInfo[],
  authority: string,
  name: string
): KindInfo | undefined {
  return kinds.find((k) => k.authority === authority && k.name === name)
}

/** Resolve a reference declaration's `kind:` pin. A bare singular (`person`)
 * resolves inside the declaring kind's authority first, then anywhere it is
 * unambiguous; a full kind reference (with a `/`) resolves directly. */
export function resolveReferenceTarget(
  kinds: KindInfo[],
  from: KindInfo,
  to: string
): KindInfo | undefined {
  if (!to) return undefined
  if (to.includes("/")) return kindByIdentity(kinds, to)
  const sameAuthority = kinds.find(
    (k) => k.authority === from.authority && k.name === to
  )
  if (sameAuthority) return sameAuthority
  const named = kinds.filter((k) => k.name === to)
  return named.length === 1 ? named[0] : undefined
}

/** The GraphQL type name a kind reference maps to, mirroring the server's
 * `GraphQLName`. A shipped authority's kind is its name PascalCased
 * (`people.substrate.reamde.dev/person` → `Person`); an INSTALLED bundle kind is
 * prefixed with the bundle's name and an underscore
 * (`google.bundles.substrate.reamde.dev/person` → `Google_Person`), because installed
 * kinds share names across bundles and the prefix disambiguates. A bare
 * shorthand name (no authority) PascalCases the same way the server does. */
export function graphqlTypeName(ref: string): string {
  const { authority, name } = splitKind(ref)
  const base = pascal(name)
  if (!authority) return base
  const labels = authority.split(".")
  // `<bundle>.bundles.<domain>` — an installed bundle's owned authority.
  if (labels.length > 1 && labels[1] === "bundles" && labels[0]) {
    return `${pascal(labels[0])}_${base}`
  }
  return base
}

function pascal(word: string): string {
  return word ? word.charAt(0).toUpperCase() + word.slice(1) : word
}

/** The kind's `reference`-typed properties: every named, directed link it
 * declares, and the ONE thing that points at another record. */
export function declaredReferences(k: KindInfo): DeclaredProperty[] {
  return declaredProperties(k).filter((p) => p.kind === "reference")
}

/** What the far side of an incoming reference is CALLED here. The name a
 * fan-in row carries is written from the source's side (`thread`, on a
 * message), which reads backwards standing on the target — so the label is
 * the declaration's `inverse` where its author wrote one, and an honest
 * fallback naming both sides where nobody did. */
export function inverseLabel(
  kinds: KindInfo[],
  fromKind: string,
  property: string
): { label: string; description?: string } {
  const source = kindByIdentity(kinds, fromKind)
  const declared = source
    ? declaredReferences(source).find((p) => p.name === property)
    : undefined
  if (declared?.inverse) {
    return { label: declared.inverse, description: declared.inverseDescription }
  }
  return {
    label: `${property} of ${splitKind(fromKind).name}`,
    description: declared?.description,
  }
}
