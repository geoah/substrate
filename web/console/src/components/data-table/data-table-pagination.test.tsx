// @vitest-environment jsdom
/** The numbered bar's contract (decision 0085): which numbers it draws, what
 * the range says, and the two places a BOUNDED count must not be believed —
 * a capped total can neither name a last page nor stop Next, because the
 * server's own cursor is what knows whether another page exists. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { DataTablePagination } from "./data-table-pagination"
import { pageItems } from "@/lib/pagination"

afterEach(cleanup)

describe("the page numbers", () => {
  it("draws every page while they fit, and elides once they do not", () => {
    expect(pageItems(1, 5)).toEqual([1, 2, 3, 4, 5])
    expect(pageItems(1, 7)).toEqual([1, 2, 3, 4, 5, 6, 7])
    // Past the window the first and the last are always drawn, so either end
    // is one click away from anywhere.
    expect(pageItems(1, 25)).toEqual([1, 2, 3, 4, 5, null, 25])
    expect(pageItems(13, 25)).toEqual([1, null, 12, 13, 14, null, 25])
    expect(pageItems(25, 25)).toEqual([1, null, 21, 22, 23, 24, 25])
  })

  it("keeps a constant width as the reader walks", () => {
    const widths = new Set(
      Array.from({ length: 40 }, (_, i) => pageItems(i + 1, 40).length)
    )
    expect([...widths]).toEqual([7])
  })
})

describe("the bar", () => {
  const base = {
    page: 3,
    pageSize: 50,
    rows: 50,
    total: 1234,
    hasNext: true,
    onPage: () => {},
  }

  it("says which rows are on screen, out of how many", () => {
    render(<DataTablePagination {...base} />)
    expect(screen.getByText("101–150")).toBeTruthy()
    expect(screen.getByText("1,234")).toBeTruthy()
  })

  it("jumps to the page whose number was clicked", () => {
    const onPage = vi.fn()
    render(<DataTablePagination {...base} onPage={onPage} />)
    fireEvent.click(screen.getByRole("button", { name: "Page 25" }))
    expect(onPage).toHaveBeenCalledWith(25)
  })

  it("marks the current page and offers no navigation off the ends", () => {
    render(<DataTablePagination {...base} page={1} hasNext={false} />)
    expect(
      screen
        .getByRole("button", { name: "Page 1" })
        .getAttribute("aria-current")
    ).toBe("page")
    for (const name of ["First page", "Previous page", "Next page"]) {
      expect(
        screen.getByRole("button", { name }).hasAttribute("disabled")
      ).toBe(true)
    }
  })

  it("does not claim a last page when the count is a floor", () => {
    // The size probe is a bounded walk, so `10000+` means "at least": a Last
    // button built on it would land in the middle of the collection.
    render(<DataTablePagination {...base} total={10000} totalCapped hasNext />)
    expect(screen.getByText("10,000+")).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Last page" }).hasAttribute("disabled")
    ).toBe(true)
    // Next still works: the page's own cursor says there is another one.
    expect(
      screen.getByRole("button", { name: "Next page" }).hasAttribute("disabled")
    ).toBe(false)
  })

  it("says where it is while the count is still unknown", () => {
    render(<DataTablePagination {...base} total={undefined} />)
    expect(screen.getByText("Page 3")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Page 3" })).toBeNull()
  })
})
