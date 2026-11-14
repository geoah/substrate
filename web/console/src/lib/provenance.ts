/** What a record is MADE OF, in words a reader has: the pure half of the
 * record page's ownership chips and its "Where it comes from" section.
 *
 * The wire says three things about where a record's values came from, none of
 * them in a reader's vocabulary. `linkedFrom` is a flat list of source
 * records each naming the mapping that owns its subject slot (decision 0088).
 * `marks` says, per property, whether sync may replace the value, the actor
 * record that set it (decision 0270), read here through the actor
 * directory, and the SOURCE RECORDS behind the value and behind every
 * alternative (decisions 0094, 0284). And a `recordmapping` declaration says
 * which properties its `map` rules write. This module folds the three into the shapes the tab
 * renders — sources grouped by the mapping that made them, a value's holder as
 * the thing it is — and it does so without React, so the grouping, the dedupe and
 * the vocabulary are tested as functions. */

import {
  actorIdentity,
  providerOfKind,
  type ActorIdentity,
} from "@/lib/actor-identity"
import { actorRefID } from "@/lib/api/actors"
import { CORE_PACKAGE, splitKind } from "@/lib/api/http"
import {
  readReference,
  type LinkedRecord,
  type PropertyAlternative,
  type PropertyMark,
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

/** How a value stands against sync, in the words the chip and the detail
 * use: the person's own pin, a pin an agent, a provider's function or an app
 * left, a pin no hand the console knows left, a value sync keeps up to date,
 * one written without a pin, and one no write marked. */
export type Standing =
  | "yours"
  | "agent"
  | "provider"
  | "app"
  | "pinned"
  | "synced"
  | "unpinned"
  | "none"

/** The id of the actor record a mark names as `setBy`. */
export function setByOf(mark: PropertyMark | undefined): string | undefined {
  return actorRefID(mark?.setBy)
}

/** How one value stands, from its mark. */
export function standingOf(mark: PropertyMark | undefined): Standing {
  if (!mark) return "none"
  const by = setByOf(mark)
  if (!mark.pinned)
    return (mark.derivedFrom ?? []).length || !by ? "synced" : "unpinned"
  if (!by) return "pinned"
  const identity = actorIdentity(by)
  switch (identity.cls) {
    case "you":
      return "yours"
    case "agent":
      return "agent"
    case "app":
      return "app"
    case "function":
    case "bundle":
      return identity.provider ? "provider" : "pinned"
  }
  return "pinned"
}

/** A source record in a sentence: its provider where a shipped one publishes
 * its kind ("Google"), else its kind's name. */
export function sourceName(path: string): string {
  const kind = splitRecordPath(path)?.kind ?? path
  return providerOfKind(kind)?.name ?? displayName(kind)
}

/** The source record an alternative is read from, "" where the read's grant
 * hides it. */
export function offerSource(alt: PropertyAlternative): string {
  return alt.derivedFrom?.ref ?? ""
}

/** The holder of one property, as the ownership chip says it. */
export interface Holder {
  identity: ActorIdentity
  /** `you`: the person's own hand; `provider`: a provider's function, or the
   * provider a synced value came from; `actor`: anything else (an agent, an
   * app, a tool, the substrate). */
  mark: "you" | "provider" | "actor"
  /** The chip's words: "You", "Google", "substrate". */
  label: string
}

/** Who holds a value: the hand that set it, else the provider (or the kind)
 * of the first source record it was derived from. */
export function holderOf(mark: PropertyMark): Holder | undefined {
  const by = setByOf(mark)
  if (by) {
    const identity = actorIdentity(by)
    if (identity.cls === "you") return { identity, mark: "you", label: "You" }
    if (identity.provider) {
      return { identity, mark: "provider", label: identity.provider.name }
    }
    return { identity, mark: "actor", label: identity.name }
  }
  const source = mark.derivedFrom?.[0]?.ref
  if (!source) return undefined
  const kind = splitRecordPath(source)?.kind ?? source
  const provider = providerOfKind(kind)
  const name = provider?.name ?? displayName(kind)
  const identity: ActorIdentity = {
    actor: "",
    cls: "bundle",
    name,
    provider,
    description: `Kept up to date from ${name}.`,
  }
  return { identity, mark: provider ? "provider" : "actor", label: name }
}

/** Whether a value carries a mark that names who or what holds it: a hand
 * that set it, or a source record it came from. */
export function marked(mark: PropertyMark | undefined): boolean {
  return Boolean(mark && holderOf(mark))
}

/** Whether a value's holding is worth a chip on its row. The record page
 * states the default once, in its meta line (the person's own hand), so a row
 * speaks only where it departs from that: a provider, an agent or a tool
 * holds it, a live source offers something else, or a source backs a value a
 * pin keeps from sync, which the row's detail offers to release. */
export function departsFromDefault(mark: PropertyMark | undefined): boolean {
  if (!mark || !holderOf(mark)) return false
  if ((mark.alternatives ?? []).length) return true
  if (mark.pinned && (mark.derivedFrom ?? []).length) return true
  return holderOf(mark)?.mark !== "you"
}

function holds(value: unknown): boolean {
  if (value === undefined || value === null || value === "") return false
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === "object") return Object.keys(value).length > 0
  return true
}

/** Whether every value the record holds is the person's own, no source
 * disagreeing: the one sentence that replaces a "You" chip on every row. A
 * record no write marked claims nothing. */
export function everyValueYours(record: SubstrateRecord): boolean {
  let seen = false
  for (const [name, mark] of Object.entries(record.marks ?? {})) {
    if (!holderOf(mark) || !holds(record.properties[name])) continue
    if (departsFromDefault(mark)) return false
    seen = true
  }
  return seen
}

/** A record an adapter wrote: one naming the provider record it was built
 * from in core's `origin`, or the provider records a packed period was
 * built from in `origins`. The owner decided these are read only: a
 * correction is the owner's own record from the owner's own source, which
 * that source's `priorities` rank above the provider's. */
export interface AdapterOutput {
  /** Who wrote it, in a sentence: the function managing its values, else
   * the provider its origin record belongs to, else the origin's kind. */
  writer: string
  /** The installed code that manages its values, where one does. */
  actor?: string
  /** The provider record it was built from, a record path: the first of a
   * packed period's `origins`. */
  origin: string
}

export function adapterOutput(
  record: SubstrateRecord
): AdapterOutput | undefined {
  const origins = record.properties?.origins
  const origin =
    readReference(record.properties?.origin)?.path ??
    (Array.isArray(origins)
      ? origins
          .map((o) => readReference(o)?.path)
          .find((p): p is string => Boolean(p))
      : undefined)
  if (!origin) return undefined
  const actor = Object.values(record.marks ?? {})
    .map(setByOf)
    .find((m) => m !== undefined && actorIdentity(m).cls === "function")
  const originKind = splitRecordPath(origin)?.kind
  const writer = actor
    ? (actorIdentity(actor).provider?.name ?? actorIdentity(actor).name)
    : originKind
      ? (providerOfKind(originKind)?.name ?? displayName(originKind))
      : origin
  return { writer, ...(actor ? { actor } : {}), origin }
}

/** Why the console offers no edit of a record. */
export type ReadOnlyReason = "provider" | "adapter"

/** Whether the console offers no edit of a record, and why: a provider's
 * copy, which the owner changes upstream, or an adapter's output, which the
 * owner corrects with a record of their own. Every door to an edit asks
 * this, the record page's sheet and header, its YAML source and the edit
 * route alike. */
export function readOnlyReason(
  record: SubstrateRecord
): ReadOnlyReason | undefined {
  if (providerOfKind(record.kind)) return "provider"
  if (adapterOutput(record)) return "adapter"
  return undefined
}

/** The amber pill's words when live sources offer something else: the one
 * provider by name, or how many sources disagree. */
export function differsLabel(mark: PropertyMark): string | undefined {
  const alts = mark.alternatives ?? []
  if (!alts.length) return undefined
  const names = [...new Set(alts.map((a) => sourceName(offerSource(a))))]
  return names.length === 1
    ? `${names[0]} differs`
    : `${alts.length} sources differ`
}

/** The standing in the reader's words. */
export function standingLabel(standing: Standing): string {
  switch (standing) {
    case "yours":
      return "Yours"
    case "agent":
      return "Set by an agent"
    case "provider":
      return "Set by provider"
    case "app":
      return "Set by an app"
    case "pinned":
      return "Pinned"
    case "synced":
      return "Synced"
    case "unpinned":
      return "Not pinned"
    default:
      return "Written"
  }
}

/** What the standing means for this value, one sentence. */
export function standingExplanation(standing: Standing): string {
  switch (standing) {
    case "yours":
      return "Syncs keep their own version but never change yours."
    case "agent":
    case "provider":
    case "app":
    case "pinned":
      return "Set for you, and syncs won’t move it. Your own edit replaces it."
    case "synced":
      return "Kept up to date by sync. Keep this value to stop that, or edit it and your version sticks."
    case "unpinned":
      return "Written without a pin, so a sync may replace it. Keep this value to stop that."
    default:
      return "Written before substrate kept track of where each value came from."
  }
}

/** Where each item of a list value comes from: the item, and the source
 * records whose offer carries it (the one the value came from first). Best
 * effort: an offer the stored value already holds is no alternative, so an
 * item only such a source carries is named only where the value came from
 * one source alone. Empty unless the value is a list and some source offers
 * a list. */
export function unionMembers(
  value: unknown,
  mark: PropertyMark
): Array<{ item: unknown; sources: string[] }> {
  if (!Array.isArray(value)) return []
  const offers: Array<{ source: string; items: unknown[] }> = []
  for (const alt of mark.alternatives ?? []) {
    if (Array.isArray(alt.value))
      offers.push({ source: offerSource(alt), items: alt.value })
  }
  if (!offers.length) return []
  const same = (a: unknown, b: unknown) =>
    JSON.stringify(a) === JSON.stringify(b)
  const only = !mark.pinned && mark.derivedFrom?.length === 1
  return value.map((item: unknown) => {
    const sources: string[] = []
    if (only) sources.push(mark.derivedFrom![0].ref)
    for (const offer of offers) {
      if (!offer.source || sources.includes(offer.source)) continue
      if (offer.items.some((x) => same(x, item))) sources.push(offer.source)
    }
    return { item, sources }
  })
}

/** When a source record last filled anything in here: the newest time of
 * the values it backs, and of the offers it makes. */
export function syncedAt(
  record: SubstrateRecord,
  source: string
): string | undefined {
  let latest: string | undefined
  const see = (at?: string) => {
    if (at && (!latest || at > latest)) latest = at
  }
  for (const mark of Object.values(record.marks ?? {})) {
    if ((mark.derivedFrom ?? []).some((l) => l.ref === source)) see(mark.setAt)
    for (const alt of mark.alternatives ?? []) {
      if (offerSource(alt) === source) see(alt.updatedAt)
    }
  }
  return latest
}
