import { readFileSync, writeFileSync } from "node:fs"
import { join } from "node:path"
import { Effect, Result } from "effect"
import { describe, expect, it } from "vitest"
import { recordId } from "../../ids"
import { csv } from "../csv"
import { mychart } from "../mychart"
import { nhs } from "../nhs"
import { numan } from "../numan"
import { randox } from "../randox"
import { blobDigest, epoch, KINDS, sourceKey } from "../shared"
import {
  fakeContext,
  inputFromRecord,
  pdfFixture,
  planDiff,
  plannedRecord,
  runResult,
  viewPlan,
} from "../testing"
import type { Context, ImportError, Input, Plan } from "../types"
import { centreKey, dxa } from "."
import { words } from "./layout"
import { decimal, grams, printedClock, printedDate } from "./printout"
import {
  bmdTable,
  C,
  C_BMD,
  C_DISTRIBUTION,
  C_TREND,
  COMPOSITION,
  compositionTable,
  header,
  PHOTO,
  pdfOf,
  scanB,
  scanC,
  TITLES,
  TRANSCRIPTION,
  transcribedPage,
  trendTables,
} from "./fixtures"

const ANSWERS = { centre: "Invented Imaging Centre", zone: "Europe/Athens" }
const NS = "dxa/invented-imaging-centre"
const ctx = (answers: Record<string, string> = ANSWERS): Context =>
  fakeContext({ answers })

async function plan(input: Input, c: Context = ctx()): Promise<Plan> {
  return Effect.runPromise(dxa.read(input, c))
}

async function failure(input: Input, c: Context = ctx()): Promise<ImportError> {
  const r = await runResult(dxa.read(input, c))
  if (Result.isSuccess(r)) throw new Error("read did not fail")
  return r.failure
}

const result = (p: Plan, key: string) =>
  plannedRecord(p, KINDS.labresult, key, NS)?.properties
const report = (p: Plan, key: string) =>
  plannedRecord(p, KINDS.labreport, key, NS)?.properties
const keysOf = (p: Plan, prefix: string) =>
  p.records
    .filter((r) => r.kind === KINDS.labresult && r.key.startsWith(prefix))
    .map((r) => r.key)

/** A plan as its golden fixture holds it; `UPDATE_GOLDEN=1` writes the
 * fixture instead (to be read before it is kept). */
function expectGolden(name: string, p: Plan) {
  const file = join(import.meta.dirname, "golden", `${name}.json`)
  if (process.env.UPDATE_GOLDEN)
    writeFileSync(file, `${JSON.stringify(viewPlan(p), null, 2)}\n`)
  const want = JSON.parse(readFileSync(file, "utf8")) as ReturnType<
    typeof viewPlan
  >
  expect(planDiff(p, want)).toEqual([])
}

// ---- other providers' marks, minimal, from their specifications ----

const OTHERS: Record<string, Input> = {
  numan: inputFromRecord("numan", {
    "me-numan-20260101.json": JSON.stringify({
      data: [
        {
          id: "1",
          type: "test",
          attributes: { short_id: "ABC001-00000001", results_raw: "{}" },
        },
      ],
    }),
  }),
  randox: inputFromRecord("randox", {
    "manifest.json": JSON.stringify({
      files: {},
      runs: [{ started: "2026-01-01T10:00:00Z" }],
    }),
    "items/visit/1.json": JSON.stringify({ id: 1, orders: [] }),
    "files/report/1.pdf": pdfFixture([
      "BT /F1 12 Tf 72 720 Td (Health report) Tj ET",
    ]),
  }),
  nhs: inputFromRecord("20260101T100000Z", {
    "manifest.json": JSON.stringify({
      tool: "nhsctl v0.1",
      startedAt: "2026-01-01T10:00:00Z",
      responses: [],
    }),
    "responses/0001-api-v1-patient-my-record.json": "{}",
  }),
  mychart: inputFromRecord("portal", {
    "manifest.json": JSON.stringify({
      tool: "mychartctl",
      provider: "portal",
      baseUrl: "https://mychart.example.org/MyChart/",
      items: {},
    }),
    "items/test-results/1.json": "{}",
  }),
  csv: inputFromRecord("results.csv", {
    "results.csv": "date,test,value,unit\n2026-01-01,LDL,2.1,mmol/L\n",
  }),
}

// ---- tests ----

describe("detecting GE Lunar printouts", () => {
  it("claims printouts by the enCORE report their PDF title names", async () => {
    const m = await Effect.runPromise(
      dxa.detect(inputFromRecord("scans", scanC()))
    )
    expect(m).toEqual({
      importer: "dxa",
      confidence: 1,
      why: "3 GE Lunar enCORE printouts (1 bone density, 1 body composition, 1 trend).",
    })
  })

  it("claims a transcribed trend page by its fields", async () => {
    const m = await Effect.runPromise(
      dxa.detect(inputFromRecord("typed", transcribedPage()))
    )
    expect(m?.why).toBe("A transcribed trend page.")
    const both = await Effect.runPromise(
      dxa.detect(inputFromRecord("all", { ...scanC(), ...transcribedPage() }))
    )
    expect(both?.why).toBe(
      "3 GE Lunar enCORE printouts (1 bone density, 1 body composition, 1 trend) and a transcribed trend page."
    )
  })

  it("does not claim a PDF with no enCORE title, nor another provider's files", async () => {
    const untitled = inputFromRecord("x", {
      "x.pdf": pdfOf([[...header(C), ...bmdTable(C_BMD)]]),
    })
    expect(await Effect.runPromise(dxa.detect(untitled))).toBeUndefined()
    for (const input of Object.values(OTHERS))
      expect(await Effect.runPromise(dxa.detect(input))).toBeUndefined()
  })

  it("is not claimed by another provider's importer", async () => {
    const input = inputFromRecord("all", { ...scanC(), ...transcribedPage() })
    for (const other of [numan, randox, nhs, mychart, csv])
      expect(await Effect.runPromise(other.detect(input))).toBeUndefined()
  })
})

describe("a printout's cells", () => {
  it("reads the 12-hour clock, πμ before noon and μμ after", () => {
    expect(printedClock("1/1/2025", "12:05:00", "πμ")?.hour).toBe(0)
    expect(printedClock("1/1/2025", "12:05:00", "μμ")?.hour).toBe(12)
    expect(printedClock("1/1/2025", "1:05:09", "μμ")).toMatchObject({
      hour: 13,
      minute: 5,
      second: 9,
    })
    expect(printedClock("1/1/2025", "13:05:00", "μμ")).toBeUndefined()
    expect(printedClock("1/1/2025", "1:05:00", "pm")).toBeUndefined()
  })

  it("reads numbers and dates as the printout writes them", () => {
    expect(decimal("20,8")).toBe("20.8")
    expect(decimal("-0,8")).toBe("-0.8")
    expect(decimal("50")).toBe("50")
    expect(decimal("1.159")).toBeUndefined()
    expect(grams("79.205")).toBe("79205")
    expect(grams("796")).toBe("796")
    expect(grams("20,8")).toBeUndefined()
    expect(printedDate("8/4/2025")).toBe("2025-04-08")
    expect(printedDate("30/2/2025")).toBeUndefined()
  })

  it("splits an item pdf.js joined into its words, each where it lies", () => {
    expect(words({ text: "18.100 57.000", left: 100, right: 113 })).toEqual([
      { text: "18.100", left: 100, right: 106 },
      { text: "57.000", left: 107, right: 113 },
    ])
    const one = { text: "2.817", left: 1, right: 2 }
    expect(words(one)).toEqual([one])
  })
})

describe("what it asks", () => {
  it("asks the centre's name and its time zone, the zone suggested from the browser", async () => {
    const qs = await Effect.runPromise(
      dxa.questions(inputFromRecord("s", scanC()), ctx({}))
    )
    expect(qs.map((q) => [q.key, q.kind, q.required])).toEqual([
      ["centre", "text", true],
      ["zone", "timezone", true],
    ])
    expect(qs[0].suggested).toBeUndefined()
    expect(qs[1].suggested).toBe(
      Intl.DateTimeFormat().resolvedOptions().timeZone
    )
  })

  it("suggests the one centre whose source this importer made", async () => {
    const id = recordId(sourceKey(NS))
    const c = fakeContext({
      sources: [
        { id, name: "Invented Imaging Centre", class: "lab" },
        { id: recordId(sourceKey("numan")), name: "Numan", class: "service" },
        {
          id: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
          name: "Renamed Centre",
          class: "lab",
        },
      ],
    })
    const qs = await Effect.runPromise(
      dxa.questions(inputFromRecord("s", scanC()), c)
    )
    expect(qs[0].suggested).toBe("Invented Imaging Centre")
  })

  it("needs both answers before it reads, and a zone this browser knows", async () => {
    const input = inputFromRecord("s", scanC())
    const none = await failure(input, ctx({}))
    expect(none._tag).toBe("NeedsAnswers")
    if (none._tag === "NeedsAnswers")
      expect(none.questions.map((q) => q.key)).toEqual(["centre", "zone"])
    const zone = await failure(
      input,
      ctx({ centre: "X", zone: "Mars/Olympus" })
    )
    if (zone._tag !== "NeedsAnswers") throw new Error(zone._tag)
    expect(zone.questions.map((q) => q.key)).toEqual(["zone"])
    const blank = await failure(input, ctx({ centre: " ", zone: "UTC" }))
    if (blank._tag !== "NeedsAnswers") throw new Error(blank._tag)
    expect(blank.questions.map((q) => q.key)).toEqual(["centre"])
  })
})

describe("one scan's printouts", () => {
  it("makes the plan of its golden fixture", async () => {
    expectGolden("scan", await plan(inputFromRecord("scans", scanC())))
  })

  it("writes the centre's source and reports keyed by scan date", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    expect(p.source).toEqual(sourceKey(NS))
    expect(p.records[0]).toEqual({
      ...sourceKey(NS),
      mode: "create",
      properties: { name: "Invented Imaging Centre", class: "lab" },
    })
    const r = report(p, "2024-07-15")
    expect(r).toMatchObject({
      lab: "Invented Imaging Centre",
      accession: "c7k2m9q1zz.dfb",
      at: "2024-07-15T09:05:12+03:00",
      issuedAt: "2024-07-15T09:12:40+03:00",
      orderedAt: null,
      status: "final",
      review: "pending",
      source: { $ref: sourceKey(NS) },
    })
    const rec = plannedRecord(p, KINDS.labreport, "2024-07-15", NS)!
    expect(rec.createOnly).toEqual(["review"])
    expect(rec.revision).toBe(epoch("2024-07-15T00:00:00Z"))
    expect(
      p.records.every((x) => x.mode === "create" || x.revision === rec.revision)
    ).toBe(true)
  })

  it("reads the T-score and Z-score from their own columns", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    expect(result(p, "2024-07-15/bmd_t_score")).toMatchObject({
      value: { value: "-0.9", unit: "{score}" },
      metric: `${KINDS.metric}/health/bmd_t_score`,
      coding: {
        system: "ge-lunar",
        code: "bmd_t_score",
        display: "T-score (total body, young adult)",
      },
      provenance: { page: 1 },
      channel: null,
    })
    expect(result(p, "2024-07-15/bmd_z_score")?.value).toEqual({
      value: "-0.2",
      unit: "{score}",
    })
  })

  it("writes each composition region's five values in its channel, by the table's row order", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    expect(result(p, "2024-07-15/lean_mass/left_arm")).toMatchObject({
      value: { value: "2290", unit: "g" },
      channel: "left_arm",
      metric: `${KINDS.metric}/health/lean_mass`,
      coding: {
        system: "ge-lunar",
        code: "reg_lean_mass/left_arm",
        display: "Lean left_arm",
      },
      upstreamId: "2024-07-15/reg_lean_mass/left_arm",
      method: "DXA whole body",
      provenance: { page: 1 },
    })
    expect(result(p, "2024-07-15/fat_mass/right_total")?.value).toEqual({
      value: "9200",
      unit: "g",
    })
    expect(result(p, "2024-07-15/bone_mass/gynoid")?.value).toEqual({
      value: "180",
      unit: "g",
    })
    expect(result(p, "2024-07-15/body_fat_fraction/trunk")?.value).toEqual({
      value: "27.7",
      unit: "%",
    })
    // The total row's values are the whole body's, with no channel.
    expect(result(p, "2024-07-15/fat_mass/total")).toBeUndefined()
    expect(result(p, "2024-07-15/fat_mass")?.value).toEqual({
      value: "18600",
      unit: "g",
    })
    expect(keysOf(p, "2024-07-15/tissue_fat_fraction/")).toHaveLength(13)
  })

  it("keeps the trend table's android and gynoid %fat once, as the composition table's", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    expect(result(p, "2024-07-15/tissue_fat_fraction/android")).toMatchObject({
      upstreamId: "2024-07-15/reg_tissue_fat/android",
      value: { value: "33.9", unit: "%" },
    })
    const trend = scanC()
    trend["scans/c-result-3.pdf"] = pdfOf(
      [
        [
          ...header(C),
          ...trendTables(C_TREND, [
            ["15/7/2024", "52,3", "34,0", "38,8", "0,87", "30,8"],
            C_DISTRIBUTION[1],
          ]),
        ],
      ],
      TITLES.trend
    )
    const differs = await plan(inputFromRecord("scans", trend))
    expect(
      result(differs, "2024-07-15/tissue_fat_fraction/android")?.value
    ).toEqual({
      value: "33.9",
      unit: "%",
    })
    expect(differs.left).toContainEqual({
      what: "Android %Fat, c7k2m9q1zz.dfb",
      why: "the trend table's figure differs from the composition table's, which is kept",
    })
  })

  it("writes regional bone density, and the total as the whole body's", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    expect(keysOf(p, "2024-07-15/bone_mineral_density")).toEqual([
      "2024-07-15/bone_mineral_density/head",
      "2024-07-15/bone_mineral_density/arms",
      "2024-07-15/bone_mineral_density/legs",
      "2024-07-15/bone_mineral_density/trunk",
      "2024-07-15/bone_mineral_density/ribs",
      "2024-07-15/bone_mineral_density/pelvis",
      "2024-07-15/bone_mineral_density/spine",
      "2024-07-15/bone_mineral_density",
    ])
    expect(result(p, "2024-07-15/bone_mineral_density")).toMatchObject({
      value: { value: "1.012", unit: "g/cm2" },
      channel: null,
      provenance: null,
      coding: {
        system: "ge-lunar",
        code: "BMD (total body)",
        display: "BMD (total body)",
      },
    })
  })

  it("writes a scan known only from the trend tables as a report of its own", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    const trendDoc = p.documents.find(
      (d) => d.name === "DXA trend 2024-07-15.pdf"
    )!
    expect(report(p, "2023-10-03")).toMatchObject({
      accession: "2023-10-03",
      at: "2023-10-03T12:00:00+03:00",
      issuedAt: null,
      file: { $doc: trendDoc.digest },
      review: "pending",
    })
    expect(result(p, "2023-10-03/body_mass")?.value).toEqual({
      value: "64.9",
      unit: "kg",
    })
    expect(result(p, "2023-10-03/bone_mineral_density")?.value).toEqual({
      value: "1.020",
      unit: "g/cm2",
    })
    expect(result(p, "2023-10-03/body_fat_centile")?.value).toEqual({
      value: "71",
      unit: "{score}",
    })
  })

  it("attaches the body composition printout under a neutral name", async () => {
    const files = scanC()
    const p = await plan(inputFromRecord("scans", files))
    expect(report(p, "2024-07-15")?.file).toEqual({
      $doc: blobDigest(files["scans/c-result-2.pdf"]),
    })
    expect(p.documents.map((d) => [d.name, d.mediaType])).toEqual([
      ["DXA trend 2024-07-15.pdf", "application/pdf"],
      ["DXA body composition 2024-07-15.pdf", "application/pdf"],
    ])
  })

  it("attaches the lowest-numbered printout where there is no body composition printout", async () => {
    const files = scanC()
    delete files["scans/c-result-2.pdf"]
    const p = await plan(inputFromRecord("scans", files))
    expect(report(p, "2024-07-15")?.file).toEqual({
      $doc: blobDigest(files["scans/c-result-1.pdf"]),
    })
    expect(keysOf(p, "2024-07-15/lean_mass")).toEqual(["2024-07-15/lean_mass"])
  })
})

describe("a re-saved scan", () => {
  it("reads every region from the supplementary table and passes over the tissue quantitation", async () => {
    const p = await plan(inputFromRecord("scans", scanB()))
    expect(keysOf(p, "2024-02-20/bone_mineral_density/")).toHaveLength(15)
    expect(
      result(p, "2024-02-20/bone_mineral_density/right_total")
    ).toMatchObject({
      value: { value: "0.999", unit: "g/cm2" },
      provenance: { page: 2 },
      channel: "right_total",
    })
    expect(
      result(p, "2024-02-20/bone_mineral_density/spine")?.provenance
    ).toEqual({ page: 2 })
    expect(result(p, "2024-02-20/bmd_t_score")).toBeUndefined()
    expect(result(p, "2024-02-20/lean_mass/legs")).toMatchObject({
      value: { value: "13400", unit: "g" },
      provenance: { page: 2 },
    })
    expect(result(p, "2024-02-20/trunk_to_total_fat_ratio")).toMatchObject({
      value: { value: "0.44", unit: "1" },
      provenance: { page: 2 },
    })
  })

  it("reads an afternoon time on the centre's winter clock", async () => {
    const p = await plan(inputFromRecord("scans", scanB()))
    expect(report(p, "2024-02-20")).toMatchObject({
      at: "2024-02-20T19:30:05+02:00",
      issuedAt: "2024-02-22T10:02:11+02:00",
      accession: "b3x8n4p0aa.dfb",
    })
    expect(result(p, "2024-02-20/body_mass")?.utcOffset).toBe(120)
  })

  it("leaves a dash centile out, and a dash in the scan's own row hides another table's", async () => {
    const p = await plan(inputFromRecord("scans", scanB()))
    expect(result(p, "2024-02-20/body_fat_centile")).toBeUndefined()
    expect(result(p, "2023-10-03/body_fat_centile")?.value).toEqual({
      value: "71",
      unit: "{score}",
    })
  })
})

describe("the trend merge", () => {
  it("makes the plan of its golden fixture", async () => {
    const input = inputFromRecord("all", {
      "scans/b-result-3-fixed.pdf": scanB()["scans/b-result-3-fixed.pdf"],
      ...transcribedPage(),
    })
    expectGolden("trend", await plan(input))
  })

  it("takes a scan's own row first, then the latest scan's table", async () => {
    const p = await plan(
      inputFromRecord("all", { ...scanB(), ...scanC(), ...transcribedPage() })
    )
    // Scan C's own row, though the later transcription restates it.
    expect(result(p, "2024-07-15/fat_mass")?.value).toEqual({
      value: "18600",
      unit: "g",
    })
    // A scan known only from tables: the latest scan's (the transcription).
    expect(result(p, "2023-10-03/fat_mass")?.value).toEqual({
      value: "21150",
      unit: "g",
    })
    expect(result(p, "2023-10-03/tissue_fat_fraction/android")?.value).toEqual({
      value: "37.3",
      unit: "%",
    })
    // Its BMD only a bone density printout lists.
    expect(result(p, "2023-10-03/bone_mineral_density")?.value).toEqual({
      value: "1.020",
      unit: "g/cm2",
    })
    // Its centile: the transcription's dash, the latest table's.
    expect(result(p, "2023-10-03/body_fat_centile")).toBeUndefined()
  })

  it("attaches a printout listing a scan before a photo, the photo where only it lists one", async () => {
    const c = scanC()
    const p = await plan(
      inputFromRecord("all", { ...scanB(), ...c, ...transcribedPage() })
    )
    expect(report(p, "2023-10-03")?.file).toEqual({
      $doc: blobDigest(c["scans/c-result-3.pdf"]),
    })
    expect(report(p, "2025-11-12")).toMatchObject({
      file: { $doc: blobDigest(PHOTO) },
      accession: "2025-11-12",
      at: "2025-11-12T12:00:00+02:00",
    })
    expect(report(p, "2026-03-05")).toMatchObject({
      file: { $doc: blobDigest(PHOTO) },
      accession: "d9t5r2w7kk.dfb",
      at: "2026-03-05T11:20:00+02:00",
      issuedAt: "2026-03-05T11:31:09+02:00",
    })
    expect(
      p.documents.find((d) => d.digest === blobDigest(PHOTO))
    ).toMatchObject({
      name: "DXA trend photo 2026-03-05.jpg",
      mediaType: "image/jpeg",
    })
    const newest = epoch("2026-03-05T00:00:00Z")
    expect(
      p.records
        .filter((r) => r.mode !== "create")
        .every((r) => r.revision === newest)
    ).toBe(true)
    expect(
      p.records.filter((r) => r.kind === KINDS.labreport).map((r) => r.key)
    ).toEqual([
      "2023-10-03",
      "2024-02-20",
      "2024-07-15",
      "2025-11-12",
      "2026-03-05",
    ])
  })

  it("makes a transcribed scan with printouts one report, its rows joining the merge", async () => {
    const page = {
      ...TRANSCRIPTION,
      scan: "15/7/2024",
      composition: [{ ...TRANSCRIPTION.composition[2], fat_mass: "18.700" }],
      distribution: [],
    }
    const p = await plan(
      inputFromRecord("all", { ...scanC(), ...transcribedPage(page) })
    )
    expect(report(p, "2024-07-15")?.accession).toBe("c7k2m9q1zz.dfb")
    // Of two of one rank, the printout's.
    expect(result(p, "2024-07-15/fat_mass")?.value).toEqual({
      value: "18600",
      unit: "g",
    })
  })
})

describe("keys", () => {
  it("keys a centre with no Latin letters by its own letters", async () => {
    expect(centreKey("Ιατρικό Κέντρο Α.Ε.")).toBe("ιατρικό-κέντρο-α-ε")
    expect(centreKey("North Hill Imaging & Diagnostics, Riverton")).toBe(
      "north-hill-imaging-diagnostics-riverton"
    )
    const p = await plan(
      inputFromRecord("scans", scanC()),
      ctx({ centre: "Ιατρικό Κέντρο", zone: "Europe/Athens" })
    )
    expect(p.source).toEqual(sourceKey("dxa/ιατρικό-κέντρο"))
    expect(p.records[1]).toMatchObject({
      kind: KINDS.labreport,
      namespace: "dxa/ιατρικό-κέντρο",
      key: "2023-10-03",
    })
  })

  it("keys results by date, measure and region", async () => {
    const p = await plan(inputFromRecord("scans", scanC()))
    const keys = keysOf(p, "2024-07-15/")
    expect(new Set(keys).size).toBe(keys.length)
    expect(keys).toContain("2024-07-15/tissue_fat_fraction/left_trunk")
    expect(keys).toContain("2024-07-15/tissue_fat_fraction")
    expect(keys).toContain("2024-07-15/android_gynoid_ratio")
    expect(keys).toContain("2024-07-15/limbs_to_trunk_fat_ratio")
    expect(keys).toHaveLength(2 + 3 + 65 + 7 + 10)
  })
})

describe("what it leaves out", () => {
  it("leaves a composition table out whose rows moved, and one with a row short", async () => {
    const files = scanC()
    const swapped = [...COMPOSITION]
    ;[swapped[11], swapped[12]] = [swapped[12], swapped[11]]
    files["scans/c-result-2.pdf"] = pdfOf(
      [[...header(C), ...compositionTable(swapped)]],
      TITLES.composition
    )
    const p = await plan(inputFromRecord("scans", files))
    expect(keysOf(p, "2024-07-15/lean_mass/")).toEqual([])
    expect(p.left).toContainEqual({
      what: "scans/c-result-2.pdf, page 1, the composition table",
      why: "its rows are not in the order it prints them (android and gynoid twelfth and thirteenth), so its regional values are left out",
    })
    files["scans/c-result-2.pdf"] = pdfOf(
      [[...header(C), ...compositionTable(COMPOSITION.slice(1))]],
      TITLES.composition
    )
    const short = await plan(inputFromRecord("scans", files))
    expect(short.left).toContainEqual({
      what: "scans/c-result-2.pdf, page 1, the composition table",
      why: "it has 13 rows, not the 14 regions it prints, so its regional values are left out",
    })
  })

  it("leaves out a bone density row of a region it does not know", async () => {
    const files = scanC()
    files["scans/c-result-1.pdf"] = pdfOf(
      [[...header(C), ...bmdTable([["L1-L4", "1,100"], ...C_BMD])]],
      TITLES.bone
    )
    const p = await plan(inputFromRecord("scans", files))
    expect(p.left).toContainEqual({
      what: "scans/c-result-1.pdf, page 1, the bone density table's row L1-L4",
      why: "its region is not one this importer knows",
    })
    expect(keysOf(p, "2024-07-15/bone_mineral_density/")).toHaveLength(7)
  })

  it("reads one scan a day, and one copy of each printout, the one with more pages", async () => {
    const later = pdfOf(
      [[...header({ ...C, time: "10:40:00 πμ" }), ...bmdTable(C_BMD)]],
      TITLES.bone
    )
    const copy = pdfOf(
      [[...header(C), ...bmdTable(C_BMD.slice(0, 3))]],
      TITLES.bone
    )
    const files = {
      ...scanC(),
      "scans/later.pdf": later,
      "scans/z-copy.pdf": copy,
    }
    const p = await plan(inputFromRecord("scans", files))
    expect(p.left).toContainEqual({
      what: "scans/later.pdf",
      why: "it is of another scan measured on 2024-07-15: a scan's report is keyed by its date, and the first scan of the day is read",
    })
    expect(p.left).toContainEqual({
      what: "scans/c-result-1.pdf",
      why: "another copy of the scan's bone density printout is read",
    })
    // The later copy in path order, of as many pages, is the one read.
    expect(keysOf(p, "2024-07-15/bone_mineral_density/")).toHaveLength(3)
  })

  it("names the field of a transcription that is not as the printout writes it, never its value", async () => {
    const bad = {
      ...TRANSCRIPTION,
      composition: [{ ...TRANSCRIPTION.composition[0], fat_mass: "12,3 kg" }],
    }
    expect(await failure(inputFromRecord("t", transcribedPage(bad)))).toEqual({
      _tag: "Unreadable",
      path: "typed/d-result-3.json",
      field: "composition[0].fat_mass",
      message:
        "is not a mass in grams as the printout writes one (a point between thousands)",
    })
    const unreal = { ...TRANSCRIPTION, scan: "30/2/2026" }
    expect(
      await failure(inputFromRecord("t", transcribedPage(unreal)))
    ).toEqual({
      _tag: "Unreadable",
      path: "typed/d-result-3.json",
      field: "scan",
      message: "is not a real date or time",
    })
    const missing: Record<string, unknown> = { ...TRANSCRIPTION }
    delete missing.analyzed
    expect(
      await failure(inputFromRecord("t", transcribedPage(missing)))
    ).toMatchObject({
      _tag: "Unreadable",
      field: "analyzed",
      message: "Missing key",
    })
  })

  it("writes a transcribed scan whose photo is missing with no document, and says so", async () => {
    const files = transcribedPage()
    delete files["typed/d-result-3.jpg"]
    const p = await plan(inputFromRecord("t", files))
    expect(report(p, "2026-03-05")?.file).toBeNull()
    expect(p.documents).toEqual([])
    expect(p.left).toContainEqual({
      what: "typed/d-result-3.json",
      why: "the photo it names is not among the files, so its scan's report names no photo",
    })
  })

  it("fails on a titled printout that will not open, and leaves out any other PDF that will not", async () => {
    const broken = new TextEncoder().encode(
      `%PDF-1.4\n<< /Title (${TITLES.bone}) >>\nnot a pdf`
    )
    expect(
      await failure(
        inputFromRecord("s", { ...scanC(), "scans/broken.pdf": broken })
      )
    ).toMatchObject({ _tag: "Unreadable", path: "scans/broken.pdf" })
    const other = new TextEncoder().encode("%PDF-1.4\nnot a pdf")
    const p = await plan(
      inputFromRecord("s", { ...scanC(), "other.pdf": other })
    )
    expect(p.left).toContainEqual({
      what: "other.pdf",
      why: "it is a PDF that will not open",
    })
  })

  it("leaves out what is not a printout or a transcribed page, and is not for an input of neither", async () => {
    const files = {
      ...scanC(),
      "scans/image.png": Uint8Array.from([0x89, 0x50, 0x4e, 0x47]),
      "scans/letter.pdf": pdfOf([[{ text: "A letter", x: 72, y: 720 }]]),
      "scans/notes.json": '{"notes": []}',
    }
    const p = await plan(inputFromRecord("s", files))
    expect(p.left).toEqual(
      expect.arrayContaining([
        {
          what: "scans/image.png",
          why: "it is not a GE Lunar printout or a transcribed trend page",
        },
        {
          what: "scans/letter.pdf",
          why: "it is not a GE Lunar printout: it names no measured time",
        },
        {
          what: "scans/notes.json",
          why: "it is not a GE Lunar printout or a transcribed trend page",
        },
      ])
    )
    expect(await failure(OTHERS.numan)).toEqual({
      _tag: "NotMine",
      message: "There is no GE Lunar printout or transcribed trend page here.",
    })
  })

  it("says what of each scan it does not read", async () => {
    const p = await plan(
      inputFromRecord("all", { ...scanC(), ...transcribedPage() })
    )
    expect(p.left.filter((l) => l.what.startsWith("the scan of"))).toEqual([
      {
        what: "the scan of 2024-07-15",
        why: "the printout header's height, weight and BMI are left out: the operator types them in",
      },
      {
        what: "the scan of 2024-07-15",
        why: "the BMD change against the baseline is left out: it is derived from the BMD values read",
      },
      {
        what: "the scan of 2026-03-05",
        why: "the printout header's height, weight and BMI are left out: the operator types them in",
      },
      {
        what: "the scan of 2026-03-05",
        why: "only the trend page was transcribed: the scan's bone density and regional composition are not in it",
      },
    ])
  })
})
