// @vitest-environment jsdom
/** The timeline: one aggregate read per chosen metric, shaped by the
 * controls (bucket, grouping, reducer, unit, sources), the metrics overlaid
 * in charts of at most two units (the second on the right axis), each
 * metric in the mark its form earns, a legend that toggles series, one
 * tooltip and a zoom from the keyboard, the numbers under each chart, and
 * the request in technical mode. Served by a stand-in records route
 * answering the proposal's worked examples. */

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
import {
  isoWithOffset,
  recordTimelineSearch,
  windowWords,
} from "@/lib/observations"
import {
  GARMIN,
  LATEST,
  LATEST_PEOPLE,
  LEO,
  METRIC,
  MIA,
  SLEEP,
  SLEEP_STAGE,
  SOURCE,
  STEPS,
  WEIGHT,
  WHOOP,
  ZONES,
  latestAnswer,
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
  if (!read.aggregate.metric) return latestAnswer(read)
  return ANSWERS[read.aggregate.metric.ref] ?? { ...LATEST, rows: [] }
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function mount(
  searchParams: string,
  technical = false,
  respond: (read: SeenRead) => AggregateResult | Response = answer
) {
  const server = observationsServer(respond)
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

/** The chart section drawing exactly these metrics, by name. */
async function lane(name: string): Promise<HTMLElement> {
  return screen.findByRole("region", { name })
}

/** Picks one value of a facet from "Add filter", then closes the menu. */
async function addFilter(facet: string, value: RegExp) {
  fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
  fireEvent.click(await screen.findByRole("option", { name: facet }))
  fireEvent.click(await screen.findByRole("option", { name: value }))
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
  await waitFor(() =>
    expect(screen.queryByRole("option", { name: value })).toBe(null)
  )
}

describe("the timeline's requests", () => {
  it("issues one read per chosen metric, each shaped by its form", async () => {
    const server = mount(`?metric=health/body_mass,health/steps&${WINDOW}`)
    await lane("Weight and Steps")
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
      // The streams' own spelling, record paths, newest stream first.
      order: [`${SOURCE}/garmin-forerunner-265`, `${SOURCE}/apple-watch-s10`],
    })
    const group = screen.getByRole("radiogroup", { name: "Group by" })
    fireEvent.click(within(group).getByRole("radio", { name: "Source" }))
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(2)
    )
    const grouped = charts(server.seen, "health/heart_rate")[1].aggregate
    // A source's channels stay apart, each its own series.
    expect(grouped.groupBy).toEqual(["source", "channel"])
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

  it("narrows to a stream's channel from the address, and lets it go", async () => {
    const server = mount(
      `?metric=health/heart_rate&source=garmin-forerunner-265&channel=workout&${WINDOW}`
    )
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/heart_rate")[0].filter.properties?.channel
    ).toEqual({ in: ["workout"] })
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Channel filter" })
    )
    await waitFor(() =>
      expect(
        charts(server.seen, "health/heart_rate").at(-1)!.filter.properties
          ?.channel
      ).toBeUndefined()
    )
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

  it("keeps a record's subsecond window from its link to the request", async () => {
    const search = recordTimelineSearch({
      metric: { ref: `${METRIC}/health/heart_rate` },
      source: { ref: `${SOURCE}/garmin-forerunner-265` },
      at: "2026-09-01T00:00:00.123Z",
      endsAt: "2026-09-01T00:00:00.456Z",
    })!
    const server = mount(`?${new URLSearchParams(search).toString()}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    const { gte, lt } = charts(server.seen, "health/heart_rate")[0].aggregate
      .range!
    expect(Date.parse(gte!)).toBe(Date.parse("2026-09-01T00:00:00.123Z"))
    expect(Date.parse(lt!)).toBe(Date.parse("2026-09-01T00:00:00.456Z"))
  })

  it("says discovery was refused, naming its problems, never that nothing was measured", async () => {
    const problem =
      "aggregate.budget.maxGroups: the latest read makes 201 groups, over 200; narrow the filter or raise maxGroups (the cap is 2000)"
    mount(`?${WINDOW}`, false, (read) =>
      read.aggregate.metric
        ? answer(read)
        : new Response(
            JSON.stringify({
              error: {
                code: "validation",
                message: "the aggregate read was refused",
                problems: [problem],
              },
            }),
            { status: 422, headers: { "Content-Type": "application/json" } }
          )
    )
    expect(await screen.findByText("The streams didn’t load")).toBeTruthy()
    expect(screen.getByText(problem)).toBeTruthy()
    expect(screen.queryByText(/Nothing has been measured/)).toBe(null)
  })

  it("draws the chosen metric when only the list of every metric is refused", async () => {
    const problem =
      "aggregate.budget.maxGroups: the read makes 2001 groups, over 2000; narrow the filter or raise maxGroups (the cap is 2000)"
    mount(`?metric=health/body_mass&${WINDOW}`, false, (read) =>
      // The list of every metric is the one read grouped by metric alone.
      read.aggregate.groupBy?.join() === "metric"
        ? new Response(
            JSON.stringify({
              error: {
                code: "validation",
                message: "the aggregate read was refused",
                problems: [problem],
              },
            }),
            { status: 422, headers: { "Content-Type": "application/json" } }
          )
        : answer(read)
    )
    await lane("Weight")
    expect(screen.queryByText("The streams didn’t load")).toBe(null)
    const note = await screen.findByRole("alert", {
      name: "The other metrics didn’t load",
    })
    expect(note.textContent).toContain(problem)
  })

  it("narrows discovery to the chosen metric and still offers the others", async () => {
    const server = mount(`?metric=health/body_mass&${WINDOW}`)
    await lane("Weight")
    const streamReads = server.seen.filter(
      (r) => !r.aggregate.metric && r.aggregate.groupBy?.length === 4
    )
    expect(streamReads.map((r) => r.filter.properties)).toEqual([
      { metric: { in: [`${METRIC}/health/body_mass`] } },
    ])
    fireEvent.click(screen.getByRole("button", { name: /^Metrics?:/ }))
    expect(
      await screen.findByRole("option", { name: /Heart rate/ })
    ).toBeTruthy()
  })

  it("offers the other sources when the chosen source's menu reopens", async () => {
    const server = mount(`?metric=health/heart_rate&${WINDOW}`)
    await lane("Heart rate")
    await addFilter("Source", /Garmin Forerunner 265/)
    // Discovery narrows to the Garmin on the server.
    await waitFor(() =>
      expect(
        server.seen.some(
          (r) =>
            r.aggregate.groupBy?.length === 4 &&
            r.filter.properties?.source?.in?.length === 1
        )
      ).toBe(true)
    )
    fireEvent.click(screen.getByRole("button", { name: /^Source/ }))
    fireEvent.click(await screen.findByRole("option", { name: /Apple Watch/ }))
    await waitFor(() =>
      expect(
        [
          ...(charts(server.seen, "health/heart_rate").at(-1)!.filter.properties
            ?.source?.in ?? []),
        ].sort()
      ).toEqual(
        [`${SOURCE}/apple-watch-s10`, `${SOURCE}/garmin-forerunner-265`].sort()
      )
    )
  })

  it("offers the other people when the chosen person's menu reopens", async () => {
    const server = mount(
      `?metric=health/heart_rate&${WINDOW}`,
      false,
      (read) =>
        read.aggregate.metric ? answer(read) : latestAnswer(read, LATEST_PEOPLE)
    )
    await lane("Heart rate")
    await addFilter("About", /mia/)
    await waitFor(() =>
      expect(
        server.seen.some(
          (r) =>
            r.aggregate.groupBy?.length === 4 &&
            r.filter.properties?.about?.in?.length === 1
        )
      ).toBe(true)
    )
    fireEvent.click(screen.getByRole("button", { name: /^About/ }))
    fireEvent.click(await screen.findByRole("option", { name: /leo/ }))
    await waitFor(() =>
      expect(
        [
          ...(charts(server.seen, "health/heart_rate").at(-1)!.filter.properties
            ?.about?.in ?? []),
        ].sort()
      ).toEqual([LEO, MIA].sort())
    )
  })

  it("says the full Source list is unavailable when only its read passes the group cap", async () => {
    const problem =
      "aggregate.budget.maxGroups: the read makes 2001 groups, over 2000; narrow the filter or raise maxGroups (the cap is 2000)"
    mount(
      `?metric=health/heart_rate&source=${encodeURIComponent(GARMIN)}&${WINDOW}`,
      false,
      (read) =>
        // The Source menu's own read is the one grouped by source alone.
        read.aggregate.groupBy?.join() === "source" && !read.aggregate.metric
          ? new Response(
              JSON.stringify({
                error: {
                  code: "validation",
                  message: "the aggregate read was refused",
                  problems: [problem],
                },
              }),
              { status: 422, headers: { "Content-Type": "application/json" } }
            )
          : answer(read)
    )
    await lane("Heart rate")
    const note = await screen.findByRole("alert", {
      name: "The full Source list is unavailable",
    })
    expect(note.textContent).toContain("more than 2,000 sources")
    expect(note.textContent).toContain(problem)
    expect(screen.queryByText("The streams didn’t load")).toBe(null)
    fireEvent.click(screen.getByRole("button", { name: /^Source/ }))
    expect(
      await screen.findByRole("option", { name: /Garmin Forerunner 265/ })
    ).toBeTruthy()
    expect(screen.queryByRole("option", { name: /Apple Watch/ })).toBe(null)
  })

  it("says the About list didn’t load, keeps the chart, and reads it again on a retry", async () => {
    let failures = 1
    mount(
      `?metric=health/heart_rate&about=${encodeURIComponent(MIA)}&${WINDOW}`,
      false,
      (read) => {
        if (read.aggregate.metric) return answer(read)
        if (read.aggregate.groupBy?.join() === "about" && failures > 0) {
          failures--
          return new Response(
            JSON.stringify({
              error: {
                code: "unavailable",
                message: "the database is restarting",
              },
            }),
            { status: 503, headers: { "Content-Type": "application/json" } }
          )
        }
        return latestAnswer(read, LATEST_PEOPLE)
      }
    )
    const heart = await lane("Heart rate")
    const note = await screen.findByRole("alert", {
      name: "The About list didn’t load",
    })
    expect(note.textContent).toContain("the database is restarting")
    fireEvent.click(screen.getByRole("button", { name: /^About/ }))
    expect(await screen.findByRole("option", { name: /mia/ })).toBeTruthy()
    expect(screen.queryByRole("option", { name: /leo/ })).toBe(null)
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    })
    fireEvent.click(within(note).getByRole("button", { name: "Try again" }))
    await waitFor(() =>
      expect(
        screen.queryByRole("alert", { name: "The About list didn’t load" })
      ).toBe(null)
    )
    fireEvent.click(screen.getByRole("button", { name: /^About/ }))
    expect(await screen.findByRole("option", { name: /leo/ })).toBeTruthy()
    expect(screen.getByRole("region", { name: "Heart rate" })).toBe(heart)
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

/** Weekly steps over the weight's three weeks, so the two answers share
 * their windows the way two reads under one bucket do. */
const WEEKLY_STEPS: AggregateResult = {
  ...STEPS,
  bucket: WEIGHT.bucket,
  rows: WEIGHT.rows.map((r, i) => ({
    ...r,
    form: "amount",
    synthetic: false,
    values: [
      {
        reducer: "sum",
        value: ["61200", "58400", "40100"][i],
        unit: "{steps}",
      },
    ],
    count: 96,
  })),
}

function overlaid(read: SeenRead): AggregateResult {
  if (read.aggregate.metric?.ref === `${METRIC}/health/steps`) {
    return WEEKLY_STEPS
  }
  return answer(read)
}

describe("the charts", () => {
  it("overlays two units in one chart, a level as a line on the left and an amount as bars on the right", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, false, overlaid)
    const chart = await lane("Weight and Steps")
    await waitFor(() =>
      expect(chart.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
        null
      )
    )
    const svg = chart.querySelector("[data-slot=timeline-chart] svg")!
    // The level, its band, on the left axis captioned with its unit.
    expect(svg.querySelector("path[fill-opacity='0.1']")).not.toBe(null)
    expect(svg.querySelector("path[stroke-width='2']")).not.toBe(null)
    expect(svg.querySelector("[data-axis=left]")!.textContent).toContain("kg")
    // The amount's bars, on the right axis, in the second colour.
    expect(svg.querySelector("[data-axis=right]")!.textContent).toContain(
      "steps"
    )
    await waitFor(() =>
      expect(svg.querySelectorAll("path[fill='var(--series-2)']")).toHaveLength(
        3
      )
    )
    expect(
      within(chart).getByRole("group", { name: "Weight" }).textContent
    ).toContain("Left axis, kg")
    expect(
      within(chart).getByRole("group", { name: "Steps" }).textContent
    ).toContain("Right axis, steps")
  })

  it("lists every series at the hovered bucket in one tooltip, each with its unit", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, false, overlaid)
    const chart = await lane("Weight and Steps")
    const group = await within(chart).findByRole("group", {
      name: "Weight, Average; Steps, Total",
    })
    act(() => group.focus())
    fireEvent.keyDown(group, { key: "ArrowRight" })
    const tip = await within(chart).findByRole("status")
    expect(tip.textContent).toContain("Weight, average")
    expect(tip.textContent).toContain("72.6 kg")
    expect(tip.textContent).toContain("Steps, total")
    expect(tip.textContent).toContain("61200 steps")
    // One crosshair, a marker on the line.
    expect(chart.querySelectorAll("[data-slot=chart-crosshair]")).toHaveLength(
      1
    )
    expect(chart.querySelectorAll("[data-slot=chart-marker]")).toHaveLength(1)
  })

  it("splits a third unit into a chart of its own", async () => {
    const server = mount(
      `?metric=health/body_mass,health/steps,health/heart_rate&${WINDOW}`,
      false,
      overlaid
    )
    await lane("Weight and Steps")
    const heart = await lane("Heart rate")
    expect(screen.getAllByRole("region")).toHaveLength(2)
    expect(within(heart).queryByRole("note")).toBe(null)
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
  })

  it("hides a series from its legend entry and shows it again", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, false, overlaid)
    const chart = await lane("Weight and Steps")
    const legend = await within(chart).findByRole("list", {
      name: "Legend, Steps",
    })
    await waitFor(() =>
      expect(
        chart.querySelectorAll("svg path[fill='var(--series-2)']")
      ).toHaveLength(3)
    )
    const entry = within(legend).getByRole("button", { name: /Steps/ })
    expect(entry.getAttribute("aria-pressed")).toBe("true")
    fireEvent.click(entry)
    expect(entry.getAttribute("aria-pressed")).toBe("false")
    expect(
      chart.querySelectorAll("svg path[fill='var(--series-2)']")
    ).toHaveLength(0)
    // The axis goes with its only series; the weight keeps its colour.
    expect(chart.querySelector("[data-axis=right]")).toBe(null)
    expect(chart.querySelector("path[stroke='var(--series-1)']")).not.toBe(null)
    fireEvent.click(entry)
    expect(
      chart.querySelectorAll("svg path[fill='var(--series-2)']")
    ).toHaveLength(3)
  })

  it("reads a metric with a summary stream raw where covered, and says so in technical mode", async () => {
    const whoopHeart = {
      ...LATEST.rows[0],
      start: "2026-10-07T07:30:00Z",
      end: "2026-10-07T07:30:00Z",
      form: "summary",
      group: { metric: `${METRIC}/health/heart_rate`, source: WHOOP },
    }
    const server = mount(`?metric=health/heart_rate&${WINDOW}`, true, (read) =>
      read.aggregate.metric
        ? answer(read)
        : latestAnswer(read, { ...LATEST, rows: [whoopHeart, ...LATEST.rows] })
    )
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    const read = charts(server.seen, "health/heart_rate")[0].aggregate
    expect(read.from).toBe("prefer-raw")
    // The measured streams say the form: a level, read as an average.
    expect(read.reducers).toEqual(["avg", "min", "max"])
    const heart = await lane("Heart rate")
    expect(heart.textContent).toContain(
      "Read with prefer-raw: raw points where they cover a bucket, a vendor summary where they do not, because WHOOP 4.0 keeps summaries"
    )
    // The reader's pick still wins.
    const from = screen.getByRole("radiogroup", { name: "Read from" })
    fireEvent.click(within(from).getByRole("radio", { name: "Raw" }))
    await waitFor(() =>
      expect(
        charts(server.seen, "health/heart_rate").at(-1)!.aggregate.from
      ).toBe("raw")
    )
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
    const legend = within(sleep).getByRole("list", {
      name: "Legend, Sleep stage",
    })
    expect(legend.textContent).toContain("Rem")
    expect(legend.textContent).toContain("Awake")
  })

  it("stacks a coded gauge's time in each state as the engine answers it", async () => {
    // A coded gauge reads a bare `timeInState`. Grouped by code, the
    // engine answers each row with its own state's value, the reducer
    // naming that state (aggregateread.go): {"timeInState": ["rem"]}.
    const stateRows = SLEEP.rows.map((r) => ({
      ...r,
      form: "gauge",
      nonAdditive: undefined,
      values: [
        {
          reducer: { timeInState: [r.group!.code!] },
          value: String(Number(r.values[0].value) * 60),
          unit: "s",
          code: r.group!.code!,
        },
      ],
    }))
    const server = mount(
      `?metric=health/sleep_stage&${WINDOW}`,
      false,
      (read) =>
        !read.aggregate.metric
          ? latestAnswer(read, {
              ...LATEST,
              rows: LATEST.rows.map((r) =>
                r.group?.metric === SLEEP_STAGE ? { ...r, form: "gauge" } : r
              ),
            })
          : { ...SLEEP, unit: "s", rows: stateRows }
    )
    const sleep = await lane("Sleep stage")
    await waitFor(() =>
      expect(charts(server.seen, "health/sleep_stage")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/sleep_stage")[0].aggregate.reducers
    ).toEqual(["timeInState"])
    await waitFor(() =>
      expect(sleep.querySelectorAll("path[data-series]")).toHaveLength(4)
    )
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
    const weight = await lane("Weight")
    // Folded until asked for; the copy is there either way.
    const show = await within(weight).findByRole("button", {
      name: "Show the numbers",
    })
    expect(screen.queryByRole("table", { name: "Weight in numbers" })).toBe(
      null
    )
    expect(screen.getByRole("button", { name: "Copy the table" })).toBeTruthy()
    fireEvent.click(show)
    const table = screen.getByRole("table", { name: "Weight in numbers" })
    const heads = within(table)
      .getAllByRole("columnheader")
      .map((h) => h.textContent)
    expect(heads).toEqual(
      expect.arrayContaining([
        "Average (kg)",
        "Lowest (kg)",
        "Highest (kg)",
        "Points",
      ])
    )
    expect(within(table).getAllByRole("row")).toHaveLength(4)
    expect(table.textContent).toContain("Filled in")
  })

  it("shows a computed value to the metric's places, the exact digits on hover", async () => {
    const server = observationsServer((read) =>
      read.aggregate.metric
        ? {
            ...WEIGHT,
            rows: [
              {
                ...WEIGHT.rows[0],
                values: [
                  { reducer: "avg", value: "72.6333333333", unit: "kg" },
                  ...WEIGHT.rows[0].values.slice(1),
                ],
              },
            ],
          }
        : LATEST
    )
    vi.stubGlobal("fetch", vi.fn(server.fetch))
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter searchParams={`?metric=health/body_mass&${WINDOW}`}>
          <ObservationsTimelinePage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    )
    const weight = await lane("Weight")
    fireEvent.click(
      await within(weight).findByRole("button", { name: "Show the numbers" })
    )
    const cell = within(weight).getByTitle("72.6333333333")
    // No displayPrecision on the fixture's metric: two places.
    expect(cell.textContent).toBe("72.63 kg")
  })

  it("reads a vendor summary from summaries until the reader picks", async () => {
    const server = mount(`?metric=health/whoop_recovery&${WINDOW}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/whoop_recovery")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/whoop_recovery")[0].aggregate
    ).toMatchObject({ from: "summaries", reducers: ["last"] })
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
    await within(plain).findByRole("button", { name: "Show the numbers" })
    expect(plain.querySelector("[data-slot=lane-request]")).toBe(null)
  })
})
