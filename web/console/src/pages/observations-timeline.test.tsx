// @vitest-environment jsdom
/** The timeline: one aggregate read per chosen metric, shaped by the
 * controls (bucket, grouping, reducer, unit, sources), a chart per metric in
 * the mark its form earns, a tooltip and a zoom from the keyboard, the
 * numbers under each chart, and the request in technical mode. Served by a
 * stand-in records route answering the proposal's worked examples. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { AggregateResult } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { isoWithOffset, windowWords } from "@/lib/observations"
import {
  LATEST,
  METRIC,
  SLEEP,
  SOURCE,
  STEPS,
  WEIGHT,
  ZONES,
  observationsServer,
  type SeenRead,
} from "@/lib/observations.test-fixtures"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))

import { ObservationsTimelinePage } from "./observations-timeline"

/** Sep 21 to Oct 11 2026 in Athens: three weeks, read in days. */
const WINDOW =
  "range=custom&start=2026-09-20T21:00:00.000Z&end=2026-10-11T21:00:00.000Z"

const ANSWERS: Record<string, AggregateResult> = {
  [`${METRIC}/health/body_mass`]: WEIGHT,
  [`${METRIC}/health/steps`]: STEPS,
  [`${METRIC}/health/sleep_stage`]: SLEEP,
  [`${METRIC}/health/heart_rate`]: ZONES,
}

function answer(read: SeenRead): AggregateResult {
  if (!read.aggregate.metric) return LATEST
  return ANSWERS[read.aggregate.metric.ref] ?? { ...LATEST, rows: [] }
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function mount(searchParams: string, technical = false) {
  const server = observationsServer(answer)
  vi.stubGlobal("fetch", vi.fn(server.fetch))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter searchParams={searchParams} hasMemory>
          <ObservationsTimelinePage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
  return server
}

/** The reads that named a metric, latest last. */
function charts(seen: SeenRead[], metric: string): SeenRead[] {
  return seen.filter((r) => r.aggregate.metric?.ref === `${METRIC}/${metric}`)
}

async function lane(name: string): Promise<HTMLElement> {
  return screen.findByRole("region", { name })
}

describe("the timeline's requests", () => {
  it("issues one read per chosen metric, each shaped by its form", async () => {
    const server = mount(`?metric=health/body_mass,health/steps&${WINDOW}`)
    await lane("Weight")
    await lane("Steps")
    await waitFor(() =>
      expect(charts(server.seen, "health/steps")).toHaveLength(1)
    )
    const weight = charts(server.seen, "health/body_mass")[0]
    expect(weight.filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
    })
    expect(weight.aggregate).toMatchObject({
      from: "raw",
      reducers: ["avg", "min", "max"],
      bucket: { calendar: "day" },
      range: {
        gte: isoWithOffset(Date.parse("2026-09-20T21:00:00Z")),
        lt: isoWithOffset(Date.parse("2026-10-11T21:00:00Z")),
      },
    })
    expect(weight.aggregate.groupBy).toBeUndefined()
    expect(charts(server.seen, "health/steps")[0].aggregate.reducers).toEqual([
      "sum",
    ])
  })

  it("resolves overlapping sources by priority unless grouped by source", async () => {
    const server = mount(`?metric=health/heart_rate&${WINDOW}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/heart_rate")[0].aggregate.sources
    ).toEqual({
      policy: "priority",
      order: ["garmin-forerunner-265", "apple-watch-s10"],
    })
    const group = screen.getByRole("radiogroup", { name: "Group by" })
    fireEvent.click(within(group).getByRole("radio", { name: "Source" }))
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(2)
    )
    const grouped = charts(server.seen, "health/heart_rate")[1].aggregate
    expect(grouped.groupBy).toEqual(["source"])
    expect(grouped.sources).toBeUndefined()
  })

  it("narrows to a stream's source by its path", async () => {
    const server = mount(
      `?metric=health/heart_rate&source=apple-watch-s10&${WINDOW}`
    )
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    const read = charts(server.seen, "health/heart_rate")[0]
    expect(read.filter.properties).toEqual({
      source: { in: [`${SOURCE}/apple-watch-s10`] },
    })
    expect(read.aggregate.sources).toBeUndefined()
  })

  it("re-reads when the bucket, the reducer or the unit changes", async () => {
    const server = mount(`?metric=health/body_mass&${WINDOW}`)
    const weight = await lane("Weight")
    await waitFor(() =>
      expect(charts(server.seen, "health/body_mass")).toHaveLength(1)
    )
    const bucket = screen.getByRole("radiogroup", { name: "Bucket" })
    fireEvent.click(within(bucket).getByRole("radio", { name: "Week" }))
    await waitFor(() =>
      expect(
        charts(server.seen, "health/body_mass").at(-1)!.aggregate.bucket
      ).toMatchObject({ calendar: "week", weekStart: "monday" })
    )

    fireEvent.click(within(weight).getByRole("button", { name: /^Show:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /Highest/ }))
    await waitFor(() =>
      expect(
        charts(server.seen, "health/body_mass").at(-1)!.aggregate.reducers
      ).toEqual(["max", "min"])
    )

    fireEvent.click(within(weight).getByRole("button", { name: /^Unit:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /\[lb_av\]/ }))
    await waitFor(() =>
      expect(
        charts(server.seen, "health/body_mass").at(-1)!.aggregate.unit
      ).toBe("[lb_av]")
    )
  })

  it("reads a category metric grouped by code", async () => {
    const server = mount(`?metric=health/sleep_stage&${WINDOW}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/sleep_stage")).toHaveLength(1)
    )
    const read = charts(server.seen, "health/sleep_stage")[0].aggregate
    expect(read.groupBy).toEqual(["code"])
    expect(read.reducers).toEqual(["sum"])
  })

  it("holds back a read past the bucket budget and says why", async () => {
    const server = mount("?metric=health/heart_rate&range=1y&bucket=15m")
    const heart = await lane("Heart rate")
    expect(
      await within(heart).findByText(/more than the 1,440 one read may hold/)
    ).toBeTruthy()
    expect(charts(server.seen, "health/heart_rate")).toEqual([])
  })
})

describe("the charts", () => {
  it("draws a level as a line with its band, and an amount as bars", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`)
    const weight = await lane("Weight")
    await waitFor(() =>
      expect(weight.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
        null
      )
    )
    const series = weight.querySelector("[data-series]")!
    expect(series.querySelector("path[fill-opacity='0.1']")).not.toBe(null)
    expect(series.querySelector("path[stroke-width='2']")).not.toBe(null)
    const steps = await lane("Steps")
    await waitFor(() =>
      expect(steps.querySelectorAll("[data-series] path")).toHaveLength(3)
    )
    for (const bar of steps.querySelectorAll("[data-series] path")) {
      expect(bar.getAttribute("fill")).toBe("var(--series-1)")
    }
  })

  it("stacks a category's codes, one colour per code, with a legend", async () => {
    mount(`?metric=health/sleep_stage&${WINDOW}`)
    const sleep = await lane("Sleep stage")
    await waitFor(() =>
      expect(sleep.querySelectorAll("path[data-series]")).toHaveLength(4)
    )
    const fills = [...sleep.querySelectorAll("path[data-series]")].map((p) =>
      p.getAttribute("fill")
    )
    expect(fills).toEqual([
      "var(--series-1)",
      "var(--series-2)",
      "var(--series-3)",
      "var(--series-4)",
    ])
    const legend = within(sleep).getByRole("list", { name: "Legend" })
    expect(legend.textContent).toContain("Rem")
    expect(legend.textContent).toContain("Awake")
  })

  it("reads a bucket out from the keyboard and zooms into it", async () => {
    const server = mount(`?metric=health/body_mass&${WINDOW}`)
    const weight = await lane("Weight")
    const chart = await within(weight).findByRole("group", {
      name: "Weight, Average",
    })
    act(() => chart.focus())
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    const tip = await within(weight).findByRole("status")
    expect(tip.textContent).toContain(
      windowWords(
        Date.parse(WEIGHT.rows[0].start),
        Date.parse(WEIGHT.rows[0].end)
      )
    )
    expect(tip.textContent).toContain("72.6 kg")
    expect(tip.textContent).toContain("Lowest")
    expect(tip.textContent).toContain("72.1 kg")
    await waitFor(() => expect(tip.textContent).toContain("Withings Body+"))
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    expect(within(weight).getByRole("status").textContent).toContain(
      "Filled in, not measured."
    )
    fireEvent.keyDown(chart, { key: "Enter" })
    await waitFor(() => {
      const last = charts(server.seen, "health/body_mass").at(-1)!
      expect(last.aggregate.range).toEqual({
        gte: isoWithOffset(Date.parse("2026-09-28T00:00:00+03:00")),
        lt: isoWithOffset(Date.parse("2026-10-05T00:00:00+03:00")),
      })
    })
  })

  it("puts the numbers under the chart, a column per reducer with its unit", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`)
    const table = await screen.findByRole("table", {
      name: "Weight in numbers",
    })
    const heads = within(table)
      .getAllByRole("columnheader")
      .map((h) => h.textContent)
    expect(heads).toEqual(
      expect.arrayContaining(["Average (kg)", "Lowest (kg)", "Highest (kg)"])
    )
    expect(within(table).getAllByRole("row")).toHaveLength(4)
    expect(table.textContent).toContain("Filled in")
    expect(screen.getByRole("button", { name: "Copy the table" })).toBeTruthy()
  })

  it("shows the request to run again, in technical mode only", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`, true)
    const weight = await lane("Weight")
    const request = await waitFor(() => {
      const el = weight.querySelector("[data-slot=lane-request]")
      expect(el).not.toBe(null)
      return el!
    })
    expect(request.textContent).toContain('"implements"')
    expect(request.textContent).toContain(`"${METRIC}/health/body_mass"`)
    expect(
      within(weight).getByRole("button", { name: "Copy the curl command" })
    ).toBeTruthy()
    cleanup()
    mount(`?metric=health/body_mass&${WINDOW}`)
    const plain = await lane("Weight")
    await within(plain).findByRole("table")
    expect(plain.querySelector("[data-slot=lane-request]")).toBe(null)
  })
})
