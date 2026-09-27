/** What a record is MADE OF, in words a reader has: the pure half of the
 * record page's ownership chips and its "Where it comes from" section.
 *
 * The wire says three things about where a record's values came from, none of
 * them in a reader's vocabulary. `linkedFrom` is a flat list of source
 * records each naming the mapping that owns its subject slot (decision 0088).
 * `propertyMeta` names each property's manager as an ACTOR string
 * (`function:providers.substrate.reamde.dev:beeper:beepersync`) and, since
 * decision 0094, the SOURCE RECORD behind the value and behind every
 * alternative. And a `recordmapping` declaration says which properties its
 * `map` rules write. This module folds the three into the shapes the tab
 * renders — sources grouped by the mapping that made them, a value's holder as
 * the thing it is — and it does so without React, so the grouping, the dedupe and
 * the vocabulary are tested as functions. */

import { actorIdentity, type ActorIdentity } from "@/lib/actor-identity"
import { CORE_PACKAGE, splitKind } from "@/lib/api/http"
import {
  readReference,
  type LinkedRecord,
  type PropertyMeta,
  type SubstrateRecord,
} from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { displayName } from "@/lib/kind-names"
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

/** A mapping in a reader's words: its declared title, else the two kinds it
 * joins ("Contact → Person"); its id is for technical mode. */
export function mappingLabel(
  mapping: string,
  mappings: readonly SubstrateRecord[],
  from: string,
  to: string
): string {
  const decl = mappings.find((m) => m.id === mapping)
  const title = decl && recordTitle(decl.properties)
  return title || `${displayName(from)} → ${displayName(to)}`
}

// ── who holds a value ────────────────────────────────────────────────────────

export type Tier = "owner" | "bundle" | "machine"

/** The holder of one property, as the ownership chip says it. */
export interface Holder {
  identity: ActorIdentity
  /** `you`: the owner's own hand; `provider`: a provider's function or
   * bundle; `actor`: anything else (an agent, a tool, the substrate). */
  mark: "you" | "provider" | "actor"
  /** The chip's words: "You", "Google", "substrate". */
  label: string
}

export function holderOf(meta: PropertyMeta): Holder | undefined {
  if (!meta.manager) return undefined
  const identity = actorIdentity(meta.manager)
  if (identity.cls === "you") return { identity, mark: "you", label: "You" }
  if (identity.provider) {
    return { identity, mark: "provider", label: identity.provider.name }
  }
  return { identity, mark: "actor", label: identity.name }
}

/** Whether a value's holding is worth a chip on its row. The record page
 * states the default once, in its meta line (the owner's own hand), so a row
 * speaks only where it departs from that: a provider, an agent or a tool
 * holds it, or a live source offers something else. */
export function departsFromDefault(meta: PropertyMeta | undefined): boolean {
  if (!meta?.manager) return false
  if ((meta.alternatives ?? []).length) return true
  return holderOf(meta)?.mark !== "you"
}

function holds(value: unknown): boolean {
  if (value === undefined || value === null || value === "") return false
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === "object") return Object.keys(value).length > 0
  return true
}

/** Whether every value the record holds is the owner's own, no source
 * disagreeing: the one sentence that replaces a "You" chip on every row. A
 * record whose holders the server never stamped claims nothing. */
export function everyValueYours(record: SubstrateRecord): boolean {
  let seen = false
  for (const [name, meta] of Object.entries(record.propertyMeta ?? {})) {
    if (!meta.manager || !holds(record.properties[name])) continue
    if (departsFromDefault(meta)) return false
    seen = true
  }
  return seen
}

/** A source's name in a sentence: its provider where it has one ("Google"),
 * else the actor's plain name. */
export function sourceName(actor: string): string {
  const identity = actorIdentity(actor)
  return identity.provider?.name ?? identity.name
}

/** The amber pill's words when live sources offer something else: the one
 * provider by name, or how many sources disagree. */
export function differsLabel(meta: PropertyMeta): string | undefined {
  const alts = meta.alternatives ?? []
  if (!alts.length) return undefined
  const names = [...new Set(alts.map((a) => sourceName(a.actor)))]
  return names.length === 1
    ? `${names[0]} differs`
    : `${names.length} sources differ`
}

/** The tier in the reader's words. A bundle-tier value an agent wrote is the
 * agent's, not a provider's. */
export function tierLabel(tier: Tier | undefined, actor?: string): string {
  switch (tier) {
    case "owner":
      return "Yours"
    case "machine":
      return "Synced"
    case "bundle":
      return actor && actorIdentity(actor).cls === "agent"
        ? "Set by an agent"
        : "Set by provider"
    default:
      return "Written"
  }
}

/** What the tier means for this value, one sentence. */
export function tierExplanation(tier: Tier | undefined): string {
  switch (tier) {
    case "owner":
      return "Syncs keep their own version but never change yours."
    case "machine":
      return "Kept up to date by a provider. If you edit it, your version sticks."
    case "bundle":
      return "Set for you, and syncs won’t move it. Your own edit replaces it."
    default:
      return "Written before substrate kept track of who holds each value."
  }
}

/** Where each item of a list value comes from: the item, and the sources
 * whose offer carries it (the holder's own source first). Best effort: a
 * source's offer is what the record keeps of it, so an item only the stored
 * value carries has no source to name. Empty unless the value is a list and
 * some source offers a list. */
export function unionMembers(
  value: unknown,
  meta: PropertyMeta
): Array<{ item: unknown; sources: string[] }> {
  if (!Array.isArray(value)) return []
  const offers: Array<{ source?: string; items: unknown[] }> = []
  for (const alt of meta.alternatives ?? []) {
    if (Array.isArray(alt.value))
      offers.push({ source: alt.source, items: alt.value })
  }
  if (!offers.length) return []
  const same = (a: unknown, b: unknown) =>
    JSON.stringify(a) === JSON.stringify(b)
  return value.map((item) => {
    const sources: string[] = []
    if (meta.tier === "machine" && meta.source) sources.push(meta.source)
    for (const offer of offers) {
      if (!offer.source || sources.includes(offer.source)) continue
      if (offer.items.some((x) => same(x, item))) sources.push(offer.source)
    }
    return { item, sources }
  })
}

/** When a source record last filled anything in here: the newest stamp of
 * the properties it backs or offers. */
export function syncedAt(
  record: SubstrateRecord,
  source: string
): string | undefined {
  let latest: string | undefined
  const see = (at?: string) => {
    if (at && (!latest || at > latest)) latest = at
  }
  for (const meta of Object.values(record.propertyMeta ?? {})) {
    if (meta.source === source) see(meta.updatedAt)
    for (const alt of meta.alternatives ?? []) {
      if (alt.source === source) see(alt.updatedAt)
    }
  }
  return latest
}
