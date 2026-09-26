import { describe, expect, it } from "vitest"

import { historyFeedFilters } from "./use-history-feed"

describe("historyFeedFilters", () => {
  it("asks the pages for values only where the reader did", () => {
    expect(historyFeedFilters({ kinds: ["a/b/c"] }).pages.values).toBe(
      undefined
    )
    expect(historyFeedFilters({ kinds: ["a/b/c"] }, true).pages).toEqual({
      kinds: ["a/b/c"],
      values: true,
    })
  })

  it("never asks the live tail for values", () => {
    expect(historyFeedFilters({ values: true }, true).tail).toEqual({})
    expect(historyFeedFilters({ actors: ["x"] }, true).tail).toEqual({
      actors: ["x"],
    })
  })
})
