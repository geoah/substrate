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

  it("labels a month of quarter hours across the whole month, a label's room apart", () => {
    const { buckets, series } = seriesOf(WEIGHT.rows, "avg")
    const view = render(
      <TimelineChart
        buckets={buckets}
        series={series.map((s) => ({
          key: s.key,
          color: "var(--series-1)",
          points: s.points,
        }))}
        // 2,880 quarter hours, past the default bucket budget.
        domain={{
          start: new Date(2026, 8, 1).getTime(),
          end: new Date(2026, 9, 1).getTime(),
        }}
        size="15m"
        label="Weight, Average"
        tooltip={() => null}
        onZoom={() => {}}
      />
    )
    const labels = [
      ...view.container.querySelectorAll("text[text-anchor=middle]"),
    ]
    const texts = labels.map((l) => l.textContent)
    expect(texts[0]).toBe("Sep 1")
    expect(texts.at(-1)).toMatch(/^Sep (2[6-9]|30)$/)
    // Each label has its room; the first is held inside the plot.
    const xs = labels.map((l) => Number(l.getAttribute("x")))
    for (let i = 2; i < xs.length; i++) {
      expect(xs[i] - xs[i - 1]).toBeGreaterThanOrEqual(84)
    }
    expect(xs[1]).toBeGreaterThan(xs[0])
  })

  it("puts each day's label under its own bars, the first one too", () => {
    const day = (i: number) => new Date(2026, 8, 18 + i).toISOString()
    const rows = Array.from({ length: 10 }, (_, i) => ({
      ...WEIGHT.rows[0],
      start: day(i),
      end: day(i + 1),
      values: [{ reducer: "sum", value: String(8000 + i), unit: "{steps}" }],
    }))
    const { buckets, series } = seriesOf(rows, "sum")
    const view = render(
      <TimelineChart
        buckets={buckets}
        series={series.map((s) => ({
          key: s.key,
          color: "var(--series-1)",
          points: s.points,
          mark: "bars",
        }))}
        domain={{ start: Date.parse(day(0)), end: Date.parse(day(10)) }}
        size="day"
        label="Steps, Total"
        tooltip={() => null}
        onZoom={() => {}}
      />
    )
    const labels = [
      ...view.container.querySelectorAll("text[text-anchor=middle]"),
    ]
    expect(labels[0].textContent).toBe("Sep 18")
    expect(
      view.container.querySelectorAll("path[fill='var(--series-1)']")
    ).toHaveLength(10)
    // The plot runs from x 52 to 706 at the fallback width of 720: ten days
    // of 65.4px, each label at its day's middle.
    const xs = labels.map((l) => Number(l.getAttribute("x")))
    const slot = (706 - 52) / 10
    expect(xs[0]).toBeCloseTo(52 + slot / 2, 0)
    for (let i = 1; i < xs.length; i++) {
      expect((xs[i] - 52) / slot - 0.5).toBeCloseTo(
        Math.round((xs[i] - 52) / slot - 0.5),
        5
      )
    }
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
