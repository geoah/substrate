/** What a record is MADE OF, in words a reader has: the pure half of the
 * Provenance tab.
 *
 * The wire says three things about where a record's values came from, none of
 * them in a reader's vocabulary. `linkedFrom` is a flat list of source
 * records each naming the mapping that owns its subject slot (decision 0088).
 * `propertyMeta` names each property's manager as an ACTOR string
 * (`function:providers.substrate.reamde.dev:beeper:beepersync`) and, since
 * decision 0094, the SOURCE RECORD behind the value and behind every
 * alternative. And a `recordmapping` declaration says which properties its
 * `map` rules write. This module folds the three into the shapes the tab
 * renders — sources grouped by the mapping that made them, an actor as the
 * thing it is — and it does so without React, so the grouping, the dedupe and
 * the vocabulary are tested as functions. */

import { CORE_PACKAGE, splitKind } from "@/lib/api/http"
import {
  readReference,
  type LinkedRecord,
  type SubstrateRecord,
} from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { splitRecordPath } from "@/lib/record-path"

// ── sources, grouped by mapping ──────────────────────────────────────────────

/** One source record, as the Sources section lists it. */
export interface SourceMember {
  /** The record path, `<kind>/<id>`: the dedupe key. */
  ref: string
  kind: string
  id: string
  /** The source's own title; the id stands in where it has none. */
  title?: string
}

/** Every source one mapping brought here. */
export interface SourceGroup {
  /** The mapping's identity, which is its record id. */
  mapping: string
  /** The mapping's title, or its local name until the declaration loads. */
  title: string
  description?: string
  /** The source kind the mapping reads: the declaration's `from`, else the
   * kind the links themselves carry. */
  from: string
  /** The subject slot on the source kind. */
  property: string
  /** The subject properties the mapping's `map` rules write, in the
   * declaration's order; empty for a link-only mapping. */
  contributes: string[]
  /** Deduplicated by record, sorted by title then id. */
  members: SourceMember[]
  /** Whether the mapping's declaration was found: a group whose mapping is
   * gone still lists its sources, because the links are what the record
   * carries, and says the declaration is missing. */
  declared: boolean
}

/** A kind reference held as a `core/kind` record reference (`{ref:
 * "substrate.reamde.dev/core/kind/<identity>"}`), or as the bare identity. */
export function mappedKind(value: unknown): string | undefined {
  const path = readReference(value)?.path
  const prefix = `${CORE_PACKAGE}/kind/`
  return path?.startsWith(prefix) ? path.slice(prefix.length) : path
}

/** The subject properties a mapping writes: the keys of its `map`. The order
 * is the declaration's, which is what the server serves. */
export function contributesOf(mapping: SubstrateRecord | undefined): string[] {
  const rules = mapping?.properties.map
  if (typeof rules !== "object" || rules === null || Array.isArray(rules))
    return []
  return Object.keys(rules)
}

const byTitle = (a: { title?: string; id: string }, b: typeof a) =>
  (a.title || a.id).localeCompare(b.title || b.id) || a.id.localeCompare(b.id)

/** Fold a record's `linkedFrom` into one group per mapping.
 *
 * Eight Beeper users converging on one person are eight ENTRIES that share a
 * mapping; a reader wants the mapping once, with its count, and the eight
 * beneath it. A source reaching the record under two mappings (two subject
 * slots, decision 0049) is a member of both groups, and a source the index
 * kept two rows for is one member: the record path is the key. */
export function groupSources(
  links: readonly LinkedRecord[],
  mappings: readonly SubstrateRecord[]
): SourceGroup[] {
  const declared = new Map(mappings.map((m) => [m.id, m]))
  const groups = new Map<string, SourceGroup>()
  for (const link of links) {
    let group = groups.get(link.mapping)
    if (!group) {
      const decl = declared.get(link.mapping)
      group = {
        mapping: link.mapping,
        title:
          (decl && recordTitle(decl.properties)) ||
          splitKind(link.mapping).name ||
          link.mapping,
        description:
          typeof decl?.properties.description === "string"
            ? decl.properties.description
            : undefined,
        from: (decl && mappedKind(decl.properties.from)) || link.kind,
        property: link.property,
        contributes: contributesOf(decl),
        members: [],
        declared: Boolean(decl),
      }
      groups.set(link.mapping, group)
    }
    if (group.members.some((m) => m.ref === link.ref)) continue
    const target = splitRecordPath(link.ref)
    group.members.push({
      ref: link.ref,
      kind: link.kind,
      id: target?.id ?? link.ref,
      title: link.title || undefined,
    })
  }
  const out = [...groups.values()]
  for (const group of out) group.members.sort(byTitle)
  return out.sort(
    (a, b) =>
      a.title.localeCompare(b.title) || a.mapping.localeCompare(b.mapping)
  )
}

/** Record path → the source's title, off the links the record already
 * carries: the ledger's source pills name records that are, by construction,
 * among the record's live sources, so no second read is needed for them. */
export function sourceTitles(
  links: readonly LinkedRecord[]
): ReadonlyMap<string, string> {
  const out = new Map<string, string>()
  for (const link of links) if (link.title) out.set(link.ref, link.title)
  return out
}

/** The mapping that brought one source record here, off the same links. */
export function mappingOfSource(
  links: readonly LinkedRecord[],
  source: string
): string | undefined {
  return links.find((l) => l.ref === source)?.mapping
}

// ── actors, as the things they are ──────────────────────────────────────────

/** An actor string read by its grammar (decision 0025): the substrate's own
 * hands are `bundle:<authority>:<package>`,
 * `function:<authority>:<package>:<name>` and `agent:…`, derived by the
 * engine; `substrate` is the engine itself; anything else is a name a request
 * asserted (`console`, `api`, `substratectl`, or whatever a script sent). */
export interface ActorWords {
  kind: "function" | "agent" | "bundle" | "engine" | "plain"
  /** What the pill says. */
  label: string
  /** The full actor string, for the hover: identity is never shortened away,
   * only moved off the line. */
  actor: string
  /** The declaration record the pill links to, where the actor has one. */
  record?: { kind: string; id: string }
}

/** Render an actor in words. `sourceKind` is the kind of the source record
 * the value came from, when the read named one: a function actor whose value
 * arrived through a mapping reads as "sync of <kind>", because that is what
 * the reader is looking at — a connector's mirror of a user, synced — and
 * the function's own name is one hover away and one click away. */
export function actorWords(actor: string, sourceKind?: string): ActorWords {
  if (actor === "substrate") {
    return { kind: "engine", label: "Engine", actor }
  }
  const parts = actor.split(":")
  const [head, authority, pkg, name] = parts
  if (head === "function" && parts.length === 4) {
    const label = sourceKind
      ? `sync of ${splitKind(sourceKind).name || sourceKind}`
      : `function ${name}`
    return {
      kind: "function",
      label,
      actor,
      record: {
        kind: `${CORE_PACKAGE}/function`,
        id: `${authority}/${pkg}/${name}`,
      },
    }
  }
  if (head === "agent" && parts.length === 4) {
    return {
      kind: "agent",
      label: `agent ${name}`,
      actor,
      record: {
        kind: `${CORE_PACKAGE}/agent`,
        id: `${authority}/${pkg}/${name}`,
      },
    }
  }
  if (head === "bundle" && parts.length === 3) {
    return {
      kind: "bundle",
      label: `bundle ${pkg}`,
      actor,
      record: { kind: `${CORE_PACKAGE}/bundle`, id: `${authority}/${pkg}` },
    }
  }
  return { kind: "plain", label: actor, actor }
}

// ── the words on the page ───────────────────────────────────────────────────

/** The vocabulary the tab explains on hover, in the words of docs/terms.md
 * § Truth and derivation and docs/projection.md § Managed properties. */
export const WORDS = {
  manager:
    "Every accepted write records its actor as the property's manager: who holds the current value. Attribution, never authorization.",
  tier: "A property manager's standing against recompute: owner > bundle > machine. Recompute overwrites only machine-held properties.",
  source:
    "The live source record the value was read from, through the mapping that links it here.",
  alternative:
    "A live source's value that differs from the stored one. The property keeps what its manager wrote until it is released; adopting an alternative is writing it.",
} as const

export type Tier = "owner" | "bundle" | "machine"

/** What a tier means for THIS value, said on the chip. */
export function tierWords(tier: Tier | undefined): {
  label: string
  detail: string
} {
  switch (tier) {
    case "owner":
      return {
        label: "held by you",
        detail:
          "You wrote this value. Recompute leaves it alone and records what the sources say as alternatives, until you release it.",
      }
    case "bundle":
      return {
        label: "pinned by a bundle",
        detail:
          "A function or agent wrote this value through its dispatch. Recompute yields to it exactly as to your own edit; a release lets it go the same way.",
      }
    case "machine":
      return {
        label: "follows sources",
        detail:
          "The sync machinery holds this value: the latest live source wins, or the union of every source lands, and a source change replaces it.",
      }
    default:
      return {
        label: "no tier",
        detail: "Written before tiers were recorded.",
      }
  }
}
