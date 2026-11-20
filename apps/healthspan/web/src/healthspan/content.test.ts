import { describe, expect, it } from "vitest"
import {
  HEALTHSPAN_CONTENT,
  notInScore,
  notInScoreAll,
  VO2_ONE_READING,
} from "./content"

const KEYS = [
  "sleepConsistency",
  "sleepHours",
  "zones13",
  "zones45",
  "strength",
  "steps",
  "vo2max",
  "rhr",
  "leanMass",
  "recovery",
  "sleepPerformance",
  "strain",
  "hrv",
  "hrvSdnn",
  "respiratoryRate",
  "spo2",
  "skinTemperature",
  "health/body_mass",
  "health/body_fat_fraction",
  "health/visceral_fat_rating",
]

describe("Healthspan info content", () => {
  it("covers every card with what, why, moves, measured by and a source", () => {
    expect(Object.keys(HEALTHSPAN_CONTENT).sort()).toEqual([...KEYS].sort())
    for (const [key, c] of Object.entries(HEALTHSPAN_CONTENT)) {
      for (const f of [c.what, c.why, c.moves, c.measuredBy])
        expect(f.trim(), key).not.toBe("")
      expect(c.sources.length, key).toBeGreaterThan(0)
      for (const s of c.sources) {
        expect(s.name.trim()).not.toBe("")
        expect(s.url).toMatch(/^https:\/\//)
      }
      const text = [
        c.what,
        c.why,
        c.moves,
        c.measuredBy,
        ...c.sources.map((s) => s.name),
      ].join(" ")
      expect(text, key).not.toMatch(/[–—·]/)
      // A vendor's own score keeps its name; nothing names a device.
      if (!["recovery", "sleepPerformance", "strain"].includes(key))
        expect([c.measuredBy, c.what].join(" "), key).not.toMatch(
          /WHOOP|Apple|Tanita scale|DXA,|Health Auto/
        )
    }
  })

  it("names all nine contributors when it says a card is not in the score", () => {
    const s = notInScore("HRV", "signal")
    expect(s).toMatch(
      /^Not in the score\. The age score adds up 9 contributors/
    )
    expect(s).toMatch(/zone 1-3 time a week, zone 4-5 time a week/)
    expect(s).toMatch(/; HRV is not one of them/)
    expect(notInScore("Weight", "body")).toMatch(/your body composition/)
    const all = notInScoreAll(["Recovery", "HRV"], ["Weight"])
    expect(all).toMatch(
      /^The 2 signals \(recovery and HRV\) and 1 body readings \(weight\)/
    )
    expect(VO2_ONE_READING).toMatch(/one point/)
  })
})
