import { readFileSync } from "node:fs"
import { join } from "node:path"
import { Effect } from "effect"
import { zipSync } from "fflate"
import { describe, expect, it } from "vitest"
import { csv } from "../csv"
import { IMPORTERS } from ".."
import { inputFromZip } from "../input"
import { keyHash, KINDS, organizationKey, sourceKey } from "../shared"
import {
  fakeContext,
  inputFromRecord,
  pdfFixture,
  planDiff,
  plannedRecord,
  runResult,
  viewPlan,
  type PlanView,
} from "../testing"
import type { Importer, Input, Plan } from "../types"
import {
  BIG_CODE,
  exampleRun,
  HISTORIC_TEST,
  REVISION,
  responses,
  runFiles,
  SESSION,
  TESTS,
  year,
} from "./fixtures/run"
import { nhs } from "."

const GOLDEN = JSON.parse(
  readFileSync(join(import.meta.dirname, "fixtures/plan.json"), "utf8")
) as PlanView

/** The example run with a year of historic results and a year whose answer
 * is in a shape the importer does not know: the golden plan's input. */
function goldenInput(): Input {
  return inputFromRecord(
    "example",
    exampleRun({
      years: [
        year(2019, [HISTORIC_TEST]),
        {
          url: "https://api.nhsapp.service.nhs.uk/v1/patient/historic-test-results/2018",
          body: { unexpected: true },
        },
      ],
    })
  )
}

const ctx = fakeContext({ ownerContact: "c-owner" })

async function plan(input: Input, c = ctx): Promise<Plan> {
  return Effect.runPromise(nhs.read(input, c))
}

const failure = async (input: Input) => {
  const r = await runResult(nhs.read(input, ctx))
  return r._tag === "Failure" ? r.failure : undefined
}

function prefixed(
  prefix: string,
  files: Record<string, string | Uint8Array>
): Record<string, string | Uint8Array> {
  return Object.fromEntries(
    Object.entries(files).map(([p, c]) => [`${prefix}${p}`, c])
  )
}

const bytes = (files: Record<string, string | Uint8Array>) =>
  Object.fromEntries(
    Object.entries(files).map(([p, c]) => [
      p,
      typeof c === "string" ? new TextEncoder().encode(c) : c,
    ])
  )

describe("the NHS App importer", () => {
  it("makes the golden plan from an invented run", async () => {
    expect(planDiff(await plan(goldenInput()), GOLDEN)).toEqual([])
  })

  it("asks nothing", async () => {
    expect(await Effect.runPromise(nhs.questions(goldenInput(), ctx))).toEqual(
      []
    )
  })

  it("names the GP practice the provider's source, created once", async () => {
    const p = await plan(goldenInput())
    expect(p.source).toEqual(sourceKey("nhs"))
    expect(p.records[0]).toEqual({
      ...sourceKey("nhs"),
      mode: "create",
      properties: { name: "The Example Surgery", class: "service" },
    })
    const report = plannedRecord(p, KINDS.labreport, "t-fbc")
    expect(report?.properties.lab).toBe("The Example Surgery")
    expect(report?.properties.source).toEqual({ $ref: sourceKey("nhs") })
  })

  it("writes each care provider by its ODS code, merged", async () => {
    const p = await plan(goldenInput())
    const orgs = p.records.filter((r) => r.kind === KINDS.organization)
    expect(orgs.map((o) => [o.namespace, o.key, o.mode])).toEqual([
      ["ods", "A12345", "merge"],
      ["ods", "FA001", "merge"],
      ["ods", "FB002", "merge"],
      ["ods", "RXX", "merge"],
    ])
    expect(orgs.map((o) => o.properties.type)).toEqual([
      "gppractice",
      "pharmacy",
      "pharmacy",
      "hospital",
    ])
    // a hospital the record names only on a waiting list has no address,
    // so a merge never clears one another importer wrote
    expect(orgs[3].properties).not.toHaveProperty("address")
  })

  it("keys and revises every record as the identity table says", async () => {
    const p = await plan(goldenInput())
    expect(p.records.every((r) => r.kind === KINDS.source || r.revision)).toBe(
      true
    )
    for (const r of p.records)
      if (r.kind !== KINDS.source && r.kind !== KINDS.communication)
        expect(r.revision).toBe(REVISION)
    expect(
      plannedRecord(p, KINDS.labresult, "t-fbc/Haemoglobin estimation")
    ).toBeDefined()
    expect(plannedRecord(p, KINDS.patient, "patient", "nhs")).toBeDefined()
    expect(
      plannedRecord(
        p,
        KINDS.condition,
        keyHash("2020-05-01T00:00:00+00:00", "Asthma")
      )
    ).toBeDefined()
    expect(
      plannedRecord(
        p,
        KINDS.referral,
        keyHash("2026-08-01T00:00:00+00:00", "RXX", "Dermatology")
      )
    ).toBeDefined()
    expect(plannedRecord(p, KINDS.medicationrequest, "course-1")).toBeDefined()
    expect(
      plannedRecord(
        p,
        KINDS.medicationrequest,
        keyHash("2026-10-27T00:00:00+00:00", "Newmed 2mg tablets")
      )
    ).toBeDefined()
    expect(
      plannedRecord(p, KINDS.medicationdispense, "EX0001-A12345-00001A/2")
    ).toBeDefined()
    for (const [kind, key] of [
      [KINDS.appointment, "ap-1"],
      [KINDS.document, "doc-2"],
      [KINDS.communication, "m-1"],
    ])
      expect(plannedRecord(p, kind, key)).toBeDefined()
    // a message keeps its own version as its revision; one with none takes
    // the run's start
    expect(plannedRecord(p, KINDS.communication, "m-1")?.revision).toBe(4)
    expect(plannedRecord(p, KINDS.communication, "m-3")?.revision).toBe(
      REVISION
    )
  })

  it("numbers consultations of one date, clinician and place in record order", async () => {
    const p = await plan(goldenInput())
    const key = keyHash(
      "2026-03-30T09:20:00.917+00:00",
      "EXAMPLE, Sam (Dr)",
      "EXAMPLE, Sam (Dr)"
    )
    expect(plannedRecord(p, KINDS.encounter, key)?.properties.summary).toBe(
      "Full blood count"
    )
    expect(
      plannedRecord(p, KINDS.encounter, `${key}-2`)?.properties.summary
    ).toBe("Kidney function")
  })

  it("names from a consultation only the test reports the plan writes", async () => {
    const p = await plan(goldenInput())
    const key = keyHash(
      "2026-03-30T09:20:00.917+00:00",
      "EXAMPLE, Sam (Dr)",
      "EXAMPLE, Sam (Dr)"
    )
    // its Mystery panel names a test none of whose values has a metric
    expect(plannedRecord(p, KINDS.encounter, key)?.properties.reports).toEqual([
      { $ref: { kind: KINDS.labreport, namespace: "nhs", key: "t-fbc" } },
    ])
    expect(plannedRecord(p, KINDS.labreport, "t-unknown")).toBeUndefined()
    // and every reference names a record written before it
    const written = new Set<string>()
    const refs = (v: unknown): string[] =>
      Array.isArray(v)
        ? v.flatMap(refs)
        : v && typeof v === "object"
          ? "$ref" in v
            ? [JSON.stringify(v.$ref)]
            : Object.values(v).flatMap(refs)
          : []
    for (const r of p.records) {
      for (const target of refs(r.properties))
        expect(written.has(target), `${r.kind} ${r.key}`).toBe(true)
      written.add(
        JSON.stringify({ kind: r.kind, namespace: r.namespace, key: r.key })
      )
    }
  })

  it("keeps a GP code id past 2^53 whole", async () => {
    const p = await plan(goldenInput())
    expect(
      plannedRecord(p, KINDS.document, "doc-2")?.properties.coding
    ).toEqual({
      system: "http://snomed.info/sct",
      descriptionId: BIG_CODE,
      display: "Discharge summary",
    })
    const file = (
      plannedRecord(p, KINDS.labreport, "t-ratio")?.properties.file as {
        $doc: string
      }
    ).$doc
    const doc = p.documents.find((d) => d.digest === file)
    expect(new TextDecoder().decode(doc?.bytes)).toContain(
      `"snomedConceptId":${BIG_CODE}`
    )
  })

  it("keeps each test as its report's file, in canonical JSON", async () => {
    const p = await plan(goldenInput())
    const file = (
      plannedRecord(p, KINDS.labreport, "t-kidney")?.properties.file as {
        $doc: string
      }
    ).$doc
    const doc = p.documents.find((d) => d.digest === file)
    expect(doc?.name).toBe("NHS test result 2026-03-30.json")
    const text = new TextDecoder().decode(doc?.bytes)
    expect(JSON.parse(text)).toEqual(TESTS[1])
    expect(text).toContain('"numericValue":90,')
    expect(text.startsWith('{"associatedTexts":[],"date":')).toBe(true)
  })

  it("stores documents under neutral names, never the record's file name", async () => {
    const p = await plan(goldenInput())
    expect(p.documents.map((d) => d.name).slice(-2)).toEqual([
      "NHS document 2026-04-02.pdf",
      "NHS document 2026-05-06.rtf",
    ])
    for (const d of p.documents) expect(d.name).not.toMatch(/Alex|1990/)
    // text served as text is the document's words
    expect(plannedRecord(p, KINDS.document, "doc-2")?.properties.text).toBe(
      "{\\rtf1 Example summary}"
    )
    expect(plannedRecord(p, KINDS.document, "doc-1")?.properties.text).toBe(
      null
    )
    expect(plannedRecord(p, KINDS.document, "doc-3")?.properties.file).toBe(
      null
    )
  })

  it("maps the app's one-word prescription statuses to the tracker's words", async () => {
    const p = await plan(goldenInput())
    const item = (n: number) =>
      plannedRecord(p, KINDS.medicationdispense, `EX0001-A12345-00001A/${n}`)
        ?.properties
    expect(item(1)).toMatchObject({
      status: "preparation",
      sourceStatus: "WithDispenser",
      statusAt: "2026-10-20T10:00:00+01:00",
      at: "2026-10-20T00:00:00Z",
      request: {
        $ref: {
          kind: KINDS.medicationrequest,
          namespace: "nhs",
          key: "course-1",
        },
      },
    })
    expect(item(2)).toMatchObject({
      status: "unknown",
      sourceStatus: "NotTracked",
    })
  })

  it("judges each acute issue by its EPS items and vaccinations, on the run's day", async () => {
    const p = await plan(goldenInput())
    const status = (date: string, name: string) =>
      plannedRecord(p, KINDS.medicationrequest, keyHash(date, name))?.properties
        .status
    // collected, and the vaccination given that day
    expect(
      status("2026-03-30T00:00:00+00:00", "Rabipur vaccine powder and solvent")
    ).toBe("completed")
    // past its validity, with no EPS item: given, by the record
    expect(
      status("2025-02-10T00:00:00+00:00", "Ixiaro suspension for injection")
    ).toBe("completed")
    expect(status("2025-01-15T00:00:00+00:00", "Pastmed 3mg tablets")).toBe(
      "unknown"
    )
    // not tracked is no evidence; the issue is inside its six months
    expect(status("2026-10-19T00:00:00+00:00", "Otherine 5mg capsules")).toBe(
      "active"
    )
    expect(plannedRecord(p, KINDS.medicationrequest, "course-1")).toMatchObject(
      {
        properties: {
          course: "continuous",
          status: "active",
          intent: "plan",
          lastIssuedAt: "2026-10-20T00:00:00Z",
        },
      }
    )
  })

  it("links the patient registration to the owner's contact where one is set", async () => {
    const p = await plan(goldenInput())
    expect(plannedRecord(p, KINDS.patient, "patient")?.properties.about).toBe(
      "substrate.reamde.dev/core/contact/c-owner"
    )
    const without = await plan(goldenInput(), fakeContext())
    expect(
      plannedRecord(without, KINDS.patient, "patient")?.properties
    ).not.toHaveProperty("about")
  })

  it("writes every property a builder knows, null where the export gives nothing", async () => {
    const p = await plan(goldenInput())
    const kinds = new Map<string, Set<string>>()
    for (const r of p.records) {
      if (r.mode !== "maintain" || r.kind === KINDS.patient) continue
      const keys = Object.keys(r.properties).sort().join(",")
      const seen = kinds.get(r.kind) ?? new Set()
      seen.add(keys)
      kinds.set(r.kind, seen)
    }
    for (const [kind, shapes] of kinds)
      expect([kind, shapes.size]).toEqual([kind, 1])
  })
})

describe("the historic test results", () => {
  it("are read year by year where the record holds its results only in part", async () => {
    const p = await plan(goldenInput())
    expect(plannedRecord(p, KINDS.labreport, "t-2019")).toBeDefined()
    expect(p.left).toContainEqual({
      what: "responses/0013-api-v1-patient-historic-test-results-2018.json",
      why: "its test results are not where the importer looks for a year's results",
    })
  })

  it("are left alone where the record holds them all", async () => {
    const p = await plan(
      inputFromRecord(
        "all",
        exampleRun({ all: true, years: [year(2019, [HISTORIC_TEST])] })
      )
    )
    expect(plannedRecord(p, KINDS.labreport, "t-2019")).toBeUndefined()
    expect(plannedRecord(p, KINDS.labreport, "t-fbc")).toBeDefined()
  })

  it("read a test listed twice once, the record's own first", async () => {
    const p = await plan(
      inputFromRecord(
        "twice",
        exampleRun({
          years: [year(2026, [{ ...TESTS[0], term: "Another name" }])],
        })
      )
    )
    expect(
      plannedRecord(p, KINDS.labreport, "t-fbc")?.properties.accession
    ).toBe("Full blood count")
  })
})

describe("detecting an nhsctl run", () => {
  it("takes a run folder by its manifest", async () => {
    expect(await Effect.runPromise(nhs.detect(goldenInput()))).toEqual({
      importer: "nhs",
      confidence: 1,
      why: "An NHS App export by nhsctl, from 2026-11-02, of 17 responses.",
    })
  })

  it("takes the folder of runs, reading the newest by its name", async () => {
    const older = runFiles(
      "2026-01-05T09:00:00Z",
      responses().map((r) =>
        r.url.endsWith("/organisation/A12345")
          ? { ...r, body: [{ id: "A12345", name: "AN OLDER NAME" }] }
          : r
      )
    )
    const input = inputFromRecord("nhs", {
      ...prefixed("20260105T090000Z/", older),
      ...prefixed("20261102T081530Z/", exampleRun()),
      "latest/README": "not a run",
    })
    const match = await Effect.runPromise(nhs.detect(input))
    expect(match?.why).toBe(
      "An NHS App export by nhsctl, the newest of 2 runs, from 2026-11-02, of 15 responses."
    )
    const p = await plan(input)
    expect(plannedRecord(p, KINDS.labreport, "t-fbc")?.revision).toBe(REVISION)
    expect(
      plannedRecord(p, KINDS.organization, "A12345")?.properties.name
    ).toBe("The Example Surgery")
  })

  it("takes a zip of a run or of the folder of runs", async () => {
    const run = await inputFromZip(
      zipSync({ ...bytes(prefixed("20261102T081530Z/", exampleRun())) }),
      "run.zip"
    )
    expect((await Effect.runPromise(nhs.detect(run)))?.confidence).toBe(1)
    const runs = await inputFromZip(
      zipSync(
        bytes({
          ...prefixed(
            "nhs/20260105T090000Z/",
            runFiles("2026-01-05T09:00:00Z", [])
          ),
          ...prefixed("nhs/20261102T081530Z/", exampleRun()),
        })
      ),
      "nhs.zip"
    )
    expect((await Effect.runPromise(nhs.detect(runs)))?.why).toContain(
      "the newest of 2 runs"
    )
    const loose = viewPlan(await plan(inputFromRecord("run.zip", exampleRun())))
    expect(planDiff(await plan(run), loose)).toEqual([])
    expect(planDiff(await plan(runs), { ...loose, name: "nhs.zip" })).toEqual(
      []
    )
  })

  it("is less sure of a run whose manifest names no tool and holds no GP record", async () => {
    const files = runFiles(STARTED_ONLY, [
      {
        url: "https://api.nhsapp.service.nhs.uk/v1/patient/demographics",
        body: {},
      },
    ])
    const m = JSON.parse(files["manifest.json"] as string) as Record<
      string,
      unknown
    >
    delete m.tool
    files["manifest.json"] = JSON.stringify(m)
    expect(
      await Effect.runPromise(nhs.detect(inputFromRecord("x", files)))
    ).toEqual({
      importer: "nhs",
      confidence: 0.8,
      why: "An NHS App export by nhsctl, from 2026-01-01, of 1 responses without the GP record.",
    })
  })

  it("does not take another provider's files", async () => {
    for (const input of OTHERS)
      expect(
        await Effect.runPromise(nhs.detect(input)),
        input.name
      ).toBeUndefined()
  })

  it("is not taken by another importer", async () => {
    const others: Importer[] = [
      csv,
      ...(await Promise.all(
        Object.entries(IMPORTERS)
          .filter(([id]) => id !== "nhs")
          .map(([, load]) => load())
      )),
    ]
    for (const other of others)
      expect(
        await Effect.runPromise(other.detect(goldenInput())),
        other.id
      ).toBeUndefined()
  })
})

const STARTED_ONLY = "2026-01-01T00:00:00Z"

/** Minimal inputs carrying each other provider's own marks. */
const OTHERS: Input[] = [
  inputFromRecord("numan", {
    "2026-01-05-numan-20260105.json": JSON.stringify({
      data: [
        {
          id: "aB3",
          type: "medical_test",
          attributes: { short_id: "XYZ001-01", lab: "Randox", results: [] },
        },
      ],
      meta: { pagination: { next_page: null } },
    }),
  }),
  inputFromRecord("randox", {
    "manifest.json": JSON.stringify({
      files: { "items/visit/1.json": { resource: "visit", id: "1" } },
      runs: [
        { started: "2026-01-20T10:00:00Z", finished: "2026-01-20T10:05:00Z" },
      ],
    }),
    "items/visit/1.json": JSON.stringify({
      id: 1,
      lastUpdated: "2026-01-20T10:00:00",
    }),
  }),
  inputFromRecord("mychart", {
    "manifest.json": JSON.stringify({
      tool: "mychartctl v0.1.0",
      provider: "example",
      baseUrl: "https://mychart.example.test/MyChart",
      items: {},
      runs: [{ startedAt: "2026-01-01T00:00:00Z" }],
    }),
    "items/test-results/WP-1.json": JSON.stringify({
      orderName: "FULL BLOOD COUNT",
    }),
  }),
  inputFromRecord("dxa", {
    "example-dexa-2026-01-01-result-1.pdf": pdfFixture([
      "BT /F1 12 Tf 72 720 Td (DXA) Tj ET",
    ]),
  }),
  inputFromRecord("csv", {
    "results.csv": "Date,Test,Value\n2026-01-01,Ferritin,80\n",
  }),
  inputFromRecord("another tool", {
    "manifest.json": JSON.stringify({
      tool: "othertool",
      startedAt: "2026-01-01T00:00:00Z",
      responses: [
        {
          file: "responses/0001.json",
          url: "https://example.test/v1/patient/my-record",
          status: 200,
        },
      ],
    }),
  }),
]

describe("reading a run that is not what nhsctl writes", () => {
  it("is not its input without a run's manifest", async () => {
    expect(
      await failure(inputFromRecord("x", { "notes.txt": "hello" }))
    ).toEqual({
      _tag: "NotMine",
      message: "This is not an nhsctl export: it holds no run's manifest.",
    })
  })

  it("names the file and the field that is not what nhsctl writes, never the value", async () => {
    const files = exampleRun()
    const m = JSON.parse(files["manifest.json"] as string) as Record<
      string,
      unknown
    >
    delete m.responses
    files["manifest.json"] = JSON.stringify(m)
    expect(await failure(inputFromRecord("x", files))).toEqual({
      _tag: "Unreadable",
      path: "manifest.json",
      field: "responses",
      message: "Missing key",
    })
    const bad = runFiles(STARTED_ONLY, [
      {
        url: "https://api.nhsapp.service.nhs.uk/v1/patient/my-record",
        body: {
          response: {
            testResults: {
              data: [
                {
                  id: 4815162342,
                  date: { value: "2026-01-01T00:00:00+00:00" },
                },
              ],
            },
          },
        },
      },
    ])
    expect(await failure(inputFromRecord("x", bad))).toEqual({
      _tag: "Unreadable",
      path: "responses/0001-api-v1-patient-my-record.json",
      field: "body.response.testResults.data[0].id",
      message: "Expected string",
    })
  })

  it("says which file the manifest names and the folder lacks", async () => {
    const files = exampleRun()
    delete files["responses/0011-api-v1-patient-my-record.json"]
    expect(await failure(inputFromRecord("x", files))).toMatchObject({
      _tag: "Unreadable",
      path: "responses/0011-api-v1-patient-my-record.json",
    })
  })

  it("names the practice by its ODS code where the run holds no answer for it", async () => {
    const p = await plan(
      inputFromRecord(
        "x",
        runFiles(
          STARTED_ONLY,
          responses().filter((r) => !r.url.includes("/organisation/"))
        )
      )
    )
    expect(p.records[0].properties.name).toBe(
      `GP practice ${SESSION.gpOdsCode}`
    )
    expect(plannedRecord(p, KINDS.organization, "A12345")).toBeUndefined()
    expect(
      plannedRecord(p, KINDS.patient, "patient")?.properties.practice
    ).toBeNull()
    expect(plannedRecord(p, KINDS.organization, "FA001")).toMatchObject(
      organizationKey("FA001")
    )
  })
})
