// @vitest-environment jsdom
/** The points chart on its own: each reading's range as a stepped wash to
 * the next reading, a converted reading drawn hollow, the page's own
 * reading ringed, and the tooltip's words for the reading under the
 * pointer. Invented readings only. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { autoBucket } from "@/lib/observations-time"
import { timeAxis } from "@/lib/time-axis"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    params,
    onFocus,
    onBlur,
  }: {
    children: ReactNode
    params?: Record<string, string>
    onFocus?: () => void
    onBlur?: () => void
  }) => (
    <a
      data-params={JSON.stringify(params ?? {})}
      onFocus={onFocus}
      onBlur={onBlur}
    >
      {children}
    </a>
  ),
}))

import { PointsChart, type ChartPoint } from "./points-chart"

afterEach(cleanup)

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

const domain = {
  start: new Date(2024, 0, 1).getTime(),
  end: new Date(2026, 0, 1).getTime(),
}

const points: ChartPoint[] = [
  {
    at: new Date(2024, 2, 10).getTime(),
    value: 3.1,
    low: 2.6,
    high: 3.4,
    tone: "good",
    title: "Example marker",
    kind: "example.com/health/result",
    id: "r1",
  },
  {
    at: new Date(2024, 8, 4).getTime(),
    value: 3.6,
    high: 3,
    tone: "poor",
    title: "Example marker",
    hollow: true,
    note: "Derived from: Mar 2, 2024",
  },
  {
    at: new Date(2025, 5, 20).getTime(),
    value: 2.9,
    low: 2.6,
    high: 3.4,
    tone: "good",
    title: "Example marker",
  },
]

function chart(highlight?: number) {
  return (
    <PointsChart
      points={points}
      domain={domain}
      unit="mmol/L"
      highlight={highlight}
      label="Example marker over time"
    />
  )
}

describe("PointsChart", () => {
  it("washes each reading's range from its instant to the next reading's", () => {
    const view = render(chart())
    const rects = [
      ...view.container.querySelectorAll("rect[data-slot=point-range]"),
    ]
    expect(rects).toHaveLength(3)
    const { x } = timeAxis(domain, autoBucket(domain), 720)
    const ends = [points[1].at, points[2].at, domain.end]
    rects.forEach((r, i) => {
      expect(Number(r.getAttribute("x"))).toBeCloseTo(x(points[i].at), 5)
      expect(Number(r.getAttribute("width"))).toBeCloseTo(
        x(ends[i]) - x(points[i].at),
        5
      )
    })
    // The second reading's range is open below: it reaches the plot's
    // bottom (a caption above: the plot runs from 24 to 224).
    const second = rects[1]
    expect(
      Number(second.getAttribute("y")) + Number(second.getAttribute("height"))
    ).toBeCloseTo(224, 5)
  })

  it("draws each range in the good tone with an edge at each printed bound", () => {
    const view = render(chart())
    const rects = [
      ...view.container.querySelectorAll("rect[data-slot=point-range]"),
    ]
    for (const r of rects) expect(r.getAttribute("fill")).toBe("var(--ok-soft)")
    const edges = (r: Element) =>
      r.parentElement!.querySelectorAll("line[data-slot=point-range-edge]")
    // The first and third readings print both bounds; the second only a
    // high one, so its open side has no edge.
    expect(edges(rects[0])).toHaveLength(2)
    expect(edges(rects[1])).toHaveLength(1)
    expect(edges(rects[2])).toHaveLength(2)
    expect(edges(rects[0])[0].getAttribute("stroke")).toBe("var(--ok)")
  })

  it("draws a converted reading hollow in its tone", () => {
    const view = render(chart())
    const hollow = view.container.querySelectorAll("circle[data-hollow]")
    expect(hollow).toHaveLength(1)
    expect(hollow[0].getAttribute("fill")).toBe("var(--background)")
    expect(hollow[0].getAttribute("stroke")).toBe("var(--destructive)")
  })

  it("rings the highlighted reading and links one that names its record", () => {
    const view = render(chart(2))
    const ring = view.container.querySelectorAll(
      "circle[data-slot=point-highlight]"
    )
    expect(ring).toHaveLength(1)
    expect(ring[0].getAttribute("r")).toBe("8")
    const dots = [...view.container.querySelectorAll("circle[data-slot=point]")]
    expect(ring[0].getAttribute("cx")).toBe(dots[2].getAttribute("cx"))
    const links = view.container.querySelectorAll("a[data-params]")
    expect(links).toHaveLength(1)
    expect(JSON.parse(links[0].getAttribute("data-params")!)).toEqual({
      authority: "example.com",
      pkg: "health",
      name: "result",
      id: "r1",
    })
  })

  it("names the nearest reading in the tooltip", () => {
    const view = render(chart())
    const svg = view.container.querySelector("svg")!
    const second = view.container.querySelectorAll("circle[data-slot=point]")[1]
    fireEvent.pointerMove(svg, {
      clientX: Number(second.getAttribute("cx")) + 3,
    })
    const tip = screen.getByRole("status").textContent
    expect(tip).toContain("Example marker")
    expect(tip).toContain("3.6")
    expect(tip).toContain("under 3")
    expect(tip).toContain("Sep 4, 2024")
    expect(tip).toContain("Derived from: Mar 2, 2024")
    fireEvent.pointerLeave(svg)
    expect(screen.queryByRole("status")).toBe(null)
  })

  it("rings a reading focused by keyboard and names it in the tooltip", () => {
    const view = render(chart())
    const link = view.container.querySelector("a[data-params]")!
    expect(link.querySelector("circle[data-slot=point-focus]")).toBeTruthy()
    fireEvent.focus(link)
    expect(screen.getByRole("status").textContent).toContain("Example marker")
    fireEvent.blur(link)
    expect(screen.queryByRole("status")).toBe(null)
  })

  it("says so when there are no readings", () => {
    render(<PointsChart points={[]} domain={domain} label="Example marker" />)
    expect(screen.getByText("No results in this range.")).toBeTruthy()
  })
})
