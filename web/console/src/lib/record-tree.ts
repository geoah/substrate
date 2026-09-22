/** Nesting a collection by a reference that points back at its own kind: a
 * `team` whose `parent` is a team, a thread whose `parent` is a thread. The
 * browse table shows the records that name no parent and opens each in place
 * onto the records that name it.
 *
 * Two reads, both the ordinary list. The roots are the page with
 * `<property>: {exists: false}` added to its filter. The children of one LEVEL
 * are one read with `<property>: {in: [<paths>]}` over every record on that
 * level, so a page of fifty roots learns which of them have children in one
 * request rather than fifty, and a chevron is drawn only where a click has
 * something to open. A level read that the page cap cut short cannot say who
 * has children, so its rows fall back to a read each, on demand.
 * `resolveTree` is that decision, pure, so the hook around it
 * (hooks/use-record-tree.ts) only fetches what it names. */

import {
  readReference,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import {
  declaredReferences,
  resolveReferenceTarget,
  type DeclaredProperty,
} from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"

/** The single-valued references a kind declares AT ITSELF, by name. A repeated
 * one is a set, not a place in a tree, and a keyed map of pointers is not a
 * parent either. */
export function selfReferences(
  kinds: KindInfo[],
  kind: KindInfo
): DeclaredProperty[] {
  return declaredReferences(kind).filter(
    (p) =>
      !p.repeated &&
      !p.keyed &&
      p.to !== undefined &&
      resolveReferenceTarget(kinds, kind, p.to)?.identity === kind.identity
  )
}

/** The property a collection nests by: `parent` where the kind declares one at
 * itself, else the first self-reference by name. `undefined` for a kind that
 * declares none, which is a flat table. */
export function nestingProperty(
  kinds: KindInfo[],
  kind: KindInfo
): DeclaredProperty | undefined {
  const own = selfReferences(kinds, kind)
  return own.find((p) => p.name === "parent") ?? own[0]
}

/** The roots' read: the caller's filter plus "names no parent". */
export function rootsFilter(
  filter: RecordFilter | undefined,
  property: string
): RecordFilter {
  return {
    ...filter,
    properties: { ...filter?.properties, [property]: { exists: false } },
  }
}

/** One level's read: the caller's filter plus "names one of these parents".
 * The values are full record paths; a bare id would also do under a pin, but
 * the path is what the value stores and what the server compares. */
export function childrenFilter(
  filter: RecordFilter | undefined,
  kind: string,
  property: string,
  parentIds: readonly string[]
): RecordFilter {
  return {
    ...filter,
    properties: {
      ...filter?.properties,
      [property]: { in: parentIds.map((id) => `${kind}/${id}`) },
    },
  }
}

/** The id of the record of the SAME kind that `property` names on `record`,
 * or `undefined`: no value, a shape that is not a reference, or a pointer at
 * another kind, which the tree does not follow. */
export function parentIdOf(
  record: SubstrateRecord,
  property: string
): string | undefined {
  const held = readReference(record.properties[property])
  if (!held) return undefined
  const target = splitRecordPath(held.path)
  return target && target.kind === record.kind ? target.id : undefined
}

/** What one level's read answered. `complete` is "no cursor": every child of
 * every parent asked about is on the page, so a parent with none here has
 * none. `error` is the read's refusal, kept so a row can say why it will not
 * open rather than spin. */
export interface ChildrenPage {
  records: SubstrateRecord[]
  complete: boolean
  error?: string
}

/** What is known about a row's children. `pending`: its level's read is on
 * the wire. `unknown`: that read was cut at the page cap or refused, so only
 * the row's own read can say. `none` and `some`: the read answered. */
export type ChildrenState = "pending" | "unknown" | "none" | "some"

export interface TreeNode {
  id: string
  /** 0 for a root. */
  depth: number
  children: ChildrenState
  /** Its children are in the rows right under it. */
  open: boolean
  /** Open, and the read that answers its children has not yet. */
  loading: boolean
  /** Open, and the children under it are the first page of more. */
  truncated: boolean
  /** Open, and the read that answers its children was refused. */
  error?: string
}

export interface ResolveTreeInput {
  roots: readonly SubstrateRecord[]
  property: string
  /** The ids the reader has opened. An id that is a leaf, or not on the
   * page, is simply ignored. */
  expanded: ReadonlySet<string>
  /** What the cache holds for one level's read over these parents. */
  lookup: (parentIds: readonly string[]) => ChildrenPage | undefined
}

export interface ResolvedTree {
  /** Every row the table draws, depth-first: each open row's children follow
   * it, in the order their read returned them. */
  rows: SubstrateRecord[]
  nodes: Map<string, TreeNode>
  /** The parent-id batches whose reads the tree needs live, in resolution
   * order: the roots' level first, then one per open row's children. */
  wanted: string[][]
}

/** How deep a chain may go before the walk stops. A guard against a stored
 * cycle reaching the page through a merge, not a limit on the data. */
const MAX_DEPTH = 32

export function resolveTree(input: ResolveTreeInput): ResolvedTree {
  const rows: SubstrateRecord[] = []
  const nodes = new Map<string, TreeNode>()
  const wanted: string[][] = []
  const seen = new Set<string>()

  function place(level: readonly SubstrateRecord[], depth: number): void {
    // A record is placed once. A pointer back at an ancestor, or at the
    // record itself, would otherwise draw the same rows without end.
    const fresh = level.filter((r) => !seen.has(r.id))
    if (!fresh.length || depth > MAX_DEPTH) return
    for (const r of fresh) seen.add(r.id)
    const ids = fresh.map((r) => r.id)
    wanted.push(ids)
    const batch = input.lookup(ids)
    const whole = batch !== undefined && batch.complete && !batch.error
    const byParent = whole
      ? groupByParent(batch.records, fresh, input.property)
      : undefined
    for (const record of fresh) {
      const open = input.expanded.has(record.id)
      const node: TreeNode = {
        id: record.id,
        depth,
        children: "pending",
        open: false,
        loading: false,
        truncated: false,
      }
      let children: SubstrateRecord[] = []
      if (byParent) {
        children = byParent.get(record.id) ?? []
        node.children = children.length ? "some" : "none"
        node.open = open && children.length > 0
      } else if (batch) {
        // Cut short or refused: the level's read cannot say who has children,
        // so every row offers to open, and opening asks about that row alone.
        node.children = "unknown"
        if (open) {
          node.open = true
          wanted.push([record.id])
          const own = input.lookup([record.id])
          if (!own) {
            node.loading = true
          } else if (own.error) {
            node.error = own.error
          } else {
            children =
              groupByParent(own.records, [record], input.property).get(
                record.id
              ) ?? []
            node.children = children.length ? "some" : "none"
            node.truncated = !own.complete
          }
        }
      }
      nodes.set(record.id, node)
      rows.push(record)
      if (node.open && children.length) place(children, depth + 1)
    }
  }

  place(input.roots, 0)
  return { rows, nodes, wanted }
}

/** The children on a level's page, by the parent each names. A pointer
 * written before a merge names the parent by a FORMER id, and the read still
 * matched it (the server walks the merge trail), so the former ids are keys
 * too. A child naming a parent that is not on this level is dropped: the
 * filter should not have returned it, and drawing it under the wrong row would
 * be worse than not drawing it. */
function groupByParent(
  children: readonly SubstrateRecord[],
  parents: readonly SubstrateRecord[],
  property: string
): Map<string, SubstrateRecord[]> {
  const canonical = new Map<string, string>()
  for (const parent of parents) {
    canonical.set(parent.id, parent.id)
    for (const former of parent.formerIds ?? []) {
      canonical.set(former, parent.id)
    }
  }
  const out = new Map<string, SubstrateRecord[]>()
  for (const child of children) {
    const named = parentIdOf(child, property)
    const id = named === undefined ? undefined : canonical.get(named)
    if (id === undefined) continue
    const list = out.get(id)
    if (list) list.push(child)
    else out.set(id, [child])
  }
  return out
}
