/** The toolbar's contract, pinned after the 2026-08-06 redlines: the × is a
 * real button that removes its filter (it used to be an svg inside the
 * trigger Button, dead under `[&_svg]:pointer-events-none`), and one active
 * filter is enough to earn the toolbar-level "Clear all". */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { DataTableFilters } from "./data-table-filters"
import type { ActiveFilter } from "@/lib/filters"
import type { DeclaredProperty } from "@/lib/definition"

const fields: DeclaredProperty[] = [
  { name: "title", kind: "string", repeated: false },
  { name: "prominence", kind: "state", repeated: false, states: ["known"] },
]

const titleFilter: ActiveFilter = { field: "title", op: "eq", value: "geo" }

afterEach(cleanup)

describe("removing an applied filter", () => {
  it("the × is a real button and removes exactly its filter", () => {
    const onChange = vi.fn()
    render(
      <DataTableFilters
        fields={fields}
        filters={[
          titleFilter,
          { field: "prominence", op: "eq", value: "known" },
        ]}
        onChange={onChange}
      />
    )

    const x = screen.getByRole("button", { name: "Remove title filter" })
    // The regression: an svg with role=button inside the trigger Button sits
    // under the Button's `[&_svg]:pointer-events-none` and can never be hit.
    // A native sibling <button> is hittable by construction.
    expect(x.tagName).toBe("BUTTON")
    expect(x.closest("[data-slot=popover-trigger]")).toBeNull()

    fireEvent.click(x)
    expect(onChange).toHaveBeenCalledExactlyOnceWith([
      { field: "prominence", op: "eq", value: "known" },
    ])
  })

  it("a prefix filter wears its trailing * in the control", () => {
    render(
      <DataTableFilters
        fields={fields}
        filters={[{ field: "title", op: "prefix", value: "geo" }]}
        onChange={vi.fn()}
      />
    )
    expect(screen.getByText("geo*")).toBeTruthy()
  })
})

describe("Clear all", () => {
  it("shows with one filter or more and clears the lot", () => {
    const onChange = vi.fn()
    render(
      <DataTableFilters
        fields={fields}
        filters={[titleFilter]}
        onChange={onChange}
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "Clear all" }))
    expect(onChange).toHaveBeenCalledExactlyOnceWith([])
  })

  it("stays out of an empty toolbar", () => {
    render(<DataTableFilters fields={fields} filters={[]} onChange={vi.fn()} />)
    expect(screen.queryByRole("button", { name: "Clear all" })).toBeNull()
  })
})
