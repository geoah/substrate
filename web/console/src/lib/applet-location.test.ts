import { describe, expect, it } from "vitest"

import {
  appletLocation,
  appletLocationKey,
  appletLocationSearch,
  appletSearch,
} from "./applet-location"

describe("an applet's address", () => {
  it("keeps the screen and the p. params, and drops the rest", () => {
    expect(
      appletSearch({ screen: "book", "p.id": "b1", other: 1, "p.": 2 })
    ).toEqual({ screen: "book", "p.id": "b1" })
  })

  it("reads a location and writes it back", () => {
    const location = appletLocation({ screen: "book", "p.id": "b1", "p.n": 2 })
    expect(location).toEqual({ screen: "book", params: { id: "b1", n: 2 } })
    expect(appletLocationSearch(location)).toEqual({
      screen: "book",
      "p.id": "b1",
      "p.n": 2,
    })
    expect(appletLocation({})).toEqual({ screen: undefined, params: undefined })
  })

  it("keys a location whatever order its params came in", () => {
    expect(appletLocationKey({ screen: "s", params: { a: 1, b: 2 } })).toBe(
      appletLocationKey({ screen: "s", params: { b: 2, a: 1 } })
    )
    expect(appletLocationKey({ screen: "s" })).toBe(
      appletLocationKey({ screen: "s", params: {} })
    )
  })
})
