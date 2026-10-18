// @vitest-environment jsdom
/** A metric record's rollups: the tiers it keeps, the default where it
 * lists none, none where it lists an empty list, and where to change them. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

import { rollupWords } from "@/lib/observations"

import { MetricRollups } from "./metric-rollups"

afterEach(cleanup)

const metric = (properties: Record<string, unknown>): SubstrateRecord => ({
  id: "health/heart_rate",
  kind: "ada.example.com/observations/metric",
  properties,
  labels: {},
  version: 1,
  createdAt: "2026-10-11T07:00:00Z",
  updatedAt: "2026-10-11T07:00:00Z",
})

describe("a metric's rollups", () => {
  it("reads the tiers kept, the default and none", () => {
    expect(rollupWords(undefined)).toBe("15m, 1h and 1d, the default")
    expect(rollupWords(["5m", "1h"])).toBe("5m and 1h")
    expect(rollupWords([])).toBe("none: every read of it takes raw points")
  })

  it("says a change re-derives them, where the owner may make one", () => {
    render(
      <MetricRollups record={metric({ rollups: ["1d"] })} readOnly={false} />
    )
    const section = screen.getByRole("region", { name: "Rollups" })
    expect(section.textContent).toContain("1d")
    expect(section.textContent).toContain(
      "a change re-derives this metric’s rollups in the background"
    )
    cleanup()
    render(<MetricRollups record={metric({})} readOnly />)
    expect(
      screen.getByRole("region", { name: "Rollups" }).textContent
    ).not.toContain("re-derives")
  })
})
