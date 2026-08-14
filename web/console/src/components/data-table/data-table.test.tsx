/** The table instance's column prefs: visibility and order edits round-trip
 * localStorage per surface, defaults apply until overridden, and a reset
 * clears the store — the owner-ruled behavior every table shares. */

import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react"
import type { OnChangeFn, SortingState } from "@tanstack/react-table"
import { afterEach, describe, expect, it } from "vitest"

import { DataTable, useDataTable, type DataTableColumn } from "./data-table"
import { DataTableColumnHeader } from "./data-table-column-header"

interface Row {
  id: string
  a: string
  b: string
  c: string
}

const COLUMNS: DataTableColumn<Row>[] = [
  { id: "a", accessorFn: (r) => r.a },
  { id: "b", accessorFn: (r) => r.b },
  { id: "c", accessorFn: (r) => r.c },
]

const DATA: Row[] = [{ id: "1", a: "x", b: "y", c: "z" }]

afterEach(() => {
  localStorage.clear()
})

describe("useDataTable column prefs", () => {
  it("shows all columns in natural order by default", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    expect(result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual([
      "a",
      "b",
      "c",
    ])
  })

  it("persists a visibility toggle and restores it on a fresh instance", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    act(() => result.current.getColumn("b")!.toggleVisibility(false))
    expect(result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual([
      "a",
      "c",
    ])
    expect(JSON.parse(localStorage.getItem("substrate.table.t")!)).toEqual({
      hidden: ["b"],
    })

    // A brand-new mount (another visit) reads the store.
    const fresh = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    expect(
      fresh.result.current.getVisibleLeafColumns().map((c) => c.id)
    ).toEqual(["a", "c"])
  })

  it("persists a reorder and restores it on a fresh instance", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    act(() => result.current.setColumnOrder(["c", "a", "b"]))
    expect(result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual([
      "c",
      "a",
      "b",
    ])
    expect(JSON.parse(localStorage.getItem("substrate.table.t")!)).toEqual({
      order: ["c", "a", "b"],
    })

    const fresh = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    expect(
      fresh.result.current.getVisibleLeafColumns().map((c) => c.id)
    ).toEqual(["c", "a", "b"])
  })

  it("clears the store when the view returns to defaults", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    act(() => result.current.setColumnOrder(["c", "a", "b"]))
    act(() => result.current.setColumnOrder(["a", "b", "c"]))
    expect(localStorage.getItem("substrate.table.t")).toBeNull()
  })

  it("applies defaultHidden until the user overrides, then remembers", () => {
    const { result } = renderHook(() =>
      useDataTable({
        columns: COLUMNS,
        data: DATA,
        prefsKey: "t",
        defaultHidden: ["c"],
      })
    )
    expect(result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual([
      "a",
      "b",
    ])
    // Showing the default-hidden column stores an explicit empty hidden.
    act(() => result.current.getColumn("c")!.toggleVisibility(true))
    expect(result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual([
      "a",
      "b",
      "c",
    ])
    expect(JSON.parse(localStorage.getItem("substrate.table.t")!)).toEqual({
      hidden: [],
    })
    const fresh = renderHook(() =>
      useDataTable({
        columns: COLUMNS,
        data: DATA,
        prefsKey: "t",
        defaultHidden: ["c"],
      })
    )
    expect(
      fresh.result.current.getVisibleLeafColumns().map((c) => c.id)
    ).toEqual(["a", "b", "c"])
  })

  it("persists a drag-resize override and restores it on a fresh instance", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    act(() => result.current.setColumnSizing((old) => ({ ...old, b: 260 })))
    expect(result.current.state.columnSizing).toEqual({ b: 260 })
    expect(JSON.parse(localStorage.getItem("substrate.table.t")!)).toEqual({
      sizing: { b: 260 },
    })

    const fresh = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    expect(fresh.result.current.state.columnSizing).toEqual({ b: 260 })
  })

  it("clears a single override (double-click) and empties the store", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    act(() => result.current.setColumnSizing({ b: 260 }))
    act(() =>
      result.current.setColumnSizing((old) => {
        const next = { ...old }
        delete next.b
        return next
      })
    )
    expect(result.current.state.columnSizing).toEqual({})
    expect(localStorage.getItem("substrate.table.t")).toBeNull()
  })

  it("resets order, visibility AND sizing atomically, clearing the store", () => {
    const { result } = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    act(() => result.current.setColumnOrder(["c", "a", "b"]))
    act(() => result.current.getColumn("b")!.toggleVisibility(false))
    act(() => result.current.setColumnSizing({ a: 300 }))
    expect(localStorage.getItem("substrate.table.t")).not.toBeNull()
    act(() => result.current.options.meta!.resetColumnPrefs!())
    expect(result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual([
      "a",
      "b",
      "c",
    ])
    expect(result.current.state.columnSizing).toEqual({})
    expect(localStorage.getItem("substrate.table.t")).toBeNull()
  })

  it("keeps prefs per surface", () => {
    const one = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "one" })
    )
    act(() => one.result.current.getColumn("a")!.toggleVisibility(false))
    const two = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "two" })
    )
    expect(two.result.current.getVisibleLeafColumns().map((c) => c.id)).toEqual(
      ["a", "b", "c"]
    )
  })
})

/** Sorting is the one instance feature the server owns (`manualSorting`), so
 * the header's job is only to hand the page the next SortingState — and with
 * `enableSortingRemoval: false` a third click never returns to unsorted. */
describe("useDataTable sorting", () => {
  const SORTABLE: DataTableColumn<Row>[] = [
    {
      id: "a",
      accessorFn: (r) => r.a,
      enableSorting: true,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title="a" />
      ),
    },
  ]

  function Surface({
    sorting,
    onSortingChange,
  }: {
    sorting: SortingState
    onSortingChange: OnChangeFn<SortingState>
  }) {
    const table = useDataTable({
      columns: SORTABLE,
      data: DATA,
      sorting,
      onSortingChange,
    })
    return <DataTable table={table} />
  }

  /** What the header hands back for a column already sorted this way. */
  function nextSorting(sorting: SortingState): SortingState {
    let received: SortingState = []
    render(
      <Surface
        sorting={sorting}
        onSortingChange={(updater) => {
          received = typeof updater === "function" ? updater(sorting) : updater
        }}
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "a" }))
    cleanup()
    return received
  }

  it("sorts ascending first, then flips, and never back to unsorted", () => {
    expect(nextSorting([])).toEqual([{ id: "a", desc: false }])
    expect(nextSorting([{ id: "a", desc: false }])).toEqual([
      { id: "a", desc: true },
    ])
    expect(nextSorting([{ id: "a", desc: true }])).toEqual([
      { id: "a", desc: false },
    ])
  })
})
