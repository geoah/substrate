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
 * (hooks/use-record-tree.ts) only fetches what it names.
 *
 * A FILTERED tree (a filter or a search is set) nests the matches rather than
 * the collection. The page is the matches, every level's read carries the same
 * filter, and a match stands at the top level unless its parent matches too,
 * in which case it sits under that parent instead (`matchedRoots`), so every
 * match is drawn exactly once. A top-level match whose parent does not match
 * carries that parent as its context, so where it lives is not lost. Rows open
 * by themselves there, because a match hidden behind a closed parent is a
 * match the reader asked for and cannot see. */

import {
  readReference,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import { declaredReferences, type DeclaredProperty } from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"

/** The single-valued references a kind declares AT ITSELF, by name. A pin is
 * a full identity (record 0098), so "at itself" is the pin equal to the kind's
 * own identity. A repeated one is a set, not a place in a tree, and a keyed
 * map of pointers is not a parent either. */
export function selfReferences(kind: KindInfo): DeclaredProperty[] {
  return declaredReferences(kind).filter(
    (p) => !p.repeated && !p.keyed && p.to === kind.identity
  )
}

/** The property a collection nests by: `parent` where the kind declares one at
 * itself, else the first self-reference by name. `undefined` for a kind that
 * declares none, which is a flat table. */
export function nestingProperty(kind: KindInfo): DeclaredProperty | undefined {
  const own = selfReferences(kind)
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
  /** Its children, once a read has answered them: what a row's badge counts
   * whether or not the row is open. */
  childRecords?: readonly SubstrateRecord[]
}

export interface ResolveTreeInput {
  roots: readonly SubstrateRecord[]
  property: string
  /** The ids the reader has toggled away from their default: opened, or,
   * under `openByDefault`, closed. An id that is a leaf, or not on the page,
   * is simply ignored. */
  expanded: ReadonlySet<string>
  /** A row whose children are known opens without being asked (the filtered
   * tree). A row whose level read was cut short still waits for a click: its
   * own read is one request per row. */
  openByDefault?: boolean
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
      const toggled = input.expanded.has(record.id)
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
        node.open =
          children.length > 0 && (input.openByDefault ? !toggled : toggled)
        if (children.length) node.childRecords = children
      } else if (batch) {
        // Cut short or refused: the level's read cannot say who has children,
        // so every row offers to open, and opening asks about that row alone.
        node.children = "unknown"
        if (toggled) {
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
            if (children.length) node.childRecords = children
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

/** Whether any row can open, so the grid reserves the chevron's width on
 * every row (titles on one level stay aligned) or on none (a tree with nothing
 * to open reads as the flat table it is). A row whose level has not answered
 * yet reserves nothing: its chevron is not known to exist. */
export function hasToggles(nodes: ReadonlyMap<string, TreeNode>): boolean {
  for (const node of nodes.values()) {
    if (node.children === "some" || node.children === "unknown") return true
  }
  return false
}

/** The distinct parents a page of matches names, which the filtered tree asks
 * the server about: those that match hold their matching children. */
export function parentIdsOf(
  page: readonly SubstrateRecord[],
  property: string
): string[] {
  const out = new Set<string>()
  for (const record of page) {
    const id = parentIdOf(record, property)
    if (id !== undefined && id !== record.id) out.add(id)
  }
  return [...out]
}

/** The read that says which of `parentIds` match the view's own filter. */
export function matchingParentsFilter(
  filter: RecordFilter | undefined,
  parentIds: readonly string[]
): RecordFilter {
  return { ...filter, ids: [...parentIds] }
}

export interface MatchedRoots {
  /** The matches that stand at the top level, in the page's order. */
  roots: SubstrateRecord[]
  /** Top-level match id → the record path of the parent it names that does
   * not match: the context the row shows beside its title. */
  context: Map<string, string>
}

/** The filtered tree's top level. A match whose parent matches is left out:
 * it is drawn under that parent, whose own level read carries the filter. A
 * match naming a parent that does not match (or that does not exist) stands
 * at the top level, with the parent as its context. `matchingParents` are the
 * parent records the matching-parents read returned; a pointer written
 * before a merge names one by a former id, so former ids count too. */
export function matchedRoots(
  page: readonly SubstrateRecord[],
  property: string,
  matchingParents: readonly SubstrateRecord[]
): MatchedRoots {
  const matching = new Map<string, SubstrateRecord>()
  for (const parent of matchingParents) {
    matching.set(parent.id, parent)
    for (const former of parent.formerIds ?? []) matching.set(former, parent)
  }
  const onPage = new Set(page.map((r) => r.id))
  // A chain of matching parents that comes back on itself has no member
  // standing on top, so each would be drawn only under another that is never
  // drawn. One member leads the cycle at the top level and the rest hang
  // under it, where the tree places each record once: the smallest id on the
  // page, so every record of the cycle picks the same one.
  const leaderOf = (start: SubstrateRecord): string | undefined => {
    const path: SubstrateRecord[] = []
    const at = new Map<string, number>()
    let cur: SubstrateRecord | undefined = start
    while (cur) {
      const i = at.get(cur.id)
      if (i !== undefined) {
        const members = path.slice(i).map((r) => r.id)
        return members.filter((id) => onPage.has(id)).sort()[0]
      }
      at.set(cur.id, path.length)
      path.push(cur)
      const parentId = parentIdOf(cur, property)
      if (parentId === undefined || parentId === cur.id) return undefined
      cur = matching.get(parentId)
    }
    return undefined
  }
  const roots: SubstrateRecord[] = []
  const context = new Map<string, string>()
  for (const record of page) {
    const parentId = parentIdOf(record, property)
    if (parentId === undefined || parentId === record.id) {
      roots.push(record)
      continue
    }
    if (matching.has(parentId) && leaderOf(record) !== record.id) continue
    roots.push(record)
    const held = readReference(record.properties[property])
    if (held) context.set(record.id, held.path)
  }
  return { roots, context }
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
