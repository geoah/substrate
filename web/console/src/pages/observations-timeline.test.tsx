// @vitest-environment jsdom
/** The timeline: each chosen metric's read shaped by the controls (bucket,
 * grouping, reducer, unit, sources), a chart's metrics read in one request
 * where they can be, the metrics overlaid in charts of at most two units
 * (the second on the right axis), each metric in the mark its form earns,
 * a legend that toggles series, one tooltip and a zoom from the keyboard,
 * the numbers under each chart, and the request in technical mode. Served
 * by a stand-in records route answering the proposal's worked examples. */

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
import type { AggregateResult, SubstrateRecord } from "@/lib/api/types"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import { recordTimelineSearch } from "@/lib/observations"
import { isoWithOffset, windowWords } from "@/lib/observations-time"
import {
  APPLE,
  BODY_MASS,
  GARMIN,
  HEART,
  LATEST,
  LATEST_CHANNELS,
  LONDON_TRIP,
  LATEST_PEOPLE,
  LEO,
  METRIC,
  METRICS,
  MIA,
  SLEEP,
  SLEEP_STAGE,
  SOURCE,
  SOURCES,
  STEPS,
  WEIGHT,
  WHOOP,
  WITHINGS,
  ZONES,
  addFilter,
  latestAnswer,
  observationsServer,
  pageFetch,
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
  respond: (read: SeenRead) => AggregateResult | Response = answer,
  kindsFail = false,
  stored?: SubstrateRecord[]
) {
  const server = observationsServer(respond, undefined, stored)
  vi.stubGlobal("fetch", vi.fn(pageFetch(server, kindsFail)))
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
  return Object.assign(server, { client })
}

/** The metric's own reads, latest last: a read of several metrics counts
 * once for each (`perMetric`). */
function charts(seen: SeenRead[], metric: string): SeenRead[] {
  return seen.filter((r) => r.aggregate.metric?.ref === `${METRIC}/${metric}`)
}

/** The chart section drawing exactly these metrics, by name. */
async function lane(name: string): Promise<HTMLElement> {
  return screen.findByRole("region", { name })
}

describe("the timeline's requests", () => {
  it("reads each chosen metric, each shaped by its form", async () => {
    const server = mount(`?metric=health/body_mass,health/steps&${WINDOW}`)
    await lane("Weight and Steps")
    await waitFor(() =>
      expect(charts(server.seen, "health/steps")).toHaveLength(1)
    )
    const weight = charts(server.seen, "health/body_mass")[0]
    expect(weight.filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
    })
    // Days where each value was recorded: no zone, the reader's placing a
    // value written without an offset, and the three weeks' local days
    // whole at any offset, from Sep 21 at +14:00 to Oct 11's end at -12:00.
    expect(weight.aggregate).toMatchObject({
      from: "raw",
      reducers: ["avg", "min", "max"],
      bucket: { calendar: "day", fallbackTimezone: "Europe/Athens" },
      range: {
        gte: isoWithOffset(Date.parse("2026-09-20T10:00:00Z")),
        lt: isoWithOffset(Date.parse("2026-10-12T12:00:00Z")),
      },
    })
    expect(weight.aggregate.bucket?.timezone).toBeUndefined()
    expect(weight.aggregate.groupBy).toBeUndefined()
    expect(charts(server.seen, "health/steps")[0].aggregate.reducers).toEqual([
      "sum",
    ])
  })

  it("reads a chart's metrics in one request, each drawn from its own rows", async () => {
    const server = mount(
      `?metric=health/body_mass,health/steps&${WINDOW}`,
      true
    )
    const chart = await lane("Weight and Steps")
    await waitFor(() =>
      expect(server.sent.filter((r) => r.aggregate.metrics)).toHaveLength(1)
    )
    // Neither metric is read on its own.
    expect(server.sent.filter((r) => r.aggregate.metric)).toEqual([])
    const [read] = server.sent.filter((r) => r.aggregate.metrics)
    expect(read.filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
    })
    // Each metric's reducers and from in its entry, the range, the bucket
    // and the budget the read's.
    expect(read.aggregate.metrics).toEqual([
      { ref: BODY_MASS, reducers: ["avg", "min", "max"], from: "raw" },
      { ref: `${METRIC}/health/steps`, reducers: ["sum"], from: "raw" },
    ])
    expect(read.aggregate).toMatchObject({
      bucket: { calendar: "day", fallbackTimezone: "Europe/Athens" },
      budget: { maxBuckets: 2880, maxSamples: 2000000, maxGroups: 400 },
    })
    for (const own of ["metric", "unit", "reducers", "from"] as const) {
      expect(read.aggregate[own]).toBeUndefined()
    }
    // Each metric's numbers are its own rows of the one answer.
    for (const show of await within(chart).findAllByRole("button", {
      name: "Show the numbers",
    })) {
      fireEvent.click(show)
    }
    const weight = within(chart).getByRole("table", {
      name: "Weight in numbers",
    })
    const steps = within(chart).getByRole("table", { name: "Steps in numbers" })
    expect(within(weight).getAllByRole("row")).toHaveLength(4)
    expect(within(weight).getByText("72.6 kg")).toBeTruthy()
    expect(within(steps).getAllByRole("row")).toHaveLength(4)
    expect(within(steps).getByText("9812 steps")).toBeTruthy()
    // A shared day's offsets are the day's across both metrics, and the
    // column says so.
    const head = within(weight).getByRole("columnheader", {
      name: "Day’s UTC offset",
    })
    expect(head.getAttribute("title")).toContain("across Weight and Steps")
    // With Technical details on, the one request, named for both.
    const requests = chart.querySelectorAll("[data-slot=lane-request]")
    expect(requests).toHaveLength(1)
    expect(requests[0].textContent).toContain("Request for Weight and Steps")
    expect(requests[0].textContent).toContain('"metrics"')
  })

  it("reads the metrics of two charts in one request", async () => {
    const server = mount(
      `?metric=health/body_mass,health/steps,health/whoop_recovery&${WINDOW}`,
      true
    )
    // A third unit is a second chart.
    const first = await lane("Weight and Steps")
    const second = await lane("Recovery")
    await waitFor(() =>
      expect(server.sent.filter((r) => r.aggregate.metrics)).toHaveLength(1)
    )
    const [read] = server.sent.filter((r) => r.aggregate.metrics)
    expect(read.aggregate.metrics?.map((m) => [m.ref, m.from])).toEqual([
      [BODY_MASS, "raw"],
      [`${METRIC}/health/steps`, "raw"],
      [`${METRIC}/health/whoop_recovery`, "summaries"],
    ])
    expect(server.sent.filter((r) => r.aggregate.metric)).toEqual([])
    await drawn(first)
    // Each chart shows the one request, named for every metric in it.
    for (const chart of [first, second]) {
      const [request] = chart.querySelectorAll("[data-slot=lane-request]")
      expect(request.textContent).toContain(
        "Request for Weight, Steps and Recovery"
      )
    }
  })

  it("reads each metric apart where the server refuses them together", async () => {
    const problem =
      "aggregate.reducers[0]: sum does not apply to a gauge: a gauge is a level"
    const server = mount(
      `?metric=health/body_mass,health/steps&${WINDOW}`,
      false,
      (read) =>
        read.aggregate.metric?.ref === `${METRIC}/health/steps`
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
    const chart = await lane("Weight and Steps")
    // The refusal stays on its own metric's row, and the other draws.
    const steps = within(chart).getByRole("group", { name: "Steps" })
    await within(steps).findByText(problem)
    const weight = within(chart).getByRole("group", { name: "Weight" })
    expect(within(weight).queryByText(problem)).toBe(null)
    await waitFor(() =>
      expect(chart.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
        null
      )
    )
    // Asked once together, then once each.
    const alone = () =>
      server.sent.flatMap((r) =>
        r.aggregate.metric ? [r.aggregate.metric.ref] : []
      )
    expect(server.sent.filter((r) => r.aggregate.metrics)).toHaveLength(1)
    expect(alone()).toEqual([BODY_MASS, `${METRIC}/health/steps`])
    // A record write refreshes the reads apart, never the refused one: a
    // refetch would clear its refusal and drop the reads apart until it
    // came back.
    await act(() => server.client.invalidateQueries({ queryKey: ["records"] }))
    await waitFor(() => expect(alone()).toHaveLength(4))
    expect(server.sent.filter((r) => r.aggregate.metrics)).toHaveLength(1)
    expect(within(steps).getByText(problem)).toBeTruthy()
  })

  it("reads each metric apart where the server predates several-metric reads", async () => {
    const server = mount(`?metric=health/body_mass,health/steps&${WINDOW}`)
    // A server without aggregate.metrics refuses the key as unknown.
    const served = globalThis.fetch
    const together: string[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = new URL(String(input), "http://test")
        const aggregate = url.searchParams.get("aggregate") ?? ""
        if (!aggregate.includes('"metrics"')) return served(input)
        together.push(aggregate)
        return Promise.resolve(
          new Response(
            JSON.stringify({
              error: {
                code: "bad_request",
                message: 'aggregate: unknown field "metrics"',
              },
            }),
            { status: 400, headers: { "Content-Type": "application/json" } }
          )
        )
      })
    )
    const chart = await lane("Weight and Steps")
    await drawn(chart)
    expect(together).toHaveLength(1)
    expect(
      server.sent.flatMap((r) =>
        r.aggregate.metric ? [r.aggregate.metric.ref] : []
      )
    ).toEqual([BODY_MASS, `${METRIC}/health/steps`])
    expect(chart.textContent).not.toContain("unknown field")
  })

  it("says once for a shared request that its answer is not exact", async () => {
    // Steps alone answer exactly; Weight's average does not.
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, true, (read) =>
      read.aggregate.metric?.ref === `${METRIC}/health/steps`
        ? { ...STEPS, exact: true }
        : answer(read)
    )
    const chart = await lane("Weight and Steps")
    await drawn(chart)
    const [request] = chart.querySelectorAll("[data-slot=lane-request]")
    expect(request.textContent).toContain(
      "Not exact: a float reduction or a rounded conversion in at least one of its metrics."
    )
    // The answer says it of the request, so no metric's row says it.
    expect(chart.textContent).not.toMatch(/(Weight|Steps): not exact/)
  })

  it("resolves overlapping sources by priority unless grouped by source", async () => {
    const server = mount(`?metric=health/heart_rate&${WINDOW}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    // No source chosen: no order, so the server ranks the sources by their
    // records' priorities for the metric, and the chart says which wins:
    // the Garmin's record ranks it first for heart rate.
    expect(
      charts(server.seen, "health/heart_rate")[0].aggregate.sources
    ).toEqual({ policy: "priority" })
    const heart = screen.getByRole("region", { name: "Heart rate" })
    await waitFor(() =>
      expect(heart.textContent).toContain(
        "Where sources overlap, Garmin Forerunner 265 is used first, then Apple Watch S10."
      )
    )
    // The rule itself is a developer's fact.
    expect(heart.textContent).not.toContain("record path")
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

  it("gives the server's ranking rule with Technical details on", async () => {
    mount(`?metric=health/heart_rate&${WINDOW}`, true)
    const heart = await lane("Heart rate")
    await waitFor(() =>
      expect(heart.textContent).toContain(
        "The server ranks the sources by each source record’s priorities for this metric, then by record path, and values naming no source last."
      )
    )
  })

  it("says values naming no source rank after every source", async () => {
    // A weight typed in by hand, beside the scale's.
    const typed = LATEST.rows.find((r) => r.group?.metric === BODY_MASS)!
    const latest = {
      ...LATEST,
      rows: [...LATEST.rows, { ...typed, group: { metric: BODY_MASS } }],
    }
    const server = mount(`?metric=health/body_mass&${WINDOW}`, false, (read) =>
      read.aggregate.metric ? answer(read) : latestAnswer(read, latest)
    )
    await waitFor(() =>
      expect(charts(server.seen, "health/body_mass")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/body_mass")[0].aggregate.sources
    ).toEqual({ policy: "priority" })
    await waitFor(() =>
      expect(
        screen.getByRole("region", { name: "Weight" }).textContent
      ).toContain(
        "Where sources overlap, Withings Body+ is used first, then values naming no source."
      )
    )
  })

  it("prefers the sources in the order they were picked, not the menu's", async () => {
    const server = mount(`?metric=health/heart_rate&${WINDOW}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    const menu = () => screen.findByRole("listbox", { name: "Source" })
    const pick = async (name: string) =>
      fireEvent.click(
        // A chosen row's name says so after the words.
        within(await menu()).getByRole("option", {
          name: new RegExp(`^${name}`),
        })
      )
    const order = () =>
      charts(server.seen, "health/heart_rate").at(-1)!.aggregate.sources
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByRole("option", { name: "Source" }))
    const offered = within(await menu())
      .getAllByRole("option")
      .map((o) => o.textContent ?? "")
    expect(offered).toEqual(["Garmin Forerunner 265", "Apple Watch S10"])
    // The menu's last first, then the rest from the active filter.
    await pick(offered[1])
    fireEvent.click(await screen.findByRole("button", { name: /^Source/ }))
    await pick(offered[0])
    await waitFor(() =>
      expect(order()).toEqual({
        policy: "priority",
        order: [`${SOURCE}/apple-watch-s10`, `${SOURCE}/garmin-forerunner-265`],
      })
    )
    // Unpicked and picked again, Apple goes last.
    await pick(offered[1])
    await pick(offered[1])
    await waitFor(() =>
      expect(order()).toEqual({
        policy: "priority",
        order: [`${SOURCE}/garmin-forerunner-265`, `${SOURCE}/apple-watch-s10`],
      })
    )
  })

  it("moves a chosen source up the priority without unpicking it", async () => {
    const server = mount(
      `?metric=health/heart_rate&source=garmin-forerunner-265,apple-watch-s10&${WINDOW}`
    )
    const order = () =>
      charts(server.seen, "health/heart_rate").at(-1)?.aggregate.sources
    await waitFor(() =>
      expect(order()).toEqual({
        policy: "priority",
        order: [`${SOURCE}/garmin-forerunner-265`, `${SOURCE}/apple-watch-s10`],
      })
    )
    fireEvent.click(screen.getByRole("button", { name: /^Source/ }))
    const ranks = await screen.findByRole("list", { name: "Source order" })
    expect(
      within(ranks)
        .getAllByRole("listitem")
        .map((li) => li.textContent)
    ).toEqual(["1Garmin Forerunner 265", "2Apple Watch S10"])
    fireEvent.click(
      within(ranks).getByRole("button", { name: "Move Apple Watch S10 up" })
    )
    await waitFor(() =>
      expect(order()).toEqual({
        policy: "priority",
        order: [`${SOURCE}/apple-watch-s10`, `${SOURCE}/garmin-forerunner-265`],
      })
    )
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

  it("offers every reducer the read's forms take, a zone reading raw points alone", async () => {
    // WHOOP keeps heart rate as summaries beside the watches' raw points.
    const whoop = {
      ...LATEST.rows[0],
      form: "summary",
      group: { metric: `${METRIC}/health/heart_rate`, source: WHOOP },
    }
    const server = mount(
      `?metric=health/heart_rate&${WINDOW}`,
      false,
      (read) =>
        read.aggregate.metric
          ? answer(read)
          : latestAnswer(read, { ...LATEST, rows: [...LATEST.rows, whoop] })
    )
    const heart = await lane("Heart rate")
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(charts(server.seen, "health/heart_rate")[0].aggregate.from).toBe(
      "prefer-raw"
    )
    fireEvent.click(within(heart).getByRole("button", { name: /^Show:/ }))
    const zone = await screen.findByRole("option", {
      name: /Time in zone 2, 114 to 133/,
    })
    expect(zone.textContent).toContain("Raw points only")
    fireEvent.click(zone)
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(2)
    )
    expect(charts(server.seen, "health/heart_rate")[1].aggregate).toMatchObject(
      {
        from: "raw",
        reducers: [{ durationIn: [114, 133] }],
        unit: "min",
      }
    )
    await waitFor(() =>
      expect(heart.textContent).toContain(
        "Reads raw points alone: no vendor summary takes time in zone 2, 114 to 133, so WHOOP 4.0’s summaries are left out."
      )
    )
  })

  it("reads one statistic of WHOOP's summaries, the average until the reader picks", async () => {
    // WHOOP keeps each cycle's average and maximum heart rate; a row reads
    // one, and the server refuses a read of both.
    const summary = (statistic: string) => ({
      ...LATEST.rows[0],
      form: "summary",
      group: {
        metric: `${METRIC}/health/heart_rate`,
        source: WHOOP,
        statistic,
      },
    })
    const latest = {
      ...LATEST,
      rows: [...LATEST.rows, summary("avg"), summary("max")],
    }
    const server = mount(
      `?metric=health/heart_rate&${WINDOW}`,
      false,
      (read) =>
        read.aggregate.metric ? answer(read) : latestAnswer(read, latest)
    )
    const heart = await lane("Heart rate")
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/heart_rate")[0].filter.properties?.statistic
    ).toEqual({ eq: "avg" })
    fireEvent.click(
      within(heart).getByRole("button", { name: "Statistic: Average" })
    )
    fireEvent.click(await screen.findByRole("option", { name: /Highest/ }))
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(2)
    )
    expect(
      charts(server.seen, "health/heart_rate")[1].filter.properties?.statistic
    ).toEqual({ eq: "max" })
  })

  it("groups by metric, source or thing; a category metric always by code", async () => {
    const server = mount(`?metric=health/body_mass&group=code&${WINDOW}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/body_mass")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/body_mass")[0].aggregate.groupBy
    ).toBeUndefined()
    const group = screen.getByRole("radiogroup", { name: "Group by" })
    expect(
      within(group)
        .getAllByRole("radio")
        .map((r) => r.textContent)
    ).toEqual(["Metric", "Source", "About"])
  })

  it("says Group by does nothing where every metric is a category", async () => {
    mount(`?metric=health/sleep_stage&group=source&${WINDOW}`)
    const sleep = await lane("Sleep stage")
    const group = screen.getByRole("radiogroup", { name: "Group by" })
    await waitFor(() =>
      expect(group.getAttribute("aria-disabled")).toBe("true")
    )
    expect(
      within(group)
        .getByRole("radio", { name: "Metric" })
        .getAttribute("aria-checked")
    ).toBe("true")
    expect(
      screen.getByText("A category metric is always grouped by its categories.")
    ).toBeTruthy()
    // Beside a metric it does cut, the category's chart says it stays.
    cleanup()
    mount(`?metric=health/sleep_stage,health/body_mass&group=source&${WINDOW}`)
    const both = await lane("Sleep stage and Weight")
    expect(
      await within(both).findByText(
        "Sleep stage: stacked by category: Group by does not apply to a category metric."
      )
    ).toBeTruthy()
    expect(
      screen
        .getByRole("radiogroup", { name: "Group by" })
        .getAttribute("aria-disabled")
    ).toBe(null)
    expect(sleep).toBeTruthy()
  })

  it("keeps a metric's reducer when another metric is picked", async () => {
    const server = mount(
      `?metric=health/body_mass&reducer=health%2Fbody_mass~max&${WINDOW}`
    )
    await drawn(await lane("Weight"))
    const weight = () => charts(server.seen, "health/body_mass").at(-1)!
    expect(weight().aggregate.reducers?.[0]).toBe("max")
    fireEvent.click(screen.getByRole("button", { name: /^Metrics?:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /Steps/ }))
    await waitFor(() =>
      expect(charts(server.seen, "health/steps").length).toBeGreaterThan(0)
    )
    expect(weight().aggregate.reducers?.[0]).toBe("max")
  })

  it("names a source chosen by id by its title, chosen in its menu", async () => {
    mount(`?metric=health/heart_rate&source=apple-watch-s10&${WINDOW}`)
    await lane("Heart rate")
    const control = screen.getByRole("button", { name: /^Source/ })
    await waitFor(() =>
      expect(control.textContent).toContain("Apple Watch S10")
    )
    fireEvent.click(control)
    const apple = await screen.findByRole("option", { name: /Apple Watch/ })
    expect(apple.getAttribute("data-chosen")).toBe("true")
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
    fireEvent.click(await screen.findByRole("option", { name: /^lb$/ }))
    await waitFor(() =>
      expect(
        charts(server.seen, "health/body_mass").at(-1)!.aggregate.unit
      ).toBe("[lb_av]")
    )
  })

  /** Leaves every read of a metric from here on unanswered. */
  function hangMetricReads() {
    const served = globalThis.fetch
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = new URL(String(input), "http://test")
        const aggregate = url.searchParams.get("aggregate")
        const read = aggregate ? JSON.parse(aggregate) : undefined
        return read?.metric || read?.metrics
          ? new Promise<Response>(() => {})
          : served(input)
      })
    )
  }

  async function drawn(section: HTMLElement) {
    await waitFor(() =>
      expect(section.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
        null
      )
    )
  }

  it("holds the chart while a changed reducer is read, and says it is updating", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`)
    const weight = await lane("Weight")
    await drawn(weight)
    hangMetricReads()
    fireEvent.click(within(weight).getByRole("button", { name: /^Show:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /Highest/ }))
    await waitFor(() => expect(within(weight).getByText("Updating…")))
    expect(weight.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
      null
    )
  })

  it("holds each metric of a shared read while a changed one is read", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`)
    const chart = await lane("Weight and Steps")
    await drawn(chart)
    hangMetricReads()
    const steps = within(chart).getByRole("group", { name: "Steps" })
    fireEvent.click(within(steps).getByRole("button", { name: /^Show:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /^Average/ }))
    // Weight's held rows draw while steps, whose held answer lacks the
    // average, is read.
    const weight = within(chart).getByRole("group", { name: "Weight" })
    await waitFor(() => expect(within(steps).getByText("Reading…")))
    expect(within(weight).getByText("Updating…")).toBeTruthy()
    expect(chart.querySelector("[data-slot=timeline-chart] svg")).not.toBe(null)
  })

  /** Leaves every read from here on unanswered. */
  function hangReads() {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {}))
    )
  }

  it("keeps the drawn chart while a metric being added is read", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`)
    await drawn(await lane("Weight"))
    hangReads()
    fireEvent.click(screen.getByRole("button", { name: /^Metrics?:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /Steps/ }))
    expect(await screen.findByLabelText("Reading the streams")).toBeTruthy()
    const weight = screen.getByRole("region", { name: "Weight" })
    expect(weight.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
      null
    )
  })

  it("re-reads no stream when a source is chosen, and holds the chart while it reads", async () => {
    const server = mount(`?metric=health/heart_rate&${WINDOW}`)
    await drawn(await lane("Heart rate"))
    const streamReads = () =>
      server.seen.filter(
        (r) => !r.aggregate.metric && r.aggregate.groupBy?.length === 5
      ).length
    const before = streamReads()
    hangMetricReads()
    await addFilter("Source", /Garmin Forerunner 265/)
    const heart = screen.getByRole("region", { name: "Heart rate" })
    await waitFor(() => expect(within(heart).getByText("Updating…")))
    expect(heart.querySelector("[data-slot=timeline-chart] svg")).not.toBe(null)
    expect(streamReads()).toBe(before)
  })

  it("offers body weight in the units people weigh in, by their symbols", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`)
    const weight = await lane("Weight")
    fireEvent.click(within(weight).getByRole("button", { name: /^Unit:/ }))
    await screen.findByRole("option", { name: /^lb$/ })
    // The stored unit is the one chosen.
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
      "kg(chosen)",
      "lb",
      "st",
    ])
  })

  it("offers every unit, each with its code, with Technical details on", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`, true)
    const weight = await lane("Weight")
    fireEvent.click(within(weight).getByRole("button", { name: /^Unit:/ }))
    await screen.findByRole("option", { name: /lb_av/ })
    const names = screen.getAllByRole("option").map((o) => o.textContent)
    // The symbol, then the code the picker adds.
    expect(names).toContain("lb[lb_av]")
    expect(names).toContain("μgug")
  })

  it("holds no kilograms under an axis in pounds while the pounds are read", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`)
    const weight = await lane("Weight")
    await drawn(weight)
    expect(within(weight).getAllByText(/72\.6/).length).toBeGreaterThan(0)
    hangMetricReads()
    fireEvent.click(within(weight).getByRole("button", { name: /^Unit:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /^lb$/ }))
    await waitFor(() => expect(within(weight).getByText("Reading…")))
    expect(within(weight).queryByText("Updating…")).toBe(null)
    expect(within(weight).queryAllByText(/72\.6/)).toEqual([])
    expect(weight.querySelector("[data-slot=timeline-chart] svg")).toBe(null)
  })

  it("holds no answer that lacks the reducer picked while it is read", async () => {
    mount(`?metric=health/body_mass&${WINDOW}`)
    const weight = await lane("Weight")
    await drawn(weight)
    hangMetricReads()
    fireEvent.click(within(weight).getByRole("button", { name: /^Show:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /Latest/ }))
    await waitFor(() => expect(within(weight).getByText("Reading…")))
    expect(within(weight).queryByText("Updating…")).toBe(null)
    expect(within(weight).queryByText(/No latest in this range/)).toBe(null)
  })

  it("says nothing of a held answer's values while the next is read", async () => {
    // Every value empty, and every row twice over in its bucket.
    const rows = WEIGHT.rows.map((r) => ({
      ...r,
      values: (r.values ?? []).map((v) => ({ ...v, value: null })),
    }))
    const odd: AggregateResult = { ...WEIGHT, rows: [...rows, ...rows] }
    const notes = [/No average in this range/, /a bucket with another row/]
    mount(
      `?metric=health/body_mass&source=withings-body-plus&${WINDOW}`,
      false,
      (read) => (read.aggregate.metric ? odd : answer(read))
    )
    const weight = await lane("Weight")
    for (const note of notes) await within(weight).findByText(note)
    hangMetricReads()
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Source filter" })
    )
    const held = await lane("Weight")
    await waitFor(() => expect(within(held).getByText("Updating…")))
    for (const note of notes) {
      expect(within(held).queryByText(note)).toBe(null)
    }
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

  it("reads a week of sleep stages in days, each night on the day it ends", async () => {
    // A week would read in hours, and a night's stage totals, one span over
    // the whole night, would fill one hour.
    const week =
      "range=custom&start=2026-10-04T21:00:00.000Z&end=2026-10-11T21:00:00.000Z"
    const server = mount(`?metric=health/sleep_stage&${week}`)
    await waitFor(() =>
      expect(charts(server.seen, "health/sleep_stage")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/sleep_stage")[0].aggregate.bucket
    ).toMatchObject({ calendar: "day", place: "end" })
    expect(
      screen
        .getByRole("radio", { name: "Automatic: day" })
        .getAttribute("aria-checked")
    ).toBe("true")
    // Heart rate alone reads the same week in hours.
    cleanup()
    const heart = mount(`?metric=health/heart_rate&${week}`)
    await waitFor(() =>
      expect(charts(heart.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(
      charts(heart.seen, "health/heart_rate")[0].aggregate.bucket
    ).toMatchObject({ calendar: "hour" })
  })

  it("says where a picked bucket is shorter than the nights it holds", async () => {
    const hour = (code: string, value: string) => ({
      ...SLEEP.rows[0],
      start: "2026-10-07T23:00:00+03:00",
      end: "2026-10-08T00:00:00+03:00",
      group: { code },
      values: [{ reducer: "sum", value, unit: "min" }],
    })
    mount(`?metric=health/sleep_stage&${WINDOW}&bucket=hour`, false, (read) =>
      read.aggregate.metric
        ? {
            ...SLEEP,
            rows: [hour("light", "282.6"), hour("deep", "79.8")],
          }
        : answer(read)
    )
    const chart = await lane("Sleep stage")
    expect(
      await within(chart).findByText(/count whole in the bucket they start in/)
    ).toBeTruthy()
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

  it("keeps the metric it opened on when a newer point of another lands", async () => {
    let latest: AggregateResult = LATEST
    const server = mount(`?${WINDOW}`, false, (read) =>
      read.aggregate.metric ? answer(read) : latestAnswer(read, latest)
    )
    // No metric in the address: the newest stream's, Heart rate.
    await drawn(await lane("Heart rate"))
    // A sync lands a newer weight, and the page reads again.
    latest = {
      ...LATEST,
      rows: LATEST.rows.map((r) =>
        r.group?.metric === BODY_MASS
          ? { ...r, start: "2026-10-08T07:00:00Z", end: "2026-10-08T07:00:00Z" }
          : r
      ),
    }
    await act(async () => {
      await server.client.invalidateQueries()
    })
    await waitFor(() =>
      expect(
        server.seen.filter((r) => r.aggregate.metric).length
      ).toBeGreaterThan(1)
    )
    expect(screen.getByRole("region", { name: "Heart rate" })).toBeTruthy()
    expect(screen.queryByRole("region", { name: "Weight" })).toBe(null)
  })

  it("opens on the newest stream under the chosen source once the metrics are cleared", async () => {
    const server = mount(`?${WINDOW}`)
    await drawn(await lane("Heart rate"))
    const metricsMenu = async (name: RegExp) => {
      fireEvent.click(screen.getByRole("button", { name: /^Metrics?:/ }))
      fireEvent.click(await screen.findByRole("option", { name }))
      fireEvent.keyDown(document.activeElement ?? document.body, {
        key: "Escape",
      })
      await waitFor(() =>
        expect(screen.queryByRole("option", { name })).toBe(null)
      )
    }
    // Weight alone, from Withings alone.
    await metricsMenu(/Weight/)
    await metricsMenu(/Heart rate/)
    await lane("Weight")
    await addFilter("Source", /Withings Body\+/)
    // No metric: the page opens on the newest stream from Withings, never
    // on the newest of every source, which the page read before.
    await metricsMenu(/Weight/)
    await waitFor(() =>
      expect(
        server.seen.some(
          (r) =>
            !r.aggregate.metric &&
            r.aggregate.groupBy?.length === 5 &&
            r.filter.properties?.source
        )
      ).toBe(true)
    )
    await drawn(await lane("Weight"))
    expect(screen.queryByRole("region", { name: "Heart rate" })).toBe(null)
    expect(screen.queryByText(/None of the chosen sources/)).toBe(null)
  })

  it("says the kinds did not load, never that nothing records measurements", async () => {
    mount(`?${WINDOW}`, false, answer, true)
    expect(await screen.findByText("The kinds didn’t load")).toBeTruthy()
    expect(screen.getByText(/the database is restarting/)).toBeTruthy()
    expect(screen.queryByText(/Nothing here records measurements/)).toBe(null)
  })

  it("says no stream matches the chosen source, never that nothing was measured", async () => {
    mount(`?source=nobody&${WINDOW}`)
    expect(
      await screen.findByText(/No stream matches the chosen sources/)
    ).toBeTruthy()
    expect(screen.queryByText(/Nothing has been measured/)).toBe(null)
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

  it("draws the other metrics where one metric's streams fail, and reads it again on a retry", async () => {
    let failures = 1
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, false, (read) => {
      const metrics = read.filter.properties?.metric as
        { in?: string[] } | undefined
      if (
        !read.aggregate.metric &&
        !read.aggregate.metrics &&
        metrics?.in?.join() === BODY_MASS &&
        failures > 0
      ) {
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
      return answer(read)
    })
    const steps = await lane("Steps")
    await waitFor(() =>
      expect(steps.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
        null
      )
    )
    expect(screen.queryByText("The streams didn’t load")).toBe(null)
    const note = await screen.findByRole("alert", {
      name: "Weight: the streams didn’t load",
    })
    expect(note.textContent).toContain("the database is restarting")
    fireEvent.click(within(note).getByRole("button", { name: "Try again" }))
    await lane("Weight and Steps")
    expect(
      screen.queryByRole("alert", { name: "Weight: the streams didn’t load" })
    ).toBe(null)
  })

  it("narrows discovery to the chosen metric and still offers the others", async () => {
    const server = mount(`?metric=health/body_mass&${WINDOW}`)
    await lane("Weight")
    const streamReads = server.seen.filter(
      (r) => !r.aggregate.metric && r.aggregate.groupBy?.length === 5
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
    // The chart narrows to the Garmin on the server.
    await waitFor(() =>
      expect(
        charts(server.seen, "health/heart_rate").at(-1)!.filter.properties
          ?.source?.in
      ).toEqual([`${SOURCE}/garmin-forerunner-265`])
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
        charts(server.seen, "health/heart_rate").at(-1)!.filter.properties
          ?.about?.in
      ).toEqual([MIA])
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
    // The menu still offers the sources the metric's own streams name.
    fireEvent.click(screen.getByRole("button", { name: /^Source/ }))
    expect(
      await screen.findByRole("option", { name: /Garmin Forerunner 265/ })
    ).toBeTruthy()
    expect(screen.getByRole("option", { name: /Apple Watch/ })).toBeTruthy()
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
    // The menu still offers the things the metric's own streams name.
    fireEvent.click(screen.getByRole("button", { name: /^About/ }))
    expect(await screen.findByRole("option", { name: /mia/ })).toBeTruthy()
    expect(screen.getByRole("option", { name: /leo/ })).toBeTruthy()
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    })
    fireEvent.click(within(note).getByRole("button", { name: "Try again" }))
    await waitFor(() =>
      expect(
        screen.queryByRole("alert", { name: "The About list didn’t load" })
      ).toBe(null)
    )
    expect(screen.getByRole("region", { name: "Heart rate" })).toBe(heart)
  })

  it("holds back a read past the bucket cap and says why", async () => {
    const server = mount("?metric=health/heart_rate&range=1y&bucket=15m")
    const heart = await lane("Heart rate")
    expect(
      await within(heart).findByText(/more than the 10,000 one read may hold/)
    ).toBeTruthy()
    expect(charts(server.seen, "health/heart_rate")).toEqual([])
  })

  it("reads 30 days in quarter hours, past the default bucket budget", async () => {
    const server = mount("?metric=health/heart_rate&range=30d&bucket=15m")
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(
      charts(server.seen, "health/heart_rate")[0].aggregate.budget
    ).toEqual({ maxBuckets: 10000 })
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

  it("says a metric with no rows measured nothing, beside one that draws", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, false, (read) =>
      read.aggregate.metric?.ref === BODY_MASS
        ? { ...WEIGHT, rows: [] }
        : overlaid(read)
    )
    const chart = await lane("Weight and Steps")
    await waitFor(() =>
      expect(chart.querySelector("[data-axis=right]")).not.toBe(null)
    )
    const weight = within(chart).getByRole("group", { name: "Weight" })
    await waitFor(() =>
      expect(weight.textContent).toContain("Nothing measured in this range.")
    )
    // The chart draws no axis for it, so its row claims none.
    expect(weight.textContent).not.toContain("Left axis")
    expect(chart.querySelector("[data-axis=left]")).toBe(null)
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
    // Digits in groups of three, as a count is shown.
    expect(tip.textContent).toContain("61,200 steps")
    // One crosshair, a marker on the line.
    expect(chart.querySelectorAll("[data-slot=chart-crosshair]")).toHaveLength(
      1
    )
    expect(chart.querySelectorAll("[data-slot=chart-marker]")).toHaveLength(1)
    // Each line's swatch is the mark it draws: a stroke, a bar's square.
    const swatches = [...tip.querySelectorAll("[data-mark]")].map((el) =>
      el.getAttribute("data-mark")
    )
    expect(swatches).toEqual(["line", "bars"])
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

  it("draws a source's channels as lines of their own, each named", async () => {
    const day = (value: string, channel?: string) => ({
      start: "2026-10-04T00:00:00+03:00",
      end: "2026-10-05T00:00:00+03:00",
      form: "gauge",
      group: channel ? { channel } : {},
      values: [{ reducer: "avg", value, unit: "/min" }],
      count: 10,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    })
    const server = mount(
      `?metric=health/heart_rate&source=whoop-4-0&${WINDOW}`,
      false,
      (read) => {
        if (!read.aggregate.metric) return latestAnswer(read, LATEST_CHANNELS)
        return {
          ...ZONES,
          rows: read.aggregate.groupBy?.includes("channel")
            ? [day("60"), day("140", "workout")]
            : [day("100")],
        }
      }
    )
    const heart = await lane("Heart rate")
    const legend = await within(heart).findByRole("list", {
      name: "Legend, Heart rate",
    })
    expect(
      within(legend)
        .getAllByRole("button")
        .map((b) => b.textContent)
    ).toEqual(["Heart rate", "Heart rate (workout)"])
    expect(
      charts(server.seen, "health/heart_rate").at(-1)!.aggregate.groupBy
    ).toEqual(["channel"])
  })

  it("hides a series from its legend entry and shows it again", async () => {
    mount(`?metric=health/body_mass,health/steps&${WINDOW}`, false, overlaid)
    const chart = await lane("Weight and Steps")
    await waitFor(() =>
      expect(
        chart.querySelectorAll("svg path[fill='var(--series-2)']")
      ).toHaveLength(3)
    )
    // A metric drawn as one series is its own legend entry: its name, with
    // its swatch, and no second line repeating it. Its form is labelled.
    const row = within(chart).getByRole("group", { name: "Steps" })
    expect(within(row).queryByRole("list")).toBe(null)
    expect(row.textContent).toContain("ReadingAmount")
    const entry = within(row).getByRole("button", { name: "Steps" })
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

  it("says where a line reads a summary in some buckets and raw points in others", async () => {
    // STEPS answers raw points on two days and Garmin's daily total on one.
    mount(`?metric=health/steps&${WINDOW}`)
    const steps = await lane("Steps")
    expect(
      await within(steps).findByText(
        "Some buckets read a vendor summary and others raw points, so a line mixes the two."
      )
    ).toBeTruthy()
  })

  it("says where every day is one vendor's daily summary, which Days cannot re-cut", async () => {
    const daily = {
      ...STEPS,
      rows: STEPS.rows.map((r) => ({
        ...r,
        form: "summary",
        from: "summaries" as const,
        sources: [GARMIN],
        count: 1,
      })),
    }
    mount(`?metric=health/steps&${WINDOW}`, false, (read) =>
      read.aggregate.metric ? daily : answer(read)
    )
    const steps = await lane("Steps")
    expect(
      await within(steps).findByText(
        "Each day is Garmin Forerunner 265’s own daily summary, cut where the vendor starts its days, so Days does not change it."
      )
    ).toBeTruthy()
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

  it("keeps each stage's colour beside another metric", async () => {
    // Heart rate comes first, yet the stages keep the colours they wear
    // alone and on a sleep's page.
    mount(`?metric=health/heart_rate,health/sleep_stage&${WINDOW}`)
    const chart = await lane("Heart rate and Sleep stage")
    await waitFor(() =>
      expect(
        chart.querySelectorAll("path[data-series][fill^='var(--series']")
      ).toHaveLength(4)
    )
    const fill = (code: string) =>
      [...chart.querySelectorAll("path[data-series]")]
        .find((p) => p.getAttribute("data-series")?.endsWith(code))
        ?.getAttribute("fill")
    // The fixture's metric lists rem, deep, light, awake.
    expect(fill("rem")).toBe("var(--series-1)")
    expect(fill("awake")).toBe("var(--series-4)")
  })

  it("gives another metric's series the colours its stages leave free", async () => {
    // The shipped sleep stage metric lists six codes; the night holds four.
    const stages = ["awake", "light", "deep", "rem", "asleep", "in_bed"]
    const stored = [
      ...METRICS.map((m) =>
        m.id === "health/sleep_stage"
          ? { ...m, properties: { ...m.properties, codeSet: stages } }
          : m
      ),
      ...SOURCES,
    ]
    const sources = [APPLE, GARMIN, WHOOP, WITHINGS]
    // Heart rate from four sources, grouped by source: four lines.
    const latest = {
      ...LATEST,
      rows: [
        ...LATEST.rows.filter((r) => r.group?.metric !== HEART),
        ...sources.map((source) => ({
          ...LATEST.rows[0],
          group: { metric: HEART, source },
        })),
      ],
    }
    const heart: AggregateResult = {
      ...WEIGHT,
      metric: HEART,
      unit: "/min",
      rows: sources.map((source, i) => ({
        ...WEIGHT.rows[0],
        group: { source },
        sources: [source],
        values: [{ reducer: "avg", value: String(60 + i), unit: "/min" }],
      })),
    }
    // Whichever metric was picked first.
    for (const [picked, name] of [
      ["health/heart_rate,health/sleep_stage", "Heart rate and Sleep stage"],
      ["health/sleep_stage,health/heart_rate", "Sleep stage and Heart rate"],
    ]) {
      cleanup()
      mount(
        `?metric=${picked}&group=source&${WINDOW}`,
        false,
        (read) =>
          !read.aggregate.metric
            ? latestAnswer(read, latest)
            : read.aggregate.metric.ref === HEART
              ? heart
              : answer(read),
        false,
        stored
      )
      const chart = await lane(name)
      const strokes = () =>
        [...chart.querySelectorAll("g[data-series] > path[stroke]")].map((p) =>
          p.getAttribute("stroke")
        )
      await waitFor(() => expect(strokes()).toHaveLength(4))
      const fills = new Map(
        [...chart.querySelectorAll("path[data-series]")].map((p) => [
          p.getAttribute("data-series")!.split("\u0000")[1],
          p.getAttribute("fill"),
        ])
      )
      // Each stage keeps its place in the metric's six codes.
      expect(fills.get("awake")).toBe("var(--series-1)")
      expect(fills.get("rem")).toBe("var(--series-4)")
      // Each source's line has a colour of its own, none a stage's or grey.
      expect(new Set(strokes()).size).toBe(4)
      for (const stroke of strokes()) {
        expect(stroke).not.toBe("var(--faint)")
        expect([...fills.values()]).not.toContain(stroke)
      }
    }
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
          value: String(Number(r.values![0].value) * 60),
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

  it("says plainly where a bucket holds no reading of its own", async () => {
    // The middle week: the hold covered it, with no weigh-in in it.
    const held = {
      ...WEIGHT,
      rows: WEIGHT.rows.map((r, i) =>
        i === 1 ? { ...r, synthetic: false, count: 0, coverage: "PT9H" } : r
      ),
    }
    mount(`?metric=health/body_mass&${WINDOW}&bucket=week`, false, (read) =>
      read.aggregate.metric ? held : answer(read)
    )
    const weight = await lane("Weight")
    const chart = await within(weight).findByRole("group", {
      name: "Weight, Average",
    })
    act(() => chart.focus())
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    const tip = within(weight).getByRole("status")
    expect(tip.textContent).toContain("Sep 28 to Oct 4, 2026")
    expect(tip.textContent).toContain(
      "No reading in this bucket; the one before it still holds."
    )
    expect(tip.textContent).not.toContain("Average")
    expect(tip.textContent).not.toContain("no answer")
  })

  it("reads a bucket out from the keyboard and zooms into it", async () => {
    // The fixture answers weeks.
    const server = mount(`?metric=health/body_mass&${WINDOW}&bucket=week`)
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
      "Sep 28 to Oct 4, 2026"
    )
    fireEvent.keyDown(chart, { key: "Enter" })
    await waitFor(() => {
      const last = charts(server.seen, "health/body_mass").at(-1)!
      expect(last.aggregate.range).toEqual({
        gte: isoWithOffset(Date.parse("2026-09-28T00:00:00+03:00")),
        lt: isoWithOffset(Date.parse("2026-10-05T00:00:00+03:00")),
      })
    })
    // The zoomed chart takes the focus, so the keys go on reading it.
    await waitFor(() =>
      expect(document.activeElement?.getAttribute("aria-label")).toBe(
        "Weight, Average"
      )
    )
    expect(document.activeElement).not.toBe(chart)
    const zoomed = document.activeElement
    // A unit read anew draws the chart anew in the same window, and that
    // chart leaves the focus where the reader put it.
    fireEvent.click(within(weight).getByRole("button", { name: /^Unit:/ }))
    fireEvent.click(await screen.findByRole("option", { name: /^lb$/ }))
    await waitFor(() =>
      expect(
        charts(server.seen, "health/body_mass").at(-1)!.aggregate.unit
      ).toBe("[lb_av]")
    )
    const redrawn = await waitFor(() => {
      const next = within(weight).getByRole("group", {
        name: "Weight, Average",
      })
      expect(next).not.toBe(zoomed)
      return next
    })
    await waitFor(() => expect(redrawn.querySelector("svg")).not.toBe(null))
    expect(document.activeElement).not.toBe(redrawn)
  })

  it("reads the days where they were lived across the London trip, or in the reader's zone", async () => {
    // Sep 19 to Sep 25 in Athens, read in days.
    const trip =
      "range=custom&start=2026-09-18T21:00:00.000Z&end=2026-09-25T21:00:00.000Z&bucket=day"
    const server = mount(`?metric=health/heart_rate&${trip}`, false, (read) =>
      read.aggregate.metric ? LONDON_TRIP : latestAnswer(read)
    )
    const heart = await lane("Heart rate")
    const days = within(screen.getByRole("radiogroup", { name: "Days" }))
    expect(
      days
        .getByRole("radio", { name: "Where I was" })
        .getAttribute("aria-checked")
    ).toBe("true")
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(1)
    )
    expect(charts(server.seen, "health/heart_rate")[0].aggregate).toMatchObject(
      {
        bucket: { calendar: "day", fallbackTimezone: "Europe/Athens" },
        range: {
          gte: isoWithOffset(Date.parse("2026-09-19T00:00:00+14:00")),
          lt: isoWithOffset(Date.parse("2026-09-26T00:00:00-12:00")),
        },
      }
    )
    // One row per date the range holds, named by its own date, with the
    // offsets it was lived at; the wider read's Sep 18 and 26 are left out.
    fireEvent.click(
      await within(heart).findByRole("button", { name: "Show the numbers" })
    )
    const table = within(heart).getByRole("table", {
      name: "Heart rate in numbers",
    })
    const lines = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((r) =>
        within(r)
          .getAllByRole("cell")
          .slice(0, 2)
          .map((c) => c.textContent)
      )
    expect(lines).toEqual([
      ["Sep 19, 2026", "+03:00"],
      ["Sep 20, 2026", "+03:00 to +01:00"],
      ["Sep 21, 2026", "+01:00"],
      ["Sep 22, 2026", "+01:00"],
      ["Sep 23, 2026", "+01:00"],
      ["Sep 24, 2026", "+01:00"],
      ["Sep 25, 2026", "+03:00"],
    ])
    // The tooltip names London's Sep 24 as the 24th, never 02:00 to 02:00.
    const chart = within(heart).getByRole("group", {
      name: "Heart rate, Average",
    })
    act(() => chart.focus())
    for (let i = 0; i < 6; i++) fireEvent.keyDown(chart, { key: "ArrowRight" })
    const tip = within(heart).getByRole("status")
    expect(tip.textContent).toContain("Sep 24, 2026")
    expect(tip.textContent).not.toContain("02:00")
    expect(tip.textContent).toContain("UTC offset+01:00")

    fireEvent.click(days.getByRole("radio", { name: "Europe/Athens" }))
    await waitFor(() =>
      expect(charts(server.seen, "health/heart_rate")).toHaveLength(2)
    )
    const zoned = charts(server.seen, "health/heart_rate")[1].aggregate
    expect(zoned.bucket).toEqual({
      calendar: "day",
      timezone: "Europe/Athens",
    })
    expect(zoned.range).toEqual({
      gte: isoWithOffset(Date.parse("2026-09-18T21:00:00Z")),
      lt: isoWithOffset(Date.parse("2026-09-25T21:00:00Z")),
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
                  ...WEIGHT.rows[0].values!.slice(1),
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
    ).toMatchObject({ from: "summaries", reducers: ["avg"] })
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
