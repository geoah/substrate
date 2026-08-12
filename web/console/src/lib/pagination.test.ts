import { describe, expect, it } from "vitest"

import { pageItems } from "./pagination"

describe("pageItems", () => {
  it("lists every page while they fit", () => {
    expect(pageItems(1, 1)).toEqual([1])
    expect(pageItems(3, 7)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it("windows around the current page with ellipses", () => {
    expect(pageItems(1, 86)).toEqual([1, 2, "…", 86])
    expect(pageItems(40, 86)).toEqual([1, "…", 39, 40, 41, "…", 86])
    expect(pageItems(86, 86)).toEqual([1, "…", 85, 86])
  })

  it("never doubles the edge pages into the window", () => {
    expect(pageItems(2, 10)).toEqual([1, 2, 3, "…", 10])
    expect(pageItems(9, 10)).toEqual([1, "…", 8, 9, 10])
  })
})
