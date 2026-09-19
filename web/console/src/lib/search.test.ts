// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"

import {
  DEFAULT_SEARCH_MODE,
  isSearchMode,
  loadSearchMode,
  saveSearchMode,
} from "./search"

describe("the search mode preference", () => {
  beforeEach(() => localStorage.clear())

  it("is words by default: free, and it answers on every repository", () => {
    expect(DEFAULT_SEARCH_MODE).toBe("lexical")
    expect(loadSearchMode()).toBe("lexical")
  })

  it("sticks once chosen, and the default clears the entry", () => {
    saveSearchMode("hybrid")
    expect(loadSearchMode()).toBe("hybrid")
    expect(localStorage.getItem("substrate.search.mode")).toBe("hybrid")
    saveSearchMode("lexical")
    expect(loadSearchMode()).toBe("lexical")
    expect(localStorage.getItem("substrate.search.mode")).toBeNull()
  })

  it("ignores a value the server would refuse", () => {
    localStorage.setItem("substrate.search.mode", "bm25")
    expect(loadSearchMode()).toBe("lexical")
    expect(isSearchMode("semantic")).toBe(true)
    expect(isSearchMode("Lexical")).toBe(false)
    expect(isSearchMode(null)).toBe(false)
  })
})
