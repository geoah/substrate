// @vitest-environment jsdom
/** The grid's pinned header: flush with the scroller's top (no spacing, no
 * margin, no offset) and covering a sliver above itself, so a scrolled row
 * never shows between the header and the scroller's edge. */

import {
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react"
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
    const { container } = render(
      <DataGrid table={result.current} label="Tasks" />
    )
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

describe("the pinned columns", () => {
  it("pins a leading control column and the title column after it, and resizes neither control", () => {
    const withBox: DataTableColumn<Row>[] = [
      {
        id: "select",
        header: "Select",
        cell: () => null,
        meta: { label: "Select", width: 36, fixed: true },
      },
      { id: "name", accessorFn: (r) => r.name, header: "Name" },
      { id: "other", accessorFn: (r) => r.id, header: "Other" },
    ]
    const { result } = renderHook(() =>
      useDataTable({
        columns: withBox,
        data: [{ id: "a", name: "A" }],
        getRowId: (r) => r.id,
      })
    )
    const { container } = render(
      <DataGrid table={result.current} label="Streams" />
    )
    const [box, name, other] = container.querySelectorAll("tbody td")
    for (const cell of [box, name]) {
      expect((cell as HTMLElement).className).toMatch(/\bsticky\b/)
    }
    expect((box as HTMLElement).style.left).toBe("0px")
    expect((name as HTMLElement).style.left).toBe("36px")
    expect((other as HTMLElement).className).not.toMatch(/\bsticky\b/)
    const heads = container.querySelectorAll("thead th")
    expect(heads[0].querySelector("[role=presentation]")).toBe(null)
    expect(heads[1].querySelector("[role=presentation]")).not.toBe(null)
    // A focused cell scrolls clear of both pinned columns.
    const region = screen.getByRole("region", { name: "Streams" })
    expect(parseFloat(region.style.scrollPaddingLeft)).toBe(36 + 160)
  })
})

describe("the sheet from the keyboard", () => {
  it("is a named, focusable region that scrolls a focused cell clear of the pinned column", () => {
    const { result } = renderHook(() =>
      useDataTable({
        columns,
        data: [{ id: "a", name: "A" }],
        getRowId: (r) => r.id,
      })
    )
    render(<DataGrid table={result.current} label="Tasks" />)
    const region = screen.getByRole("region", { name: "Tasks" })
    expect(region.tabIndex).toBe(0)
    expect(region.style.scrollPaddingTop).toBe("34px")
    expect(parseFloat(region.style.scrollPaddingLeft)).toBeGreaterThan(0)
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
        label="Tasks"
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

describe("the sorted column", () => {
  it("says which column is sorted and which way, and nothing on the rest", () => {
    const { result, rerender } = renderHook(
      ({ sorting }: { sorting: { id: string; desc: boolean }[] }) =>
        useDataTable({
          columns,
          data: [{ id: "a", name: "A" }],
          getRowId: (r) => r.id,
          sorting,
        }),
      { initialProps: { sorting: [{ id: "name", desc: true }] } }
    )
    const { rerender: redraw } = render(
      <DataGrid table={result.current} label="Tasks" />
    )
    const sortOf = () =>
      screen
        .getAllByRole("columnheader")
        .map((th) => th.getAttribute("aria-sort"))
    expect(sortOf()).toEqual(["descending", null])
    rerender({ sorting: [{ id: "other", desc: false }] })
    redraw(<DataGrid table={result.current} label="Tasks" />)
    expect(sortOf()).toEqual([null, "ascending"])
  })
})

describe("groups", () => {
  it("heads each run of rows, counts it and folds it", () => {
    const data = [
      { id: "a", name: "high" },
      { id: "b", name: "high" },
      { id: "c", name: "low" },
    ]
    const { result } = renderHook(() =>
      useDataTable({ columns, data, getRowId: (r) => r.id })
    )
    const collapsed = new Set<string>()
    const toggled: string[] = []
    const draw = () => (
      <DataGrid
        table={result.current}
        label="Tasks"
        groups={{
          keyOf: (r) => r.name,
          head: (key) => <span>{`${key} group`}</span>,
          label: (key) => `Priority: ${key}`,
          collapsed,
          onToggle: (key) => toggled.push(key),
        }}
      />
    )
    const { container, rerender } = render(draw())
    const bodies = container.querySelectorAll("tbody[data-slot=grid-group]")
    expect(bodies).toHaveLength(2)
    expect(
      [...bodies].map((b) => b.querySelectorAll("[data-slot=grid-row]").length)
    ).toEqual([2, 1])
    expect(screen.getAllByRole("rowheader").map((h) => h.textContent)).toEqual([
      "high group",
      "low group",
    ])
    fireEvent.click(screen.getByRole("button", { name: "Hide Priority: high" }))
    expect(toggled).toEqual(["high"])
    collapsed.add("high")
    rerender(draw())
    expect(
      screen
        .getByRole("button", { name: "Show Priority: high" })
        .getAttribute("aria-expanded")
    ).toBe("false")
    expect(container.querySelectorAll("[data-slot=grid-row]")).toHaveLength(1)
  })
})
