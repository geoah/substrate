// @vitest-environment jsdom
/** The Columns menu: the columns that hold something lead, the empty ones
 * wait under one disclosure at the end, and the button says how many are
 * hidden, so the footer never has to. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { useDataTable, type DataTableColumn } from "./data-table"
import { DataTableViewOptions } from "./data-table-view-options"

interface Row {
  id: string
  a: string
  b: string
  c: string
}

const COLUMNS: DataTableColumn<Row>[] = ["a", "b", "c"].map((id) => ({
  id,
  accessorFn: (r: Row) => r[id as "a" | "b" | "c"],
  meta: { label: id.toUpperCase() },
}))

function Menu({ autoHidden }: { autoHidden: string[] }) {
  const table = useDataTable({
    columns: COLUMNS,
    data: [{ id: "1", a: "x", b: "", c: "" }],
    autoHidden,
  })
  return <DataTableViewOptions table={table} toolbar />
}

afterEach(() => {
  cleanup()
  localStorage.clear()
})

describe("DataTableViewOptions", () => {
  it("badges the hidden count on the button", () => {
    render(<Menu autoHidden={["b", "c"]} />)
    const button = screen.getByRole("button", {
      name: "Configure columns, 2 hidden",
    })
    expect(button.textContent).toContain("2 hidden")
  })

  it("keeps the empty columns under one disclosure at the end", () => {
    render(<Menu autoHidden={["b", "c"]} />)
    fireEvent.click(screen.getByRole("button", { name: /Configure columns/ }))
    expect(screen.queryByText("B")).toBeNull()
    const fold = screen.getByRole("button", { name: "2 empty" })
    expect(fold.getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(fold)
    const group = screen.getByRole("group", { name: "Empty columns" })
    expect(group.textContent).toContain("B")
    expect(group.textContent).toContain("C")
    // Turning one on brings it up among the columns that show.
    fireEvent.click(screen.getByRole("checkbox", { name: "B" }))
    expect(group.textContent).not.toContain("B")
    expect(screen.getByRole("button", { name: "Move B up" })).toBeTruthy()
  })

  it("says nothing of hidden columns when none are", () => {
    render(<Menu autoHidden={[]} />)
    expect(
      screen.getByRole("button", { name: "Configure columns" }).textContent
    ).not.toContain("hidden")
  })
})
