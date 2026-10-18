/** The timeline's choices as the address keeps them. */

import { describe, expect, it } from "vitest"

import { choiceOf, readChoices, writeChoice } from "@/lib/timeline-address"

describe("per-metric choices in the address", () => {
  it("round-trips a metric id holding a slash and a tilde", () => {
    const tokens = writeChoice(null, "health/odd~id", "stddev")
    expect(readChoices(tokens).get("health/odd~id")).toBe("stddev")
    expect(writeChoice(tokens, "health/odd~id", "")).toEqual([])
  })

  it("reads and writes a metric's choice whichever way it is spelled", () => {
    const path = "samples.substrate.reamde.dev/observations/metric/health/steps"
    const tokens = writeChoice(null, path, "max")
    expect(choiceOf(readChoices(tokens), "health/steps")).toBe("max")
    // Written by id, the path's token goes: one choice per metric.
    const next = writeChoice(tokens, "health/steps", "count")
    expect(next).toEqual([`${encodeURIComponent("health/steps")}~count`])
    expect(writeChoice(next, path, "")).toEqual([])
  })
})
