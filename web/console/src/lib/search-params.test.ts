import { describe, expect, it } from "vitest"

import { searchText } from "./search-params"

describe("searchText", () => {
  it("reads every value the router parses as the text it came from", () => {
    expect(searchText("hello")).toBe("hello")
    expect(searchText(2024)).toBe("2024")
    expect(searchText(true)).toBe("true")
    expect(searchText(false)).toBe("false")
    expect(searchText(null)).toBe("null")
    expect(searchText({ task: "recipes" })).toBe('{"task":"recipes"}')
    expect(searchText([1, 2])).toBe("[1,2]")
  })

  it("drops what is absent or empty", () => {
    expect(searchText(undefined)).toBeUndefined()
    expect(searchText("")).toBeUndefined()
  })
})
