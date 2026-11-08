import { describe, expect, it } from "vitest"
import { comparePositions, positionAfter, positionBetween } from "./position"

describe("positions", () => {
  it("orders as numbers, an absent one last", () => {
    expect(comparePositions("2", "10")).toBeLessThan(0)
    expect(comparePositions("1.5", "1.25")).toBeGreaterThan(0)
    expect(comparePositions("-1", "0")).toBeLessThan(0)
    expect(comparePositions("1.0", "1")).toBe(0)
    expect(comparePositions(undefined, "1")).toBeGreaterThan(0)
    expect(comparePositions("1", undefined)).toBeLessThan(0)
  })

  it("puts a row between its neighbours exactly", () => {
    expect(positionBetween("1", "2")).toBe("1.5")
    expect(positionBetween("1", "1.5")).toBe("1.25")
    expect(positionBetween("-1", "1")).toBe("0")
    expect(positionBetween(undefined, undefined)).toBe("1")
    expect(positionBetween("3.7", undefined)).toBe("4")
    expect(positionBetween(undefined, "3")).toBe("2")
  })

  it("never runs out of room halving", () => {
    let low = "1"
    const high = "2"
    for (let i = 0; i < 80; i++) {
      const mid = positionBetween(low, high)
      expect(comparePositions(low, mid)).toBeLessThan(0)
      expect(comparePositions(mid, high)).toBeLessThan(0)
      low = mid
    }
  })

  it("places after the highest", () => {
    expect(positionAfter(["3", undefined, "10", "2.5"])).toBe("11")
    expect(positionAfter([])).toBe("1")
  })
})
