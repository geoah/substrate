import { describe, expect, it } from "vitest"
import { demoLabIndex } from "../demo/labs"
import { DEFAULT_DISPLAY } from "../lib/display"
import { sourceMarkers, sourcesByCount } from "../lib/sourceMarkers"
import { pageDomain } from "../lib/timeDomain"
import { applyFilters, DEFAULT_FILTERS, type LabsFilters } from "./labsFilters"
import {
  bandLines,
  byPanel,
  cardBands,
  chartBands,
  detailsLines,
  flatOrder,
  labsSummary,
  labViews,
  metricCard,
  metricInfo,
  rangesLine,
  resultWhere,
  selectedContext,
  seriesStatus,
  trendText,
  type LabView,
} from "./view"

// The demo member, as `?demo&today=2026-10-25` shows it.
const today = new Date("2026-10-25T00:00:00")
const index = demoLabIndex(today)
const views = labViews(index, today)
const markers = sourceMarkers(
  sourcesByCount(index.results, index.sources),
  index.sources
)
const byCode = (code: string) => {
  const v = views.find((x) => x.code === code)
  if (!v) throw new Error(`no view ${code}`)
  return v
}
const card = (code: string, range: LabsFilters["range"] = "2Y") =>
  metricCard(
    index,
    byCode(code),
    pageDomain(range, today, index.results),
    markers
  )

describe("statuses on the demo member", () => {
  it("holds the statuses the filters read", () => {
    expect([...byCode("ldl_cholesterol").statuses].sort()).toEqual([
      "range",
      "target",
    ])
    expect([...byCode("vitamin_d").statuses]).toContain("range")
    expect([...byCode("triglycerides").statuses]).toEqual(["target"])
    expect([...byCode("psa").statuses]).toContain("overdue")
    expect([...byCode("creatinine").statuses]).toEqual(["ok"])
  })

  it("gives each card its word, tone and edge", () => {
    expect(card("ldl_cholesterol").status).toMatchObject({
      key: "range",
      badge: { word: "High", tone: "danger" },
      edge: "danger",
    })
    expect(card("triglycerides").status).toMatchObject({
      key: "target",
      badge: { word: "Borderline High", tone: "warning" },
      edge: "warning",
    })
    expect(card("psa").status).toMatchObject({
      key: "overdue",
      badge: { word: "Overdue", tone: "info" },
      overdueBadge: false,
      edge: "info",
    })
    const hdl = card("hdl_cholesterol").status
    expect(hdl.key).toBe("ok")
    expect(hdl.badge?.tone).toBe("success")
    expect(hdl.edge).toBeUndefined()
  })

  it("gives a result with no range and no target no badge", () => {
    const lean = byCode("health/lean_mass")
    expect(seriesStatus(lean.lead, lean.entry, today)).toEqual({
      key: "none",
      overdueBadge: false,
    })
  })
})

describe("cards on the demo member", () => {
  it("shows an empty span for an entry that starts in the last year", () => {
    const crp = card("hs_crp", "5Y")
    expect(crp.emptySpan?.label).toMatch(
      /^No results before [A-Z][a-z]{2,3} 2026$/
    )
    expect(crp.trendText.label).toBe("Too few results for a trend")
    expect(card("creatinine", "All").emptySpan).toBeUndefined()
  })

  it("fits trends both ways and tones them by the goal", () => {
    const hdl = card("hdl_cholesterol")
    expect(hdl.trend?.direction).toBe("up")
    expect(hdl.trendTone).toBe("success")
    const alt = card("alt")
    expect(alt.trend?.direction).toBe("up")
    expect(alt.trendTone).toBe("danger")
    const cho = card("total_cholesterol")
    expect(cho.trend?.direction).toBe("down")
    expect(cho.trendTone).toBe("success")
    expect(cho.trendText.label).toMatch(/^\d+\.\d%\/month$/)
    expect(cho.trendText.tooltip).toMatch(
      /^Best-fit line through the \d+ results/
    )
    expect(cho.trendText.aria).toMatch(/falling .* the better direction$/)
  })

  it("lists both labs in the legend with counts summing to the results", () => {
    const ldl = card("ldl_cholesterol")
    expect(ldl.legend.map((l) => l.name).sort()).toEqual([
      "Demo Clinic",
      "Demo Lab",
    ])
    expect(ldl.legend.reduce((a, l) => a + l.count, 0)).toBe(
      ldl.series.results.length
    )
    expect(new Set(ldl.legend.map((l) => l.marker)).size).toBe(2)
    expect(ldl.latestLine?.text).toMatch(
      /^Latest: \d+ [A-Z][a-z]{2,3} 2026, Demo Lab$/
    )
    expect(ldl.description).toBe("Low-density lipoprotein cholesterol")
  })

  it("flags points by the rule they break", () => {
    const ldl = card("ldl_cholesterol")
    expect(ldl.points[ldl.points.length - 1].tone).toBe("danger")
    const tri = card("triglycerides")
    expect(tri.points[tri.points.length - 1].tone).toBe("warning")
  })

  it("counts the legend in the range and says so", () => {
    // 2Y holds 6 of LDL's 9 panels: 4 of the clinic's 6 and 2 of the lab's 3.
    expect(card("ldl_cholesterol").legend.map((l) => l.label)).toEqual([
      "Demo Clinic, 4 of 6 results",
      "Demo Lab, 2 of 3 results",
    ])
    expect(card("ldl_cholesterol", "All").legend.map((l) => l.label)).toEqual([
      "Demo Clinic, 6 results",
      "Demo Lab, 3 results",
    ])
  })

  it("says why a chart is empty when every result predates the range", () => {
    const psa = card("psa")
    expect(psa.drawn).toBe(0)
    expect(psa.emptyText).toMatch(
      /^No results in this range; latest [A-Z][a-z]{2,3} 2024$/
    )
  })

  it("speaks a flat trend once", () => {
    const flat = trendText(
      {
        ratePerMonth: 0.3,
        direction: "flat",
        n: 4,
        from: { at: "", value: 1 },
        to: { at: "", value: 1 },
      },
      byCode("ldl_cholesterol").entry
    )
    expect(flat.aria).toBe("Trend: flat, 0.3 percent of the average per month")
  })

  it("shows the HbA1c twin and the DXA regions", () => {
    expect(card("hba1c").twin).toMatch(/%$/)
    const fat = byCode("health/body_fat_fraction")
    expect(fat.series.map((s) => s.channel ?? "")).toEqual([
      "",
      "trunk",
      "arms",
      "legs",
    ])
    expect(card("health/body_fat_fraction").regionNote).toBeUndefined()
  })

  it("reads every unit form into the display unit", () => {
    // Vitamin B12 is written only in pg/mL and read in pmol/L.
    const b12 = byCode("vitamin_b12").lead
    expect(b12.specimens.every((s) => s.via === "converted")).toBe(true)
    expect(b12.results.every((r) => r.unit === "pmol/L")).toBe(true)
    // The clinic's vitamin D is in ng/mL: its twin shows, but for the
    // newest, whose twin is not written yet.
    const vitd = byCode("vitamin_d").lead.specimens
    const clinic = vitd.filter((s) => s.shown.source === "lab/demo-clinic")
    expect(clinic.map((s) => s.via)).toEqual([
      "twin",
      "twin",
      "twin",
      "twin",
      "twin",
      "converted",
    ])
    // Apolipoprotein B: two metrics of one form, in g/L.
    const apob = byCode("apolipoprotein_b").lead
    expect(new Set(apob.results.map((r) => r.metric)).size).toBe(2)
    expect(apob.results.every((r) => r.unit === "g/L")).toBe(true)
    // PSA's metric record names no code: it is the entry's by its id.
    expect(byCode("psa").subject.entry?.code).toBe("psa")
  })

  it("orders a panel by the catalogue", () => {
    const lipids = byPanel(views).find((p) => p.panel === "Lipid panel")
    expect(lipids?.views.slice(0, 4).map((v) => v.code)).toEqual([
      "total_cholesterol",
      "triglycerides",
      "ldl_cholesterol",
      "hdl_cholesterol",
    ])
  })

  it("counts results and sources in the subtitle", () => {
    expect(labsSummary(index, markers)).toMatch(
      /^\d[\d,]* results from 3 sources, /
    )
  })
})

describe("readouts and bands", () => {
  const ldl = byCode("ldl_cholesterol")
  it("says which result is selected, with its own lab range", () => {
    const clinic = ldl.lead.results.find((r) => r.source === "lab/demo-clinic")
    const lab = ldl.lead.results.find((r) => r.source === "lab/demo")
    if (!clinic || !lab) throw new Error("demo has both labs")
    expect(selectedContext(index, ldl, clinic)).toMatch(
      /^Selected: \d+ [A-Z][a-z]{2,3} \d{4}, Demo Clinic, lab up to 3.40 mmol\/L$/
    )
    expect(selectedContext(index, ldl, lab)).toMatch(
      /^Selected: \d+ [A-Z][a-z]{2,3} \d{4}, Demo Lab, fasting, lab up to 3.00 mmol\/L$/
    )
  })
  it("writes the three details lines", () => {
    expect(card("triglycerides").detailsLines).toEqual([
      "Demo Clinic, 4 of 6 results. Demo Lab, 2 of 3 results.",
      "Lower is better",
      "Checked every 12 months",
    ])
    expect(detailsLines(byCode("health/lean_mass"), [])).toEqual(["", "", ""])
  })
  it("draws target zones in their tones and the lab range as a neutral dotted fill", () => {
    const newest = ldl.lead.results[ldl.lead.results.length - 1]
    const bands = chartBands(index, ldl.entry, newest)
    expect(bands.map((b) => [b.tone, b.variant, b.stroke])).toEqual([
      ["success", undefined, undefined],
      ["danger", undefined, undefined],
      ["neutral", undefined, "dotted"],
    ])
    expect(
      chartBands(index, ldl.entry, newest, {
        targetZones: false,
        labRange: false,
      })
    ).toEqual([])
    // No catalogue target: still neutral, never a target colour.
    const wbc = byCode("wbc")
    const w = chartBands(
      index,
      undefined,
      wbc.lead.results[wbc.lead.results.length - 1]
    )
    expect(w.every((b) => b.tone === "neutral")).toBe(true)
  })
  it("draws no zone on a card with no point in the range", () => {
    expect(
      cardBands(
        index,
        card("psa"),
        pageDomain("2Y", today, index.results),
        DEFAULT_DISPLAY.labs
      )
    ).toEqual([])
  })
  it("prints the catalogue bands with their unit", () => {
    expect(bandLines(ldl).map((b) => b.text)).toEqual([
      "Optimal: up to 3.00 mmol/L",
      "High: 3.00 mmol/L or more",
    ])
  })
  it("says no trend below three results", () => {
    const t = trendText(undefined, ldl.entry)
    expect(t.label).toBe("Too few results for a trend")
    expect(t.tooltip).toBe("A trend needs three results in this range.")
    expect(t.aria).toBe(
      "Too few results for a trend: it needs three results in this range"
    )
  })
})

describe("headline lines", () => {
  it("names the ranges that applied to a result, the target once without a unit", () => {
    const tri = byCode("triglycerides")
    const newest = tri.lead.results[tri.lead.results.length - 1]
    expect(rangesLine(tri, newest)).toBe(
      "Lab up to 2.30 mmol/L, target up to 1.70"
    )
    expect(rangesLine(tri, newest, false)).toBe("Lab up to 2.30 mmol/L")
    const lean = byCode("health/lean_mass")
    expect(rangesLine(lean, lean.lead.results[0])).toBe("")
  })
  it("gives the date, source and fasting of a result", () => {
    const tri = byCode("triglycerides")
    const w = resultWhere(index, tri.lead.results[tri.lead.results.length - 1])
    expect(w.source).toBe("Demo Lab")
    expect(w.fasting).toBe(true)
    expect(w.date).toMatch(/^\d+ [A-Z][a-z]{2,3} 2026$/)
  })
})

describe("flatOrder", () => {
  it("orders by status, then panel, then catalogue", () => {
    const keys = flatOrder(views).map((v) => v.status.key)
    const rank = { range: 0, target: 1, overdue: 2, ok: 3, none: 4 }
    expect([...keys].sort((a, b) => rank[a] - rank[b])).toEqual(keys)
    expect(
      flatOrder(views)
        .slice(0, 2)
        .map((v) => v.code)
    ).toEqual(["ldl_cholesterol", "vitamin_d"])
  })
})

describe("metricInfo", () => {
  const ldl = byCode("ldl_cholesterol")
  const newest = ldl.lead.results[ldl.lead.results.length - 1]
  it("holds the description, direction, bands, lab range and cadence", () => {
    const info = metricInfo(index, ldl, newest, "High")
    expect(info.description).toBe("Low-density lipoprotein cholesterol")
    expect(info.direction).toBe("Lower is better.")
    expect(info.bands.map((b) => b.label)).toEqual(["Optimal", "High"])
    expect(info.labRange).toBe("Lab range (Demo Lab): up to 3.00 mmol/L")
    expect(info.cadence).toMatch(/^Checked every 12 months, last /)
    expect(info.assessment).toBeUndefined()
    expect(info.sources.length).toBeGreaterThan(0)
  })
  it("adds the assessment when the status word matches a key", () => {
    const cho = byCode("total_cholesterol")
    const last = cho.lead.results[cho.lead.results.length - 1]
    expect(metricInfo(index, cho, last, "High").assessment).toBe(
      "High levels may increase risk of heart disease"
    )
    expect(metricInfo(index, cho, last, "Desirable").assessment).toBeUndefined()
  })
})

describe("filters on the demo member", () => {
  const ids = (o: Partial<LabsFilters>, vs: LabView[] = views) => {
    const f = { ...DEFAULT_FILTERS, ...o }
    return applyFilters(vs, f, pageDomain(f.range, today, index.results)).map(
      (v) => v.code
    )
  }
  const three = ["ldl_cholesterol", "triglycerides", "vitamin_d"]
  it("keeps the important entries by default", () => {
    expect(ids({}).every((c) => byCode(c).important)).toBe(true)
    expect(ids({}).length).toBeLessThanOrEqual(23)
  })
  it("selects by status", () => {
    const range = ids({ scope: "all", status: ["range"] })
    expect(range).toContain("ldl_cholesterol")
    expect(range).toContain("vitamin_d")
    expect(range).not.toContain("triglycerides")
    expect(ids({ status: ["target"] })).toEqual(
      expect.arrayContaining(["ldl_cholesterol", "triglycerides"])
    )
    expect(ids({ status: ["range", "target"] })).toEqual(
      expect.arrayContaining(three)
    )
    const ok = ids({ scope: "all", status: ["ok"] })
    for (const c of three) expect(ok).not.toContain(c)
    expect(ids({ status: ["overdue"] })).toContain("psa")
  })
  it("hides the yearly-only entries under the clinic's source", () => {
    const clinic = ids({ source: "lab/demo-clinic" })
    expect(clinic).not.toContain("apolipoprotein_b")
    expect(clinic).toContain("ldl_cholesterol")
  })
  it("leaves PSA out of a woman's important entries", () => {
    expect(ids({}, labViews(index, today, "female"))).not.toContain("psa")
  })
})
