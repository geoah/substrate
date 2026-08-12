/** The table instance's column prefs: visibility and order edits round-trip
 * localStorage per surface, defaults apply until overridden, and a reset
 * clears the store — the owner-ruled behavior every table shares. */

import { act, renderHook } from "@testing-library/react"
import type { ColumnDef } from "@tanstack/react-table"
import { afterEach, describe, expect, it } from "vitest"

import { useDataTable } from "./data-table"

interface Row {
  id: string
  a: string
  b: string
  c: string
}

const COLUMNS: ColumnDef<Row, unknown>[] = [
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
    expect(result.current.getState().columnSizing).toEqual({ b: 260 })
    expect(JSON.parse(localStorage.getItem("substrate.table.t")!)).toEqual({
      sizing: { b: 260 },
    })

    const fresh = renderHook(() =>
      useDataTable({ columns: COLUMNS, data: DATA, prefsKey: "t" })
    )
    expect(fresh.result.current.getState().columnSizing).toEqual({ b: 260 })
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
    expect(result.current.getState().columnSizing).toEqual({})
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
    expect(result.current.getState().columnSizing).toEqual({})
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
