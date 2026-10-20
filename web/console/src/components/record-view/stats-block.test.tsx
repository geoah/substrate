// @vitest-environment jsdom
/** A record view's stats block: a tile per item drawn from one batched read
 * over the record's window (a dial with its band word, a bar against the
 * baseline, a number with its delta in a tone), a metric with no row said
 * as not measured, a metric no record holds dropped from the read and said
 * on its own tile, and the record's own properties as tiles, a dotted path
 * reading into a keyed map. */

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
  METRIC,
  METRICS,
  RECOVERY,
  SLEEP_STAGE,
  STEPS_METRIC,
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
    properties: {
      strain: { type: "float", displayName: "Strain" },
      scores: { type: "float", keyed: true },
    },
  },
}

const ENERGY = `${METRIC}/health/energy_expended`
const GHOST = `${METRIC}/health/ghost`

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

const DAY = record(CYCLE.identity, "c-1", {
  begins: "2026-10-10T22:00:00Z",
  ends: "2026-10-11T06:00:00Z",
  strain: 12.4,
  scores: { sleepConsistency: 81 },
})

function row(r: Partial<AggregateRow>): AggregateRow {
  return {
    start: "2026-10-10T22:00:00Z",
    end: "2026-10-11T06:00:00Z",
    values: [],
    count: 1,
    bounded: 0,
    synthetic: false,
    ...r,
  }
}

/** What the window read answers per metric. */
const VALUES: Record<string, AggregateRow["values"]> = {
  [RECOVERY]: [{ reducer: "last", value: "72", unit: "%" }],
  [HEART]: [
    { reducer: "avg", value: "58", unit: "/min" },
    { reducer: "max", value: "131", unit: "/min" },
  ],
  [ENERGY]: [{ reducer: "sum", value: "2100", unit: "kJ" }],
}

/** Three earlier days of each metric, for the baseline. */
const DAYS: Record<string, string[]> = {
  [RECOVERY]: ["60", "64", "62"],
  [HEART]: ["61", "62", "63"],
}

/** What a test answers an aggregate read with instead, where it does. */
type Answer = (aggregate: Aggregate) => Response | AggregateRow[] | undefined

function refusal(status: number, problems: string[]): Response {
  return new Response(
    JSON.stringify({
      error: { code: "unprocessable", message: problems[0], problems },
    }),
    { status }
  )
}

function mount(
  sections: unknown[],
  answer?: Answer,
  window: Record<string, unknown> = { start: "begins", end: "ends" }
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
        return metric
          ? ok(metric)
          : new Response(
              JSON.stringify({ error: { code: "not_found", message: "no" } }),
              { status: 404 }
            )
      }
      const raw = url.searchParams.get("aggregate")
      if (!raw) return ok({ records: [], head: 1, generation: "g" })
      const aggregate = JSON.parse(raw) as Aggregate
      seen.push(aggregate)
      const own = answer?.(aggregate)
      if (own instanceof Response) return own
      if (own) {
        return ok({
          exact: true,
          rows: own,
          head: { changelog: 1, generation: "g" },
        })
      }
      const entries = aggregate.metrics ?? []
      const ghost = entries.findIndex((e) => e.ref === GHOST)
      if (ghost >= 0) {
        return new Response(
          JSON.stringify({
            error: {
              code: "unprocessable",
              message: "no such metric",
              problems: [`aggregate.metrics[${ghost}].ref: no record holds it`],
            },
          }),
          { status: 422 }
        )
      }
      const rows = aggregate.bucket
        ? entries.flatMap((e) =>
            (DAYS[e.ref] ?? []).map((value, i) =>
              row({
                start: `2026-10-0${7 + i}T21:00:00Z`,
                end: `2026-10-0${8 + i}T21:00:00Z`,
                group: { metric: e.ref },
                values: e.reducers!.map((r) => ({ reducer: r, value })),
              })
            )
          )
        : entries.flatMap((e) =>
            VALUES[e.ref]
              ? [row({ group: { metric: e.ref }, values: VALUES[e.ref] })]
              : []
          )
      return ok({ exact: true, rows, head: { changelog: 1, generation: "g" } })
    })
  )
  const view = record(RECORD_VIEW_KIND, "fitness/cycle", {
    appliesTo: { ref: `substrate.reamde.dev/core/kind/${CYCLE.identity}` },
    name: "This cycle",
    window,
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

function tile(group: HTMLElement, label: string): HTMLElement {
  const tiles = [
    ...group.querySelectorAll<HTMLElement>("[data-slot=stat-tile]"),
  ]
  const found = tiles.find((t) => t.firstElementChild?.textContent === label)
  if (!found)
    throw new Error(
      `no tile ${label}: ${tiles.map((t) => t.textContent).join(" | ")}`
    )
  return found
}

it("draws a tile per item from one read, with bands, a baseline and its delta", async () => {
  const { seen } = mount([
    {
      stats: {
        label: "Figures",
        baseline: 30,
        bands: [
          { metric: { ref: RECOVERY }, high: 34, label: "Red", tone: "poor" },
          {
            metric: { ref: RECOVERY },
            low: 34,
            high: 67,
            label: "Yellow",
            tone: "fair",
          },
          { metric: { ref: RECOVERY }, low: 67, label: "Green", tone: "good" },
        ],
        items: [
          {
            metric: { ref: RECOVERY },
            label: "Recovery",
            show: "dial",
            min: 0,
            max: 100,
            better: "higher",
          },
          {
            metric: { ref: HEART },
            reducer: "avg",
            label: "Average heart rate",
            show: "bar",
            better: "lower",
          },
          { metric: { ref: ENERGY }, reducer: "sum", label: "Energy" },
          { metric: { ref: STEPS_METRIC }, reducer: "sum", label: "Steps" },
          { property: "strain" },
          { property: "scores.sleepConsistency" },
        ],
      },
    },
  ])
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(tile(group, "Energy").textContent).toContain("2,100")
  )
  // The dial and its band word.
  const recovery = tile(group, "Recovery")
  expect(
    recovery.querySelector("[data-slot=stat-dial]")?.textContent
  ).toContain("72")
  expect(recovery.querySelector("[data-slot=stat-band]")?.textContent).toBe(
    "Green"
  )
  // The delta against the three earlier days' average of 62, good upwards.
  // Recovery is in percent: its change is in points, with no relative part.
  const delta = recovery.querySelector<HTMLElement>("[data-slot=stat-delta]")!
  expect(delta.textContent).toBe("+10 points vs the 3-day average of 62 %")
  expect(delta.style.color).toBe("var(--ok)")
  expect(recovery.textContent).toContain(
    "against the average day of the previous 3"
  )
  // A bar marks its value; a fall in heart rate is good.
  const heart = tile(group, "Average heart rate")
  expect(heart.textContent).toContain("58")
  expect(
    heart.querySelector("[data-slot=range-bar]")?.getAttribute("data-value-at")
  ).toBeTruthy()
  expect(
    heart.querySelector<HTMLElement>("[data-slot=stat-delta]")!.style.color
  ).toBe("var(--ok)")
  // A metric with no row over the window.
  expect(tile(group, "Steps").textContent).toContain(
    "Not measured during this cycle"
  )
  // The record's own properties: the declared label, and a keyed map's
  // entry in words.
  expect(tile(group, "Strain").textContent).toContain("12.4")
  expect(tile(group, "Sleep consistency").textContent).toContain("81")
  // One window read and one baseline read, the four metrics batched.
  const value = seen.filter((a) => !a.bucket)
  expect(value).toHaveLength(1)
  expect(value[0].metrics?.map((m) => m.ref)).toEqual([
    RECOVERY,
    HEART,
    ENERGY,
    STEPS_METRIC,
  ])
  expect(seen.filter((a) => a.bucket)).toHaveLength(1)
})

it("drops a metric no record holds from the read and says so on its tile alone", async () => {
  const { seen } = mount([
    {
      stats: {
        label: "Figures",
        items: [
          { metric: { ref: GHOST }, label: "Ghost" },
          { metric: { ref: RECOVERY }, label: "Recovery" },
        ],
      },
    },
  ])
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(tile(group, "Ghost").textContent).toContain(
      `No record names the metric ${GHOST}`
    )
  )
  expect(tile(group, "Recovery").textContent).toContain("72")
  // Asked once with the missing metric, once without it.
  expect(seen.map((a) => a.metrics?.map((m) => m.ref))).toEqual([
    [GHOST, RECOVERY],
    [RECOVERY],
  ])
  expect(within(group).queryByText(/This read failed/)).toBe(null)
})

// Only a `.ref` refusal says a metric names no record. A unit, reducer or
// statistic the metric does not admit is the view's to fix, in the
// server's words.
it("shows an entry refusal that is no missing metric as the server's words", async () => {
  const problem =
    "aggregate.metrics[0]: the summaries from x hold the statistics avg and max, and a row reads one; name it with aggregate.metrics[0].statistic"
  const { seen } = mount(
    [
      {
        stats: {
          label: "Figures",
          items: [
            { metric: { ref: HEART }, label: "Pulse", from: "summaries" },
          ],
        },
      },
    ],
    () => refusal(422, [problem])
  )
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(group.textContent).toContain(`This read failed: ${problem}`)
  )
  expect(group.textContent).not.toContain("No record names the metric")
  // Asked once: nothing is dropped.
  expect(seen).toHaveLength(1)
})

it("draws the tiles when only the baseline read fails, and says so", async () => {
  mount(
    [
      {
        stats: {
          label: "Figures",
          baseline: 365,
          items: [{ metric: { ref: RECOVERY }, label: "Recovery" }],
        },
      },
    ],
    (aggregate) =>
      aggregate.bucket
        ? refusal(422, ["aggregate.budget.maxBuckets: over 1440"])
        : undefined
  )
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(tile(group, "Recovery").textContent).toContain("72")
  )
  expect(tile(group, "Recovery").querySelector("[data-slot=stat-delta]")).toBe(
    null
  )
  expect(group.textContent).toContain(
    "This read failed: aggregate.budget.maxBuckets: over 1440"
  )
})

// Grouped by code, the server answers no row for a state the window held
// none of; read ungrouped it answers 0.
it("reads a time in a state ungrouped and draws a window without the state as 0", async () => {
  const { seen } = mount(
    [
      {
        stats: {
          label: "Figures",
          baseline: 30,
          items: [
            {
              metric: { ref: SLEEP_STAGE },
              reducer: "timeInState",
              code: "rem",
              label: "REM",
            },
          ],
        },
      },
    ],
    (aggregate) => {
      const values = (value: string) => [
        { reducer: { timeInState: ["rem"] }, value, unit: "ms" },
      ]
      if (!aggregate.bucket) {
        return [row({ group: { metric: SLEEP_STAGE }, values: values("0") })]
      }
      return ["0", "3600000", "0"].map((value, i) =>
        row({
          start: `2026-10-0${7 + i}T21:00:00Z`,
          end: `2026-10-0${8 + i}T21:00:00Z`,
          group: { metric: SLEEP_STAGE },
          values: values(value),
        })
      )
    }
  )
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(tile(group, "REM").textContent).not.toContain("Not measured")
  )
  expect(tile(group, "REM").textContent).toContain(
    "against the average day of the previous 3"
  )
  expect(seen.every((a) => a.groupBy === undefined)).toBe(true)
})

// A gauge's last reading of a day is one arbitrary instant (a room's light
// at midnight): its baseline is each day's average, whatever the tile's
// reducer, while an amount keeps its sum a day.
it("compares a gauge's last reading with the average of each earlier day's average", async () => {
  const { seen } = mount(
    [
      {
        stats: {
          label: "Figures",
          baseline: 30,
          items: [
            { metric: { ref: HEART }, label: "Pulse" },
            { metric: { ref: ENERGY }, reducer: "sum", label: "Energy" },
          ],
        },
      },
    ],
    (aggregate) => {
      const entries = aggregate.metrics ?? []
      if (!aggregate.bucket) {
        return [
          row({
            group: { metric: HEART },
            values: [{ reducer: "last", value: "70", unit: "/min" }],
          }),
          row({
            group: { metric: ENERGY },
            values: [{ reducer: "sum", value: "2100", unit: "kJ" }],
          }),
        ]
      }
      // Each earlier day: its last pulse was 0 at midnight, its average 60.
      return entries.flatMap((e) =>
        [0, 1, 2].map((i) =>
          row({
            start: `2026-10-0${7 + i}T21:00:00Z`,
            end: `2026-10-0${8 + i}T21:00:00Z`,
            group: { metric: e.ref },
            values: e.reducers!.map((r) => ({
              reducer: r,
              value:
                e.ref === ENERGY
                  ? "2000"
                  : r === "avg"
                    ? "60"
                    : r === "last"
                      ? "0"
                      : null,
            })),
          })
        )
      )
    }
  )
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(
      tile(group, "Pulse").querySelector("[data-slot=stat-delta]")?.textContent
    ).toBe("+10 /min (+17%) vs the 3-day average of 60 /min")
  )
  expect(
    tile(group, "Energy").querySelector("[data-slot=stat-delta]")?.textContent
  ).toBe("+100 kJ (+5%) vs the 3-day average of 2,000 kJ")
  const baseline = seen.find((a) => a.bucket)!
  expect(baseline.metrics?.map((m) => [m.ref, m.reducers])).toEqual([
    [HEART, ["avg"]],
    [ENERGY, ["sum"]],
  ])
})

it("leaves out a block under empty: hide where no item holds a value, and its view with it", async () => {
  const { seen } = mount(
    [
      {
        stats: {
          label: "Scores",
          empty: "hide",
          items: [
            { metric: { ref: STEPS_METRIC }, reducer: "sum", label: "Steps" },
            { property: "scores.recovery" },
          ],
        },
      },
    ],
    () => []
  )
  const view = document.querySelector<HTMLElement>("[data-record-view]")!
  // Its read is answered, and nothing is drawn: the view stays hidden.
  await waitFor(() => expect(seen).toHaveLength(1))
  await new Promise((r) => setTimeout(r, 50))
  expect(view.querySelector("[data-slot=stat-tile]")).toBe(null)
  expect(screen.queryByText(/Loading/)).toBe(null)
  expect(view.hidden).toBe(true)
})

it("draws a block under empty: hide where one item holds a value", async () => {
  mount(
    [
      {
        stats: {
          label: "Scores",
          empty: "hide",
          items: [
            { metric: { ref: STEPS_METRIC }, reducer: "sum", label: "Steps" },
            { property: "scores.sleepConsistency" },
          ],
        },
      },
    ],
    () => []
  )
  const group = await screen.findByRole("group", { name: "Scores" })
  expect(tile(group, "Steps").textContent).toContain("Not measured")
  expect(tile(group, "Sleep consistency").textContent).toContain("81")
  expect(
    document.querySelector<HTMLElement>("[data-record-view]")!.hidden
  ).toBe(false)
})

it("says an item not measured in a last window by its length", async () => {
  mount(
    [
      {
        stats: {
          label: "Figures",
          items: [{ metric: { ref: STEPS_METRIC }, label: "Steps" }],
        },
      },
    ],
    () => [],
    { last: "P1D" }
  )
  const group = await screen.findByRole("group", { name: "Figures" })
  await waitFor(() =>
    expect(tile(group, "Steps").textContent).toContain(
      "Not measured in the last day"
    )
  )
})
