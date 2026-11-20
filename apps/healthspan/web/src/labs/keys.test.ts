import { describe, expect, it } from "vitest"
import { DEFAULT_DISPLAY } from "../lib/display"
import type { SourceMark } from "../lib/sourceMarkers"
import { labsKeyItems } from "./keys"

const markers = new Map<string, SourceMark>([
  [
    "lab/clinic",
    { id: "lab/clinic", name: "Clinic B", count: 120, marker: "circle" },
  ],
  ["lab/a", { id: "lab/a", name: "Lab A", count: 90, marker: "square" }],
  ["lab/scan", { id: "lab/scan", name: "Scan C", count: 7, marker: "diamond" }],
])
const labels = (items: { label: string }[]) => items.map((i) => i.label)

describe("labsKeyItems", () => {
  it("lists every layer with the defaults, sources first in marker order", () => {
    const items = labsKeyItems(DEFAULT_DISPLAY.labs, markers)
    expect(labels(items)).toEqual([
      "Clinic B",
      "Lab A",
      "Scan C",
      "Results",
      "Trend, best fit over the shown dates",
      "Target: good",
      "Target: borderline",
      "Target: poor",
      "Latest lab range",
      "No results yet",
      "Out of range or in a poor zone",
      "Off target",
    ])
    expect(items.slice(0, 3).map((i) => i.glyph)).toEqual([
      { kind: "marker", shape: "circle" },
      { kind: "marker", shape: "square" },
      { kind: "marker", shape: "diamond" },
    ])
    // The lab range is its own neutral glyph, never a target colour.
    expect(items.find((i) => i.key === "lab-range")?.glyph).toEqual({
      kind: "band",
      tone: "neutral",
      stroke: "dotted",
    })
  })
  it("drops the layers that are switched off", () => {
    const off = labsKeyItems(
      {
        ...DEFAULT_DISPLAY.labs,
        trendline: false,
        targetZones: false,
        labRange: false,
      },
      markers
    )
    expect(labels(off)).not.toContain("Trend, best fit over the shown dates")
    expect(labels(off).filter((l) => l.startsWith("Target"))).toEqual([])
    expect(labels(off)).not.toContain("Latest lab range")
  })
  it("draws every source as a circle with shapes off", () => {
    const items = labsKeyItems(
      { ...DEFAULT_DISPLAY.labs, sourceShapes: false },
      markers
    )
    expect(
      items
        .slice(0, 3)
        .every((i) => i.glyph.kind === "marker" && i.glyph.shape === "circle")
    ).toBe(true)
  })
  it("lists only what the visible charts draw", () => {
    const items = labsKeyItems(DEFAULT_DISPLAY.labs, markers, {
      sources: new Set(["lab/a"]),
      zones: new Set(["success"]),
      labRange: false,
      trend: false,
      hatch: false,
      outOfRange: false,
    })
    expect(labels(items)).toEqual([
      "Lab A",
      "Results",
      "Target: good",
      "Off target",
    ])
  })
})
