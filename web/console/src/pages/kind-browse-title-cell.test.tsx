// @vitest-environment jsdom
/** The collection's title cell: the title is the one link to the record, a
 * parent's subtask chip says what it counts in words, and a filtered match
 * whose parent is not drawn says which record it is in. */

import { cleanup, render } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { RowTreeProvider } from "@/components/data-table/data-table-tree"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { buildColumns } from "@/pages/kind-browse-columns"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    className,
    "aria-label": label,
  }: {
    children: ReactNode
    className?: string
    "aria-label"?: string
  }) => (
    <a className={className} aria-label={label}>
      {children}
    </a>
  ),
}))

afterEach(cleanup)

const TASK = "acme.test/tasks/task"
const task: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "acme.test",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      name: { type: "string" },
      status: {
        type: "state",
        states: ["open", "done"],
        initial: "open",
      },
      parent: { type: "reference", kind: TASK },
    },
  },
}

const child: SubstrateRecord = {
  id: "c",
  kind: TASK,
  properties: {
    title: "A rather long task title that will not fit",
    name: "A rather long task title that will not fit",
    parent: { ref: `${TASK}/p` },
  },
  labels: {},
  version: 1,
  createdAt: "2026-09-26T00:00:00Z",
  updatedAt: "2026-09-26T00:00:00Z",
}

function renderTitle(
  context?: Map<string, string>,
  childRecords?: SubstrateRecord[]
) {
  const titles = new Map([[`${TASK}/p`, "Plan the budget"]])
  const [title] = buildColumns(task, [task], titles, { childNoun: "subtasks" })
  const cell = title.cell as (ctx: unknown) => ReactNode
  return render(
    <RowTreeProvider
      tree={{
        nodes: new Map([
          [
            "c",
            {
              id: "c",
              depth: 0,
              children: childRecords ? "some" : "none",
              childRecords,
              open: false,
              loading: false,
              truncated: false,
            },
          ],
        ]),
        toggle: () => {},
        context,
      }}
    >
      {cell({ row: { original: child } })}
    </RowTreeProvider>
  )
}

describe("the title cell", () => {
  // A hover-only Open button beside the title was a second way to the same
  // place that no keyboard could reach (review T5).
  it("links the record from its title alone", () => {
    const { container } = renderTitle()
    const links = [...container.querySelectorAll("a")]
    expect(links).toHaveLength(1)
    expect(links[0].textContent).toBe(
      "A rather long task title that will not fit"
    )
    expect(links[0].className).toMatch(/\btruncate\b/)
  })

  it("says what a parent's chip counts, in words", () => {
    const sub = (id: string, status: string): SubstrateRecord => ({
      ...child,
      id,
      properties: { ...child.properties, status },
    })
    const { container } = renderTitle(undefined, [
      sub("a", "done"),
      sub("b", "open"),
      sub("d", "open"),
    ])
    const chip = container.querySelector("[data-slot=subtask-count]")
    expect(chip?.textContent).toContain("1 of 3 subtasks done")
    expect(chip?.getAttribute("title")).toBe("1 of 3 subtasks done")
  })

  it("names the parent a match belongs to when that parent is not drawn", () => {
    const { container } = renderTitle(new Map([["c", `${TASK}/p`]]))
    const hint = container.querySelector("[data-slot=tree-context]")
    expect(hint?.textContent).toBe("inPlan the budget")
  })

  it("says nothing about a parent otherwise", () => {
    const { container } = renderTitle()
    expect(container.querySelector("[data-slot=tree-context]")).toBeNull()
  })
})
