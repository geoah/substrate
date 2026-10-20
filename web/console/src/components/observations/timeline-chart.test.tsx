// @vitest-environment jsdom
/** The chart on its own: a bucket picked from the keyboard reads out, and a
 * picked bucket that a newer answer no longer has is dropped rather than
 * read (the smoke run's crash after a zoom: the pointer rested on bucket 40
 * of an answer that came back with 18). */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { WEIGHT, seriesOf } from "@/lib/observations.test-fixtures"
import { timeAxis } from "@/lib/time-axis"

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

  it("joins a level across a bucket its hold covered with no reading", () => {
    // Weigh-ins two days apart: the day between is a row with no reading,
    // which the hold covered.
    const between = {
      ...WEIGHT.rows[1],
      synthetic: false,
      count: 0,
      coverage: "PT7H10M",
    }
    const joined = render(chart([WEIGHT.rows[0], between, WEIGHT.rows[2]]))
    const d =
      joined.container
        .querySelector("path[stroke-width='2']")
        ?.getAttribute("d") ?? ""
    expect(d.match(/M/g)).toHaveLength(1)
    expect(d.match(/L/g)).toHaveLength(1)
    cleanup()
    // A gap the read filled is no reading held, and still breaks the line.
    const filled = render(chart(WEIGHT.rows))
    const gap =
      filled.container
        .querySelector("path[stroke-width='2']")
        ?.getAttribute("d") ?? ""
    expect(gap.match(/M/g)).toHaveLength(2)
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

/** The left axis's scale as drawn: two tick labels' values and heights. */
function leftScale(container: HTMLElement): (v: number) => number {
  const ticks = [...container.querySelectorAll("g[data-axis=left] text")]
    .filter((t) => t.getAttribute("dominant-baseline") === "middle")
    .map((t) => ({
      v: Number(t.textContent),
      y: Number(t.getAttribute("y")),
    }))
  const [a, b] = [ticks[0], ticks.at(-1)!]
  return (v) => a.y + ((v - a.v) / (b.v - a.v)) * (b.y - a.y)
}

function extras(props: Partial<Parameters<typeof TimelineChart>[0]>) {
  const { buckets, series } = seriesOf(WEIGHT.rows, "avg")
  return (
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
      tooltip={() => null}
      onZoom={() => {}}
      {...props}
    />
  )
}

describe("TimelineChart bands, highlight, height and dash", () => {
  it("washes a band between its scaled bounds, its label at the right edge", () => {
    const view = render(
      extras({
        bands: [
          { low: 70, high: 75, color: "var(--ok-soft)", label: "Steady" },
        ],
      })
    )
    const rect = view.container.querySelector("rect[data-slot=chart-band]")!
    const y = leftScale(view.container)
    expect(rect.getAttribute("fill")).toBe("var(--ok-soft)")
    expect(Number(rect.getAttribute("y"))).toBeCloseTo(y(75), 5)
    expect(Number(rect.getAttribute("height"))).toBeCloseTo(y(70) - y(75), 5)
    const label = [...view.container.querySelectorAll("text")].find(
      (t) => t.textContent === "Steady"
    )!
    expect(label.getAttribute("text-anchor")).toBe("end")
    // The plot runs to 706 at the fallback width of 720.
    expect(label.getAttribute("x")).toBe("702")
  })

  it("reaches the plot's top with a band that has no upper bound", () => {
    const view = render(
      extras({ bands: [{ low: 74, color: "var(--warn-soft)" }] })
    )
    const rect = view.container.querySelector("rect[data-slot=chart-band]")!
    const y = leftScale(view.container)
    // No caption: the plot starts at 10 and is 200 tall.
    expect(rect.getAttribute("y")).toBe("10")
    expect(Number(rect.getAttribute("height"))).toBeCloseTo(y(74) - 10, 5)
  })

  it("tints the highlighted bucket's column, and ignores one out of range", () => {
    const view = render(extras({ highlight: 1 }))
    const rect = view.container.querySelector(
      "rect[data-slot=chart-highlight]"
    )!
    const { buckets } = seriesOf(WEIGHT.rows, "avg")
    const { x } = timeAxis(domain, "week", 720)
    expect(Number(rect.getAttribute("x"))).toBeCloseTo(x(buckets[1].start), 5)
    expect(Number(rect.getAttribute("width"))).toBeCloseTo(
      x(buckets[1].end) - x(buckets[1].start),
      5
    )
    expect(rect.getAttribute("height")).toBe("200")
    cleanup()
    const out = render(extras({ highlight: 9 }))
    expect(out.container.querySelector("rect[data-slot=chart-highlight]")).toBe(
      null
    )
  })

  it("draws a strip 120 tall", () => {
    const view = render(extras({ height: 120 }))
    // 10 above the plot, 26 under it for the x axis.
    expect(view.container.querySelector("svg")!.getAttribute("height")).toBe(
      "156"
    )
  })

  it("dashes a dashed series's line and draws it no end dots", () => {
    const { buckets, series } = seriesOf(WEIGHT.rows, "avg")
    const view = render(
      extras({
        series: series.map((s) => ({
          key: s.key,
          color: "var(--series-2)",
          points: s.points,
          dash: true,
        })),
      })
    )
    expect(buckets.length).toBeGreaterThan(0)
    const path = view.container.querySelector("path[stroke-width='2']")!
    expect(path.getAttribute("stroke-dasharray")).toBe("4 2")
    expect(
      view.container.querySelectorAll("g[data-series] circle")
    ).toHaveLength(0)
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
