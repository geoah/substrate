/* eslint-disable react-refresh/only-export-components -- the provider, its
 * hook and the toggle are one module by design; nothing here hot-reloads
 * alone. */

/** The tree a grid's rows may form: the indent and the chevron that open a
 * row onto its children, drawn INSIDE the title cell so the tree reads where
 * the eye already is. A context feeds it, so the column factory stays a
 * function of the kind alone; without a provider the cell is the plain cell,
 * and every flat table is untouched. */

import { createContext, useContext } from "react"
import { ChevronRightIcon, TriangleAlertIcon } from "lucide-react"

import { Spinner } from "@/components/ui/spinner"
import type { TreeNode } from "@/lib/record-tree"
import { cn } from "@/lib/utils"

export interface RowTree {
  nodes: ReadonlyMap<string, TreeNode>
  toggle: (id: string) => void
}

const RowTreeContext = createContext<RowTree | null>(null)

export function RowTreeProvider({
  tree,
  children,
}: {
  /** `null` draws the table flat. */
  tree: RowTree | null
  children: React.ReactNode
}) {
  return (
    <RowTreeContext.Provider value={tree}>{children}</RowTreeContext.Provider>
  )
}

/** One level of nesting, in px. */
export const INDENT_PX = 22

/** The row's place in the tree, or `undefined` in a flat table. */
export function useRowTreeNode(
  id: string
): { node: TreeNode; toggle: () => void } | undefined {
  const tree = useContext(RowTreeContext)
  const node = tree?.nodes.get(id)
  if (!tree || !node) return undefined
  return { node, toggle: () => tree.toggle(id) }
}

/** The chevron that opens a row, or its width in blank where there is
 * nothing (yet) to open, so titles on one level stay aligned. */
export function TreeToggle({
  node,
  onToggle,
  noun = "rows",
}: {
  node: TreeNode
  onToggle: () => void
  /** What the children are called, for the accessible name. */
  noun?: string
}) {
  if (node.children === "none" || node.children === "pending") {
    return <span aria-hidden className="w-[18px] shrink-0" />
  }
  const title =
    node.error ??
    (node.truncated
      ? "More than one read returns: the first page is shown"
      : undefined)
  return (
    <button
      type="button"
      aria-expanded={node.open}
      aria-label={`${node.open ? "Hide" : "Show"} ${noun}`}
      title={title}
      className="inline-grid size-[18px] shrink-0 cursor-pointer place-items-center rounded-[4px] text-faint outline-none hover:bg-border-strong hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
      onClick={(e) => {
        e.stopPropagation()
        onToggle()
      }}
    >
      {node.loading ? (
        <Spinner className="size-3" />
      ) : node.error ? (
        <TriangleAlertIcon className="size-3 text-destructive" />
      ) : (
        <ChevronRightIcon
          className={cn(
            "size-[13px] transition-transform",
            node.open && "rotate-90"
          )}
        />
      )}
    </button>
  )
}
