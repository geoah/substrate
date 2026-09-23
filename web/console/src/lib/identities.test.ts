import { describe, expect, it } from "vitest"

import { typeaheadQuery } from "./identities"

describe("typeaheadQuery", () => {
  it("makes every plain word a prefix, so a name is found before it is finished", () => {
    expect(typeaheadQuery("gra")).toBe("gra*")
    expect(typeaheadQuery("  ada  love ")).toBe("ada* love*")
  })

  it("leaves a word that already carries an operator as written", () => {
    expect(typeaheadQuery('-draft "weekly sync" lay* a OR b')).toBe(
      '-draft "weekly sync" lay* a* OR b*'
    )
  })

  it("is empty for nothing typed", () => {
    expect(typeaheadQuery("   ")).toBe("")
  })
})
