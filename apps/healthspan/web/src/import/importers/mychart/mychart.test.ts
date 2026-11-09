// @vitest-environment node
import { Effect } from "effect"
import { strToU8, zipSync } from "fflate"
import { describe, expect, it } from "vitest"
import { IMPORTERS } from ".."
import { inputFromZip } from "../input"
import { KINDS } from "../shared"
import {
  fakeContext,
  inputFromRecord,
  planDiff,
  plannedRecord,
  runResult,
  viewPlan,
  type PlanView,
} from "../testing"
import type { Context, ImportError, Input, Plan } from "../types"
import { dotnetInstant, plusMinutes, prescriptionStatus } from "./clinical"
import { exportFiles, HOST, png, REVISION } from "./fixture"
import { safeId } from "./folder"
import { ANSWERS, mychart, odsCode } from "."
import golden from "./plan.golden.json"

const NS = `mychart/${HOST}`
const NAME = "Example Hospitals NHS Foundation Trust"
const answers = { [ANSWERS.name]: NAME, [ANSWERS.ods]: "rx1 " }
const ctx = (over: Parameters<typeof fakeContext>[0] = {}) =>
  fakeContext({ answers, ownerContact: "c1", ...over })

type Files = Record<string, string | Uint8Array>

/** The fixture with one JSON file changed. */
function edit(
  files: Files,
  path: string,
  change: (value: Record<string, unknown>) => void
): Files {
  const value = JSON.parse(files[path] as string) as Record<string, unknown>
  change(value)
  return { ...files, [path]: JSON.stringify(value) }
}

const input = (files: Files = exportFiles()) =>
  inputFromRecord("example-export", files)

async function plan(i: Input = input(), c: Context = ctx()): Promise<Plan> {
  return Effect.runPromise(mychart.read(i, c))
}

async function failure(i: Input, c: Context = ctx()): Promise<ImportError> {
  const r = await runResult(mychart.read(i, c))
  if (r._tag !== "Failure") throw new Error("the read did not fail")
  return r.failure
}

const record = (p: Plan, kind: string, key: string) => {
  const r = plannedRecord(p, kind, key)
  if (!r) throw new Error(`no ${kind} ${key}`)
  return r
}

describe("detecting a mychartctl export", () => {
  it("claims the folder by its manifest and its items", async () => {
    const m = await Effect.runPromise(mychart.detect(input()))
    expect(m).toEqual({
      importer: "mychart",
      confidence: 1,
      why: `A mychartctl export of the MyChart portal at ${HOST}: 4 test results, 5 visits, 4 conversations`,
    })
  })

  it("claims its zip, and the one portal folder inside a picked parent", async () => {
    const files = exportFiles()
    const zip = zipSync(
      Object.fromEntries(
        Object.entries(files).map(([p, c]) => [
          `export/${p}`,
          typeof c === "string" ? strToU8(c) : c,
        ])
      )
    )
    const zipped = await inputFromZip(zip, "export.zip")
    expect((await Effect.runPromise(mychart.detect(zipped)))?.confidence).toBe(
      1
    )
    expect((await plan(zipped)).records).toHaveLength(35)
    const parent = inputFromRecord(
      "mychart",
      Object.fromEntries(Object.entries(files).map(([p, c]) => [`one/${p}`, c]))
    )
    expect((await Effect.runPromise(mychart.detect(parent)))?.confidence).toBe(
      1
    )
    expect(viewPlan(await plan(parent)).records).toEqual(
      viewPlan(await plan()).records
    )
  })

  it("is unsure of a manifest with no items beside it", async () => {
    const files = { "manifest.json": exportFiles()["manifest.json"] }
    const m = await Effect.runPromise(mychart.detect(input(files)))
    expect(m?.confidence).toBe(0.5)
  })

  it("does not claim another fetcher's folder or another provider's files", async () => {
    const others: Record<string, Files> = {
      nhs: {
        "manifest.json": JSON.stringify({
          tool: "nhsctl dev",
          startedAt: "2026-01-01T00:00:00Z",
          responses: [],
        }),
        "responses/0001.json": "{}",
      },
      randox: {
        "manifest.json": JSON.stringify({
          files: {},
          runs: [{ started: "2026-01-01T00:00:00Z" }],
        }),
        "items/visit/1.json": "{}",
      },
      numan: {
        "export-numan-20260101.json": JSON.stringify({
          data: [{ id: "a", type: "medical_test", attributes: {} }],
        }),
      },
      dxa: {
        "scan-result-1.pdf": "%PDF-1.4",
        "scan-result-3.json": JSON.stringify({ rows: [] }),
      },
      notMyChart: {
        "manifest.json": JSON.stringify({
          tool: "otherctl",
          baseUrl: "https://example.com/MyChart",
          runs: [],
        }),
        "items/visits/a.json": "{}",
      },
    }
    for (const files of Object.values(others))
      expect(await Effect.runPromise(mychart.detect(input(files)))).toBe(
        undefined
      )
  })

  it("is claimed by no other importer", async () => {
    for (const [id, load] of Object.entries(IMPORTERS)) {
      if (id === "mychart") continue
      const other = await load()
      expect(
        await Effect.runPromise(other.detect(input())),
        `${id} claims a MyChart export`
      ).toBe(undefined)
    }
  })
})

describe("what the export does not say", () => {
  it("asks the hospital's name, suggested from the export, and its ODS code", async () => {
    const qs = await Effect.runPromise(mychart.questions(input(), ctx()))
    expect(qs.map((q) => [q.key, q.kind, q.required, q.suggested])).toEqual([
      [ANSWERS.name, "text", false, "Example MyChart"],
      [ANSWERS.ods, "text", true, undefined],
    ])
  })

  it("asks a portal outside .uk for its time zone", async () => {
    const files = edit(exportFiles(), "manifest.json", (m) => {
      m.baseUrl = "https://mychart.example.org/MyChart"
    })
    const qs = await Effect.runPromise(mychart.questions(input(files), ctx()))
    expect(qs.map((q) => [q.key, q.kind])).toContainEqual([
      ANSWERS.zone,
      "timezone",
    ])
    const needs = await failure(input(files))
    expect(needs).toMatchObject({
      _tag: "NeedsAnswers",
      questions: [{ key: ANSWERS.zone }],
    })
    const p = await plan(
      input(files),
      ctx({ answers: { ...answers, [ANSWERS.zone]: "America/New_York" } })
    )
    expect(record(p, KINDS.appointment, "CSN-4").properties.at).toBe(
      "2026-07-01T04:30:00-04:00"
    )
  })

  it("needs the ODS code, and says when an answer is not one", async () => {
    expect(
      await failure(input(), ctx({ answers: { [ANSWERS.name]: NAME } }))
    ).toMatchObject({ _tag: "NeedsAnswers", questions: [{ key: ANSWERS.ods }] })
    const wrong = await failure(
      input(),
      ctx({ answers: { ...answers, [ANSWERS.ods]: "R!" } })
    )
    if (wrong._tag !== "NeedsAnswers") throw new Error("no questions")
    expect(wrong.questions[0].help).toMatch(/^That is not an ODS code\./)
    expect(odsCode(" rrx ")).toBe("RRX")
    expect(odsCode("A1 2345")).toBe("A12345")
    expect(odsCode("ab")).toBeUndefined()
  })

  it("takes the export's name where the owner gives none", async () => {
    const p = await plan(input(), ctx({ answers: { [ANSWERS.ods]: "RX1" } }))
    expect(p.records[0].properties.name).toBe("Example MyChart")
    expect(record(p, KINDS.patient, "patient").properties.service).toBe(
      "Example MyChart"
    )
  })
})

describe("reading the export", () => {
  it("makes the plan its golden fixture holds", async () => {
    expect(planDiff(await plan(), golden as PlanView)).toEqual([])
  })

  it("writes the source first, then the organizations, each report before its results, and a record after what it names", async () => {
    const p = await plan()
    expect(p.source).toEqual({
      kind: KINDS.source,
      namespace: "source",
      key: NS,
    })
    expect(p.records[0]).toMatchObject({
      kind: KINDS.source,
      mode: "create",
      properties: { name: NAME, class: "service" },
    })
    expect(p.records[0].revision).toBeUndefined()
    expect(p.records[1]).toMatchObject({
      kind: KINDS.organization,
      namespace: "ods",
      key: "RX1",
      mode: "merge",
    })
    const at = (kind: string, key: string) =>
      p.records.findIndex((r) => r.kind === kind && r.key === key)
    expect(at(KINDS.labreport, "RES-A")).toBeLessThan(
      at(KINDS.labresult, "RES-A/C-HB")
    )
    expect(at(KINDS.labreport, "RES-A")).toBeLessThan(
      at(KINDS.encounter, "CSN-1")
    )
    expect(at(KINDS.appointment, "CSN-1")).toBeLessThan(
      at(KINDS.encounter, "CSN-1")
    )
    expect(at(KINDS.encounter, "CSN-1")).toBeLessThan(
      at(KINDS.document, "AVS-1")
    )
    expect(at(KINDS.medicationrequest, "MED-1")).toBeLessThan(
      at(KINDS.medicationdispense, "MED-1/2026-03-03")
    )
  })

  it("revises every record but the source by the newest run's start", async () => {
    const p = await plan()
    for (const r of p.records.slice(1)) expect(r.revision).toBe(REVISION)
  })

  it("keys a lab report by MyChart's result key whole, and a result by its component", async () => {
    const long = `WP-24${"A".repeat(200)}-2F-3D`
    const files = edit(
      exportFiles(),
      "items/test-results/ORDER-A.json",
      (o) => {
        ;(o.results as { key: string }[])[0].key = long
      }
    )
    const p = await plan(input(files))
    expect(record(p, KINDS.labreport, long).namespace).toBe(NS)
    expect(plannedRecord(p, KINDS.labresult, `${long}/C-HB`)).toBeDefined()
    // no component id: the component's name
    expect(
      record(p, KINDS.labresult, `${long}/C-reactive protein`).properties
        .upstreamId
    ).toBe(`${long}/C-reactive protein`)
  })

  it("keeps a value MyChart files under two orders once, in the first by sample time and key", async () => {
    const p = await plan()
    expect(plannedRecord(p, KINDS.labresult, "RES-0/C-TSH")).toBeDefined()
    expect(plannedRecord(p, KINDS.labreport, "RES-B")).toBeUndefined()
    // the visit the emptied order was filed at names only what is written
    expect(record(p, KINDS.encounter, "CSN-1").properties.reports).toEqual([
      {
        $ref: { kind: KINDS.labreport, namespace: NS, key: "RES-A" },
      },
    ])
  })

  it("names the hospital as the lab where MyChart names none", async () => {
    const p = await plan()
    expect(record(p, KINDS.labreport, "RES-0").properties.lab).toBe(NAME)
    expect(record(p, KINDS.labreport, "RES-A").properties.lab).toBe(
      "Example Pathology"
    )
  })

  it("keeps a JSON report's order as canonical JSON under a neutral name", async () => {
    const p = await plan()
    const file = record(p, KINDS.labreport, "RES-A").properties.file as {
      $doc: string
    }
    const doc = p.documents.find((d) => d.digest === file.$doc)!
    expect(doc.name).toBe("MyChart lab report 2026-03-02.json")
    const text = new TextDecoder().decode(doc.bytes)
    expect(text.startsWith('{"key":"ORDER-A","orderName":')).toBe(true)
  })

  it("says how each visit happened: its summary's words, else its type", async () => {
    const p = await plan()
    const channel = (key: string) =>
      record(p, KINDS.appointment, key).properties.channel
    expect(channel("CSN-1")).toBe("telephone")
    expect(channel("CSN-2")).toBe("inperson")
    expect(channel("CSN-4")).toBe("telephone")
    expect(channel("CSN-5")).toBe("video")
    expect(record(p, KINDS.encounter, "CSN-3").properties.class).toBe(
      "emergency"
    )
    expect(plannedRecord(p, KINDS.appointment, "CSN-3")).toBeUndefined()
  })

  it("files an imaging result under no visit at the outpatient attendance before it", async () => {
    const p = await plan()
    const enc = { $ref: { kind: KINDS.encounter, namespace: NS, key: "CSN-2" } }
    expect(record(p, KINDS.document, "RES-IMG").properties.encounter).toEqual(
      enc
    )
    expect(record(p, KINDS.document, "SCAN-2").properties.encounter).toEqual(
      enc
    )
    // three hours later is too late
    const files = edit(
      exportFiles(),
      "items/test-results/ORDER-D.json",
      (o) => {
        ;(
          o.results as { orderMetadata: { prioritizedInstantISO: string } }[]
        )[0].orderMetadata.prioritizedInstantISO = "2026-03-05T13:00:01+00:00"
      }
    )
    const later = await plan(input(files))
    expect(
      record(later, KINDS.document, "RES-IMG").properties.encounter
    ).toBeNull()
  })

  it("reads the patient's numbers from the letters, the practice by its ODS code, and about from the owner's contact", async () => {
    const p = await plan()
    const patient = record(p, KINDS.patient, "patient")
    expect(patient.properties).toMatchObject({
      name: "Ada Example",
      identifiers: [
        { value: "1234567", label: "Hospital number" },
        { value: "9999999999", label: "NHS number" },
      ],
      practice: {
        $ref: { kind: KINDS.organization, namespace: "ods", key: "A12345" },
      },
      about: "substrate.reamde.dev/core/contact/c1",
      phone: "07000 000000",
    })
    const none = await plan(input(), fakeContext({ answers }))
    expect("about" in record(none, KINDS.patient, "patient").properties).toBe(
      false
    )
  })

  it("keys a medicine by its id, else its name, and its dispense by its date", async () => {
    const p = await plan()
    expect(record(p, KINDS.medicationrequest, "MED-1").properties.status).toBe(
      "completed"
    )
    expect(record(p, KINDS.medicationrequest, "MED-2").properties.status).toBe(
      "active"
    )
    expect(
      record(p, KINDS.medicationrequest, "otherol 5mg").properties
    ).toMatchObject({ course: null, intent: null, organization: null })
    expect(
      record(p, KINDS.medicationdispense, "MED-1/2026-03-03").properties
        .pharmacy
    ).toEqual({
      $ref: {
        kind: KINDS.organization,
        namespace: NS,
        key: "pharmacy/PH-1",
      },
    })
    expect(record(p, KINDS.organization, "pharmacy/PH-1").mode).toBe("merge")
  })

  it("leaves out the owner's messages, banners repeated across most messages and images shaped like logos", async () => {
    const p = await plan()
    expect(plannedRecord(p, KINDS.communication, "MSG-OWN")).toBeUndefined()
    const attachments = (key: string) =>
      record(p, KINDS.communication, key).properties.attachments
    expect(attachments("MSG-1")).toHaveLength(1)
    expect(attachments("MSG-2")).toHaveLength(1)
    expect(attachments("MSG-3")).toBeNull()
    expect(p.left).toContainEqual({
      what: "4 images embedded in messages",
      why: "banners, logos and QR codes: an image repeated across most messages, or shaped like a logo",
    })
  })

  it("keeps an image only one message carries", async () => {
    const files = exportFiles()
    for (const p of Object.keys(files))
      if (/CONV-[234]/.test(p)) delete files[p]
    files["files/messages/CONV-1-2.png"] = png(1000, 1000, 9)
    const p = await plan(input(files))
    expect(
      record(p, KINDS.communication, "MSG-1").properties.attachments
    ).toHaveLength(1)
  })

  it("keeps a visit document pdf.js cannot read, without its words", async () => {
    const files = {
      ...exportFiles(),
      "files/visit-documents/AVS-1.pdf": strToU8("not a PDF"),
    }
    const p = await plan(input(files))
    expect(record(p, KINDS.document, "AVS-1").properties.text).toBeNull()
    expect(p.left).toContainEqual({
      what: "files/visit-documents/AVS-1.pdf",
      why: "pdf.js could not read its words; the file is kept without them",
    })
  })
})

describe("what is not the export's", () => {
  it("is not this importer's input without a mychartctl manifest", async () => {
    expect(await failure(input({ "a.json": "{}" }))).toMatchObject({
      _tag: "NotMine",
    })
  })

  it("names the file and the field that is not what MyChart promises", async () => {
    const files = edit(exportFiles(), "items/visits/CSN-2.json", (v) => {
      v.IsPastVisit = "yes"
    })
    expect(await failure(input(files))).toEqual({
      _tag: "Unreadable",
      path: "items/visits/CSN-2.json",
      field: "IsPastVisit",
      message: expect.any(String) as string,
    })
    const norun = edit(exportFiles(), "manifest.json", (m) => {
      m.runs = []
    })
    expect(await failure(input(norun))).toMatchObject({
      _tag: "Unreadable",
      path: "manifest.json",
      field: "runs",
    })
  })
})

describe("the small readings", () => {
  it("judges a prescription's status from its refill details", () => {
    const today = "2026-03-10"
    expect(
      prescriptionStatus(
        { refillExpirationDate: "1 June 2026", hasRefillsRemaining: false },
        today
      )
    ).toBe("active")
    expect(
      prescriptionStatus(
        {
          refillsRemainingKey: "numrefills_expired",
          lastDispense: { dispenseDate: "1 March 2026" },
        },
        today
      )
    ).toBe("completed")
    expect(
      prescriptionStatus(
        {
          hasRefillsRemaining: true,
          lastDispense: { dispenseDate: "1 March 2026" },
        },
        today
      )
    ).toBe("active")
    expect(
      prescriptionStatus({ refillsRemainingKey: "numrefills_expired" }, today)
    ).toBe("stopped")
    expect(
      prescriptionStatus({ refillExpirationDate: "1 March 2026" }, today)
    ).toBe("stopped")
    expect(
      prescriptionStatus(
        {
          refillsRemainingKey: "numrefills_expired",
          hasRefillsRemaining: true,
          lastDispense: { dispenseDate: "1 January 2026" },
        },
        today
      )
    ).toBe("stopped")
    expect(prescriptionStatus({}, today)).toBe("unknown")
  })

  it("reads .NET instants in the portal's zone, and adds minutes at the stamp's own offset", () => {
    expect(dotnetInstant("/Date(1767261600000)/", "Europe/London")).toBe(
      "2026-01-01T10:00:00+00:00"
    )
    expect(dotnetInstant("/Date(1782900000123)/", "Europe/London")).toBe(
      "2026-07-01T11:00:00.123000+01:00"
    )
    expect(dotnetInstant("2026-01-01", "Europe/London")).toBeNull()
    expect(plusMinutes("2026-10-25T01:30:00+01:00", 60)).toBe(
      "2026-10-25T02:30:00+01:00"
    )
    expect(plusMinutes("2026-10-25T01:30:00+01:00", 0)).toBeNull()
  })

  it("names a file as mychartctl does", () => {
    expect(safeId("WP-24abc-3D")).toBe("WP-24abc-3D")
    expect(safeId(" a/b c^^ ")).toBe("a_b_c")
    expect(safeId("..x..")).toBe("x")
    expect(safeId("x".repeat(200))).toMatch(/^x{160}-[0-9a-f]{8}$/)
  })
})
