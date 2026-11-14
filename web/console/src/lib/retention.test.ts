import { describe, expect, it } from "vitest"

import { retentionQueryOptions } from "./api/kinds"
import {
  CONTACT_KIND,
  KIND_KIND,
  PACKAGE_KIND,
  RETENTION_KIND,
} from "./core-kinds"
import { recordWriteReaches } from "./api/records"
import { retentionLine, retentionText } from "./retention"

describe("retentionText", () => {
  it("reads the three forms", () => {
    expect(retentionText("all")).toBe("all of it")
    expect(retentionText("none")).toBe("none past the latest state")
    expect(retentionText("P30D")).toBe("30 days")
    expect(retentionText("P1W")).toBe("1 week")
    expect(retentionText("P1DT12H")).toBe("1 day 12 hours")
  })

  it("shows a value it does not recognise as written", () => {
    expect(retentionText("P1Y")).toBe("P1Y")
  })
})

describe("retentionLine", () => {
  it("says what applies, and what the kind declares under an override", () => {
    expect(retentionLine({ kind: "a.example.com/p/k", effective: "all" })).toBe(
      "history kept: all of it"
    )
    expect(
      retentionLine({
        kind: "a.example.com/p/k",
        declared: "P30D",
        override: "P90D",
        overrideId: "keep-longer",
        effective: "P90D",
      })
    ).toBe(
      "history kept: 90 days (set in this repository; the kind declares 30 days)"
    )
  })
})

describe("retentionQueryOptions", () => {
  it("is refreshed by a write of an override or a declaration, and of nothing else", () => {
    const key = retentionQueryOptions.queryKey
    for (const kind of [RETENTION_KIND, KIND_KIND, PACKAGE_KIND]) {
      expect(recordWriteReaches(key, kind, "x")).toBe(true)
    }
    expect(recordWriteReaches(key, CONTACT_KIND, "x")).toBe(false)
  })
})
