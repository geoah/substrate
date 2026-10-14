// @vitest-environment jsdom
/** A record page's record views: each block type drawn from the view
 * record alone (a heading, text, chosen properties, related records, a
 * chart as a line or as bars, coded spans), a section the console cannot
 * draw said as a notice while the rest still draw, and nothing at all for
 * a kind with no view. */

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
import { RECORD_VIEW_KIND } from "@/lib/record-view"
import {
  AUTHORITY,
  GARMIN,
  KINDS,
  METRIC,
  METRICS,
  OBSERVATION,
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

import { RecordViews } from "./record-views"
import { useRecordViews } from "@/hooks/use-record-views"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function kindOf(name: string, properties: Record<string, unknown>): KindInfo {
  return {
    identity: `${AUTHORITY}/fitness/${name}`,
    name,
    authority: AUTHORITY,
    package: "fitness",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      authority: AUTHORITY,
      package: "fitness",
      names: { singular: name },
      traits: ["substrate.reamde.dev/core/temporal(range)"],
      properties,
    },
  }
}

const SESSION = kindOf("session", {
  sport: { type: "string", description: "the sport" },
  route: {
    type: "reference",
    kind: `${AUTHORITY}/fitness/route`,
    description: "the route",
  },
})
const LAP = kindOf("lap", {
  session: { type: "reference", kind: SESSION.identity },
})
const ROUTE = kindOf("route", { name: { type: "string" } })
const ALL = [...KINDS, SESSION, LAP, ROUTE]

const PULSE = `${METRIC}/health/heart_rate`
const STAGE = `${METRIC}/health/sleep_stage`

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-11T07:00:00Z",
    updatedAt: "2026-10-11T07:00:00Z",
  }
}

const RUN = record(SESSION.identity, "run-1", {
  sport: "running",
  route: { ref: `${ROUTE.identity}/river` },
  begins: "2026-10-11T05:25:00Z",
  ends: "2026-10-11T06:59:00Z",
  by: { ref: GARMIN },
})

const WINDOW = { start: "begins", end: "ends", about: "who", source: "by" }

function view(id: string, properties: Record<string, unknown>) {
  return record(RECORD_VIEW_KIND, id, {
    appliesTo: { ref: `substrate.reamde.dev/core/kind/${SESSION.identity}` },
    ...properties,
  })
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

/** Three minutes of heart rate, by the minute. */
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

const BANDS = answer(
  [
    row({
      form: "gauge",
      values: [
        { reducer: { durationIn: [95, 114] }, value: "12", unit: "min" },
        { reducer: { durationIn: [114, 133] }, value: "40.5", unit: "min" },
      ],
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
  return record(OBSERVATION, `stage/${code}/${at}`, {
    code,
    at,
    endsAt,
    metric: { ref: STAGE },
    form: "amount",
  })
}

/** A night, stage after stage, inside the run's window for brevity. */
const SEGMENTS = [
  stage("awake", "2026-10-11T05:25:00Z", "2026-10-11T05:35:00Z"),
  stage("light", "2026-10-11T05:35:00Z", "2026-10-11T06:00:00Z"),
  stage("deep", "2026-10-11T06:00:00Z", "2026-10-11T06:30:00Z"),
  stage("rem", "2026-10-11T06:30:00Z", "2026-10-11T06:59:00Z"),
]

interface Seen {
  path: string
  filter: RecordFilter
  aggregate?: Aggregate
  count: boolean
}

/** Mounts the views of `kind` on `rec`, answering every read the way a
 * repository holding `views` would. */
function mount(
  views: SubstrateRecord[],
  rec: SubstrateRecord = RUN,
  kind: KindInfo = SESSION
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
      const count = url.searchParams.get("count") === "1"
      seen.push({ path: url.pathname, filter, aggregate, count })
      const ok = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200 })
      if (url.pathname !== "/api/v1/records") {
        // One record by its path: a metric record.
        const metric = METRICS.find((m) =>
          url.pathname.endsWith(`/${encodeURIComponent(m.id)}`)
        )
        return metric
          ? ok(metric)
          : new Response(
              JSON.stringify({ error: { code: "not_found", message: "no" } }),
              { status: 404 }
            )
      }
      if (aggregate) {
        if (aggregate.metric?.ref === STAGE) return ok(TOTALS)
        return ok(aggregate.bucket ? CURVE : BANDS)
      }
      const kinds = filter.kinds ?? []
      if (kinds.includes(RECORD_VIEW_KIND)) {
        return ok({ records: views, head: 1, generation: "g" })
      }
      if (kinds.includes(LAP.identity)) {
        return ok({
          records: [record(LAP.identity, "lap-1", {})],
          head: 1,
          generation: "g",
          count: 7,
        })
      }
      if (filter.properties?.metric) {
        return ok({ records: SEGMENTS, head: 1, generation: "g" })
      }
      return ok({ records: [], head: 1, generation: "g" })
    })
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  function Page() {
    const shown = useRecordViews(kind)
    return <RecordViews views={shown} record={rec} kind={kind} kinds={ALL} />
  }
  const { container } = render(
    <QueryClientProvider client={client}>
      <Page />
    </QueryClientProvider>
  )
  return { seen, container }
}

describe("a record view", () => {
  it("draws a heading, text, chosen properties and related records", async () => {
    const { seen } = mount([
      view("fitness/session", {
        name: "This session",
        intro: "What the session held.",
        sections: [
          { heading: { text: "At a glance", note: "the basics" } },
          { text: { text: "Every run counts." } },
          {
            properties: {
              label: "Basics",
              list: [
                { property: "sport", label: "Sport" },
                { property: "unset" },
              ],
            },
          },
          { related: { label: "Route", property: "route" } },
          {
            related: {
              label: "Laps",
              kind: LAP.identity,
              via: "session",
              show: "count",
            },
          },
        ],
      }),
    ])
    const section = await screen.findByRole("region", { name: "This session" })
    expect(section.textContent).toContain("What the session held.")
    expect(
      within(section).getByRole("heading", { name: "At a glance" })
    ).toBeTruthy()
    expect(section.textContent).toContain("the basics")
    expect(section.textContent).toContain("Every run counts.")
    const basics = within(section).getByRole("group", { name: "Basics" })
    expect(basics.textContent).toContain("Sport")
    expect(basics.textContent).toContain("running")
    // A property the record does not hold is left out.
    expect(basics.querySelectorAll("dt")).toHaveLength(1)
    const route = within(section).getByRole("group", { name: "Route" })
    expect(route.querySelectorAll("li")).toHaveLength(1)
    const laps = within(section).getByRole("group", { name: "Laps" })
    await waitFor(() => expect(laps.textContent).toBe("Laps7"))
    // The laps read is the records of that kind pointing here, counted.
    const read = seen.find((s) => s.filter.kinds?.includes(LAP.identity))!
    expect(read.filter.properties).toEqual({
      session: { eq: `${SESSION.identity}/run-1` },
    })
    expect(read.count).toBe(true)
  })

  it("draws a chart as a line over the record's window and as bars over all of it", async () => {
    const { seen } = mount([
      view("fitness/session", {
        name: "During this session",
        window: WINDOW,
        sections: [
          {
            chart: {
              label: "Pulse",
              metrics: ["health/heart_rate"],
              reducers: [
                { reducer: "avg", label: "Average" },
                { reducer: "min", label: "Lowest" },
                { reducer: "max", label: "Highest" },
              ],
            },
          },
          {
            chart: {
              label: "Time in bands",
              metrics: ["health/heart_rate"],
              bucket: "whole",
              unit: "min",
              reducers: [
                { reducer: "durationIn", low: 95, high: 114, label: "Easy" },
                { reducer: "durationIn", low: 114, high: 133, label: "Steady" },
              ],
            },
          },
        ],
      }),
    ])
    const section = await screen.findByRole("region", {
      name: "During this session",
    })
    const chart = await within(section).findByRole("group", {
      name: "Heart rate during this session",
    })
    const line = seen.find((s) => s.aggregate?.bucket)!
    expect(line.aggregate).toMatchObject({
      metric: { ref: PULSE },
      bucket: { every: "PT1M" },
      reducers: ["avg", "min", "max"],
      sources: { policy: "priority", order: [GARMIN] },
    })
    expect(line.filter.properties).toEqual({ about: { exists: false } })
    // The tooltip names each reducer as the view labels it.
    act(() => chart.focus())
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    expect(within(chart).getByRole("status").textContent).toContain(
      "Average75 /minLowest70 /minHighest140 /min"
    )
    const bands = within(section).getByRole("group", { name: "Time in bands" })
    await waitFor(() => expect(bands.textContent).toContain("Steady"))
    expect(bands.textContent).toContain("40m 30s")
    expect(bands.textContent).toContain("12m")
    const whole = seen.find((s) => s.aggregate && !s.aggregate.bucket)!
    expect(whole.aggregate).toMatchObject({
      reducers: [{ durationIn: [95, 114] }, { durationIn: [114, 133] }],
      unit: "min",
    })
    // The timeline over the same window, with the line's metric.
    const link = within(section).getByText("Open in the timeline").closest("a")!
    expect(JSON.parse(link.getAttribute("data-search")!)).toEqual({
      metric: PULSE,
      range: "custom",
      start: "2026-10-11T05:25:00.000Z",
      end: "2026-10-11T06:59:00.000Z",
    })
    // A zoom reads part of the window, and the bars stay whole.
    fireEvent.keyDown(chart, { key: "Enter" })
    await screen.findByRole("button", { name: "Show the whole session" })
    expect(seen.filter((s) => s.aggregate?.bucket)).toHaveLength(2)
    expect(seen.filter((s) => s.aggregate && !s.aggregate.bucket)).toHaveLength(
      1
    )
  })

  it("draws coded spans in the view's order of codes, under its labels, and the time in each", async () => {
    const { seen } = mount([
      view("fitness/session", {
        name: "Stages",
        window: WINDOW,
        sections: [
          {
            spans: {
              label: "Sleep stages",
              metric: "health/sleep_stage",
              codes: [
                { code: "awake", label: "Awake" },
                { code: "rem", label: "REM" },
                { code: "light", label: "Light" },
                { code: "deep", label: "Deep" },
              ],
            },
          },
        ],
      }),
    ])
    const stages = await screen.findByRole("group", { name: "Sleep stages" })
    const rows = await within(stages).findByRole("img", {
      name: /^Sleep stages from/,
    })
    expect(
      [...rows.querySelectorAll("rect[data-code]")].map((r) =>
        r.getAttribute("data-code")
      )
    ).toEqual(["awake", "light", "deep", "rem"])
    expect(
      [...rows.querySelectorAll("text[text-anchor=end]")].map(
        (t) => t.textContent
      )
    ).toEqual(["Awake", "REM", "Light", "Deep"])
    // Each code in the metric's own colour (its codeSet: rem, deep, light,
    // awake).
    const fill = (code: string) =>
      rows.querySelector(`rect[data-code=${code}]`)!.getAttribute("fill")
    expect(fill("rem")).toBe("var(--series-1)")
    expect(fill("awake")).toBe("var(--series-4)")
    await waitFor(() => expect(stages.textContent).toContain("3h 31m"))
    // The spans are the record's own source's, in its window.
    const list = seen.find((s) => s.filter.properties?.metric)!
    expect(list.filter.properties).toMatchObject({
      metric: { eq: STAGE },
      source: { eq: GARMIN },
      at: { gte: "2026-10-11T05:25:00.000Z", lt: "2026-10-11T06:59:00.000Z" },
    })
  })

  it("says why where it cannot draw a section, and draws the rest", async () => {
    mount([
      view("fitness/session", {
        name: "Partly drawn",
        sections: [
          { gauge: { text: "from a newer console package" } },
          { heading: { text: "Two" }, text: { text: "blocks" } },
          { chart: { metrics: ["m"], reducers: [{ reducer: "median" }] } },
          {
            chart: {
              label: "Unbound",
              metrics: ["m"],
              reducers: [{ reducer: "avg" }],
            },
          },
          { heading: { text: "Still here" } },
        ],
      }),
    ])
    const section = await screen.findByRole("region", { name: "Partly drawn" })
    expect(
      within(section)
        .getAllByRole("note")
        .map((n) => n.textContent)
    ).toEqual([
      "This section holds a block this console does not draw: gauge.",
      "This section holds 2 blocks (heading, text); a section holds one.",
      "This chart names a reducer this console does not read: median.",
    ])
    // A chart in a view that binds no window has nothing to read.
    expect(
      within(section).getByRole("group", { name: "Unbound" }).textContent
    ).toContain("Nothing to read here")
    expect(
      within(section).getByRole("heading", { name: "Still here" })
    ).toBeTruthy()
  })
})

it("draws nothing for a kind with no view, and asks for the kind's views by its declaration", async () => {
  const { seen, container } = mount([])
  await waitFor(() =>
    expect(seen.some((s) => s.filter.kinds?.includes(RECORD_VIEW_KIND))).toBe(
      true
    )
  )
  expect(seen[0].filter).toEqual({
    kinds: [RECORD_VIEW_KIND],
    properties: {
      appliesTo: { eq: `substrate.reamde.dev/core/kind/${SESSION.identity}` },
    },
  })
  // Settled, and still nothing drawn.
  await act(async () => {})
  expect(container.textContent).toBe("")
  expect(seen).toHaveLength(1)
})
