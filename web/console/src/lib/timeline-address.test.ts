/** The timeline's choices as the address keeps them. */

import { describe, expect, it } from "vitest"

import { readChoices, writeChoice } from "@/lib/timeline-address"

describe("per-metric choices in the address", () => {
  it("round-trips a metric id holding a slash and a tilde", () => {
    const tokens = writeChoice(null, "health/odd~id", "twa")
    expect(readChoices(tokens).get("health/odd~id")).toBe("twa")
    expect(writeChoice(tokens, "health/odd~id", "")).toEqual([])
  })
})
