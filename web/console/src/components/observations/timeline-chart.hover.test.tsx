// @vitest-environment jsdom
/** Hovering the chart moves the crosshair and the tooltip alone: the marks
 * (axes, bars, stacks and lines) are laid out once for an answer, not once
 * for every bucket the pointer crosses. */

import { cleanup, fireEvent, render } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import * as chart from "@/lib/chart"
import { WEIGHT, seriesOf } from "@/lib/observations.test-fixtures"

import { TimelineChart } from "./timeline-chart"

vi.mock("@/lib/chart", async (actual) => {
  const real = await actual<typeof import("@/lib/chart")>()
  return {
    ...real,
    linePath: vi.fn(real.linePath),
    niceDomain: vi.fn(real.niceDomain),
  }
})

afterEach(cleanup)

it("lays the marks out once while the pointer crosses the buckets", () => {
  const { buckets, series } = seriesOf(WEIGHT.rows, "avg")
  const view = render(
    <TimelineChart
      buckets={buckets}
      series={series.map((s) => ({
        key: s.key,
        color: "var(--series-1)",
        points: s.points,
        mark: "line",
        band: true,
      }))}
      domain={{
        start: Date.parse(WEIGHT.rows[0].start),
        end: Date.parse(WEIGHT.rows[2].end),
      }}
      size="week"
      label="Weight, Average"
      tooltip={(b) => `bucket ${b}`}
      onZoom={() => {}}
    />
  )
  const svg = view.container.querySelector("svg")!
  vi.mocked(chart.linePath).mockClear()
  vi.mocked(chart.niceDomain).mockClear()
  // Across the three weekly windows of the plot (x 52 to 706).
  for (const clientX of [60, 300, 690]) {
    fireEvent.pointerMove(svg, { clientX })
  }
  expect(view.container.querySelector("[data-slot=chart-crosshair]")).not.toBe(
    null
  )
  expect(chart.linePath).not.toHaveBeenCalled()
  expect(chart.niceDomain).not.toHaveBeenCalled()
})
