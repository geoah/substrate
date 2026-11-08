import { comparePositions } from "@substrate/client"
import { describe, expect, it } from "vitest"
import { moves } from "./position"

/** The order the rows draw in once `writes` are laid over `positions`. */
function drawn(
  positions: Record<string, string | undefined>,
  writes: Record<string, string>
): string[] {
  const p = { ...positions, ...writes }
  return Object.keys(p).sort(
    (a, b) => comparePositions(p[a], p[b]) || (a < b ? -1 : 1)
  )
}

describe("moves", () => {
  const pos = { a: "1", b: "2", c: "3", d: "4" }
  const order = ["a", "b", "c", "d"]

  it("writes the moved record alone where its neighbours leave room", () => {
    expect(moves("d", 0, order, pos)).toEqual({ d: "0" })
    expect(moves("a", 3, order, pos)).toEqual({ a: "5" })
    expect(moves("d", 1, order, pos)).toEqual({ d: "1.5" })
  })

  it("gives a number to a record before it that has none", () => {
    const p = { a: undefined, b: "2", c: "3" }
    const w = moves("c", 1, ["a", "b", "c"], p)
    expect(w).toEqual({ a: "1", c: "1.5" })
    expect(drawn(p, w)).toEqual(["a", "c", "b"])
  })

  it("lifts a record after it that two devices gave the same number", () => {
    const p = { a: "1", b: "2", c: "2", d: "3" }
    const w = moves("d", 2, ["a", "b", "c", "d"], p)
    expect(drawn(p, w)).toEqual(["a", "b", "d", "c"])
  })

  it("spreads the records out when every number is the same", () => {
    const p = { a: "1", b: "1", c: "1" }
    const w = moves("a", 2, ["a", "b", "c"], p)
    expect(drawn(p, w)).toEqual(["b", "c", "a"])
  })

  it("keeps a gap halved again and again in order and within 20 digits", () => {
    let p: Record<string, string | undefined> = { a: "1", b: "2", x: "3" }
    for (let i = 0; i < 80; i++) {
      p = { ...p, ...moves("x", 1, drawn(p, {}), p) }
      expect(drawn(p, {})[1]).toBe("x")
      p = { ...p, ...moves("b", 1, drawn(p, {}), p) }
      expect(drawn(p, {})[1]).toBe("b")
    }
    for (const v of Object.values(p))
      expect(
        v!.replace(/\D/g, "").replace(/^0+/, "").length
      ).toBeLessThanOrEqual(20)
  })
})
