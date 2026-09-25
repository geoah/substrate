// @vitest-environment jsdom
/** The collection's title cell: the Open button takes its room from the title
 * rather than covering it, and a filtered match whose parent is not drawn
 * says which record it is in. */

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

function renderTitle(context?: Map<string, string>) {
  const titles = new Map([[`${TASK}/p`, "Plan the budget"]])
  const [title] = buildColumns(task, [task], titles)
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
              children: "none",
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
  it("lays the Open button after the title, in the flow, never over it", () => {
    const { container } = renderTitle()
    const links = [...container.querySelectorAll("a")]
    const open = links.find((a) =>
      a.getAttribute("aria-label")?.startsWith("Open")
    )
    expect(open).toBeTruthy()
    expect(open!.className).not.toMatch(/\babsolute\b/)
    expect(open!.className).toMatch(/\bshrink-0\b/)
    // The title is the one that gives way.
    expect(links[0].className).toMatch(/\btruncate\b/)
    expect(links.indexOf(open!)).toBeGreaterThan(0)
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
