// @vitest-environment jsdom
/** A record view's table block: the records pointing here with the view's
 * columns, a converted twin moved under the row it was converted from and
 * one whose referent is not listed kept in place, both marked; the
 * records sharing this record's metric with the observed defaults, the
 * page's own row current, and their values over time as points, the twin
 * hollow and a result in another unit listed and not drawn; a kind binding
 * no observed or result trait drawing its own `value` and nesting rows with
 * no word of the lab's; and a table that hides when empty drawing nothing. */

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

import { CORE_PACKAGE } from "@/lib/api/http"
import type { KindInfo, RecordFilter, SubstrateRecord } from "@/lib/api/types"
import { OBSERVED_TRAIT } from "@/lib/core-kinds"
import {
  AUTHORITY,
  GARMIN,
  KINDS,
  METRIC,
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

function kindOf(
  name: string,
  traits: string[],
  properties: Record<string, unknown>
): KindInfo {
  return {
    identity: `${AUTHORITY}/lab/${name}`,
    name,
    authority: AUTHORITY,
    package: "lab",
    version: 1,
    source: "installed",
    description: "",
    definition: {
      authority: AUTHORITY,
      package: "lab",
      names: { singular: name },
      traits,
      properties,
    },
  }
}

const REPORT = kindOf("report", [], { lab: { type: "string" } })
const RESULT = kindOf(
  "result",
  [`${OBSERVED_TRAIT}(single)`, `${CORE_PACKAGE}/result`],
  {
    metric: { type: "reference", kind: METRIC },
    value: { type: "quantity" },
    interpretation: { type: "json" },
    referenceRanges: { type: "json" },
    at: { type: "datetime" },
    source: { type: "reference" },
    about: { type: "reference", kind: "any" },
    report: { type: "reference", kind: REPORT.identity },
    twinOf: { type: "reference", kind: `${AUTHORITY}/lab/result` },
    specimen: { type: "string", displayName: "Specimen" },
  }
)
/** A kind binding neither observed nor result, whose `value` is its own
 * number and whose `about` is a topic in words. */
const TASK = kindOf("task", [], {
  title: { type: "string" },
  value: { type: "decimal" },
  strain: { type: "float" },
  count: { type: "int" },
  about: { type: "string" },
  report: { type: "reference", kind: REPORT.identity },
  parent: { type: "reference", kind: `${AUTHORITY}/lab/task` },
})
const ALL = [...KINDS, REPORT, RESULT, TASK]

const GLUCOSE = `${METRIC}/lab/glucose`

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

const mmol = (value: string) => ({ value, unit: "mmol/L" })
const RANGE = { low: mmol("3.9"), high: mmol("5.6") }

const THE_REPORT = record(REPORT.identity, "march", { lab: "Example Lab" })
const pathOf = (r: SubstrateRecord) => `${r.kind}/${r.id}`

function result(
  id: string,
  at: string,
  value: { value: string; unit: string; comparator?: string },
  more: Record<string, unknown> = {}
): SubstrateRecord {
  return record(RESULT.identity, id, {
    metric: { ref: GLUCOSE },
    at,
    value,
    source: { ref: GARMIN },
    report: { ref: pathOf(THE_REPORT) },
    specimen: "serum",
    ...more,
  })
}

const MARCH = result("march", "2026-03-02T08:00:00Z", mmol("5.1"), {
  interpretation: [{ code: "N" }],
  referenceRanges: [RANGE],
})
/** March's value converted to another unit. */
const MARCH_TWIN = result(
  "march-mg",
  "2026-03-02T08:00:00Z",
  { value: "92", unit: "mg/dL" },
  { twinOf: { ref: pathOf(MARCH) } }
)
const SEPTEMBER = result("september", "2026-09-07T08:00:00Z", mmol("6.2"), {
  interpretation: [{ code: "H" }],
  referenceRanges: [RANGE, { text: "fasting" }],
})
/** September's value converted again, in the same unit. */
const SEPTEMBER_TWIN = result(
  "september-copy",
  "2026-09-07T08:00:00Z",
  mmol("6.2"),
  { twinOf: { ref: pathOf(SEPTEMBER) } }
)
/** A twin of a result this list does not hold. */
const STRAY = result("stray", "2026-06-01T08:00:00Z", mmol("4.8"), {
  twinOf: { ref: `${RESULT.identity}/elsewhere` },
})

function mount(
  view: Record<string, unknown>,
  rec: SubstrateRecord,
  kind: KindInfo,
  listed: SubstrateRecord[],
  more = false,
  listedKind: KindInfo = RESULT
) {
  const seen: RecordFilter[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://test")
      const ok = (body: unknown) =>
        new Response(JSON.stringify(body), { status: 200 })
      if (url.pathname !== "/api/v1/records") {
        return new Response("{}", { status: 404 })
      }
      const filter = JSON.parse(
        url.searchParams.get("filter") ?? "{}"
      ) as RecordFilter
      seen.push(filter)
      if (filter.kinds?.includes(listedKind.identity)) {
        return ok({
          records: listed,
          head: 1,
          generation: "g",
          ...(more ? { cursor: "next" } : {}),
        })
      }
      return ok({ records: [], head: 1, generation: "g" })
    })
  )
  const v = record(RECORD_VIEW_KIND, "lab/view", {
    appliesTo: { ref: `substrate.reamde.dev/core/kind/${kind.identity}` },
    ...view,
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const { container } = render(
    <QueryClientProvider client={client}>
      <RecordViews views={viewsOf([v])} record={rec} kind={kind} kinds={ALL} />
    </QueryClientProvider>
  )
  return { seen, container }
}

const ids = (table: HTMLElement) =>
  [...table.querySelectorAll("tbody tr")].map((r) =>
    r.getAttribute("data-record")!.split("/").pop()
  )

it("lists the records pointing here with the view's columns, a twin under the row it came from", async () => {
  const { seen } = mount(
    {
      name: "This report",
      sections: [
        {
          table: {
            label: "Results",
            kind: { ref: `substrate.reamde.dev/core/kind/${RESULT.identity}` },
            via: "report",
            columns: [
              "metric",
              "value",
              "interpretation",
              "referenceRanges",
              "specimen",
            ],
            orderBy: "at:asc",
            nest: "twinOf",
          },
        },
      ],
    },
    THE_REPORT,
    REPORT,
    [MARCH_TWIN, MARCH, SEPTEMBER, STRAY],
    true
  )
  const group = await screen.findByRole("group", { name: "Results" })
  const table = await within(group).findByRole("table")
  expect([...table.querySelectorAll("th")].map((th) => th.textContent)).toEqual(
    ["Metric", "Value", "Interpretation", "Reference ranges", "Specimen"]
  )
  // The twin moves under March; the stray twin stays in place; both say so.
  expect(ids(table)).toEqual(["march", "march-mg", "september", "stray"])
  const rows = [...table.querySelectorAll<HTMLElement>("tbody tr")]
  expect(rows[1].getAttribute("data-nest-ref")).toBe("true")
  expect(rows[1].querySelector("td")!.className).toContain("pl-6")
  expect(rows[3].getAttribute("data-nest-ref")).toBe("true")
  expect(rows[3].querySelector("td")!.className).not.toContain("pl-6")
  expect(rows[0].getAttribute("data-nest-ref")).toBe(null)
  // The value, the flag in words, the first range and how many more.
  expect(rows[0].textContent).toContain("5.1 mmol/L")
  expect(rows[0].textContent).toContain("normal")
  expect(rows[2].textContent).toContain("high")
  expect(rows[2].textContent).toContain("3.9 to 5.6 mmol/L +1 more")
  expect(rows[0].textContent).toContain("serum")
  expect(group.textContent).toContain("and more")
  // The records whose report is this one, about the owner.
  const read = seen.find((f) => f.kinds?.includes(RESULT.identity))!
  expect(read.properties).toEqual({
    report: { eq: pathOf(THE_REPORT) },
    about: { exists: false },
  })
})

it("draws the results sharing this one's metric as points, its own ringed and the twin hollow", async () => {
  const { seen } = mount(
    {
      name: "This analyte over time",
      sections: [
        {
          table: {
            kind: { ref: `substrate.reamde.dev/core/kind/${RESULT.identity}` },
            sameAs: "metric",
            chart: true,
            nest: "twinOf",
          },
        },
      ],
    },
    MARCH,
    RESULT,
    [SEPTEMBER_TWIN, SEPTEMBER, MARCH_TWIN, MARCH]
  )
  const section = await screen.findByRole("region", {
    name: "This analyte over time",
  })
  const table = await within(section).findByRole("table")
  expect([...table.querySelectorAll("th")].map((th) => th.textContent)).toEqual(
    ["When", "Value", "Flag", "Range", "Source"]
  )
  // The page's own result is current.
  const own = table.querySelector<HTMLElement>("tr[aria-current=true]")!
  expect(own.getAttribute("data-record")).toBe(pathOf(MARCH))
  expect(own.className).toContain("bg-selection")
  // Three points in mmol/L, the converted copy hollow; the mg/dL twin is
  // listed and not drawn.
  const chart = section.querySelector("[data-slot=points-chart]")!
  await waitFor(() =>
    expect(chart.querySelectorAll("[data-slot=point]")).toHaveLength(3)
  )
  expect(chart.querySelectorAll("[data-slot=point][data-hollow]")).toHaveLength(
    1
  )
  expect(chart.querySelectorAll("[data-slot=point-highlight]")).toHaveLength(1)
  expect(section.textContent).toContain(
    "1 result in a unit this chart cannot convert to is listed and not drawn."
  )
  const read = seen.find((f) => f.kinds?.includes(RESULT.identity))!
  expect(read.properties?.metric).toEqual({ eq: GLUCOSE })
})

it("converts a reading in another unit of the same measure and lists one of another measure", async () => {
  const MASS = `${METRIC}/body/lean_mass`
  const lean = (id: string, at: string, value: string, unit: string) =>
    result(id, at, { value, unit }, { metric: { ref: MASS } })
  const KG = lean("a", "2026-01-02T08:00:00Z", "52.5", "kg")
  const G = lean("b", "2026-04-02T08:00:00Z", "53100", "g")
  const PERCENT = lean("c", "2026-07-02T08:00:00Z", "71", "%")
  mount(
    {
      name: "Lean mass",
      sections: [
        {
          table: {
            kind: { ref: `substrate.reamde.dev/core/kind/${RESULT.identity}` },
            sharing: ["metric", "channel"],
            chart: true,
          },
        },
      ],
    },
    KG,
    RESULT,
    [KG, G, PERCENT]
  )
  const section = await screen.findByRole("region", { name: "Lean mass" })
  const chart = await waitFor(() => {
    const c = section.querySelector("[data-slot=points-chart]")
    expect(c).toBeTruthy()
    return c!
  })
  // 53100 g is drawn as 53.1 kg beside 52.5 kg; the percent is listed.
  expect(chart.querySelectorAll("[data-slot=point]")).toHaveLength(2)
  const ys = [...chart.querySelectorAll("circle[data-slot=point]")].map((c) =>
    Number(c.getAttribute("cy"))
  )
  expect(ys[1]).toBeLessThan(ys[0])
  expect(section.textContent).toContain(
    "1 result in a unit this chart cannot convert to is listed and not drawn."
  )
})

it("draws one hairline chain per channel, and a bound as a marker", async () => {
  const region = (id: string, at: string, value: string, channel?: string) =>
    result(id, at, mmol(value), channel ? { channel } : {})
  const listed = [
    region("t1", "2026-01-02T08:00:00Z", "5", "trunk"),
    region("a1", "2026-02-02T08:00:00Z", "3", "left_arm"),
    region("t2", "2026-03-02T08:00:00Z", "5.5", "trunk"),
    region("a2", "2026-04-02T08:00:00Z", "3.2", "left_arm"),
    result("low", "2026-05-02T08:00:00Z", {
      value: "0.5",
      unit: "mmol/L",
      comparator: "<",
    }),
  ]
  const { seen } = mount(
    {
      name: "By region",
      sections: [
        {
          table: {
            kind: { ref: `substrate.reamde.dev/core/kind/${RESULT.identity}` },
            sharing: ["metric", "channel"],
            chart: true,
          },
        },
      ],
    },
    listed[0],
    RESULT,
    listed
  )
  const section = await screen.findByRole("region", { name: "By region" })
  const chart = await waitFor(() => {
    const c = section.querySelector("[data-slot=points-chart]")
    expect(c).toBeTruthy()
    return c!
  })
  expect(chart.querySelectorAll("[data-slot=point]")).toHaveLength(5)
  expect(chart.querySelectorAll("path[data-bound=below]")).toHaveLength(1)
  expect(chart.querySelectorAll("[data-slot=point-hairline]")).toHaveLength(2)
  const read = seen.find((f) => f.kinds?.includes(RESULT.identity))!
  expect(read.properties).toMatchObject({
    metric: { eq: GLUCOSE },
    channel: { eq: "trunk" },
  })
})

it("draws a kind's own value and nests its rows with no word of the lab's where it binds no observed or result trait", async () => {
  const task = (id: string, more: Record<string, unknown>) =>
    record(TASK.identity, id, {
      report: { ref: pathOf(THE_REPORT) },
      ...more,
    })
  const BOIL = task("boil", { title: "Boil", value: 120.5 })
  const PRIMER = task("primer", {
    title: "Primer",
    value: 30,
    about: "paint",
    parent: { ref: `${TASK.identity}/boil` },
  })
  const { seen } = mount(
    {
      name: "This report",
      sections: [
        {
          table: {
            label: "Tasks",
            kind: { ref: `substrate.reamde.dev/core/kind/${TASK.identity}` },
            via: "report",
            columns: ["title", "value"],
            nest: "parent",
          },
        },
      ],
    },
    THE_REPORT,
    REPORT,
    [PRIMER, BOIL],
    false,
    TASK
  )
  const group = await screen.findByRole("group", { name: "Tasks" })
  const table = await within(group).findByRole("table")
  expect(ids(table)).toEqual(["boil", "primer"])
  const rows = [...table.querySelectorAll<HTMLElement>("tbody tr")]
  const cells = rows.map((r) =>
    [...r.querySelectorAll("td")].map((td) => td.textContent)
  )
  expect(cells[0][1]).toContain("120.5")
  expect(cells[1][1]).toContain("30")
  // Nested under Boil by indentation alone.
  expect(cells[1][0]).toBe("Primer")
  expect(rows[1].querySelector("td")!.className).toContain("pl-6")
  // Its `about` is a topic, not who the record is about: no narrowing.
  const read = seen.find((f) => f.kinds?.includes(TASK.identity))!
  expect(read.properties).toEqual({ report: { eq: pathOf(THE_REPORT) } })
})

it("draws nothing for a table that hides when empty, and says None for one that says so", async () => {
  const { container } = mount(
    {
      name: "Lab results",
      sections: [
        { heading: { text: "Kept" } },
        {
          table: {
            label: "Hidden",
            kind: { ref: `substrate.reamde.dev/core/kind/${RESULT.identity}` },
            via: "metric",
            empty: "hide",
          },
        },
        {
          table: {
            label: "Said",
            kind: { ref: `substrate.reamde.dev/core/kind/${RESULT.identity}` },
            via: "metric",
          },
        },
      ],
    },
    record(`${METRIC}`, "lab/glucose", { name: "Glucose" }),
    KINDS[2],
    []
  )
  const said = await screen.findByRole("group", { name: "Said" })
  await waitFor(() => expect(said.textContent).toContain("None."))
  expect(screen.queryByRole("group", { name: "Hidden" })).toBe(null)
  expect(container.querySelector("section")!.hidden).toBe(false)
})

// A float or decimal declares no display precision: a cell shows two
// fraction digits at most, and the stored digits on hover.
it("draws a fractional property to two fraction digits at most, an integer as it is", async () => {
  const task = (id: string, more: Record<string, unknown>) =>
    record(TASK.identity, id, { report: { ref: pathOf(THE_REPORT) }, ...more })
  mount(
    {
      name: "This report",
      sections: [
        {
          table: {
            label: "Tasks",
            kind: { ref: `substrate.reamde.dev/core/kind/${TASK.identity}` },
            via: "report",
            columns: ["title", "strain", "value", "count"],
          },
        },
      ],
    },
    THE_REPORT,
    REPORT,
    [
      task("run", { title: "Run", strain: 8.3985, value: "19.999", count: 7 }),
      task("walk", { title: "Walk", strain: 12, value: "4.5", count: 12 }),
    ],
    false,
    TASK
  )
  const group = await screen.findByRole("group", { name: "Tasks" })
  const table = await within(group).findByRole("table")
  const cells = [...table.querySelectorAll("tbody tr")].map((r) =>
    [...r.querySelectorAll("td")].map((td) => td.textContent)
  )
  expect(cells).toEqual([
    ["Run", "8.4", "20", "7"],
    ["Walk", "12", "4.5", "12"],
  ])
  expect(within(table).getByText("8.4").getAttribute("title")).toBe("8.3985")
})
