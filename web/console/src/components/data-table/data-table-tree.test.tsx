// @vitest-environment jsdom
/** The title cell's tree gutter: a nested table reserves the chevron's width
 * on every row only while some row can open, so a tree with nothing to open
 * reads as the flat table it is. */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import {
  RowTreeProvider,
  TreeToggle,
  useRowTreeNode,
  type RowTree,
} from "./data-table-tree"
import type { TreeNode } from "@/lib/record-tree"

afterEach(cleanup)

function node(id: string, children: TreeNode["children"]): TreeNode {
  return {
    id,
    depth: 0,
    children,
    open: false,
    loading: false,
    truncated: false,
  }
}

function Cell({ id }: { id: string }) {
  const tree = useRowTreeNode(id)
  if (!tree) return null
  return (
    <div data-testid={id}>
      <TreeToggle
        node={tree.node}
        onToggle={tree.toggle}
        gutter={tree.gutter}
      />
      {tree.context && <span data-slot="context">{tree.context}</span>}
    </div>
  )
}

function mount(tree: RowTree, ids: string[]) {
  return render(
    <RowTreeProvider tree={tree}>
      {ids.map((id) => (
        <Cell key={id} id={id} />
      ))}
    </RowTreeProvider>
  )
}

describe("the tree gutter", () => {
  it("reserves nothing when no row can open", () => {
    const { getByTestId } = mount(
      {
        nodes: new Map([
          ["a", node("a", "none")],
          ["b", node("b", "pending")],
        ]),
        toggle: () => {},
      },
      ["a", "b"]
    )
    expect(getByTestId("a").querySelector("[data-slot=tree-gutter]")).toBeNull()
    expect(getByTestId("b").querySelector("[data-slot=tree-gutter]")).toBeNull()
  })

  it("keeps a leaf aligned with a sibling that can open", () => {
    const { getByTestId } = mount(
      {
        nodes: new Map([
          ["a", node("a", "none")],
          ["b", node("b", "some")],
        ]),
        toggle: () => {},
      },
      ["a", "b"]
    )
    expect(
      getByTestId("a").querySelector("[data-slot=tree-gutter]")
    ).toBeTruthy()
    expect(getByTestId("b").querySelector("button[aria-expanded]")).toBeTruthy()
  })

  it("hands a row the parent it belongs to when that parent is not drawn", () => {
    const { getByTestId } = mount(
      {
        nodes: new Map([["a", node("a", "none")]]),
        toggle: () => {},
        context: new Map([["a", "acme.test/tasks/task/p"]]),
      },
      ["a"]
    )
    expect(getByTestId("a").textContent).toBe("acme.test/tasks/task/p")
  })
})
