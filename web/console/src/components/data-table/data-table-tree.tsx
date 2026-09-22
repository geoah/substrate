/** The tree a table's rows may form: the indent and the chevron that open a
 * row onto its children, drawn INSIDE the identity cell so the tree reads
 * where the eye already is. A context feeds it, so the column factory stays a
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
const INDENT_PX = 20

/** The identity cell of a row that may sit in a tree: indented to its depth,
 * with the chevron that opens it before the content. */
export function TreeCell({
  id,
  children,
}: {
  id: string
  children: React.ReactNode
}) {
  const tree = useContext(RowTreeContext)
  const node = tree?.nodes.get(id)
  if (!tree || !node) return <>{children}</>
  return (
    <span
      className="flex min-w-0 items-center gap-1"
      style={{ paddingLeft: node.depth * INDENT_PX }}
      data-depth={node.depth}
    >
      <TreeToggle node={node} onToggle={() => tree.toggle(id)} />
      <span className="min-w-0 flex-1">{children}</span>
    </span>
  )
}

function TreeToggle({
  node,
  onToggle,
}: {
  node: TreeNode
  onToggle: () => void
}) {
  // A row known to be a leaf, or not yet known at all, keeps the chevron's
  // width so the titles on one level stay aligned.
  if (node.children === "none" || node.children === "pending") {
    return <span aria-hidden className="size-4 shrink-0" />
  }
  const title =
    node.error ??
    (node.truncated
      ? "More children than one read returns: the first page is shown"
      : undefined)
  return (
    <button
      type="button"
      aria-expanded={node.open}
      aria-label={node.open ? "Collapse" : "Expand"}
      title={title}
      className="flex size-4 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground hover:bg-muted hover:text-foreground"
      // The row itself navigates on click; the chevron must not.
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
            "size-3.5 transition-transform",
            node.open && "rotate-90"
          )}
        />
      )}
    </button>
  )
}
