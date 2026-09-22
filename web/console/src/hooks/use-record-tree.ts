/** The browse table's tree as a hook: which rows are open, and the reads that
 * answer what is under them.
 *
 * `resolveTree` (lib/record-tree.ts) decides from what the cache already holds
 * which level reads a render needs; this hook runs exactly those through
 * `useQueries`. A read that lands re-renders the tree, and the next level's
 * read is named on that render. Reading the cache during render is sound
 * here because every key read is subscribed in the same render, and TanStack
 * re-renders on the first snapshot that differs from what was drawn. */

import { useMemo, useState } from "react"
import { useQueries, useQueryClient } from "@tanstack/react-query"

import { recordsQueryOptions, type ListParams } from "@/lib/api/records"
import type {
  KindInfo,
  Page,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"
import {
  childrenFilter,
  resolveTree,
  type ChildrenPage,
  type TreeNode,
} from "@/lib/record-tree"

/** How many children one level's read asks for: the server's own page cap. A
 * level past it answers with a cursor, which `resolveTree` reads as "ask per
 * row". */
export const CHILDREN_PAGE = 500

export interface RecordTreeOptions {
  kind: KindInfo | undefined
  /** The self-reference to nest by; `undefined` draws the rows flat. */
  property: DeclaredProperty | undefined
  /** The page of top-level rows. */
  roots: SubstrateRecord[]
  /** The view's own filter, applied at every level. */
  filter: RecordFilter | undefined
  orderBy: string
  /** The references the page expands, so a child's reference columns read as
   * names the way a root's do. */
  expand: readonly string[]
}

export interface RecordTree {
  /** The table's rows: the roots, with each open row's children under it. */
  rows: SubstrateRecord[]
  nodes: ReadonlyMap<string, TreeNode>
  toggle: (id: string) => void
  /** Every referent the level reads carried in `included`, by record path. */
  included: Record<string, SubstrateRecord>
  /** A tree is being drawn. */
  active: boolean
}

/** One level's read: the kind's collection, narrowed to the records naming one
 * of `parentIds`, in the table's own order, expanding what the table expands. */
export function childrenListParams(
  kind: KindInfo,
  property: string,
  filter: RecordFilter | undefined,
  orderBy: string,
  expand: readonly string[],
  parentIds: readonly string[]
): ListParams {
  return {
    kinds: [kind.identity],
    first: CHILDREN_PAGE,
    filter: childrenFilter(filter, kind.identity, property, parentIds),
    orderBy,
    expand: expand.length ? [...expand] : undefined,
  }
}

const NO_NODES: ReadonlyMap<string, TreeNode> = new Map()

export function useRecordTree(opts: RecordTreeOptions): RecordTree {
  const client = useQueryClient()
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set())
  // Another collection is other ids: a row here must not open because a row
  // there with the same id was open. State, not a ref, adjusted while
  // rendering: React's sanctioned shape for "reset on a prop change".
  const identity = opts.kind?.identity
  const [lastIdentity, setLastIdentity] = useState(identity)
  if (lastIdentity !== identity) {
    setLastIdentity(identity)
    setExpanded(new Set())
  }

  const { kind, property } = opts
  const active = kind !== undefined && property !== undefined
  const optionsFor = (ids: readonly string[]) =>
    recordsQueryOptions(
      childrenListParams(
        kind!,
        property!.name,
        opts.filter,
        opts.orderBy,
        opts.expand,
        ids
      )
    )
  const lookup = (ids: readonly string[]): ChildrenPage | undefined => {
    const state = client.getQueryState<Page>(optionsFor(ids).queryKey)
    if (state?.data) {
      return { records: state.data.records, complete: !state.data.cursor }
    }
    if (state?.status === "error") {
      return {
        records: [],
        complete: false,
        error: state.error?.message ?? "the read failed",
      }
    }
    return undefined
  }

  const tree = active
    ? resolveTree({
        roots: opts.roots,
        property: property.name,
        expanded,
        lookup,
      })
    : undefined
  const wanted = tree?.wanted ?? []
  useQueries({ queries: wanted.map(optionsFor) })

  // The referents every level read carried, merged once per change to any of
  // those reads: the cache's own update stamps are the key, so a render that
  // fetched nothing new hands the table the same object and its columns stand.
  const states = wanted.map((ids) =>
    client.getQueryState<Page>(optionsFor(ids).queryKey)
  )
  const includedKey = states.map((s) => s?.dataUpdatedAt ?? 0).join(",")
  const included = useMemo(
    () => {
      const out: Record<string, SubstrateRecord> = {}
      for (const state of states) {
        if (state?.data?.included) Object.assign(out, state.data.included)
      }
      return out
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `states` is re-read from the cache every render; the stamps say whether any of it changed
    [includedKey]
  )

  function toggle(id: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return {
    rows: tree ? tree.rows : opts.roots,
    nodes: tree?.nodes ?? NO_NODES,
    toggle,
    included,
    active,
  }
}
