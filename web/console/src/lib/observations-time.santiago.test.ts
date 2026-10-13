/** Axis ticks in a zone whose clocks change at midnight. America/Santiago
 * moves from 00:00 to 01:00 on 2026-09-06, so that day starts at 01:00 and
 * every other day at 00:00, as the server's calendar days do. The zone is
 * set for this file alone; vite.config.ts pins Europe/Athens for the rest,
 * whose clocks change at 03:00 and 04:00. */

import { afterAll, beforeAll, describe, expect, it, vi } from "vitest"

import { axisTicks, isoWithOffset } from "@/lib/observations-time"

beforeAll(() => {
  vi.stubEnv("TZ", "America/Santiago")
})

afterAll(() => {
  vi.unstubAllEnvs()
})

describe("axis ticks where the clocks change at midnight", () => {
  it("starts each day at its own first instant", () => {
    const start = new Date(2026, 8, 3).getTime()
    const end = new Date(2026, 8, 10).getTime()
    expect(isoWithOffset(start)).toBe("2026-09-03T00:00:00-04:00")
    expect(axisTicks({ start, end }, "day").map(isoWithOffset)).toEqual([
      "2026-09-03T00:00:00-04:00",
      "2026-09-04T00:00:00-04:00",
      "2026-09-05T00:00:00-04:00",
      "2026-09-06T01:00:00-03:00",
      "2026-09-07T00:00:00-03:00",
      "2026-09-08T00:00:00-03:00",
      "2026-09-09T00:00:00-03:00",
    ])
  })
})
