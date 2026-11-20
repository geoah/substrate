// The MyChart importer: the folder mychartctl writes for one Epic MyChart
// portal (or its zip) as a plan: the hospital as the source of its lab
// results and as an organization, the lab reports and results, and the
// clinical record (apps/healthspan/docs/importers/06-mychart.md).

import { Effect } from "effect"
import {
  knownZone,
  organizationRecord,
  pdfPages,
  pdfText,
  readBytes,
  runStarted,
  sourceKey,
  sourceRecord,
  strip,
  address,
  orUnreadable,
  readJson,
  type Address,
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
  Question,
} from "../types"
import { readClinical, type LoadedFile } from "./clinical"
import {
  findManifest,
  Folder,
  isMyChartManifest,
  peekJson,
  portalHost,
  type Found,
} from "./folder"
import { readLabs, sortResults } from "./labs"
import {
  Allergy,
  Conversation,
  HealthIssue,
  HealthSummary,
  HealthUpkeep,
  Letter,
  Letters,
  Manifest,
  Medication,
  PersonalContact,
  PersonalDetails,
  Relationships,
  Report,
  TestResult,
  Visit,
  VisitDetails,
  VisitNotes,
} from "./schemas"

/** The keys of the importer's questions. */
export const ANSWERS = {
  name: "hospitalName",
  ods: "hospitalOds",
  zone: "timeZone",
} as const

/** The zone of a portal under `.uk`. */
const LONDON = "Europe/London"

/** The folders whose downloads the record names. */
const DOWNLOADS = ["visit-documents", "test-results", "messages"] as const

interface Marks {
  found: Found
  manifest: Manifest
  host: string | undefined
}

/** The input's mychartctl manifest, where it has one. */
function marks(input: Input): Effect.Effect<Marks | undefined> {
  const found = findManifest(input)
  if (!found) return Effect.succeed(undefined)
  return peekJson(found.manifest).pipe(
    Effect.map((v) =>
      isMyChartManifest(v)
        ? { found, manifest: v, host: portalHost(v.baseUrl) }
        : undefined
    )
  )
}

function count(input: Input, prefix: string, resource: string): number {
  const dir = `${prefix}items/${resource}/`
  return input.files.filter(
    (f) => f.path.startsWith(dir) && f.path.endsWith(".json")
  ).length
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`
}

/** The hospital's name as the export gives it: the organization its
 * visits name, else the one its medicines name. */
function exportName(
  visits: readonly { value: Visit }[],
  medications: readonly unknown[] = []
): string | undefined {
  for (const { value } of visits) {
    const name = strip(value.Organization?.OrganizationName ?? "")
    if (name) return name
  }
  for (const m of medications) {
    const o = (m as { organization?: { organizationName?: unknown } })
      ?.organization
    const name =
      typeof o?.organizationName === "string" ? strip(o.organizationName) : ""
    if (name) return name
  }
  return undefined
}

/** Whether a portal shows its times in London: it is under `.uk`. */
function inLondon(host: string | undefined): boolean {
  return !!host && /\.uk$/.test(host)
}

function nameQuestion(suggested: string | undefined): Question {
  const q: Question = {
    key: ANSWERS.name,
    label: "The hospital's name",
    help: "The name of the hospital or trust that runs the portal. It names the source of its lab results and the hospital in its record. The export names the portal's organization, which may be a shorter name.",
    kind: "text",
    required: !suggested,
  }
  if (suggested) q.suggested = suggested
  return q
}

function odsQuestion(problem?: string): Question {
  return {
    key: ANSWERS.ods,
    label: "The hospital's ODS code",
    help:
      (problem ? `${problem} ` : "") +
      "The NHS Organisation Data Service code of the hospital's trust (three characters for a trust). The export does not state it. The hospital's record is kept under it, so another import that names the same hospital names one record.",
    kind: "text",
    required: true,
  }
}

function zoneQuestion(): Question {
  return {
    key: ANSWERS.zone,
    label: "The portal's time zone",
    help: "The zone the portal shows its times in. The export names none; a portal under .uk is in Europe/London.",
    kind: "timezone",
    required: true,
  }
}

/** An ODS code as the register writes it: upper case, no spaces; undefined
 * for anything that is not one. */
export function odsCode(text: string | undefined): string | undefined {
  const s = strip(text ?? "")
    .replace(/\s+/g, "")
    .toUpperCase()
  return /^[A-Z0-9]{3,10}$/.test(s) ? s : undefined
}

/** A file's downloads read: bytes, and a visit document's words. */
function loadDownloads(
  folder: Folder
): Effect.Effect<
  { files: Map<string, LoadedFile>; left: LeftOut[] },
  ImportError
> {
  return Effect.gen(function* () {
    const files = new Map<string, LoadedFile>()
    const left: LeftOut[] = []
    for (const resource of DOWNLOADS) {
      const dir = `${folder.prefix}files/${resource}/`
      for (const file of folder.input.files) {
        if (!file.path.startsWith(dir)) continue
        const bytes = yield* readBytes(file)
        const loaded: LoadedFile = { path: file.path, bytes }
        if (resource === "visit-documents" && /\.pdf$/i.test(file.path)) {
          const text = yield* pdfPages(bytes, file.path).pipe(
            Effect.map((pages) => pdfText(pages) ?? null),
            Effect.catch(() => Effect.succeed(undefined))
          )
          if (text === undefined)
            left.push({
              what: file.path,
              why: "pdf.js could not read its words; the file is kept without them",
            })
          loaded.text = text ?? null
        }
        files.set(file.path, loaded)
      }
    }
    return { files, left }
  })
}

function read(input: Input, ctx: Context): Effect.Effect<Plan, ImportError> {
  return Effect.gen(function* () {
    const m = yield* marks(input)
    if (!m)
      return yield* Effect.fail<ImportError>({
        _tag: "NotMine",
        message: "There is no mychartctl manifest.json in it.",
      })
    const manifestPath = m.found.manifest.path
    const manifest = yield* readJson(Manifest, m.found.manifest)
    const host = yield* orUnreadable(
      m.host,
      manifestPath,
      "the portal's address is not a web address",
      "baseUrl"
    )
    const revision = yield* orUnreadable(
      runStarted(manifest),
      manifestPath,
      "the manifest names no run start",
      "runs"
    )
    const folder = new Folder(input, m.found.prefix)
    const visits = yield* folder.decodeItems("visits", Visit)
    const medications = yield* folder.decodeItems("medications", Medication)

    // ---- what the export does not say ----
    const suggested = exportName(
      visits,
      medications.map((x) => x.raw)
    )
    const name = strip(ctx.answers[ANSWERS.name] ?? "") || suggested
    const odsAnswer = ctx.answers[ANSWERS.ods]
    const ods = odsCode(odsAnswer)
    const zoneAnswer = strip(ctx.answers[ANSWERS.zone] ?? "")
    const zone = inLondon(host)
      ? LONDON
      : knownZone(zoneAnswer)
        ? zoneAnswer
        : undefined
    const missing: Question[] = []
    if (!name) missing.push(nameQuestion(suggested))
    if (!ods)
      missing.push(
        odsQuestion(
          strip(odsAnswer ?? "") ? "That is not an ODS code." : undefined
        )
      )
    if (!zone) missing.push(zoneQuestion())
    if (missing.length || !name || !ods || !zone)
      return yield* Effect.fail<ImportError>({
        _tag: "NeedsAnswers",
        questions: missing,
      })

    // ---- the files ----
    const namespace = `mychart/${host}`
    const source = sourceKey(namespace)
    const testResults = yield* folder.decodeItems("test-results", TestResult)
    const results = sortResults(testResults)
    const downloads = yield* loadDownloads(folder)
    const ex = {
      visits,
      details: yield* folder.decodeItems("visit-details", VisitDetails),
      visitNotes: yield* folder.decodeItems("visit-notes", VisitNotes),
      clinicalNotes: yield* folder.decodeItems("clinical-notes", Report),
      letterList: yield* folder.list("letters", Letters),
      letters: yield* folder.decodeItems("letters", Letter),
      results,
      reports: yield* folder.decodeItems("test-result-reports", Report),
      medications,
      allergies: yield* folder.decodeItems("allergies", Allergy),
      healthIssues: yield* folder.decodeItems("health-issues", HealthIssue),
      conversations: yield* folder.decodeItems("messages", Conversation),
      personalDetails: yield* folder.list("personal-details", PersonalDetails),
      personalContact: yield* folder.list("personal-contact", PersonalContact),
      healthUpkeep: yield* folder.list("health-upkeep", HealthUpkeep),
      file: (resource: string, id: string) => {
        const f = folder.file(resource, id)
        return f ? downloads.files.get(f.path) : undefined
      },
    }

    // ---- the hospital ----
    const org = visits.find((v) => v.value.Organization?.OrganizationIdentifier)
      ?.value.Organization
    const a = org?.DiscreteAddress
    const hospitalAddress: Address | null = a
      ? address(a.StreetAddress ?? [], a.City, a.Zip, a.Country)
      : null
    const hospital = organizationRecord(
      {
        ods,
        name,
        type: "hospital",
        address: hospitalAddress,
        identifiers: org?.OrganizationIdentifier
          ? [
              {
                value: org.OrganizationIdentifier,
                label: "Epic Care Everywhere id",
              },
            ]
          : [],
      },
      revision
    )

    // ---- the lab results, then the record ----
    const labs = readLabs(results, {
      namespace,
      source,
      zone,
      revision,
      hospital: name,
      metrics: ctx.metrics,
      units: ctx.units,
    })
    const clinical = readClinical(ex, {
      namespace,
      zone,
      revision,
      today: new Date(revision * 1000).toISOString().slice(0, 10),
      hospital: { name, ods },
      ownerContact: ctx.ownerContact,
      labReports: labs.written,
    })

    const left: LeftOut[] = [...labs.left, ...clinical.left, ...downloads.left]
    const contacts =
      (yield* folder.list("personal-relationships", Relationships))?.contacts ??
      []
    if (contacts.length)
      left.push({
        what: `${plural(contacts.length, "emergency contact")} (lists/personal-relationships.json)`,
        why: "the patient kind has no field for one",
      })
    for (const list of [
      "histories",
      "goals-patient",
      "goals-care-team",
      "care-journeys",
    ])
      if (folder.hasList(list))
        left.push({ what: `lists/${list}.json`, why: "no kind holds it" })
    if ((yield* folder.list("health-summary", HealthSummary))?.header?.height)
      left.push({
        what: "the health summary's height and weight",
        why: "measurements, not clinical records",
      })
    const immunizations = count(input, folder.prefix, "immunizations")
    if (immunizations)
      left.push({
        what: plural(immunizations, "immunization"),
        why: "this import does not read MyChart's immunizations yet",
      })

    const documents: PlannedDocument[] = []
    for (const d of [...labs.documents, ...clinical.documents])
      if (!documents.some((x) => x.digest === d.digest)) documents.push(d)
    return {
      importer: "mychart",
      name: input.name,
      source,
      records: [
        sourceRecord({ namespace, name, class: "service" }),
        hospital,
        ...clinical.organizations,
        ...labs.records,
        ...clinical.records,
      ],
      documents,
      left,
    } satisfies Plan
  })
}

export const mychart: Importer = {
  id: "mychart",
  name: "MyChart export",
  description:
    "The folder mychartctl writes for one Epic MyChart portal, or a zip of it: the hospital's lab results and your record there.",
  accepts: { kind: "folder" },

  detect: (input) =>
    marks(input).pipe(
      Effect.map((m): Match | undefined => {
        if (!m) return undefined
        const p = m.found.prefix
        const where = m.host ? ` at ${m.host}` : ""
        const items = input.files.some((f) => f.path.startsWith(`${p}items/`))
        if (!items)
          return {
            importer: "mychart",
            confidence: 0.5,
            why: `A mychartctl manifest for the MyChart portal${where}, with no items beside it`,
          }
        return {
          importer: "mychart",
          confidence: 1,
          why: `A mychartctl export of the MyChart portal${where}: ${plural(count(input, p, "test-results"), "test result")}, ${plural(count(input, p, "visits"), "visit")}, ${plural(count(input, p, "messages"), "conversation")}`,
        }
      })
    ),

  questions: (input) =>
    Effect.gen(function* () {
      const m = yield* marks(input)
      if (!m) return []
      const folder = new Folder(input, m.found.prefix)
      const visits = yield* folder
        .decodeItems("visits", Visit)
        .pipe(Effect.orElseSucceed(() => []))
      const medications = yield* folder
        .decodeItems("medications", Medication)
        .pipe(Effect.orElseSucceed(() => []))
      const questions = [
        nameQuestion(
          exportName(
            visits,
            medications.map((x) => x.raw)
          )
        ),
        odsQuestion(),
      ]
      if (!inLondon(m.host)) questions.push(zoneQuestion())
      return questions
    }),

  read,
}
