// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"

import {
  DEFAULT_SEARCH_MODE,
  EVERYDAY_PURPOSES,
  QUICK_PURPOSES,
  searchPurposes,
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

describe("what a search reaches", () => {
  it("is what you keep and its details, in both modes, by default", () => {
    expect(EVERYDAY_PURPOSES).toEqual(["primary", "supporting"])
    for (const technical of [false, true]) {
      expect(searchPurposes({ technical, includeSystem: false })).toEqual(
        EVERYDAY_PURPOSES
      )
    }
  })

  it("adds the substrate's own records only on request in technical mode", () => {
    expect(
      searchPurposes({ technical: true, includeSystem: true })
    ).toBeUndefined()
    expect(searchPurposes({ technical: false, includeSystem: true })).toEqual(
      EVERYDAY_PURPOSES
    )
  })

  it("is the primary kinds alone in ⌘K and the @ picker", () => {
    expect(QUICK_PURPOSES).toEqual(["primary"])
    for (const technical of [false, true]) {
      expect(
        searchPurposes({ quick: true, technical, includeSystem: false })
      ).toEqual(QUICK_PURPOSES)
    }
    expect(
      searchPurposes({ quick: true, technical: true, includeSystem: true })
    ).toBeUndefined()
    expect(
      searchPurposes({
        quick: true,
        narrowed: true,
        technical: false,
        includeSystem: false,
      })
    ).toBeUndefined()
  })

  it("searches a collection picked by name whatever its purpose", () => {
    expect(
      searchPurposes({ narrowed: true, technical: false, includeSystem: false })
    ).toBeUndefined()
  })
})
