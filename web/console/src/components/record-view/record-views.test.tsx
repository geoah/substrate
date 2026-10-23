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
  AggregateReducer,
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
  WHOOP,
} from "@/lib/observations.test-fixtures"
import { NONE_TOKEN } from "@/lib/observations"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    search,
  }: {
    children: ReactNode
    search?: Record<string, string>
  }) => <a data-search={JSON.stringify(search ?? {})}>{children}</a>,
}))

import { spanWords } from "./span-words"
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
  kind: KindInfo = SESSION,
  opts: {
    metrics?: SubstrateRecord[]
    /** Holds every metric record read until it resolves. */
    metricsAnswer?: Promise<void>
    /** The coded metric's totals, `TOTALS` where absent. */
    totals?: AggregateResult
    /** Whether the spans' list goes on past its page (a cursor). */
    spansMore?: boolean
    /** A line chart's answer, `CURVE` where absent. */
    curve?: AggregateResult
    /** Holds every line read after the first until it resolves: a zoom. */
    zoomAnswer?: Promise<void>
    /** Metrics whose reads answer no rows. */
    unmeasured?: string[]
    /** The summaries a list read of `form: summary` finds, by its metric
     * and its window. */
    summaries?: SubstrateRecord[]
    /** The summaries read's pages, one per cursor, where it has several. */
    summaryPageSize?: number
    /** Summary reads that fail before one answers. */
    summaryFailures?: number
  } = {}
) {
  const metrics = opts.metrics ?? METRICS
  const seen: Seen[] = []
  const failures = { summaries: opts.summaryFailures ?? 0 }
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
        await opts.metricsAnswer
        const metric = metrics.find((m) =>
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
        if (aggregate.metric?.ref === STAGE) return ok(opts.totals ?? TOTALS)
        const first = aggregate.metrics?.[0]?.ref
        if (first && opts.unmeasured?.includes(first)) return ok(answer([]))
        if (
          aggregate.bucket &&
          seen.filter((s) => s.aggregate?.bucket).length > 1
        ) {
          await opts.zoomAnswer
        }
        const answered = aggregate.bucket ? (opts.curve ?? CURVE) : BANDS
        // A several-metric read names each row's metric.
        const metric = aggregate.metrics?.[0]?.ref
        return ok(
          metric
            ? {
                ...answered,
                rows: answered.rows.map((r) => ({
                  ...r,
                  group: { ...r.group, metric },
                })),
              }
            : answered
        )
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
      const form = filter.properties?.form as { eq?: string } | undefined
      if (form?.eq === "summary") {
        const p = filter.properties as Record<string, Record<string, string>>
        const records = (opts.summaries ?? []).filter((r) => {
          const q = r.properties
          return (
            (q.metric as { ref: string }).ref === p.metric.eq &&
            Date.parse(q.at as string) >= Date.parse(p.at.gte) &&
            Date.parse(q.endsAt as string) <= Date.parse(p.endsAt.lte)
          )
        })
        if (failures.summaries > 0) {
          failures.summaries--
          return new Response(
            JSON.stringify({
              error: {
                code: "unavailable",
                message: "the database is restarting",
              },
            }),
            { status: 503 }
          )
        }
        const size = opts.summaryPageSize ?? records.length
        const from = Number(url.searchParams.get("after") ?? 0)
        const next = from + size
        return ok({
          records: records.slice(from, next),
          head: 1,
          generation: "g",
          ...(next < records.length ? { cursor: String(next) } : {}),
        })
      }
      if (filter.properties?.metric) {
        // The server's window read (`at` bounded on both ends) refuses
        // `count`: its page merges computed occurrences.
        const at = filter.properties.at as Record<string, unknown> | undefined
        const window =
          at !== undefined &&
          ("gte" in at || "gt" in at) &&
          ("lt" in at || "lte" in at)
        if (window && count) {
          return new Response(
            JSON.stringify({
              error: {
                code: "bad_request",
                message: "count is not supported on a window read",
              },
            }),
            { status: 400 }
          )
        }
        return ok({
          records: SEGMENTS,
          head: 1,
          generation: "g",
          ...(opts.spansMore ? { cursor: "w.next" } : {}),
        })
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
              metrics: [{ ref: PULSE }],
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
              metrics: [{ ref: PULSE }],
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
      metrics: [{ ref: PULSE, reducers: ["avg", "min", "max"] }],
      bucket: { every: "PT1M" },
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
      metrics: [
        {
          ref: PULSE,
          reducers: [{ durationIn: [95, 114] }, { durationIn: [114, 133] }],
          durationUnit: "min",
        },
      ],
    })
    // The timeline over the same window, with the line's metric.
    const link = within(section).getByText("Open in the timeline").closest("a")!
    expect(JSON.parse(link.getAttribute("data-search")!)).toEqual({
      metric: PULSE,
      about: NONE_TOKEN,
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

  // useQueries starts each new read with no answer of its own, so the
  // chart asks for the newest one of the same read over another range.
  it("keeps the line on screen while a zoom reads", async () => {
    let release = () => {}
    const zoomAnswer = new Promise<void>((resolve) => {
      release = resolve
    })
    const { seen, container } = mount(
      [
        view("fitness/session", {
          name: "During this session",
          window: WINDOW,
          sections: [
            {
              chart: {
                label: "Pulse",
                metrics: [{ ref: PULSE }],
                reducers: [{ reducer: "avg" }],
              },
            },
          ],
        }),
      ],
      RUN,
      SESSION,
      { zoomAnswer }
    )
    const section = await screen.findByRole("region", {
      name: "During this session",
    })
    const chart = await within(section).findByRole("group", {
      name: "Heart rate during this session",
    })
    act(() => chart.focus())
    fireEvent.keyDown(chart, { key: "ArrowRight" })
    fireEvent.keyDown(chart, { key: "Enter" })
    await screen.findByRole("button", { name: "Show the whole session" })
    await waitFor(() =>
      expect(seen.filter((s) => s.aggregate?.bucket)).toHaveLength(2)
    )
    expect(
      within(section).getByRole("group", {
        name: "Heart rate during this session",
      })
    ).toBeTruthy()
    expect(container.querySelector("[data-slot=skeleton]")).toBe(null)
    release()
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
              metric: { ref: STAGE },
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
          {
            chart: {
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "median" }],
            },
          },
          {
            chart: {
              label: "Unbound",
              metrics: [{ ref: PULSE }],
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

const STAGES_VIEW = (sections?: unknown[]) =>
  view("fitness/session", {
    name: "Stages",
    window: WINDOW,
    sections: sections ?? [
      {
        spans: {
          label: "Sleep stages",
          metric: { ref: STAGE },
          codes: [
            { code: "awake", label: "Awake" },
            { code: "rem", label: "REM" },
            { code: "light", label: "Light" },
            { code: "deep", label: "Deep" },
          ],
        },
      },
    ],
  })

it("draws the spans of the source the totals answered from, and says when it cuts the list", async () => {
  const fed = (sources: string[]) => ({
    ...TOTALS,
    rows: TOTALS.rows.map((r, i) => ({
      ...r,
      sources: [sources[i % sources.length]],
    })),
  })
  const withPolicy = (sources: string) =>
    STAGES_VIEW([
      {
        spans: { label: "Sleep stages", metric: { ref: STAGE }, sources },
      },
    ])
  const listed = (seen: Seen[]) =>
    seen.find((s) => s.filter.properties?.metric && !s.aggregate)!

  // prefer: the record's own source, where it fed the totals.
  const own = mount([withPolicy("prefer")], RUN, SESSION, {
    totals: fed([WHOOP, GARMIN]),
  })
  let stages = await screen.findByRole("group", { name: "Sleep stages" })
  await within(stages).findByRole("img", { name: /^Sleep stages from/ })
  expect(listed(own.seen).filter.properties?.source).toEqual({ eq: GARMIN })
  expect(listed(own.seen).count).toBe(false)
  expect(stages.textContent).toContain("the spans drawn are")
  cleanup()

  // priority: the source that fed the most of the totals, and a list past
  // its page says how much of it is drawn.
  const ranked = mount([withPolicy("priority")], RUN, SESSION, {
    totals: fed([WHOOP, WHOOP, WHOOP, GARMIN]),
    spansMore: true,
  })
  stages = await screen.findByRole("group", { name: "Sleep stages" })
  await within(stages).findByRole("img", { name: /^Sleep stages from/ })
  expect(listed(ranked.seen).filter.properties?.source).toEqual({ eq: WHOOP })
  expect(stages.textContent).toContain(
    "Shows the first 4 spans that start inside this window; more follow."
  )
  expect(stages.textContent).toContain("the first of 2 sources")
})

it("draws no span before the metric's own codes give each its colour", async () => {
  let answer!: () => void
  const metricsAnswer = new Promise<void>((resolve) => (answer = resolve))
  const { seen } = mount([STAGES_VIEW()], RUN, SESSION, { metricsAnswer })
  const stages = await screen.findByRole("group", { name: "Sleep stages" })
  // The spans would be read by now; nothing is drawn or read until the
  // metric record says which colour each code wears.
  await act(async () => {})
  expect(stages.querySelector("rect[data-code]")).toBe(null)
  expect(seen.some((s) => s.filter.properties?.metric)).toBe(false)
  answer()
  const rows = await within(stages).findByRole("img", {
    name: /^Sleep stages from/,
  })
  const fill = (code: string) =>
    rows.querySelector(`rect[data-code=${code}]`)!.getAttribute("fill")
  expect(fill("rem")).toBe("var(--series-1)")
  expect(fill("awake")).toBe("var(--series-4)")
})

it("says a spans block draws coded amounts, and reads nothing for a coded gauge", async () => {
  const door = {
    ...METRICS[0],
    id: "health/sleep_stage",
    properties: {
      name: "Door",
      form: "gauge",
      codeSet: ["open", "closed"],
    },
  }
  const { seen } = mount([STAGES_VIEW()], RUN, SESSION, { metrics: [door] })
  const section = await screen.findByRole("region", { name: "Stages" })
  await waitFor(() =>
    expect(within(section).getByRole("note").textContent).toBe(
      "This spans block draws a coded amount metric, and Door is a gauge."
    )
  )
  expect(seen.some((s) => s.aggregate)).toBe(false)
  expect(seen.some((s) => s.filter.properties?.metric)).toBe(false)
})

it("leaves out the blocks that read a window the record does not hold", async () => {
  const open = record(SESSION.identity, "run-2", {
    sport: "running",
    begins: "2026-10-11T05:25:00Z",
    by: { ref: GARMIN },
  })
  mount(
    [
      STAGES_VIEW([
        { heading: { text: "Kept" } },
        {
          chart: {
            label: "Pulse",
            metrics: [{ ref: PULSE }],
            reducers: [{ reducer: "avg" }],
          },
        },
        { spans: { label: "Sleep stages", metric: { ref: STAGE } } },
      ]),
      { ...STAGES_VIEW(), id: "fitness/z-only-spans" },
    ],
    open
  )
  const section = await screen.findByRole("region", { name: "Stages" })
  expect(within(section).getByRole("heading", { name: "Kept" })).toBeTruthy()
  expect(within(section).queryByRole("group", { name: "Pulse" })).toBe(null)
  expect(within(section).queryByRole("group", { name: "Sleep stages" })).toBe(
    null
  )
  expect(section.textContent).not.toContain("Nothing to read here")
  // A view with nothing else to draw is left out whole.
  expect(screen.getAllByRole("region")).toHaveLength(1)
})

// The axis took the answer's `unit`, the metric's, whatever the reducer: a
// heart rate chart of the time in a zone captioned its minutes /min.
it("captions a line with its reducer's own unit, and a count with none", async () => {
  const minutes = (reducer: AggregateReducer, value: string, unit?: string) =>
    answer(
      CURVE.rows.map((r) =>
        row({ ...r, values: [{ reducer, value, ...(unit ? { unit } : {}) }] })
      )
    )
  const chart = (reducers: Record<string, unknown>[]) =>
    view("fitness/session", {
      name: "Effort",
      window: WINDOW,
      sections: [{ chart: { metrics: [{ ref: PULSE }], reducers } }],
    })
  const caption = async () => {
    const group = await screen.findByRole("group", {
      name: "Heart rate during this session",
    })
    return group.querySelector("[data-axis=left] text.fill-muted-foreground")
      ?.textContent
  }
  mount(
    [chart([{ reducer: "durationIn", low: 95, high: 114 }])],
    RUN,
    SESSION,
    {
      curve: minutes({ durationIn: [95, 114] }, "4", "min"),
    }
  )
  expect(await caption()).toBe("min")
  cleanup()
  mount([chart([{ reducer: "count" }])], RUN, SESSION, {
    curve: minutes("count", "12"),
  })
  expect(await caption()).toBeUndefined()
})

it("says this console does not draw lanes, and draws the rest", async () => {
  mount([
    view("fitness/session", {
      name: "Lanes",
      window: WINDOW,
      sections: [
        { lanes: { label: "States", rows: [{ metric: { ref: STAGE } }] } },
        { heading: { text: "After" } },
      ],
    }),
  ])
  const section = await screen.findByRole("region", { name: "Lanes" })
  expect(within(section).getByRole("note").textContent).toBe(
    "This console does not draw lanes yet."
  )
  expect(within(section).getByRole("heading", { name: "After" })).toBeTruthy()
})

it("says so far over a window that ends at the time of reading", async () => {
  const running = record(SESSION.identity, "run-3", {
    begins: "2026-10-11T05:25:00Z",
    by: { ref: GARMIN },
  })
  mount(
    [
      view("fitness/session", {
        name: "Open",
        window: { ...WINDOW, open: true },
        sections: [{ heading: { text: "Now" } }],
      }),
      view("fitness/session-closed", {
        name: "Closed",
        window: WINDOW,
        sections: [{ heading: { text: "Then" } }],
      }),
    ],
    running
  )
  const open = await screen.findByRole("region", { name: "Open" })
  expect(open.textContent).toContain("so far")
  const closed = await screen.findByRole("region", { name: "Closed" })
  expect(closed.textContent).not.toContain("so far")
})

it("words a view's last window by its length, never as the record or so far", async () => {
  mount([
    view("fitness/session-week", {
      name: "Week",
      window: { last: "P7D", aboutSelf: true },
      sections: [{ heading: { text: "Lately" } }],
    }),
  ])
  const week = await screen.findByRole("region", { name: "Week" })
  expect(week.textContent).toContain("the last 7 days")
  expect(week.textContent).not.toContain("so far")
})

it("says a chart over a last window measured nothing in the last day", async () => {
  mount(
    [
      view("fitness/session-day", {
        name: "Day",
        window: { last: "P1D", aboutSelf: true },
        sections: [
          {
            chart: {
              label: "Pulse",
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "avg" }],
            },
          },
        ],
      }),
    ],
    RUN,
    SESSION,
    { curve: answer([]) }
  )
  const day = await screen.findByRole("region", { name: "Day" })
  await waitFor(() =>
    expect(day.textContent).toContain(
      "No heart rate was measured in the last day."
    )
  )
  expect(day.textContent).not.toContain("this session")
})

it("says a last window's span in words a block can use", () => {
  const last = (ms: number) => ({
    window: { start: 0, end: ms },
    open: true,
    lastWindow: true,
    observed: [],
  })
  expect(spanWords(last(86_400_000), "room")).toEqual({
    during: "in the last day",
    whole: "the last day",
    soFar: "",
  })
  expect(spanWords(last(7 * 86_400_000), "room").during).toBe(
    "in the last 7 days"
  )
  expect(spanWords(last(6 * 3_600_000), "room").whole).toBe("the last 6 hours")
  expect(
    spanWords(
      { window: { start: 0, end: 1 }, open: true, observed: [] },
      "cycle"
    )
  ).toEqual({
    during: "during this cycle",
    whole: "the whole cycle",
    soFar: "so far",
  })
})

it("keeps a view of tables that hide when empty hidden until one has rows", async () => {
  const tableOf = (kind: string) => ({
    table: {
      kind: { ref: `substrate.reamde.dev/core/kind/${kind}` },
      via: "session",
      empty: "hide",
    },
  })
  const { seen, container } = mount([
    view("fitness/a-empty", {
      name: "No routes",
      sections: [tableOf(ROUTE.identity)],
    }),
    view("fitness/b-laps", { name: "Laps", sections: [tableOf(LAP.identity)] }),
  ])
  const laps = await screen.findByRole("region", { name: "Laps" })
  await waitFor(() => expect(laps.hidden).toBe(false))
  await waitFor(() =>
    expect(seen.some((s) => s.filter.kinds?.includes(ROUTE.identity))).toBe(
      true
    )
  )
  await act(async () => {})
  const empty = container.querySelector<HTMLElement>(
    "section[data-record-view='fitness/a-empty']"
  )!
  expect(empty.hidden).toBe(true)
  expect(empty.querySelector("table")).toBe(null)
})

it("leaves out a chart under empty: hide where no metric was measured, and its view with it", async () => {
  const { container } = mount(
    [
      view("fitness/session-night", {
        name: "Night",
        window: { start: "begins", end: "ends", about: "who", source: "by" },
        sections: [
          {
            chart: {
              label: "Pulse",
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "avg" }],
              empty: "hide",
            },
          },
        ],
      }),
    ],
    RUN,
    SESSION,
    { curve: answer([]) }
  )
  const night = () =>
    container.querySelector<HTMLElement>(
      "section[data-record-view='fitness/session-night']"
    )
  // The chart is left out once its read answers nothing, and the view,
  // all of whose blocks hide, stays hidden.
  await waitFor(() =>
    expect(night()?.querySelector("[role=group][aria-label=Pulse]")).toBe(null)
  )
  expect(night()!.hidden).toBe(true)
  expect(night()!.textContent).not.toContain("was measured")
})

it("keeps a chart left out under empty: hide out while another chart is zoomed", async () => {
  const ENERGY = `${METRIC}/health/energy_expended`
  const { container } = mount(
    [
      view("fitness/session", {
        name: "During this session",
        window: WINDOW,
        sections: [
          {
            chart: {
              label: "Pulse",
              metrics: [{ ref: PULSE }],
              reducers: [{ reducer: "avg" }],
            },
          },
          {
            chart: {
              label: "Energy",
              metrics: [{ ref: ENERGY }],
              reducers: [{ reducer: "sum" }],
              empty: "hide",
            },
          },
        ],
      }),
    ],
    RUN,
    SESSION,
    { unmeasured: [ENERGY] }
  )
  const section = await screen.findByRole("region", {
    name: "During this session",
  })
  const energy = () =>
    container.querySelector("[role=group][aria-label=Energy]")
  const chart = await within(section).findByRole("group", {
    name: "Heart rate during this session",
  })
  await waitFor(() => expect(energy()).toBe(null))
  act(() => chart.focus())
  fireEvent.keyDown(chart, { key: "ArrowRight" })
  fireEvent.keyDown(chart, { key: "Enter" })
  await screen.findByRole("button", { name: "Show the whole session" })
  expect(energy()).toBe(null)
  expect(section.textContent).not.toContain("was measured")
})

it("draws a chart under empty: hide where one metric holds a value", async () => {
  mount([
    view("fitness/session-night", {
      name: "Night",
      window: { start: "begins", end: "ends", about: "who", source: "by" },
      sections: [
        {
          chart: {
            label: "Pulse",
            metrics: [{ ref: PULSE }],
            reducers: [{ reducer: "avg" }],
            empty: "hide",
          },
        },
      ],
    }),
  ])
  const night = await screen.findByRole("region", { name: "Night" })
  await waitFor(() => expect(night.hidden).toBe(false))
  await waitFor(() =>
    expect(night.querySelector("[aria-roledescription=chart]")).toBeTruthy()
  )
})

describe("a chart whose line has no value over the window", () => {
  const HRV = `${METRIC}/health/hrv_rmssd`
  const HRV_METRIC = record(METRIC, "health/hrv_rmssd", {
    title: "Heart rate variability (RMSSD)",
    name: "Heart rate variability (RMSSD)",
    form: "gauge",
    unit: "ms",
    bucketPlace: "end",
  })
  // A WHOOP night: WHOOP writes no samples, its HRV as one vendor summary
  // over the sleep, and its cycle's heart rate as summaries from the same
  // sleep onset to the next, which reach past the night.
  const NIGHT = record(SESSION.identity, "night-1", {
    begins: "2026-10-10T22:40:00Z",
    ends: "2026-10-11T06:50:00Z",
    by: { ref: WHOOP },
  })
  function summary(
    id: string,
    metric: string,
    value: string,
    unit: string,
    at: string,
    endsAt: string,
    statistic: string
  ): SubstrateRecord {
    return record(OBSERVATION, id, {
      metric: { ref: metric },
      form: "summary",
      value: { value, unit },
      statistic,
      at,
      endsAt,
      source: { ref: WHOOP },
    })
  }
  const SUMMARIES = [
    summary(
      "whoop/hrv",
      HRV,
      "62",
      "ms",
      "2026-10-10T22:40:00Z",
      "2026-10-11T06:50:00Z",
      "vendor"
    ),
    summary(
      "whoop/cycle-avg",
      PULSE,
      "64",
      "/min",
      "2026-10-10T22:40:00Z",
      "2026-10-11T23:05:00Z",
      "avg"
    ),
  ]
  const NIGHT_VIEW = view("fitness/session-night", {
    name: "Night",
    window: WINDOW,
    sections: [
      {
        chart: {
          label: "Heart rate and HRV",
          metrics: [{ ref: PULSE }, { ref: HRV }],
          reducers: [
            { reducer: "avg" },
            { reducer: "min" },
            { reducer: "max" },
          ],
          empty: "hide",
        },
      },
    ],
  })

  it("draws a WHOOP night's HRV from its nightly summary", async () => {
    const { seen } = mount([NIGHT_VIEW], NIGHT, SESSION, {
      metrics: [...METRICS, HRV_METRIC],
      unmeasured: [PULSE, HRV],
      curve: answer([]),
      summaries: SUMMARIES,
    })
    const night = await screen.findByRole("region", { name: "Night" })
    const hrv = await within(night).findByRole("group", {
      name: "Heart rate variability (RMSSD)",
    })
    const chart = await within(hrv).findByRole("group", {
      name: "Heart rate variability (RMSSD) during this session, from summaries",
    })
    expect(night.hidden).toBe(false)
    // One line, held level across the night it covers.
    // One level stroke across the night, never a slope.
    const line = chart.querySelector("[data-slot=chart-level]")
    expect(line?.getAttribute("d")).toMatch(/^M[\d.]+ [\d.]+H[\d.]+$/)
    expect(hrv.textContent).toContain(
      "No samples of heart rate variability (RMSSD) were kept during this session."
    )
    // The cycle's average heart rate reaches past the night, so heart rate
    // stays unmeasured rather than drawn from the day's value.
    const pulse = within(night).getByRole("group", { name: "Heart rate" })
    await waitFor(() =>
      expect(pulse.textContent).toContain(
        "No heart rate was measured during this session."
      )
    )
    const read = seen.find(
      (s) =>
        (s.filter.properties?.form as { eq?: string } | undefined)?.eq ===
          "summary" &&
        (s.filter.properties?.metric as { eq?: string }).eq === HRV
    )!
    expect(read.filter.properties).toMatchObject({
      at: { gte: "2026-10-10T22:40:00.000Z" },
      endsAt: { lte: "2026-10-11T06:50:00.000Z" },
    })
  })

  it("still leaves the chart out under empty: hide where no summary lies inside the window", async () => {
    const { container } = mount([NIGHT_VIEW], NIGHT, SESSION, {
      metrics: [...METRICS, HRV_METRIC],
      unmeasured: [PULSE, HRV],
      curve: answer([]),
      summaries: SUMMARIES.slice(1),
    })
    const night = () =>
      container.querySelector<HTMLElement>(
        "section[data-record-view='fitness/session-night']"
      )
    await waitFor(() => expect(night()?.hidden).toBe(true))
    expect(night()!.textContent).not.toContain("was measured")
  })
})

describe("a chart's summaries read", () => {
  const HRV = `${METRIC}/health/hrv_rmssd`
  const HRV_METRIC = record(METRIC, "health/hrv_rmssd", {
    title: "Heart rate variability (RMSSD)",
    name: "Heart rate variability (RMSSD)",
    form: "gauge",
    unit: "ms",
  })
  const NIGHT = record(SESSION.identity, "night-2", {
    begins: "2026-10-10T22:40:00Z",
    ends: "2026-10-11T06:50:00Z",
    by: { ref: WHOOP },
  })
  const VIEW = view("fitness/session-night", {
    name: "Night",
    window: WINDOW,
    sections: [
      {
        chart: {
          label: "HRV",
          metrics: [{ ref: HRV }],
          reducers: [{ reducer: "avg" }],
          empty: "hide",
        },
      },
    ],
  })
  function summary(
    id: string,
    source: string,
    value: string,
    at: string,
    endsAt: string
  ) {
    return record(OBSERVATION, id, {
      metric: { ref: HRV },
      form: "summary",
      value: { value, unit: "ms" },
      statistic: "vendor",
      at,
      endsAt,
      source: { ref: source },
    })
  }

  it("says a failed summaries read with a retry, never hiding the chart", async () => {
    mount([VIEW], NIGHT, SESSION, {
      metrics: [...METRICS, HRV_METRIC],
      unmeasured: [HRV],
      curve: answer([]),
      summaryFailures: 1,
      summaries: [
        summary(
          "w",
          WHOOP,
          "62",
          "2026-10-10T22:40:00Z",
          "2026-10-11T06:50:00Z"
        ),
      ],
    })
    const night = await screen.findByRole("region", { name: "Night" })
    const retry = await within(night).findByRole("button", {
      name: "Read again",
    })
    expect(night.hidden).toBe(false)
    expect(night.querySelector("[data-slot=read-failed]")).toBeTruthy()
    fireEvent.click(retry)
    await waitFor(() =>
      expect(night.querySelector("[data-slot=chart-level]")).toBeTruthy()
    )
  })

  it("says a summary that is a bound rather than drawing it as a value", async () => {
    mount([VIEW], NIGHT, SESSION, {
      metrics: [...METRICS, HRV_METRIC],
      unmeasured: [HRV],
      curve: answer([]),
      summaries: [
        record(OBSERVATION, "w-bound", {
          metric: { ref: HRV },
          form: "summary",
          value: { value: "20", unit: "ms", comparator: "<" },
          statistic: "vendor",
          at: "2026-10-10T22:40:00Z",
          endsAt: "2026-10-11T06:50:00Z",
          source: { ref: WHOOP },
        }),
      ],
    })
    const night = await screen.findByRole("region", { name: "Night" })
    await waitFor(() =>
      expect(night.textContent).toContain("are bounds, not values")
    )
    expect(night.textContent).toContain("<20 ms")
    expect(night.querySelector("[data-slot=chart-level]")).toBe(null)
  })

  it("reads past the first page, where the record's own source's summary is", async () => {
    // The Garmin's readings inside the night fill the first page; WHOOP's
    // night, which the record names, is on the second and wins.
    const naps = Array.from({ length: 3 }, (_, i) =>
      summary(
        `g${i}`,
        GARMIN,
        "40",
        `2026-10-11T01:0${i}:00Z`,
        `2026-10-11T01:0${i + 1}:00Z`
      )
    )
    const { seen } = mount([VIEW], NIGHT, SESSION, {
      metrics: [...METRICS, HRV_METRIC],
      unmeasured: [HRV],
      curve: answer([]),
      summaryPageSize: 3,
      summaries: [
        ...naps,
        summary(
          "w",
          WHOOP,
          "62",
          "2026-10-11T00:00:00Z",
          "2026-10-11T06:50:00Z"
        ),
      ],
    })
    const night = await screen.findByRole("region", { name: "Night" })
    await waitFor(() =>
      expect(night.querySelector("[data-slot=chart-level]")).toBeTruthy()
    )
    expect(
      seen.filter(
        (r) =>
          (r.filter.properties?.form as { eq?: string } | undefined)?.eq ===
          "summary"
      )
    ).toHaveLength(2)
    // The night's own source's summary alone is drawn.
    expect(night.textContent).toContain("whoop-4-0")
    expect(night.textContent).not.toContain("garmin")
  })
})
