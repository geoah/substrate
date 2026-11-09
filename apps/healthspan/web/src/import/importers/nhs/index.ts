// The NHS App, as the folder `nhsctl` writes for one run (or the folder of
// its runs, the newest read, or a zip of either): the GP record's test
// results as lab reports and results, and the whole clinical record
// (apps/healthspan/docs/importers/05-nhs.md). The GP practice is the source
// of the test results; every record's revision is the run's start, a
// message's its own version.

import { Effect } from "effect"
import {
  decodeValue,
  epoch,
  orUnreadable,
  plannedDocument,
  sourceKey,
  sourceRecord,
} from "../shared"
import type {
  Context,
  ImportError,
  Importer,
  Input,
  LeftOut,
  Match,
  Plan,
  PlannedDocument,
} from "../types"
import { careProviders, clinicalRecords, organizationRecords } from "./clinical"
import { testRecords } from "./labs"
import {
  answers,
  bodyBytes,
  bodyOf,
  decodeAt,
  decodeBody,
  findRun,
  lastBody,
  urlParts,
  type Run,
} from "./run"
import {
  Appointments,
  Courses,
  Demographics,
  MessagePage,
  MyRecord,
  NominatedPharmacy,
  Organisations,
  PdsRecord,
  Prescriptions,
  SecondaryCare,
  TestResult,
  type Message,
} from "./schema"

/** The provider's account: one NHS App login. */
export const NAMESPACE = "nhs"

const MY_RECORD = "/v1/patient/my-record"
const HISTORIC = /^\/v1\/patient\/historic-test-results\/\d+$/
const DOWNLOAD = /\/v1\/documents\/([^/]+)\/download$/

/** The run's start as a date, for what the screen shows. */
function day(stamp: string): string {
  return stamp.slice(0, 10)
}

function detect(input: Input): Effect.Effect<Match | undefined> {
  return findRun(input).pipe(
    Effect.map((run): Match | undefined => {
      if (!run) return undefined
      const m = run.manifest
      const record = answers(run, MY_RECORD).length > 0
      const runs = run.runs > 1 ? `the newest of ${run.runs} runs, ` : ""
      return {
        importer: "nhs",
        confidence: m.tool?.startsWith("nhsctl") && record ? 1 : 0.8,
        why: `An NHS App export by nhsctl, ${runs}from ${day(m.startedAt)}, of ${m.responses.length} responses${record ? "" : " without the GP record"}.`,
      }
    }),
    Effect.catchCause(() => Effect.succeed(undefined))
  )
}

/** A test list wherever a year's answer keeps it: the body itself, its
 * `response`, a `historicTestResults` or `testResults` section's `data`,
 * or its own `data`. */
function testList(body: unknown): unknown[] | undefined {
  if (Array.isArray(body)) return body as unknown[]
  if (!body || typeof body !== "object") return undefined
  const o = body as Record<string, unknown>
  if (o.response && typeof o.response === "object") return testList(o.response)
  for (const k of ["historicTestResults", "testResults"]) {
    const s = o[k]
    if (s && typeof s === "object") {
      const data = (s as Record<string, unknown>).data
      if (Array.isArray(data)) return data as unknown[]
    }
  }
  return Array.isArray(o.data) ? (o.data as unknown[]) : undefined
}

/** The tests of a list, each decoded and kept as it came. */
function decodeTests(
  list: readonly unknown[],
  path: string,
  field: string
): Effect.Effect<{ test: TestResult; raw: unknown }[], ImportError> {
  return Effect.forEach(list, (raw, i) =>
    decodeValue(TestResult, raw, path).pipe(
      Effect.mapError((e) =>
        e._tag === "Unreadable"
          ? { ...e, field: `${field}[${i}]${e.field ? `.${e.field}` : ""}` }
          : e
      ),
      Effect.map((test) => ({ test, raw }))
    )
  )
}

/** The record's test results, and where the record says it holds them only
 * in part, its historic results and every year's results the run
 * downloaded; a test listed twice is read the first time. */
function readTests(
  run: Run,
  raw: unknown,
  record: MyRecord,
  left: LeftOut[]
): Effect.Effect<{ test: TestResult; raw: unknown }[], ImportError> {
  return Effect.gen(function* () {
    const hits = answers(run, MY_RECORD)
    const path = `${run.prefix}${hits[hits.length - 1]?.file ?? ""}`
    const response = (raw as { response?: Record<string, unknown> } | null)
      ?.response
    const section = (k: string) =>
      (response?.[k] as { data?: unknown[] } | null)?.data ?? []
    const out = yield* decodeTests(
      section("testResults"),
      path,
      "body.response.testResults.data"
    )
    if (record.response?.testResults?.hasAllHistoricResults === false) {
      out.push(
        ...(yield* decodeTests(
          section("historicTestResults"),
          path,
          "body.response.historicTestResults.data"
        ))
      )
      const years = new Map<string, (typeof run.manifest.responses)[number]>()
      for (const e of answers(run, HISTORIC)) years.set(urlParts(e.url).path, e)
      for (const e of years.values()) {
        const body = yield* bodyOf(run, e)
        if (body === null || body === undefined) continue
        const list = testList(body)
        if (!list) {
          left.push({
            what: `${run.prefix}${e.file}`,
            why: "its test results are not where the importer looks for a year's results",
          })
          continue
        }
        out.push(
          ...(yield* decodeTests(list, `${run.prefix}${e.file}`, "body"))
        )
      }
    }
    const seen = new Set<string>()
    return out.filter(({ test }) =>
      seen.has(test.id) ? false : (seen.add(test.id), true)
    )
  })
}

/** Every inbox page's messages, each once: the last copy, where the first
 * came. */
function readMessages(run: Run): Effect.Effect<Message[], ImportError> {
  return Effect.gen(function* () {
    const byId = new Map<string, Message>()
    for (const e of answers(run, "/v2/api/users/me/messages")) {
      const page = yield* decodeBody(run, e, MessagePage)
      for (const m of page?.messages ?? []) byId.set(m.id, m)
    }
    return [...byId.values()]
  })
}

/** Each document's file the run downloaded, by the document's id: the
 * last download of each, stored under a neutral name, with its words where
 * it is text. */
function readFiles(
  run: Run,
  documents: readonly {
    documentIdentifier: string
    extension?: string | null
    effectiveDate: { value?: string | null }
  }[]
): Effect.Effect<
  Map<string, { document: PlannedDocument; text: string | null }>,
  ImportError
> {
  return Effect.gen(function* () {
    const downloads = new Map<string, (typeof run.manifest.responses)[number]>()
    for (const e of run.manifest.responses) {
      const m = DOWNLOAD.exec(urlParts(e.url).path)
      if (m && e.status === 200 && e.bodyFile) downloads.set(m[1], e)
    }
    const out = new Map<
      string,
      { document: PlannedDocument; text: string | null }
    >()
    for (const d of documents) {
      const e = downloads.get(d.documentIdentifier)
      if (!e) continue
      const bytes = yield* bodyBytes(run, e)
      const mediaType =
        (e.contentType || "application/octet-stream").split(";")[0].trim() ||
        "application/octet-stream"
      const ext = (d.extension ?? "").toLowerCase() || undefined
      out.set(d.documentIdentifier, {
        document: plannedDocument(bytes, {
          provider: "NHS",
          kind: "document",
          date: (d.effectiveDate.value ?? "").slice(0, 10),
          mediaType,
          ext,
        }),
        text: mediaType.startsWith("text/")
          ? new TextDecoder("utf-8", { ignoreBOM: true }).decode(bytes)
          : null,
      })
    }
    return out
  })
}

function read(input: Input, ctx: Context): Effect.Effect<Plan, ImportError> {
  return Effect.gen(function* () {
    const run = yield* findRun(input)
    if (!run)
      return yield* Effect.fail<ImportError>({
        _tag: "NotMine",
        message: "This is not an nhsctl export: it holds no run's manifest.",
      })
    const m = run.manifest
    const revision = yield* orUnreadable(
      epoch(m.startedAt),
      `${run.prefix}manifest.json`,
      "the run's start is not an ISO 8601 time",
      "startedAt"
    )
    const now = new Date(revision * 1000).toISOString().slice(0, 10)
    const session = m.session ?? {}
    const left: LeftOut[] = []

    // the GP record, kept as it came for the test results' files
    const recordHits = answers(run, MY_RECORD)
    const recordEntry = recordHits[recordHits.length - 1]
    const raw = recordEntry ? yield* bodyOf(run, recordEntry) : undefined
    const record: MyRecord =
      recordEntry && raw !== null && raw !== undefined
        ? yield* decodeAt(MyRecord, raw, `${run.prefix}${recordEntry.file}`)
        : {}
    if (!recordEntry)
      left.push({
        what: `${run.prefix}manifest.json`,
        why: "the run holds no GP record, so no test results, consultations, problems, allergies, vaccinations, medicines or documents are read",
      })

    // the care providers
    const code = session.gpOdsCode
    const organisations = code
      ? yield* lastBody(run, `/v1/api/organisation/${code}`, Organisations)
      : undefined
    const pharmacy = (yield* lastBody(
      run,
      "/v1/patient/nominated-pharmacy",
      NominatedPharmacy
    ))?.pharmacyDetails
    const eps = yield* lastBody(run, "/api/v1/prescriptions", Prescriptions)
    const prescriptions = [
      ...(eps?.prescriptions ?? []),
      ...(eps?.pastPrescriptions ?? []),
    ]
    const waits =
      (yield* lastBody(run, "/v1/SecondaryCare/summary/", SecondaryCare))
        ?.waitTimes ?? []
    const orgs = careProviders({
      organisations,
      pharmacy,
      prescriptions,
      waits,
    })
    const practiceName =
      (code && orgs.get(code)?.name) ||
      (code ? `GP practice ${code}` : "GP practice")

    // the test results
    const source = sourceKey(NAMESPACE)
    const tests = yield* readTests(run, raw, record, left)
    const labs = testRecords(
      tests,
      {
        namespace: NAMESPACE,
        source,
        lab: practiceName,
        revision,
      },
      ctx
    )
    left.push(...labs.left)

    // the rest of the record
    const demographics = (yield* lastBody(
      run,
      "/v1/patient/demographics",
      Demographics
    ))?.response
    const home = (yield* lastBody(run, "/v1/patient/pds-record", PdsRecord))
      ?.currentHomeAddress
    const courses =
      (yield* lastBody(run, "/v1/patient/courses", Courses))?.courses ?? []
    const booked = yield* lastBody(
      run,
      "/v1/patient/appointments",
      Appointments
    )
    const messages = yield* readMessages(run)
    const documents = record.response?.documents?.data ?? []
    const files = yield* readFiles(run, documents)
    for (const d of documents)
      if (!files.has(d.documentIdentifier))
        left.push({
          what: `a document of ${(d.effectiveDate.value ?? "").slice(0, 10) || "no date"}`,
          why: "the export holds no file for it, so it is written without one",
        })
    const clinical = clinicalRecords({
      namespace: NAMESPACE,
      revision,
      now,
      session,
      orgs,
      pharmacy: pharmacy?.odsCode,
      record: record.response ?? {},
      demographics,
      home,
      courses,
      prescriptions,
      appointments: {
        past: booked?.pastAppointments ?? [],
        upcoming: booked?.upcomingAppointments ?? [],
      },
      waits,
      messages,
      files,
      reports: labs.written,
      ownerContact: ctx.ownerContact,
    })

    const documentsOut = [...labs.documents]
    for (const f of files.values())
      if (!documentsOut.some((d) => d.digest === f.document.digest))
        documentsOut.push(f.document)
    return {
      importer: "nhs",
      name: input.name,
      source,
      records: [
        sourceRecord({
          namespace: NAMESPACE,
          name: practiceName,
          class: "service",
        }),
        ...organizationRecords(orgs, revision),
        ...labs.records,
        ...clinical,
      ],
      documents: documentsOut,
      left,
    } satisfies Plan
  })
}

export const nhs: Importer = {
  id: "nhs",
  name: "NHS App export",
  description:
    "The folder nhsctl writes for a run of the NHS App, or the folder of its runs, or a zip of either: the GP record's test results and the whole clinical record.",
  accepts: { kind: "folder" },
  detect,
  questions: () => Effect.succeed([]),
  read,
}
