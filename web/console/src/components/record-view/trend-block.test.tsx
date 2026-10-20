// @vitest-environment jsdom
/** A record view's trend block: a strip per item over the calendar days
 * ending with the record's, every item in one read with the trailing
 * average, the record's day tinted, the average dashed, and a caption with
 * the record's day against that average or that nothing was measured. The
 * suite runs in Europe/Athens, so the record's evening in UTC is the next
 * local day. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import type {
  Aggregate,
  AggregateRow,
  KindInfo,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  AUTHORITY,
  HEART,
  KINDS,
  METRICS,
  RECOVERY,
} from "@/lib/observations.test-fixtures"
import { RECORD_VIEW_KIND, viewsOf } from "@/lib/record-view"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

import { RecordViews } from "./record-views"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const CYCLE: KindInfo = {
  identity: `${AUTHORITY}/fitness/cycle`,
  name: "cycle",
  authority: AUTHORITY,
  package: "fitness",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: AUTHORITY,
    package: "fitness",
    names: { singular: "cycle" },
    traits: ["substrate.reamde.dev/core/temporal(range)"],
    properties: {},
  },
}

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

/** Starts at 01:00 on Oct 11 in Athens. */
const DAY = record(CYCLE.identity, "c-1", {
  begins: "2026-10-10T22:00:00Z",
  ends: "2026-10-11T20:00:00Z",
})

/** One row per local day of the read's range for each metric: recovery
 * 50, 51, ... with a trailing average of 52; heart rate on every day but
 * the last. */
function rowsFor(aggregate: Aggregate): AggregateRow[] {
  const start = new Date(Date.parse(aggregate.range!.gte))
  const end = Date.parse(aggregate.range!.lt)
  const out: AggregateRow[] = []
  for (let i = 0; ; i++) {
    const s = new Date(
      start.getFullYear(),
      start.getMonth(),
      start.getDate() + i
    )
    const e = new Date(
      start.getFullYear(),
      start.getMonth(),
      start.getDate() + i + 1
    )
    if (s.getTime() >= end) break
    const last = e.getTime() >= end
    for (const entry of aggregate.metrics ?? []) {
      if (entry.ref === HEART && last) continue
      out.push({
        start: s.toISOString(),
        end: e.toISOString(),
        group: { metric: entry.ref },
        count: 1,
        bounded: 0,
        synthetic: false,
        values: [
          ...entry.reducers!.map((r) => ({
            reducer: r,
            value: String(entry.ref === RECOVERY ? 50 + i : 60),
            unit: entry.ref === RECOVERY ? "%" : "/min",
          })),
          {
            reducer: "avg",
            value: entry.ref === RECOVERY ? "52" : "60",
            rolling: 3,
            of: entry.reducers![0],
            n: 3,
          },
        ],
      })
    }
  }
  return out
}

function mount(
  sections: unknown[],
  rows: (aggregate: Aggregate) => AggregateRow[] = rowsFor
) {
  const seen: Aggregate[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://test")
      const ok = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200 })
      if (url.pathname !== "/api/v1/records") {
        const metric = METRICS.find((m) =>
          url.pathname.endsWith(`/${encodeURIComponent(m.id)}`)
        )
        return metric ? ok(metric) : new Response("{}", { status: 404 })
      }
      const raw = url.searchParams.get("aggregate")
      if (!raw) return ok({ records: [], head: 1, generation: "g" })
      const aggregate = JSON.parse(raw) as Aggregate
      seen.push(aggregate)
      return ok({
        exact: true,
        rows: rows(aggregate),
        head: { changelog: 1, generation: "g" },
      })
    })
  )
  const view = record(RECORD_VIEW_KIND, "fitness/cycle", {
    appliesTo: { ref: `substrate.reamde.dev/core/kind/${CYCLE.identity}` },
    name: "This cycle",
    window: { start: "begins", end: "ends" },
    sections,
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RecordViews
        views={viewsOf([view])}
        record={DAY}
        kind={CYCLE}
        kinds={[...KINDS, CYCLE]}
      />
    </QueryClientProvider>
  )
  return { seen }
}

it("draws a strip per item over the days ending with the record's, its day tinted and the average dashed", async () => {
  const { seen } = mount([
    {
      trend: {
        label: "The last week",
        span: 7,
        bucket: "day",
        rolling: 3,
        bands: [
          { metric: { ref: RECOVERY }, low: 67, label: "Green", tone: "good" },
        ],
        items: [
          {
            metric: { ref: RECOVERY },
            label: "Recovery",
            show: "bars",
            better: "higher",
          },
          { metric: { ref: HEART }, reducer: "avg" },
        ],
      },
    },
  ])
  const block = await screen.findByRole("group", { name: "The last week" })
  const recovery = await within(block).findByRole("group", { name: "Recovery" })
  await waitFor(() =>
    expect(
      recovery.querySelector("[data-slot=trend-caption]")?.textContent
    ).toContain("on Oct 11")
  )
  const caption = recovery.querySelector("[data-slot=trend-caption]")!
  // The record's day, 56, against the trailing average of 52.
  // Recovery is in percent: its change is in points.
  expect(caption.textContent).toMatch(
    /^56.* on Oct 11, \+4 points vs the average of 52 %$/
  )
  expect(caption.querySelector("span")!.style.color).toBe("var(--ok)")
  // The record's day is the seventh of seven, tinted.
  const highlight = recovery.querySelector("[data-slot=chart-highlight]")!
  const bars = [...recovery.querySelectorAll("g[data-series=last] path")].map(
    (r) => Number(/-?[\d.]+/.exec(r.getAttribute("d")!)![0])
  )
  expect(bars).toHaveLength(7)
  expect(Number(highlight.getAttribute("x"))).toBeGreaterThan(
    Math.max(...bars.slice(0, 6))
  )
  // The trailing average, dashed; the band washes behind the strip.
  expect(
    recovery
      .querySelector("[data-series='last:rolling'] path")
      ?.getAttribute("stroke-dasharray")
  ).toBe("4 2")
  expect(recovery.querySelectorAll("[data-slot=chart-band]")).toHaveLength(1)
  // The second strip takes the metric's name, and its record's day holds
  // nothing.
  const heart = within(block).getByRole("group", { name: "Heart rate" })
  expect(heart.querySelector("[data-slot=trend-caption]")!.textContent).toBe(
    "Nothing measured in this day"
  )
  expect(heart.querySelectorAll("[data-slot=chart-band]")).toHaveLength(0)
  // One read: both metrics by the local day over seven days, rolling.
  expect(seen).toHaveLength(1)
  expect(seen[0]).toMatchObject({
    metrics: [{ ref: RECOVERY }, { ref: HEART }],
    bucket: { calendar: "day", timezone: "Europe/Athens" },
    rolling: { buckets: 3, reducers: ["avg"], through: "previous" },
  })
  expect(new Date(seen[0].range!.gte).getDate()).toBe(5)
  expect(new Date(seen[0].range!.lt).getDate()).toBe(12)
})

const SCORES = {
  trend: {
    label: "Scores",
    span: 7,
    empty: "hide",
    items: [{ metric: { ref: RECOVERY }, label: "Recovery" }],
  },
}

it("leaves out a block under empty: hide where no strip holds a value, and its view with it", async () => {
  // The days hold only a trailing average, which is no value of a strip.
  const { seen } = mount([SCORES], (aggregate) =>
    rowsFor(aggregate).map((row) => ({
      ...row,
      values: (row.values ?? []).filter((v) => v.rolling !== undefined),
    }))
  )
  const view = document.querySelector<HTMLElement>("[data-record-view]")!
  await waitFor(() => expect(seen).toHaveLength(1))
  await new Promise((r) => setTimeout(r, 50))
  expect(view.querySelector("[data-slot=trend-caption]")).toBe(null)
  expect(view.hidden).toBe(true)
})

it("draws a block under empty: hide where a strip holds a value", async () => {
  mount([SCORES])
  const block = await screen.findByRole("group", { name: "Scores" })
  await within(block).findByRole("group", { name: "Recovery" })
  expect(
    document.querySelector<HTMLElement>("[data-record-view]")!.hidden
  ).toBe(false)
})
