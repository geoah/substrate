// @vitest-environment jsdom
/** One enum look everywhere: a ladder climbs with its declared order, the
 * quiet words read plain, and the property sheet draws a value on the same
 * hue the grid does. */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { EnumTag } from "./enum-tag"
import { DeclaredValue } from "@/components/property-sheet/property-value"
import type { DeclaredProperty } from "@/lib/definition"
import { enumHue } from "@/lib/enum-hue"
import type { PropSpec } from "@/lib/record-schema"

afterEach(cleanup)

const VALUES = [
  { value: "none", label: "" },
  { value: "low", label: "" },
  { value: "medium", label: "" },
  { value: "high", label: "" },
  { value: "urgent", label: "" },
]

const PRIORITY: DeclaredProperty = {
  name: "priority",
  kind: "enum",
  repeated: false,
  values: VALUES,
}

const SPEC = {
  name: "priority",
  label: "Priority",
  kind: "enum",
  required: false,
  repeated: false,
  keyed: false,
  managed: false,
  values: VALUES,
} as PropSpec

describe("enumHue", () => {
  it("climbs a ladder from quiet to loud in declared order", () => {
    expect(["low", "medium", "high", "urgent"].map((v) => enumHue(PRIORITY, v)))
      .toMatchInlineSnapshot(`
        [
          "blue",
          "yellow",
          "orange",
          "red",
        ]
      `)
  })

  it("leaves the quiet words plain", () => {
    expect(enumHue(PRIORITY, "none")).toBeUndefined()
  })

  it("gives any other enum a stable hue per value", () => {
    const kind = { name: "category", values: [] }
    expect(enumHue(kind, "travel")).toBe(enumHue(kind, "travel"))
  })
})

describe("EnumTag", () => {
  it("reads the value's words", () => {
    const { container } = render(<EnumTag prop={PRIORITY} value="high" />)
    expect(container.textContent).toBe("High")
  })

  it("is the same tag on the property sheet as in the grid", () => {
    const grid = render(<EnumTag prop={PRIORITY} value="high" />)
    const gridHue = grid.container
      .querySelector("[data-slot=enum-tag]")
      ?.getAttribute("data-hue")
    cleanup()
    const sheet = render(<DeclaredValue spec={SPEC} value="high" />)
    const tag = sheet.container.querySelector("[data-slot=enum-tag]")
    expect(tag?.textContent).toBe("High")
    expect(tag?.getAttribute("data-hue")).toBe(gridHue)
    expect(gridHue).toBe("orange")
  })

  it("draws each value of a repeated enum as its own tag", () => {
    const { container } = render(
      <DeclaredValue
        spec={{ ...SPEC, repeated: true }}
        value={["low", "high"]}
      />
    )
    const tags = container.querySelectorAll("[data-slot=enum-tag]")
    expect([...tags].map((t) => t.getAttribute("data-hue"))).toEqual([
      "blue",
      "orange",
    ])
  })
})
