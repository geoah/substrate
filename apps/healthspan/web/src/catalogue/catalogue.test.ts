// @vitest-environment node
import { readFileSync } from "node:fs"
import { describe, expect, it } from "vitest"
import {
  CATALOGUE,
  PANELS,
  assessmentFor,
  catalogueOrder,
  directionPhrase,
  entryByCode,
  forSex,
  nameKey,
  panelFor,
  targetBand,
} from "."
import { LAB_CONTENT } from "./content"

const entry = (code: string) => {
  const e = entryByCode(code)
  if (!e) throw new Error(`no entry ${code}`)
  return e
}

// The shared catalogue's metric records: each id with its unit and LOINC
// codes, read from the YAML the packages install.
const KINDS = new URL(
  "../../../../../kinds/substrate.reamde.dev/",
  import.meta.url
)
function sharedMetrics(): Map<string, { unit: string; codes: string[] }> {
  const out = new Map<string, { unit: string; codes: string[] }>()
  for (const file of ["labs/metrics.yaml", "health/metrics.yaml"]) {
    const text = readFileSync(new URL(file, KINDS), "utf8")
    for (const doc of text.split(/^---$/m)) {
      const id = /^ {2}id: (\S+)$/m.exec(doc)?.[1]
      const unit =
        /unit:\n\s+ref: substrate\.reamde\.dev\/core\/unit\/(\S+)/.exec(
          doc
        )?.[1]
      if (!id || !unit) continue
      const codes = [
        ...doc.matchAll(/- system: "http:\/\/loinc\.org"\n\s+code: "([^"]+)"/g),
      ].map((m) => m[1])
      out.set(id, { unit, codes })
    }
  }
  return out
}

describe("catalogue", () => {
  const shared = sharedMetrics()

  it("keeps the 23 important entries", () => {
    expect(
      CATALOGUE.filter((e) => e.important)
        .map((e) => e.code)
        .sort()
    ).toEqual(
      [
        "egfr",
        "cystatin_c",
        "total_cholesterol",
        "triglycerides",
        "ldl_cholesterol",
        "hdl_cholesterol",
        "non_hdl_cholesterol",
        "apolipoprotein_b",
        "lipoprotein_a",
        "tsh",
        "testosterone",
        "alt",
        "ast",
        "vitamin_d",
        "vitamin_b12",
        "ferritin",
        "haemoglobin",
        "hba1c",
        "fasting_glucose",
        "insulin",
        "hs_crp",
        "psa",
        "uric_acid",
      ].sort()
    )
  })

  it("names only the shared catalogue's metrics, in their display unit", () => {
    expect(shared.size).toBeGreaterThan(100)
    for (const e of CATALOGUE) {
      expect(e.metric, e.code).toMatch(/^(labs|health)\//)
      const m = shared.get(e.metric as string)
      expect(m, e.code).toBeDefined()
      expect(m?.unit, e.code).toBe(e.displayUnit)
    }
  })

  it("takes every LOINC code from the shared catalogue, the display form's first", () => {
    const known = new Set([...shared.values()].flatMap((m) => m.codes))
    for (const e of CATALOGUE) {
      for (const c of e.loinc) expect(known.has(c), `${e.code} ${c}`).toBe(true)
      const own = shared.get(e.metric as string)?.codes ?? []
      if (e.loinc.length) expect(own, e.code).toContain(e.loinc[0])
    }
  })

  it("gives every code to one entry and every name to one entry", () => {
    const codes = CATALOGUE.flatMap((e) => e.loinc)
    expect(new Set(codes).size).toBe(codes.length)
    const names = new Map<string, string>()
    for (const e of CATALOGUE)
      for (const n of [e.name, e.code, ...e.aliases]) {
        const k = nameKey(n)
        const had = names.get(k)
        expect(
          had === undefined || had === e.code,
          `${n}: ${had} and ${e.code}`
        ).toBe(true)
        names.set(k, e.code)
      }
  })

  it("keeps the 16 panels in order", () => {
    expect(PANELS).toHaveLength(16)
    expect(PANELS[0]).toBe("Kidney function")
    expect(PANELS[15]).toBe("Blood pressure")
  })

  it("holds its bands in the display unit", () => {
    // Haemoglobin: 120 to 180 g/L is 12 to 18 g/dL.
    expect(
      entry("haemoglobin").target?.find((b) => b.tone === "success")
    ).toMatchObject({ from: 12, to: 18 })
    // Apolipoprotein B: 50 to 100 mg/dL is 0.5 to 1 g/L.
    expect(
      entry("apolipoprotein_b").target?.find((b) => b.tone === "success")
    ).toMatchObject({ from: 0.5, to: 1 })
    // Vitamin B12: 197 to 771 ng/L at 1355.37 g/mol.
    const b12 = entry("vitamin_b12").target?.find((b) => b.tone === "success")
    expect(b12?.from).toBeCloseTo((197 / 1355.37) * 1000, -1)
    expect(b12?.to).toBeCloseTo((771 / 1355.37) * 1000, -1)
    expect(targetBand(entry("ldl_cholesterol"), 3.4)?.label).toBe("High")
  })

  it("names a category panel for what sits outside the panels", () => {
    expect(panelFor(undefined, "labs")).toBe("Other blood tests")
    expect(panelFor(undefined, "body")).toBe("Body composition")
    expect(panelFor(entry("alt"), "labs")).toBe("Liver function")
    expect(panelFor(entry("esr"), "labs")).toBe("Other blood tests")
  })

  it("sets PSA two-yearly and lipids yearly", () => {
    expect(entry("psa").cadenceMonths).toBe(24)
    expect(entry("ldl_cholesterol").cadenceMonths).toBe(12)
  })

  it("carries a description on every entry", () => {
    for (const e of CATALOGUE) expect(e.description, e.code).toBeTruthy()
    expect(entry("ldl_cholesterol").description).toBe(
      "Low-density lipoprotein cholesterol"
    )
    expect(entry("hs_crp").description).toBe(
      "High-sensitivity C-reactive protein - inflammation marker for cardiovascular risk"
    )
  })

  it("adds no em or en dash, no middot and no brand to any text", () => {
    for (const e of CATALOGUE) {
      const texts = [
        e.name,
        e.description ?? "",
        ...e.aliases,
        ...Object.values(e.assessments ?? {}),
        ...(e.target ?? []).map((b) => b.label),
      ]
      for (const t of texts) {
        expect(t, e.code).not.toMatch(/[–—·]/)
        expect(t, e.code).not.toMatch(/mellified|whoop/i)
      }
    }
  })

  it("keeps the two assessments, keyed by the lower-case word", () => {
    expect(
      CATALOGUE.filter((e) => e.assessments)
        .map((e) => e.code)
        .sort()
    ).toEqual(["total_cholesterol", "triglycerides"])
    expect(assessmentFor(entry("total_cholesterol"), "High")).toBe(
      "High levels may increase risk of heart disease"
    )
    expect(
      assessmentFor(entry("total_cholesterol"), "Desirable")
    ).toBeUndefined()
    expect(assessmentFor(entry("ldl_cholesterol"), "High")).toBeUndefined()
  })

  it("phrases the better direction", () => {
    expect(directionPhrase(entry("ldl_cholesterol"))).toBe("Lower is better")
    expect(directionPhrase(entry("hdl_cholesterol"))).toBe("Higher is better")
    expect(directionPhrase(entry("tsh"))).toBe("In range is best")
    expect(directionPhrase(entry("height"))).toBeUndefined()
    expect(directionPhrase(undefined)).toBeUndefined()
  })

  it("orders entries as the catalogue lists them", () => {
    expect(catalogueOrder("creatinine")).toBeLessThan(
      catalogueOrder("ldl_cholesterol") as number
    )
    expect(catalogueOrder("labs/igg")).toBeUndefined()
  })

  it("gives every important entry why it matters and what moves it, with sources", () => {
    for (const e of CATALOGUE.filter((x) => x.important)) {
      expect(e.why, e.code).toBeTruthy()
      expect(e.moves, e.code).toBeTruthy()
      expect(e.sources?.length, e.code).toBeGreaterThan(0)
    }
    expect(entry("ldl_cholesterol").why).toBe(
      "The cholesterol carried into artery walls; lower is better for heart risk."
    )
  })
})

describe("targets by sex", () => {
  it("keeps every entry as it is while the sex is unknown", () => {
    expect(forSex(entry("testosterone"), undefined)).toBe(entry("testosterone"))
  })
  it("drops a one-sex test's target and importance for the other sex", () => {
    const psa = forSex(entry("psa"), "female")
    expect(psa.important).toBe(false)
    expect(psa.target).toBeUndefined()
    expect(forSex(entry("psa"), "male")).toBe(entry("psa"))
  })
  it("drops the male hormone bands for a woman, leaving the lab's range", () => {
    expect(forSex(entry("testosterone"), "female").target).toBeUndefined()
    expect(forSex(entry("testosterone"), "male").target).toHaveLength(3)
  })
  it("takes a sex's own bands where it has them", () => {
    const waist = forSex(entry("waist_circumference"), "female")
    expect(targetBand(waist, 90)?.label).toBe("High Risk")
    expect(targetBand(entry("waist_circumference"), 90)?.label).toBe("At Risk")
  })
})

describe("lab content", () => {
  const important = CATALOGUE.filter((e) => e.important)
  it("covers exactly the important entries", () => {
    expect(Object.keys(LAB_CONTENT).sort()).toEqual(
      important.map((e) => e.code).sort()
    )
  })
  it("has why, moves and at least one https source each", () => {
    for (const [code, c] of Object.entries(LAB_CONTENT)) {
      expect(c.why.length, code).toBeGreaterThan(20)
      expect(c.moves.length, code).toBeGreaterThan(10)
      expect(c.sources.length, code).toBeGreaterThan(0)
      for (const s of c.sources) {
        expect(s.title, code).toBeTruthy()
        expect(s.url, code).toMatch(/^https:\/\/[a-z0-9.-]+\.[a-z]+\//)
      }
    }
  })
  it("adds no em or en dash and no middot", () => {
    expect(JSON.stringify(LAB_CONTENT)).not.toMatch(/[–—·]/)
  })
})
