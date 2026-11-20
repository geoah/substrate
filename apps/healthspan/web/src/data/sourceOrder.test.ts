import { describe, expect, it } from "vitest"
import { RECORD_ORDER_CAP, recordOrder } from "./sourceOrder"

const sources = [
  { id: "app/b", class: "app", name: "B app" },
  { id: "ring/a", class: "ring", name: "A ring" },
  { id: "watch/z", class: "watch", name: "Z watch" },
  { id: "service/y", class: "service", name: "Y service" },
  { id: "scale/x", class: "scale", name: "X scale" },
]

describe("recordOrder", () => {
  it("puts the owner's order first, then the rest by class and name", () => {
    const holding = new Set(sources.map((s) => s.id))
    expect(recordOrder(["app/b"], sources, holding)).toEqual([
      "app/b",
      "watch/z",
      "ring/a",
      "service/y",
      "scale/x",
    ])
  })

  it("leaves out a source holding no records, even the owner's", () => {
    expect(
      recordOrder(["ring/a"], sources, new Set(["watch/z", "app/b"]))
    ).toEqual(["watch/z", "app/b"])
  })

  it("names a holder with no source record last", () => {
    expect(recordOrder([], sources, new Set(["watch/z", "lost/q"]))).toEqual([
      "watch/z",
      "lost/q",
    ])
  })

  it("caps the order at the server's limit", () => {
    const many = Array.from({ length: 150 }, (_, i) => ({
      id: `s/${String(i).padStart(3, "0")}`,
      name: String(i).padStart(3, "0"),
    }))
    const order = recordOrder([], many, new Set(many.map((s) => s.id)))
    expect(order).toHaveLength(RECORD_ORDER_CAP)
  })
})
