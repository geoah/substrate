/** The browse table's tree as a hook: which rows are open, and the reads that
 * answer what is under them.
 *
 * `resolveTree` (lib/record-tree.ts) decides from what the cache already holds
 * which level reads a render needs; this hook runs exactly those through
 * `useQueries`. A read that lands re-renders the tree, and the next level's
 * read is named on that render. Reading the cache during render is sound
 * here because every key read is subscribed in the same render, and TanStack
 * re-renders on the first snapshot that differs from what was drawn.
 *
 * FILTERED (`filtered`), the page is the matches, not the roots: one more read
 * asks which of the parents the page names match too, and `matchedRoots`
 * keeps the rest at the top level (lib/record-tree.ts says why). Until that
 * read answers the tree is `loading`, because drawing the page flat first
 * would show rows that are about to move under a parent. */

import { useMemo, useState } from "react"
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query"

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
  matchedRoots,
  matchingParentsFilter,
  parentIdsOf,
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
  /** The page: the top-level rows, or under `filtered` the matches. */
  roots: SubstrateRecord[]
  /** The view's own filter, applied at every level. */
  filter: RecordFilter | undefined
  /** A filter or a search is set: the page is its matches, nested under the
   * matches they belong to, and rows open by themselves. */
  filtered?: boolean
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
  /** The filtered tree's top level is not known yet. */
  loading: boolean
  /** Top-level row id → the record path of the parent it belongs to, where
   * that parent is not among the matches. Empty outside a filtered tree. */
  context: ReadonlyMap<string, string>
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
const NO_CONTEXT: ReadonlyMap<string, string> = new Map()

export function useRecordTree(opts: RecordTreeOptions): RecordTree {
  const client = useQueryClient()
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set())
  // Another collection is other ids: a row here must not open because a row
  // there with the same id was open. State, not a ref, adjusted while
  // rendering: React's sanctioned shape for "reset on a prop change".
  // A new filter is a new question, and what a toggle means flips with it
  // (opened, or closed under a filter), so the rows start from their default.
  const scope = `${opts.kind?.identity ?? ""} ${opts.filtered ? "f" : "-"} ${JSON.stringify(opts.filter ?? null)}`
  const [lastScope, setLastScope] = useState(scope)
  if (lastScope !== scope) {
    setLastScope(scope)
    setExpanded(new Set())
  }

  const { kind, property } = opts
  const active = kind !== undefined && property !== undefined
  const filtered = active && Boolean(opts.filtered)

  const parentIds = useMemo(
    () => (filtered && property ? parentIdsOf(opts.roots, property.name) : []),
    [filtered, opts.roots, property]
  )
  const parents = useQuery({
    ...recordsQueryOptions({
      kinds: [kind?.identity ?? ""],
      first: CHILDREN_PAGE,
      filter: matchingParentsFilter(opts.filter, parentIds),
    }),
    enabled: filtered && parentIds.length > 0,
  })
  // The list options keep the previous key's data while the next one loads;
  // another page's parents would hide the wrong rows.
  const parentsKnown =
    parentIds.length === 0 ||
    (parents.data !== undefined && !parents.isPlaceholderData) ||
    parents.isError
  const matched = useMemo(
    () =>
      filtered && property && parentsKnown
        ? matchedRoots(
            opts.roots,
            property.name,
            parentIds.length ? (parents.data?.records ?? []) : []
          )
        : undefined,
    [filtered, parentsKnown, opts.roots, property, parentIds, parents.data]
  )
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

  const loading = filtered && !matched
  const tree =
    active && !loading
      ? resolveTree({
          roots: matched ? matched.roots : opts.roots,
          property: property.name,
          expanded,
          lookup,
          openByDefault: filtered,
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
    rows: tree ? tree.rows : loading ? [] : opts.roots,
    nodes: tree?.nodes ?? NO_NODES,
    toggle,
    included,
    active,
    loading,
    context: matched?.context ?? NO_CONTEXT,
  }
}
