// @vitest-environment jsdom
/** The record page's "During this" for a workout and a sleep: the heart
 * rate over the record's span, a workout's time in each zone, a sleep's
 * hypnogram where its stages follow one another and its minutes in each
 * stage otherwise, the timeline over the same window, and nothing for a
 * record that is no workout or sleep. */

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
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type {
  Aggregate,
  AggregateResult,
  AggregateRow,
  KindInfo,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  AUTHORITY,
  GARMIN,
  KINDS,
  METRIC,
  OBSERVATION,
  WHOOP,
} from "@/lib/observations.test-fixtures"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    search,
  }: {
    children: ReactNode
    search?: Record<string, string>
  }) => <a data-search={JSON.stringify(search ?? {})}>{children}</a>,
}))

import { DuringSection } from "./during-section"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function healthKind(name: string): KindInfo {
  return {
    identity: `${AUTHORITY}/health/${name}`,
    name,
    authority: AUTHORITY,
    package: "health",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      authority: AUTHORITY,
      package: "health",
      names: { singular: name },
      traits: ["substrate.reamde.dev/core/temporal(range)"],
    },
  }
}

const WORKOUT = healthKind("workout")
const SLEEP = healthKind("sleep")
const ALL = [...KINDS, WORKOUT, SLEEP]
const HEART = `${METRIC}/health/heart_rate`
const STAGE = `${METRIC}/health/sleep_stage`

function record(
  kind: KindInfo,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: kind.identity,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-11T07:00:00Z",
    updatedAt: "2026-10-11T07:00:00Z",
  }
}

function row(r: Partial<AggregateRow>): AggregateRow {
  return {
    start: "2026-10-11T05:25:00Z",
    end: "2026-10-11T06:59:00Z",
    values: [],
    count: 1,
    bounded: 0,
    synthetic: false,
    unavailable: false,
    ...r,
  }
}

function answer(rows: AggregateRow[], unit = "/min"): AggregateResult {
  return {
    unit,
    from: "raw",
    exact: false,
    rows,
    head: { changelog: 1, generation: "g" },
  }
}

/** Three minutes of a run's heart rate, by the minute. */
const CURVE = answer(
  ["75", "118", "131"].map((avg, i) =>
    row({
      start: new Date(Date.parse("2026-10-11T05:25:00Z") + i * 60_000)
        .toISOString()
        .replace(".000", ""),
      end: new Date(Date.parse("2026-10-11T05:26:00Z") + i * 60_000)
        .toISOString()
        .replace(".000", ""),
      form: "gauge",
      values: [
        { reducer: "avg", value: avg, unit: "/min" },
        { reducer: "min", value: "70", unit: "/min" },
        { reducer: "max", value: "140", unit: "/min" },
      ],
    })
  )
)

const ZONES = answer(
  [
    row({
      form: "gauge",
      values: [
        ["95", "114", "12"],
        ["114", "133", "40.5"],
        ["133", "152", "30"],
        ["152", "171", "8"],
        ["171", "220", "0"],
      ].map(([lo, hi, value]) => ({
        reducer: { durationIn: [Number(lo), Number(hi)] },
        value,
        unit: "min",
      })),
    }),
  ],
  "min"
)

const TOTALS = answer(
  [
    ["awake", "38"],
    ["light", "211"],
    ["deep", "71"],
    ["rem", "94"],
  ].map(([code, value]) =>
    row({
      group: { code },
      form: "amount",
      values: [{ reducer: "sum", value, unit: "min" }],
    })
  ),
  "min"
)

function stage(code: string, at: string, endsAt: string): SubstrateRecord {
  return record(
    { ...KINDS[0], identity: OBSERVATION } as KindInfo,
    `stage/${code}/${at}`,
    { code, at, endsAt, metric: { ref: STAGE }, form: "amount" }
  )
}

/** Garmin's night, stage after stage. */
const SEGMENTS = [
  stage("awake", "2026-10-06T20:10:00Z", "2026-10-06T20:20:00Z"),
  stage("light", "2026-10-06T20:20:00Z", "2026-10-06T21:05:00Z"),
  stage("deep", "2026-10-06T21:05:00Z", "2026-10-06T22:00:00Z"),
  stage("rem", "2026-10-06T22:00:00Z", "2026-10-06T22:30:00Z"),
  stage("light", "2026-10-06T22:30:00Z", "2026-10-07T03:20:00Z"),
]

/** WHOOP's night: one span per stage over the whole night. */
const SPANS = ["awake", "light", "deep", "rem"].map((code) =>
  stage(code, "2026-10-06T20:10:00Z", "2026-10-07T03:20:00Z")
)

interface Seen {
  filter: RecordFilter
  aggregate?: Aggregate
}

function mount(
  rec: SubstrateRecord,
  kind: KindInfo,
  stages: SubstrateRecord[] = SEGMENTS
) {
  const seen: Seen[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://test")
      const filter = JSON.parse(
        url.searchParams.get("filter") ?? "{}"
      ) as RecordFilter
      const raw = url.searchParams.get("aggregate")
      const aggregate = raw ? (JSON.parse(raw) as Aggregate) : undefined
      seen.push({ filter, aggregate })
      const body = !aggregate
        ? { records: stages, head: 1, generation: "g" }
        : aggregate.metric?.ref === STAGE
          ? TOTALS
          : aggregate.bucket
            ? CURVE
            : ZONES
      return new Response(JSON.stringify(body), { status: 200 })
    })
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <DuringSection record={rec} kind={kind} kinds={ALL} />
    </QueryClientProvider>
  )
  return seen
}

const RUN = record(WORKOUT, "garmin/ada/21100008900", {
  sport: "running",
  at: "2026-10-11T05:25:00Z",
  endsAt: "2026-10-11T06:59:00Z",
  source: { ref: GARMIN },
})

const NIGHT = record(SLEEP, "garmin/ada/x4b2c9e1", {
  at: "2026-10-06T20:10:00Z",
  endsAt: "2026-10-07T03:20:00Z",
  source: { ref: GARMIN },
})

describe("during a workout", () => {
  it("draws the heart rate over its span and its time in each zone", async () => {
    const seen = mount(RUN, WORKOUT)
    const section = await screen.findByRole("region", { name: "During this" })
    const heart = within(section).getByRole("group", { name: "Heart rate" })
    await waitFor(() =>
      expect(heart.querySelector("[data-slot=timeline-chart] svg")).not.toBe(
        null
      )
    )
    const curve = seen.find((s) => s.aggregate?.bucket)!
    expect(curve.aggregate).toMatchObject({
      metric: { ref: HEART },
      bucket: { every: "PT1M" },
      sources: { policy: "priority", order: [GARMIN] },
    })
    expect(curve.filter.properties).toEqual({ about: { exists: false } })
    const zones = within(section).getByRole("group", {
      name: "Time in heart rate zones",
    })
    await waitFor(() =>
      expect(zones.textContent).toContain("Zone 2, 114 to 133")
    )
    expect(zones.textContent).toContain("40m 30s")
    expect(zones.textContent).toContain("12m")
    // A workout has no sleep stages.
    expect(seen.some((s) => s.aggregate?.metric?.ref === STAGE)).toBe(false)
    // The timeline over the same window.
    const link = within(section).getByText("Open in the timeline").closest("a")!
    expect(JSON.parse(link.getAttribute("data-search")!)).toEqual({
      metric: HEART,
      range: "custom",
      start: "2026-10-11T05:25:00.000Z",
      end: "2026-10-11T06:59:00.000Z",
    })
  })

  it("zooms into part of the span and back to the whole workout", async () => {
    const seen = mount(RUN, WORKOUT)
    const chart = await screen.findByRole("group", {
      name: "Heart rate during this workout",
    })
    act(() => chart.focus())
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    expect(within(chart).getByRole("status").textContent).toContain(
      "Average 75 /min, lowest 70 /min, highest 140 /min"
    )
    fireEvent.keyDown(chart, { key: "Enter" })
    await waitFor(() =>
      expect(seen.filter((s) => s.aggregate?.bucket)).toHaveLength(2)
    )
    expect(seen.filter((s) => s.aggregate?.bucket)[1].aggregate?.range).toEqual(
      {
        gte: expect.stringMatching(/^2026-10-11T08:25:00/),
        lt: expect.stringMatching(/^2026-10-11T08:26:00/),
      }
    )
    fireEvent.click(
      await screen.findByRole("button", { name: "Show the whole workout" })
    )
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Show the whole workout" })
      ).toBe(null)
    )
  })
})

describe("during a sleep", () => {
  it("draws the night's stages in order, and its minutes in each", async () => {
    const seen = mount(NIGHT, SLEEP)
    const stages = await screen.findByRole("group", { name: "Sleep stages" })
    const night = await within(stages).findByRole("img", {
      name: /^Sleep stages from/,
    })
    expect(night.querySelectorAll("rect[data-stage]")).toHaveLength(5)
    expect(
      [...night.querySelectorAll("rect[data-stage]")].map((r) =>
        r.getAttribute("data-stage")
      )
    ).toEqual(["awake", "light", "deep", "rem", "light"])
    await waitFor(() => expect(stages.textContent).toContain("Deep"))
    expect(stages.textContent).toContain("3h 31m")
    // The stages are the sleep's own source's, in its window.
    const list = seen.find((s) => !s.aggregate)!
    expect(list.filter.properties).toMatchObject({
      metric: { eq: STAGE },
      source: { eq: GARMIN },
      at: { gte: "2026-10-06T20:10:00.000Z", lt: "2026-10-07T03:20:00.000Z" },
    })
    // No zones for a night.
    expect(
      screen.queryByRole("group", { name: "Time in heart rate zones" })
    ).toBe(null)
    const link = screen.getByText("Open in the timeline").closest("a")!
    expect(JSON.parse(link.getAttribute("data-search")!).metric).toBe(
      `${HEART},${STAGE}`
    )
  })

  it("shows the totals alone where each stage spans the whole night", async () => {
    mount(
      { ...NIGHT, properties: { ...NIGHT.properties, source: { ref: WHOOP } } },
      SLEEP,
      SPANS
    )
    const stages = await screen.findByRole("group", { name: "Sleep stages" })
    await waitFor(() => expect(stages.textContent).toContain("Rem"))
    expect(within(stages).queryByRole("img")).toBe(null)
    expect(stages.textContent).toContain("1h 34m")
  })
})

it("shows nothing for a record that is no workout or sleep", () => {
  const { container } = render(
    <QueryClientProvider client={new QueryClient()}>
      <DuringSection
        record={record(WORKOUT, "x", { at: "2026-10-11T05:25:00Z" })}
        kind={WORKOUT}
        kinds={ALL}
      />
    </QueryClientProvider>
  )
  expect(container.textContent).toBe("")
})
