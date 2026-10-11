// @vitest-environment jsdom
/** The chart on its own: a bucket picked from the keyboard reads out, and a
 * picked bucket that a newer answer no longer has is dropped rather than
 * read (the smoke run's crash after a zoom: the pointer rested on bucket 40
 * of an answer that came back with 18). */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { seriesOf } from "@/lib/observations"
import { WEIGHT } from "@/lib/observations.test-fixtures"

import { TimelineChart, type ChartSeries } from "./timeline-chart"

afterEach(cleanup)

const domain = {
  start: Date.parse(WEIGHT.rows[0].start),
  end: Date.parse(WEIGHT.rows[2].end),
}

function chart(rows: typeof WEIGHT.rows) {
  const { buckets, series } = seriesOf(rows, "avg")
  const shown: ChartSeries[] = series.map((s) => ({
    key: s.key,
    label: "Weight",
    color: "var(--series-1)",
    points: s.points,
  }))
  return (
    <TimelineChart
      mark="line"
      band
      buckets={buckets}
      series={shown}
      domain={domain}
      size="week"
      label="Weight, Average"
      tooltip={(b) => `bucket ${b}`}
      onZoom={() => {}}
    />
  )
}

describe("TimelineChart", () => {
  it("drops a picked bucket the next answer does not have", () => {
    const view = render(chart(WEIGHT.rows))
    const group = screen.getByRole("group", { name: "Weight, Average" })
    act(() => group.focus())
    fireEvent.keyDown(group, { key: "End" })
    expect(screen.getByRole("status").textContent).toBe("bucket 2")
    // The same chart, a shorter answer: no throw, no stale readout.
    view.rerender(chart(WEIGHT.rows.slice(0, 1)))
    expect(screen.queryByRole("status")).toBe(null)
  })
})
