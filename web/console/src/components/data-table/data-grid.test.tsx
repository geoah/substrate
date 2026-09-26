// @vitest-environment jsdom
/** The grid's pinned header: flush with the scroller's top (no spacing, no
 * margin, no offset) and covering a sliver above itself, so a scrolled row
 * never shows between the header and the scroller's edge. */

import { cleanup, render, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { DataGrid } from "./data-grid"
import { useDataTable, type DataTableColumn } from "./data-table"

afterEach(cleanup)

interface Row {
  id: string
  name: string
}

const columns: DataTableColumn<Row>[] = [
  { id: "name", accessorFn: (r) => r.name, header: "Name" },
  { id: "other", accessorFn: (r) => r.id, header: "Other" },
]

describe("the pinned header", () => {
  it("sits flush at the top of the scroller and covers above itself", () => {
    const { result } = renderHook(() =>
      useDataTable({
        columns,
        data: [{ id: "a", name: "A" }],
        getRowId: (r) => r.id,
      })
    )
    const { container } = render(<DataGrid table={result.current} />)
    const scroller = container.querySelector("[data-slot=data-grid]")!
    expect(scroller.className).not.toMatch(/\b(p|pt|py)-/)
    const table = scroller.querySelector("table")!
    expect(table.className).toMatch(/\bborder-spacing-0\b/)
    for (const th of scroller.querySelectorAll("th")) {
      expect(th.className).toMatch(/\bsticky\b/)
      expect(th.className).toMatch(/\btop-0\b/)
      expect(th.className).toMatch(/\bbg-background\b/)
      expect(th.className).toContain("0_-2px_0_var(--background)")
    }
  })
})

describe("changed rows", () => {
  it("tints a fresh row opaquely and eases a fading one back", () => {
    const { result } = renderHook(() =>
      useDataTable({
        columns,
        data: [
          { id: "a", name: "A" },
          { id: "b", name: "B" },
          { id: "c", name: "C" },
        ],
        getRowId: (r) => r.id,
      })
    )
    const { container } = render(
      <DataGrid
        table={result.current}
        marks={
          new Map([
            ["a", "fresh"],
            ["b", "fading"],
          ])
        }
      />
    )
    const rows = [...container.querySelectorAll("tbody tr")]
    expect(rows.map((r) => r.getAttribute("data-changed"))).toEqual([
      "fresh",
      "fading",
      null,
    ])
    const [fresh, fading, still] = rows.map((r) => r.querySelector("td")!)
    expect(fresh.className).toContain("var(--primary)")
    expect(fresh.className).not.toMatch(/\bbg-background\b/)
    expect(fading.className).toMatch(/\bbg-background\b/)
    expect(fading.className).toContain("transition-[background-color]")
    expect(still.className).not.toContain("transition-[background-color]")
  })
})
