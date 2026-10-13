// @vitest-environment jsdom
/** The chart on its own: a bucket picked from the keyboard reads out, and a
 * picked bucket that a newer answer no longer has is dropped rather than
 * read (the smoke run's crash after a zoom: the pointer rested on bucket 40
 * of an answer that came back with 18). */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { WEIGHT, seriesOf } from "@/lib/observations.test-fixtures"

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
    color: "var(--series-1)",
    points: s.points,
    mark: "line",
    band: true,
  }))
  return (
    <TimelineChart
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

  it("breaks the line where the answer skipped a week", () => {
    // The middle week is not in the answer at all: no row, filled or not.
    const view = render(chart([WEIGHT.rows[0], WEIGHT.rows[2]]))
    const line = view.container.querySelector("path[stroke-width='2']")!
    const d = line.getAttribute("d") ?? ""
    expect(d.match(/M/g)).toHaveLength(2)
    expect(d).not.toContain("L")
  })
})

describe("a drag", () => {
  // jsdom has no PointerEvent; a MouseEvent carries what the chart reads.
  if (!("PointerEvent" in window)) {
    Object.assign(window, {
      PointerEvent: class extends MouseEvent {
        pointerId: number
        constructor(type: string, init: PointerEventInit = {}) {
          super(type, init)
          this.pointerId = init.pointerId ?? 1
        }
      },
    })
  }

  it("zooms into every bucket it crossed when released past the plot", () => {
    const onZoom = vi.fn()
    const { buckets, series } = seriesOf(WEIGHT.rows, "avg")
    const view = render(
      <TimelineChart
        buckets={buckets}
        series={series.map((s) => ({
          key: s.key,
          color: "var(--series-1)",
          points: s.points,
        }))}
        domain={domain}
        size="week"
        label="Weight, Average"
        tooltip={(b) => `bucket ${b}`}
        onZoom={onZoom}
      />
    )
    const svg = view.container.querySelector("svg")!
    // The plot runs from x 52 to 706 at the fallback width of 720: three
    // weeks. Pressed on the first, dragged to the last, released in the
    // right margin.
    fireEvent.pointerDown(svg, { clientX: 60, pointerId: 1 })
    fireEvent.pointerMove(svg, { clientX: 690, pointerId: 1 })
    fireEvent.pointerUp(svg, { clientX: 716, pointerId: 1 })
    expect(onZoom).toHaveBeenCalledTimes(1)
    expect(onZoom.mock.calls[0][0]).toEqual({
      start: buckets[0].start,
      end: buckets[buckets.length - 1].end,
    })
  })
})
