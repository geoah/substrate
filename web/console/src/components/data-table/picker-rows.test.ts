/** What the reference picker lists: the chosen first, then the page's rows
 * that hold the typed text, then what only the server's search found. */

import { describe, expect, it } from "vitest"

import { localMatch, pickerRows } from "./picker-rows"

const opt = (value: string, title: string) => ({
  value,
  title,
  description: "",
})

describe("pickerRows", () => {
  const page = [opt("ada", "Ada Lovelace"), opt("geoah", "geoah@example.com")]

  it("leads with the chosen, titled from the page or the title read", () => {
    const rows = pickerRows(
      ["gone", "ada"],
      new Map([["gone", "Gone Person"]]),
      page,
      [],
      ""
    )
    expect(rows.map((r) => [r.value, r.title])).toEqual([
      ["gone", "Gone Person"],
      ["ada", "Ada Lovelace"],
      ["geoah", "geoah@example.com"],
    ])
  })

  it("filters the page by the typed text, a fragment of an email included", () => {
    const rows = pickerRows([], new Map(), page, [], "oah@exa")
    expect(rows.map((r) => r.value)).toEqual(["geoah"])
  })

  it("adds what the server found past the page, once", () => {
    const rows = pickerRows(
      [],
      new Map(),
      page,
      [opt("geoah", "geoah@example.com"), opt("zed", "Zed")],
      "ge"
    )
    expect(rows.map((r) => r.value)).toEqual(["geoah", "zed"])
  })

  it("matches the id as well as the title", () => {
    expect(localMatch(opt("frances", "Frances Allen"), "FRAN")).toBe(true)
    expect(localMatch(opt("x1", "Frances Allen"), "x1")).toBe(true)
    expect(localMatch(opt("x1", "Frances Allen"), "zz")).toBe(false)
  })
})
