/** The record page's pure decisions: which fan-in groups it lists and how
 * much of each is done, what a record points to, what a change row says, and
 * the header's who-and-when. */

import { actorIdentity } from "@/lib/actor-identity"
import { CORE_PACKAGE } from "@/lib/api/http"
import type { ReferencingGroup, ReferencingRow } from "@/lib/api/records"
import {
  readReference,
  type ChangeRow,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { changedProperties } from "@/lib/changelog"
import { declaredReferences, kindPurpose } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { displayName, lowerFirst } from "@/lib/kind-names"
import { splitRecordPath } from "@/lib/record-path"
import { humanizeName, propSpecsByName } from "@/lib/record-schema"

/** How many rows a group shows before "Show all". */
export const GROUP_FOLD = 10

/** The state a kind's progress counts, when it has a done-like one. */
export function doneState(
  kind: KindInfo | undefined
): { property: string; done: string; initial?: string } | undefined {
  if (!kind) return undefined
  for (const spec of propSpecsByName(kind)) {
    if (spec.kind !== "state") continue
    const done = (spec.states ?? []).find((s) =>
      ["done", "completed", "closed"].includes(s)
    )
    if (done) return { property: spec.name, done, initial: spec.initial }
  }
  return undefined
}

/** A group's rows as a reader scans them: what is not done yet first, then
 * soonest due, then by title. */
export function sortConnected(
  rows: ReferencingRow[],
  progress: { property: string; done: string } | undefined,
  due: string | undefined
): ReferencingRow[] {
  const isDone = (r: ReferencingRow) =>
    progress ? r.record.properties[progress.property] === progress.done : false
  const when = (r: ReferencingRow) => {
    const v = due ? r.record.properties[due] : undefined
    return typeof v === "string" ? v : "\uffff"
  }
  const title = (r: ReferencingRow) => recordTitle(r.record.properties)
  return [...rows].sort(
    (a, b) =>
      Number(isDone(a)) - Number(isDone(b)) ||
      when(a).localeCompare(when(b)) ||
      title(a).localeCompare(title(b))
  )
}

/** The groups a record page lists: the fan-in, minus the mapping slots its
 * sources point through. */
export function connectedGroups(
  groups: ReferencingGroup[],
  record: SubstrateRecord
): ReferencingGroup[] {
  const slots = new Set(
    (record.linkedFrom ?? []).map((l) => `${l.kind}\u0000${l.property}`)
  )
  return groups.filter((g) => !slots.has(`${g.kind}\u0000${g.property}`))
}

/** The two machinery kinds a person still meets on a record page, each with
 * the review page its rows open. */
export const MERGE_REQUEST_KIND = `${CORE_PACKAGE}/recordmergerequest`
export const CHANGE_REQUEST_KIND = `${CORE_PACKAGE}/recordpatchrequest`
export const REVIEW_ROUTES: Readonly<
  Record<string, "/merge-requests/$id" | "/change-requests/$id">
> = {
  [MERGE_REQUEST_KIND]: "/merge-requests/$id",
  [CHANGE_REQUEST_KIND]: "/change-requests/$id",
}

/** The groups everyday mode lists: no machinery but the merge and change
 * requests, and those as one group each, whichever of the request's
 * references points here. */
export function everydayGroups(
  groups: ReferencingGroup[],
  kinds: ReadonlyMap<string, KindInfo>
): ReferencingGroup[] {
  const out: ReferencingGroup[] = []
  const review = new Map<string, ReferencingGroup>()
  for (const group of groups) {
    if (REVIEW_ROUTES[group.kind]) {
      const held = review.get(group.kind)
      if (!held) {
        const merged = { ...group, rows: [...group.rows] }
        review.set(group.kind, merged)
        out.push(merged)
        continue
      }
      for (const row of group.rows)
        if (!held.rows.some((r) => r.record.id === row.record.id))
          held.rows.push(row)
      continue
    }
    if (kindPurpose(kinds.get(group.kind) ?? group.kind) === "internal")
      continue
    out.push(group)
  }
  return out
}

/** The records this one points to, off its own declared references. */
export function outgoingOf(
  record: SubstrateRecord,
  kind: KindInfo | undefined
): Array<{ property: string; label: string; kind: string; id: string }> {
  if (!kind) return []
  const out: Array<{
    property: string
    label: string
    kind: string
    id: string
  }> = []
  const labels = new Map(propSpecsByName(kind).map((s) => [s.name, s.label]))
  for (const pointer of declaredReferences(kind)) {
    const value = record.properties[pointer.name]
    for (const one of Array.isArray(value) ? value : [value]) {
      const held = readReference(one)
      const target = held ? splitRecordPath(held.path) : undefined
      if (!target) continue
      out.push({
        property: pointer.name,
        label: labels.get(pointer.name) ?? humanizeName(pointer.name),
        ...target,
      })
    }
  }
  return out
}

/** What one change did, as the words after the actor. */
export function changeSentence(
  row: ChangeRow,
  record: SubstrateRecord
): string {
  const noun = lowerFirst(displayName(record.kind))
  const winner = String(row.payload?.winner ?? "")
  const loser = String(row.payload?.loser ?? "")
  switch (row.op) {
    case "put":
      if (row.payload?.created === true) return `added this ${noun}`
      if (row.payload?.restored === true) return `restored this ${noun}`
      return changedProperties(row).length ? "changed" : `saved this ${noun}`
    case "patch":
      return stateMoves(row).length ? "moved" : "changed"
    case "delete":
      return `deleted this ${noun}`
    case "merge":
      return row.recordId === loser
        ? `combined this into another ${noun}`
        : `combined another ${noun} into this one`
    case "split":
      return row.recordId === winner
        ? `split another ${noun} off this one`
        : `split this off another ${noun}`
    case "gc":
      return "cleaned this up"
    default:
      return row.op
  }
}

export function stateMoves(row: ChangeRow): Array<[string, string]> {
  const states = row.payload?.states
  if (!states || typeof states !== "object" || Array.isArray(states)) return []
  return Object.entries(states as Record<string, unknown>)
    .filter(([, v]) => typeof v === "string")
    .map(([k, v]) => [k, v as string])
}

export function plainChanges(row: ChangeRow): string[] {
  const moved = new Set(stateMoves(row).map(([k]) => k))
  return changedProperties(row).filter((p) => !moved.has(p))
}

/** "you", or the actor's plain name, inside a sentence. */
function who(actor: string): string {
  const identity = actorIdentity(actor)
  return identity.cls === "you" ? "you" : identity.name
}

/** The meta line's two facts, off the change rows the page holds: who added
 * it (only when the creating row is among them) and who changed it last. */
export function headerFacts(
  record: SubstrateRecord,
  rows: ChangeRow[]
): { addedBy?: string; changedBy?: string; changed: boolean } {
  const created = rows.find((r) => r.payload?.created === true)
  const latest = rows[0]
  const changed = record.updatedAt !== record.createdAt
  return {
    addedBy: created ? who(created.actor) : undefined,
    changedBy:
      changed && latest && latest !== created ? who(latest.actor) : undefined,
    changed,
  }
}

/** Whether a record has merges to show: only a record that won one carries
 * former ids. */
export function hasMerges(record: SubstrateRecord): boolean {
  return (record.formerIds ?? []).length > 0
}
