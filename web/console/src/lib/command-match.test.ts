import { describe, expect, it } from "vitest"

import { matchesWordPrefixes, typeAheadQuery, words } from "./command-match"

describe("matchesWordPrefixes", () => {
  it("matches every typed word against the start of a word", () => {
    expect(matchesWordPrefixes("dri", "Drive files")).toBe(true)
    expect(matchesWordPrefixes("fi dri", "Drive files")).toBe(true)
    expect(matchesWordPrefixes("DATA", "All data")).toBe(true)
  })

  it("never matches letters picked out of the middle of words", () => {
    expect(matchesWordPrefixes("lisb", "Drive files")).toBe(false)
    expect(matchesWordPrefixes("iles", "Drive files")).toBe(false)
    expect(matchesWordPrefixes("drive x", "Drive files")).toBe(false)
  })

  it("matches everything when nothing is typed", () => {
    expect(matchesWordPrefixes("  ", "Home")).toBe(true)
  })

  it("splits a reference into its words", () => {
    expect(words("ada.example.com/tasks/task")).toEqual([
      "ada",
      "example",
      "com",
      "tasks",
      "task",
    ])
    expect(matchesWordPrefixes("tasks/ta", "ada.example.com/tasks/task")).toBe(
      true
    )
  })
})

describe("typeAheadQuery", () => {
  it("matches the word being typed as a prefix", () => {
    expect(typeAheadQuery("lisb")).toBe("lisb*")
    expect(typeAheadQuery("prepare lisb")).toBe("prepare lisb*")
    expect(typeAheadQuery("  lisb")).toBe("lisb*")
  })

  it("leaves the grammar the reader wrote alone", () => {
    expect(typeAheadQuery("lisbon ")).toBe("lisbon")
    expect(typeAheadQuery("lay*")).toBe("lay*")
    expect(typeAheadQuery('"rack lay')).toBe('"rack lay')
    expect(typeAheadQuery('"rack layout"')).toBe('"rack layout"')
    expect(typeAheadQuery("rack -lunch")).toBe("rack -lunch")
    expect(typeAheadQuery("rack OR")).toBe("rack OR")
    expect(typeAheadQuery("")).toBe("")
  })
})
