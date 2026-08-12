import { afterEach, describe, expect, it } from "vitest"

import {
  columnVisibilityOf,
  loadTablePrefs,
  orderedColumns,
  prefsDelta,
  saveTablePrefs,
} from "./table-prefs"

const IDS = ["time", "actor", "action", "record", "summary"]

afterEach(() => {
  localStorage.clear()
})

describe("orderedColumns", () => {
  it("returns the natural order when nothing is stored", () => {
    expect(orderedColumns(IDS)).toEqual(IDS)
    expect(orderedColumns(IDS, [])).toEqual(IDS)
  })

  it("applies a stored order", () => {
    expect(
      orderedColumns(IDS, ["record", "time", "actor", "action", "summary"])
    ).toEqual(["record", "time", "actor", "action", "summary"])
  })

  it("drops ids that no longer exist", () => {
    expect(orderedColumns(["a", "b"], ["b", "ghost", "a"])).toEqual(["b", "a"])
  })

  it("slots columns the store never saw at their natural position", () => {
    // "action" is new since the prefs were stored; it sits back between
    // "actor" and "record", not at an edge.
    expect(orderedColumns(IDS, ["summary", "time", "actor", "record"])).toEqual(
      ["summary", "time", "actor", "action", "record"]
    )
  })

  it("puts a new leading column at the front when its predecessor is gone", () => {
    expect(orderedColumns(["new", "a", "b"], ["b", "a"])).toEqual([
      "new",
      "b",
      "a",
    ])
  })
})

describe("columnVisibilityOf", () => {
  it("shows everything by default", () => {
    expect(columnVisibilityOf(["a", "b"])).toEqual({ a: true, b: true })
  })

  it("hides the stored list, ignoring unknown ids", () => {
    expect(columnVisibilityOf(["a", "b"], ["b", "ghost"])).toEqual({
      a: true,
      b: false,
    })
  })
})

describe("prefsDelta", () => {
  it("stores nothing for an untouched surface", () => {
    const delta = prefsDelta(IDS, IDS, columnVisibilityOf(IDS))
    expect(delta).toEqual({})
  })

  it("stores order only when it differs from natural", () => {
    const shuffled = ["actor", "time", "action", "record", "summary"]
    expect(prefsDelta(IDS, shuffled, columnVisibilityOf(IDS)).order).toEqual(
      shuffled
    )
  })

  it("stores hidden only when it differs from the surface default", () => {
    const hiddenGroup = columnVisibilityOf(IDS, ["summary"])
    expect(prefsDelta(IDS, IDS, hiddenGroup, ["summary"])).toEqual({})
    expect(prefsDelta(IDS, IDS, hiddenGroup)).toEqual({ hidden: ["summary"] })
  })

  it("stores an explicit EMPTY hidden when the default hides something", () => {
    // The surface hides "summary" by default; the user showed everything.
    const allVisible = columnVisibilityOf(IDS)
    expect(prefsDelta(IDS, IDS, allVisible, ["summary"])).toEqual({
      hidden: [],
    })
  })

  it("stores sizing only for columns that still exist", () => {
    const vis = columnVisibilityOf(IDS)
    expect(prefsDelta(IDS, IDS, vis, [], { actor: 220, ghost: 500 })).toEqual({
      sizing: { actor: 220 },
    })
    expect(prefsDelta(IDS, IDS, vis, [], { ghost: 500 })).toEqual({})
    expect(prefsDelta(IDS, IDS, vis, [], {})).toEqual({})
  })
})

describe("save/load round-trip", () => {
  it("round-trips order and hidden per surface", () => {
    saveTablePrefs("changelog", { order: ["b", "a"], hidden: ["c"] })
    expect(loadTablePrefs("changelog")).toEqual({
      order: ["b", "a"],
      hidden: ["c"],
    })
    expect(loadTablePrefs("other")).toBeNull()
  })

  it("keeps an explicit empty hidden array (show-everything override)", () => {
    saveTablePrefs("changelog", { hidden: [] })
    expect(loadTablePrefs("changelog")).toEqual({ hidden: [] })
  })

  it("removes the entry entirely on an all-default save", () => {
    saveTablePrefs("changelog", { order: ["b", "a"] })
    saveTablePrefs("changelog", {})
    expect(localStorage.getItem("substrate.table.changelog")).toBeNull()
    expect(loadTablePrefs("changelog")).toBeNull()
  })

  it("survives garbage in the store", () => {
    localStorage.setItem("substrate.table.changelog", "{not json")
    expect(loadTablePrefs("changelog")).toBeNull()
    localStorage.setItem(
      "substrate.table.changelog",
      JSON.stringify({ order: [1] })
    )
    expect(loadTablePrefs("changelog")).toBeNull()
  })

  it("round-trips sizing alongside order and hidden", () => {
    saveTablePrefs("changelog", { order: ["b", "a"], sizing: { a: 240 } })
    expect(loadTablePrefs("changelog")).toEqual({
      order: ["b", "a"],
      sizing: { a: 240 },
    })
    // sizing alone keeps the entry; clearing it clears the entry.
    saveTablePrefs("changelog", { sizing: { a: 240 } })
    expect(loadTablePrefs("changelog")).toEqual({ sizing: { a: 240 } })
    saveTablePrefs("changelog", { sizing: {} })
    expect(localStorage.getItem("substrate.table.changelog")).toBeNull()
  })

  it("drops malformed sizing values on load, keeping the valid ones", () => {
    localStorage.setItem(
      "substrate.table.changelog",
      JSON.stringify({
        sizing: { a: 240.6, b: "wide", c: -10, d: NaN, e: null },
      })
    )
    expect(loadTablePrefs("changelog")).toEqual({ sizing: { a: 241 } })
    localStorage.setItem(
      "substrate.table.changelog",
      JSON.stringify({ sizing: { b: "wide" } })
    )
    expect(loadTablePrefs("changelog")).toBeNull()
    localStorage.setItem(
      "substrate.table.changelog",
      JSON.stringify({ sizing: [240] })
    )
    expect(loadTablePrefs("changelog")).toBeNull()
  })
})
