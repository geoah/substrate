// The Randox Health importer (apps/healthspan/docs/importers/02-randox.md):
// the folder randoxctl writes from the Randox Health portal, or its zip.
// Each reported order of each visit is a lab report keyed by its sample id,
// with its health report PDF and its results, every test with the bands of
// every panel that prints it; each visit's Tanita body composition sheet is
// a report of its own, read from its PDF's content stream, its regions as
// the results' channel. A weighing both give is written once.

import { Effect } from "effect"
import type {
  Context,
  ImportError,
  Importer,
  Input,
  LeftOut,
  Match,
  Plan,
  PlannedDocument,
  PlannedRecord,
} from "../types"
import {
  dateText,
  epoch,
  flagOfRange,
  labRecords,
  orUnreadable,
  plannedDocument,
  readBytes,
  readJson,
  runStarted,
  slug,
  sourceKey,
  sourceRecord,
  strip,
  zoned,
  type LabReportInput,
  type LabRow,
} from "../shared"
import {
  decodeAt,
  layoutOf,
  manifestIsRandox,
  RandoxListing,
  RandoxManifest,
  RandoxReport,
  RandoxVisit,
  visitIsRandox,
  visitStem,
  type Layout,
} from "./files"
import { readSheet, type Sheet } from "./tanita"
import { SKIPPED_TESTS } from "./tests"
import {
  fastedFor,
  measureOf,
  portalTime,
  randoxBands,
  randoxRows,
  type Occurrence,
  type RandoxRow,
} from "./visit"

/** The provider's account: one Randox Health account per repository. */
export const NAMESPACE = "randox"
/** The portal's times are UK wall-clock with no offset. */
export const ZONE = "Europe/London"
/** The lab a visit's report names. */
export const LAB = "Randox Health"

const SOURCE = sourceKey(NAMESPACE)
const HEALTH_REPORT = "SCFIRSTPHP"
const SHEET = "SCFIRSTBCA"
const TRACKING = "SCTRACKRES"

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`

function detect(input: Input): Effect.Effect<Match | undefined> {
  return Effect.gen(function* () {
    const layout = layoutOf(input)
    if (!layout) return undefined
    const visits = plural(layout.visits.length, "visit")
    if (yield* manifestIsRandox(layout.manifest))
      return {
        importer: "randox" as const,
        confidence: 1,
        why: `A Randox Health export of ${visits}: randoxctl's manifest names the portal's visits`,
      }
    if (yield* visitIsRandox(layout.visits[0]))
      return {
        importer: "randox" as const,
        confidence: 0.6,
        why: `Randox Health visits in items/visit (${visits}), without randoxctl's manifest`,
      }
    return undefined
  })
}

/** A published, final PDF of an order in the visit's listing. */
function listed(
  listing: RandoxListing,
  orderId: string,
  kind: string
): string | undefined {
  return listing
    .filter((r) => r.published && !r.isPreliminaryReport && r.fileName)
    .map((r) => r.fileName!)
    .find((n) => n.includes(`_${orderId}_${kind}_`))
}

/** What one order adds to the plan. */
interface OrderPlan {
  records: PlannedRecord[]
  documents: PlannedDocument[]
  left: LeftOut[]
}

/** A body composition sheet's rows as lab rows of its own report. */
function sheetRows(sheet: Sheet, sample: string): LabRow[] {
  const model = sheet.model ? ` ${sheet.model}` : ""
  const system = sheet.model ? `tanita-${slug(sheet.model)}` : "tanita"
  return sheet.rows.map((r) => {
    const ranged = !!(r.low && r.high)
    const flag = ranged ? flagOfRange(r.value, r.low, r.high) : undefined
    return {
      key: `${sample}/bia/${r.measure}${r.channel ? `/${r.channel}` : ""}`,
      analyte: r.analyte,
      coding: { system, code: r.name, display: r.name },
      value: r.value,
      unit: r.unit,
      bands: ranged ? [{ low: r.low, high: r.high, lab: "desirable" }] : [],
      flag: flag ?? null,
      flagText: flag ? SHEET_FLAGS[flag] : null,
      method: `BIA, Tanita${model}`,
      channel: r.channel ?? null,
      upstreamId: `${sheet.id}/${sheet.minute}/${r.name}`,
      page: 1,
    }
  })
}

const SHEET_FLAGS: Readonly<Record<string, string>> = {
  N: "Within desirable range",
  H: "Above desirable range",
  L: "Below desirable range",
}

const where = (row: { coding: { display?: string } }, at: string) =>
  `${row.coding.display}, ${at}`

/** Of each measurement both the order and its visit's sheet give (the
 * weight, the BMI), one row is kept: the order's where it has bands or the
 * sheet's has none, else the sheet's. */
function deduplicate(
  rows: RandoxRow[],
  sheet: LabRow[],
  sample: string,
  sheetAccession: string
): { rows: RandoxRow[]; sheet: LabRow[]; left: LeftOut[] } {
  const ours = new Map<string, RandoxRow>()
  for (const r of rows) {
    const m = measureOf(r)
    if (m && !ours.has(m)) ours.set(m, r)
  }
  const dropOurs = new Set<string>()
  const dropSheet = new Set<string>()
  for (const r of sheet) {
    const m = measureOf(r)
    const mine = m === undefined ? undefined : ours.get(m)
    if (!m || !mine) continue
    if (mine.bands?.length || !r.bands?.length) dropSheet.add(m)
    else dropOurs.add(m)
  }
  const left: LeftOut[] = []
  const keptRows = rows.filter((r) => {
    const m = measureOf(r)
    if (!m || !dropOurs.has(m)) return true
    left.push({
      what: where(r, sample),
      why: "the body composition sheet gives the same measurement with its desirable range; the sheet's is kept",
    })
    return false
  })
  const keptSheet = sheet.filter((r) => {
    const m = measureOf(r)
    if (!m || !dropSheet.has(m)) return true
    left.push({
      what: where(r, sheetAccession),
      why: "the visit's results give the same measurement; theirs is kept",
    })
    return false
  })
  return { rows: keptRows, sheet: keptSheet, left }
}

/** One visit file's orders as reports and results. */
function readVisit(
  layout: Layout,
  index: number,
  fallback: number | undefined,
  taken: Set<string>,
  ctx: Context
): Effect.Effect<OrderPlan, ImportError> {
  return Effect.gen(function* () {
    const file = layout.visits[index]
    const path = file.path
    const visit = yield* readJson(RandoxVisit, file)
    const listingFile = layout.file(
      `items/visit-reports/${visitStem(file)}.json`
    )
    const listing = listingFile
      ? yield* readJson(RandoxListing, listingFile)
      : []
    const out: OrderPlan = { records: [], documents: [], left: [] }
    const tracking = listing.filter(
      (r) =>
        r.published &&
        !r.isPreliminaryReport &&
        r.fileName?.includes(`_${TRACKING}_`)
    )
    if (tracking.length)
      out.left.push({
        what: `${listingFile!.path}: the biomarker tracking report`,
        why: "it repeats the visits' results as charts; it is not read",
      })
    const orders = visit.nexusOrders ?? []
    for (let i = 0; i < orders.length; i++) {
      const order = orders[i]
      const at = `nexusOrders[${i}]`
      const sampleId = strip(order.nexusExternalNumber ?? "")
      if (!order.isReported || !order.phpReportData) {
        out.left.push({
          what: `${path}: order ${sampleId || String(order.id)}`,
          why: "it is not reported yet",
        })
        continue
      }
      const sample = yield* orUnreadable(
        sampleId || undefined,
        path,
        "a reported order has no sample id",
        `${at}.nexusExternalNumber`
      )
      if (taken.has(sample)) {
        out.left.push({
          what: `${path}: order ${sample}`,
          why: "an earlier visit has an order with the same sample id; the first is kept",
        })
        continue
      }
      taken.add(sample)
      const php = yield* decodeAt(
        RandoxReport,
        order.phpReportData,
        path,
        `${at}.phpReportData`
      )
      const summary = php.summaryData
      const field = `${at}.phpReportData.summaryData`
      const collected = yield* orUnreadable(
        portalTime(summary.sampleCollectionDate),
        path,
        "the sample's collection time is not an ISO 8601 time",
        `${field}.sampleCollectionDate`
      )
      let fasted = false
      if (summary.fastingDate) {
        const fast = yield* orUnreadable(
          portalTime(summary.fastingDate),
          path,
          "the fast's start is not an ISO 8601 time",
          `${field}.fastingDate`
        )
        fasted = fastedFor(collected, fast)
      }
      const issued = summary.resultDate
        ? yield* orUnreadable(
            portalTime(summary.resultDate),
            path,
            "the result time is not an ISO 8601 time",
            `${field}.resultDate`
          )
        : undefined
      const revision = yield* orUnreadable(
        visit.lastUpdated ? epoch(visit.lastUpdated, ZONE) : fallback,
        path,
        visit.lastUpdated
          ? "the visit's last update is not an ISO 8601 time"
          : "the visit has no last update and the folder's manifest names no run",
        "lastUpdated"
      )

      // Each test once, in the panel that first prints it, with every
      // panel's printing of it.
      const tests = new Map<string, Occurrence[]>()
      for (const panel of php.panels ?? [])
        for (const t of panel.tests ?? []) {
          const skipped = SKIPPED_TESTS[t.name]
          if (skipped) {
            out.left.push({ what: `${t.name}, ${sample}`, why: skipped })
            continue
          }
          if (
            t.secondaryReferenceRange !== null &&
            t.secondaryReferenceRange !== undefined
          )
            out.left.push({
              what: `${t.name}, ${sample}`,
              why: `the ${panel.name} panel's second reference range is not read`,
            })
          const shown = t.result?.displayValue
          const list = tests.get(t.name) ?? []
          list.push({
            panel: panel.name,
            result: strip(
              shown === null || shown === undefined ? "" : String(shown)
            ),
            unit: t.referenceRange?.unit ?? null,
            bands: randoxBands(t.referenceRange),
          })
          tests.set(t.name, list)
        }
      let rows = randoxRows(tests, {
        sample,
        fasted,
        programme: order.nexusOrderProgrammeName,
      })

      const orderId = String(order.id)
      const atText = yield* orUnreadable(
        zoned(collected, ZONE),
        path,
        "the collection time has no London offset",
        `${field}.sampleCollectionDate`
      )
      const report: LabReportInput = {
        namespace: NAMESPACE,
        key: sample,
        source: SOURCE,
        lab: LAB,
        accession: sample,
        at: atText,
        orderedAt: null,
        issuedAt: issued ? (zoned(issued, ZONE) ?? null) : null,
        status: "final",
        revision,
        review: true,
      }
      let pdf: PlannedDocument | undefined
      const pdfName = listed(listing, orderId, HEALTH_REPORT)
      if (pdfName) {
        const f = layout.file(`files/report/${pdfName}.pdf`)
        if (f) {
          pdf = plannedDocument(yield* readBytes(f), {
            provider: "Randox",
            kind: "health report",
            date: dateText(collected),
            mediaType: "application/pdf",
            ext: "pdf",
          })
          report.file = pdf.digest
        } else
          out.left.push({
            what: `the health report of ${sample}`,
            why: "the visit's listing names it, and it is not in the folder",
          })
      }

      // The visit's body composition sheet, a report of its own.
      let sheet:
        | { report: LabReportInput; rows: LabRow[]; doc: PlannedDocument }
        | undefined
      const sheetName = listed(listing, orderId, SHEET)
      if (sheetName) {
        const f = layout.file(`files/report/${sheetName}.pdf`)
        if (f) {
          const bytes = yield* readBytes(f)
          const read = yield* readSheet(bytes, f.path)
          const doc = plannedDocument(bytes, {
            provider: "Randox",
            kind: "body composition",
            date: dateText(read.printed),
            mediaType: "application/pdf",
            ext: "pdf",
          })
          sheet = {
            report: {
              namespace: NAMESPACE,
              key: `${sample}/bia`,
              source: SOURCE,
              lab: `${LAB} (Tanita${read.model ? ` ${read.model}` : ""})`,
              accession: `${read.id} ${read.minute.replace("T", " ")}`,
              at: read.at,
              orderedAt: null,
              issuedAt: read.at,
              status: "final",
              file: doc.digest,
              revision,
              review: true,
            },
            rows: sheetRows(read, sample),
            doc,
          }
        } else
          out.left.push({
            what: `the body composition sheet of ${sample}`,
            why: "the visit's listing names it, and it is not in the folder; its values are not read",
          })
      }
      if (sheet) {
        const d = deduplicate(rows, sheet.rows, sample, sheet.report.accession!)
        rows = d.rows
        sheet.rows = d.sheet
        out.left.push(...d.left)
      }

      const lab = labRecords(report, rows, ctx)
      out.records.push(...lab.records)
      out.left.push(...lab.left)
      if (pdf && lab.records.length) out.documents.push(pdf)
      if (sheet) {
        const bia = labRecords(sheet.report, sheet.rows, ctx)
        out.records.push(...bia.records)
        out.left.push(...bia.left)
        if (bia.records.length) out.documents.push(sheet.doc)
      }
    }
    return out
  })
}

function read(input: Input, ctx: Context): Effect.Effect<Plan, ImportError> {
  return Effect.gen(function* () {
    const layout = layoutOf(input)
    if (!layout)
      return yield* Effect.fail<ImportError>({
        _tag: "NotMine",
        message:
          "There is no items/visit folder of a Randox Health export here.",
      })
    const manifest = layout.manifest
      ? yield* readJson(RandoxManifest, layout.manifest)
      : undefined
    const fallback = manifest ? runStarted(manifest) : undefined
    const plan: Plan = {
      importer: "randox",
      name: input.name,
      source: SOURCE,
      records: [
        sourceRecord({
          namespace: NAMESPACE,
          name: "Randox",
          class: "lab",
          manufacturer: "Randox Laboratories",
        }),
      ],
      documents: [],
      left: [],
    }
    const taken = new Set<string>()
    const digests = new Set<string>()
    for (let i = 0; i < layout.visits.length; i++) {
      const v = yield* readVisit(layout, i, fallback, taken, ctx)
      plan.records.push(...v.records)
      plan.left.push(...v.left)
      for (const d of v.documents)
        if (!digests.has(d.digest)) {
          digests.add(d.digest)
          plan.documents.push(d)
        }
    }
    return plan
  })
}

export const randox: Importer = {
  id: "randox",
  name: "Randox Health export",
  description:
    "The folder randoxctl writes from the Randox Health portal, or its zip: every visit's results with their bands, its health report and its Tanita body composition sheet.",
  accepts: { kind: "folder" },
  detect,
  questions: () => Effect.succeed([]),
  read,
}
