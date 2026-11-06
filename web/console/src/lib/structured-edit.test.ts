/** An edit inside a structured property is the property read back with one
 * thing changed: every container on the way copied, everything else the very
 * value that was read. */

import { describe, expect, it } from "vitest"

import {
  getIn,
  insertAt,
  moveAt,
  pathKey,
  removeAt,
  setIn,
  unsetIn,
} from "@/lib/structured-edit"

const stored = () => [
  {
    key: "walk",
    sets: [{ key: "w1", repsMin: 6, repsMax: 6, rirMin: "4" }],
    extra: { kept: [] },
  },
  { key: "squat", sets: [] },
]

describe("getIn", () => {
  it("reads through fields and indices, and undefined where nothing is", () => {
    expect(getIn(stored(), [0, "sets", 0, "repsMin"])).toBe(6)
    expect(getIn(stored(), [5, "key"])).toBeUndefined()
    expect(getIn(stored(), [0, "key", "deeper"])).toBeUndefined()
  })
})

describe("setIn", () => {
  it("writes one value and leaves the value it was given untouched", () => {
    const before = stored()
    const after = setIn(before, [0, "sets", 0, "repsMin"], 8) as ReturnType<
      typeof stored
    >
    expect(before[0].sets[0].repsMin).toBe(6)
    expect(after[0].sets[0]).toEqual({
      key: "w1",
      repsMin: 8,
      repsMax: 6,
      rirMin: "4",
    })
    // What the edit did not touch is the very value that was read.
    expect(after[1]).toBe(before[1])
    expect(after[0].extra).toBe(before[0].extra)
  })

  it("makes an object missing on the way", () => {
    expect(setIn({}, ["score", "value"], 3)).toEqual({ score: { value: 3 } })
    expect(setIn(undefined, ["a"], 1)).toEqual({ a: 1 })
  })

  it("refuses an index no item holds", () => {
    expect(() => setIn(stored(), [2, "key"], "x")).toThrow(RangeError)
  })
})

describe("unsetIn", () => {
  it("takes a field out of its object, never writing null or zero", () => {
    const after = unsetIn(stored(), [0, "sets", 0, "rirMin"]) as ReturnType<
      typeof stored
    >
    expect(after[0].sets[0]).toEqual({ key: "w1", repsMin: 6, repsMax: 6 })
    expect("rirMin" in after[0].sets[0]).toBe(false)
  })

  it("leaves the value as it was where the field is not held", () => {
    const before = stored()
    expect(unsetIn(before, [0, "nothing"])).toBe(before)
    expect(unsetIn(before, [9, "key"])).toBe(before)
  })
})

describe("insertAt", () => {
  it("inserts before an index, and at the end past it", () => {
    const after = insertAt(stored(), [0, "sets"], 0, { key: "w0" })
    expect(getIn(after, [0, "sets", 0, "key"])).toBe("w0")
    expect(getIn(after, [0, "sets", 1, "key"])).toBe("w1")
    const end = insertAt(stored(), [], 99, { key: "last" })
    expect(getIn(end, [2, "key"])).toBe("last")
  })

  it("makes a list nobody holds yet", () => {
    expect(insertAt(undefined, [], 0, { a: 1 })).toEqual([{ a: 1 }])
    expect(insertAt({}, ["sets"], 0, { key: "s1" })).toEqual({
      sets: [{ key: "s1" }],
    })
  })
})

describe("removeAt", () => {
  it("takes one item out, and an emptied list stays a list", () => {
    const after = removeAt(stored(), [0, "sets"], 0)
    expect(getIn(after, [0, "sets"])).toEqual([])
    expect(removeAt([{ a: 1 }], [], 0)).toEqual([])
  })

  it("leaves the value as it was for an index no item holds", () => {
    const before = stored()
    expect(removeAt(before, [], 7)).toBe(before)
  })
})

describe("moveAt", () => {
  it("moves one item and keeps the rest in order", () => {
    const after = moveAt([1, 2, 3], [], 0, 2)
    expect(after).toEqual([2, 3, 1])
    expect(moveAt([1, 2, 3], [], 2, 1)).toEqual([1, 3, 2])
  })

  it("leaves the value as it was for a move past either end", () => {
    const list = [1, 2]
    expect(moveAt(list, [], 0, -1)).toBe(list)
    expect(moveAt(list, [], 1, 2)).toBe(list)
  })
})

describe("pathKey", () => {
  it("joins the property and the path", () => {
    expect(pathKey("slots", [5, "sets", 0, "repsMin"])).toBe(
      "slots/5/sets/0/repsMin"
    )
  })
})
