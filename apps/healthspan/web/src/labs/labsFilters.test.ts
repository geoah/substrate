import { describe, expect, it } from "vitest"
import type { LabStatus } from "../rules/labs"
import type { TimeDomain } from "../lib/timeDomain"
import {
  activeSecondary,
  applyFilters,
  DEFAULT_FILTERS,
  filterCounts,
  filtersToParams,
  isFiltered,
  parseFilters,
  type Filterable,
  type LabsFilters,
} from "./labsFilters"

const domain: TimeDomain = {
  start: new Date("2024-10-25T12:00:00Z"),
  end: new Date("2026-10-25T12:00:00Z"),
}

const view = (
  code: string,
  o: {
    important?: boolean
    statuses?: LabStatus[]
    panel?: string
    finding?: boolean
    aliases?: string[]
    results?: { at: string; source?: string }[]
  } = {}
): Filterable => ({
  code,
  name: code.replace(/_/g, " "),
  panel: o.panel ?? "Lipid panel",
  important: o.important ?? true,
  statuses: new Set(o.statuses ?? ["ok"]),
  finding: o.finding ? {} : undefined,
  entry: { aliases: o.aliases ?? [] },
  series: [
    { results: o.results ?? [{ at: "2026-06-01T00:00:00Z", source: "lab/a" }] },
  ],
})

const views = [
  view("ldl_cholesterol", {
    statuses: ["range", "target"],
    finding: true,
    aliases: ["LDL-C"],
  }),
  view("triglycerides", { statuses: ["target"], finding: true }),
  view("vitamin_d", {
    statuses: ["range", "target"],
    finding: true,
    panel: "Vitamins",
  }),
  view("tsh", { statuses: ["ok"], panel: "Thyroid" }),
  view("psa", {
    statuses: ["ok", "overdue"],
    finding: true,
    panel: "Prostate",
    results: [{ at: "2023-01-01T00:00:00Z", source: "lab/b" }],
  }),
  view("labs/igg", { important: false, statuses: ["ok"], panel: "Other" }),
  view("labs/old", {
    important: false,
    statuses: ["ok"],
    panel: "Other",
    results: [{ at: "2022-01-01T00:00:00Z", source: "lab/a" }],
  }),
]

const f = (o: Partial<LabsFilters>): LabsFilters => ({
  ...DEFAULT_FILTERS,
  ...o,
})
const ids = (o: Partial<LabsFilters>) =>
  applyFilters(views, f(o), domain).map((v) => v.code)

describe("the query round trip", () => {
  it("omits every default", () => {
    expect(filtersToParams(DEFAULT_FILTERS).toString()).toBe("")
    expect(parseFilters(new URLSearchParams())).toEqual(DEFAULT_FILTERS)
    expect(isFiltered(DEFAULT_FILTERS)).toBe(false)
  })
  it("comma-joins status in a fixed order and round-trips", () => {
    const filters = f({
      scope: "all",
      status: ["target", "range"],
      range: "5Y",
      panel: "Lipid panel",
      source: "lab/demo-clinic",
      q: "chol",
    })
    const params = filtersToParams(filters)
    expect(params.get("status")).toBe("range,target")
    expect(params.toString()).toBe(
      "scope=all&status=range%2Ctarget&range=5Y&panel=Lipid+panel&source=lab%2Fdemo-clinic&q=chol"
    )
    expect(parseFilters(params)).toEqual({
      ...filters,
      status: ["range", "target"],
    })
    expect(isFiltered(filters)).toBe(true)
  })
  it("drops unknown values back to the defaults", () => {
    const p = parseFilters(
      new URLSearchParams("scope=some&status=range,bogus&range=3Y")
    )
    expect(p).toEqual(f({ status: ["range"] }))
  })
})

describe("applyFilters", () => {
  it("defaults to the important entries in the range, keeping findings", () => {
    // psa is outside the range but has a finding; igg is not important.
    expect(ids({})).toEqual([
      "ldl_cholesterol",
      "triglycerides",
      "vitamin_d",
      "tsh",
      "psa",
    ])
  })
  it("hides a view with no result in the range and no finding", () => {
    expect(ids({ scope: "all" })).not.toContain("labs/old")
    expect(ids({ scope: "all", range: "All" })).toContain("labs/old")
  })
  it("ORs within status", () => {
    expect(ids({ status: ["range"] })).toEqual(["ldl_cholesterol", "vitamin_d"])
    expect(ids({ status: ["target"] })).toEqual([
      "ldl_cholesterol",
      "triglycerides",
      "vitamin_d",
    ])
    expect(ids({ status: ["range", "overdue"] })).toEqual([
      "ldl_cholesterol",
      "vitamin_d",
      "psa",
    ])
  })
  it("lets ok exclude range and target but not overdue", () => {
    expect(ids({ status: ["ok"] })).toEqual(["tsh", "psa"])
    expect(ids({ status: ["overdue"] })).toEqual(["psa"])
  })
  it("ANDs across groups", () => {
    expect(ids({ scope: "all", status: ["ok"], panel: "Other" })).toEqual([
      "labs/igg",
    ])
    expect(ids({ status: ["range"], panel: "Vitamins" })).toEqual(["vitamin_d"])
    expect(ids({ status: ["range"], q: "LDL" })).toEqual(["ldl_cholesterol"])
  })
  it("keeps a panel", () => {
    expect(ids({ panel: "Lipid panel" })).toEqual([
      "ldl_cholesterol",
      "triglycerides",
    ])
  })
  it("keeps views with a result from the source inside the range", () => {
    // old's only lab/a result is older than the range and it has no finding.
    expect(ids({ scope: "all", source: "lab/a" })).not.toContain("labs/old")
    expect(ids({ source: "lab/b", range: "All" })).toEqual(["psa"])
    expect(ids({ source: "lab/a" })).toHaveLength(4)
  })
  it("matches a view kept for a finding on any of its results", () => {
    // psa's only lab/b result is older than the range, but psa is overdue.
    expect(ids({ status: ["overdue"], source: "lab/b" })).toEqual(["psa"])
    expect(ids({ source: "lab/b" })).toEqual(["psa"])
  })
  it("searches the name, the code and the aliases without case", () => {
    expect(ids({ q: "Vitamin" })).toEqual(["vitamin_d"])
    expect(ids({ q: "LABS/IGG", scope: "all" })).toEqual(["labs/igg"])
    expect(ids({ q: "ldl-c" })).toEqual(["ldl_cholesterol"])
  })
})

describe("clearing", () => {
  it("resets every key, panel and source included", () => {
    const set = f({
      scope: "all",
      status: ["range"],
      range: "5Y",
      panel: "Lipid panel",
      source: "lab/a",
      q: "x",
    })
    expect(activeSecondary(set)).toBe(4)
    const cleared = { ...set, ...DEFAULT_FILTERS }
    expect(filtersToParams(cleared).toString()).toBe("")
    expect(activeSecondary(cleared)).toBe(0)
  })
})

describe("filterCounts", () => {
  it("counts the scopes before every other filter", () => {
    const c = filterCounts(
      views,
      f({ status: ["range"], panel: "Thyroid" }),
      domain
    )
    expect(c.scope).toEqual({ all: 7, important: 5 })
  })
  it("counts each status among the views passing the other groups", () => {
    const c = filterCounts(views, f({ status: ["range"] }), domain)
    expect(c.status).toEqual({ range: 2, target: 3, overdue: 1, ok: 2 })
  })
})
