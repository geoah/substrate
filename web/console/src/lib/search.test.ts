// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"

import {
  DEFAULT_SEARCH_MODE,
  SEARCH_MODE_DESCRIPTION,
  SEARCH_MODE_DETAIL,
  SEARCH_MODES,
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

describe("the mode blurbs", () => {
  it("say what each arm does in the reader's words, the machinery kept for technical mode", () => {
    for (const mode of SEARCH_MODES) {
      expect(SEARCH_MODE_DESCRIPTION[mode]).not.toMatch(
        /embedding|arm|fused|provider/i
      )
      expect(SEARCH_MODE_DETAIL[mode]).toBeTruthy()
    }
    expect(SEARCH_MODE_DESCRIPTION.lexical).toBe(
      "Matches the words you typed. Instant."
    )
  })
})
