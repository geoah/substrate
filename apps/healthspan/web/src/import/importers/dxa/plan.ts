// The scans the printouts and the transcribed pages are of, the trend merge
// that gives each scan's whole-body figures, and the plan: the centre's
// source, one report per scan, its results
// (apps/healthspan/docs/importers/04-dxa.md, What it writes).

import type {
  Context,
  LeftOut,
  Plan,
  PlannedDocument,
  PlannedRecord,
  RecordKey,
} from "../types"
import {
  dateText,
  epoch,
  labRecords,
  plannedDocument,
  sameDecimal,
  sourceKey,
  sourceRecord,
  wallClock,
  zoned,
  type LabReportInput,
  type LabRow,
  type WallClock,
} from "../shared"
import {
  RATIOS,
  REGIONAL,
  REGIONAL_BMD,
  resultPart,
  T_SCORE,
  WHOLE_BODY,
  Z_SCORE,
  type Day,
  type Measure,
  type Region,
  type WholeBody,
} from "./measures"
import type { Printout, PrintoutKind, TrendRow } from "./printout"

/** A file a report can name, with what it is for its neutral name. */
export interface Source {
  path: string
  bytes: Uint8Array
  /** `body composition`, `bone density`, `trend`, `trend photo`. */
  kind: string
  /** The date of the scan it is of. */
  day: Day
  mediaType: string
  ext: string
}

export interface PrintoutRead {
  printout: Printout
  bytes: Uint8Array
}

export interface TranscriptionRead {
  path: string
  day: Day
  measured: WallClock
  analyzed: WallClock
  accession: string
  trend: TrendRow[]
  photo?: Source
}

export interface Answers {
  centre: string
  zone: string
  namespace: string
}

const PROVIDER = "DXA"
const CODING = "ge-lunar"
const METHOD = "DXA whole body"

/** The number the scanner gives each printout of a scan, the order a
 * scan's printouts are kept in: its report names its body composition
 * printout, else its lowest-numbered. */
const NUMBER: Record<PrintoutKind, number> = {
  "bone density": 1,
  "body composition": 2,
  trend: 3,
}

/** The order a scan's printouts join the trend merge in, the scripts'
 * order: of two figures of one rank, the later read is kept. */
const MERGE_ORDER: Record<PrintoutKind, number> = {
  trend: 0,
  "bone density": 1,
  "body composition": 2,
}

function printoutSource(p: PrintoutRead, day: Day): Source {
  return {
    path: p.printout.path,
    bytes: p.bytes,
    kind: p.printout.kind,
    day,
    mediaType: "application/pdf",
    ext: "pdf",
  }
}

/** A scan read from its printouts. */
interface Scan {
  day: Day
  /** One printout of each kind, by the scanner's number. */
  printouts: PrintoutRead[]
}

/** The printouts as scans: grouped by the date they were measured on; of
 * two scans of one date the first measured time's are kept, and of two
 * printouts of one kind of a scan, the one with more pages (a re-saved
 * printout adds the supplementary table), else the later in path order. */
function scansOf(reads: readonly PrintoutRead[], left: LeftOut[]): Scan[] {
  const byDay = new Map<Day, PrintoutRead[]>()
  for (const r of reads) {
    const day = dateText(r.printout.measured)
    byDay.set(day, [...(byDay.get(day) ?? []), r])
  }
  const scans: Scan[] = []
  for (const [day, all] of byDay) {
    const time = (r: PrintoutRead) => {
      const m = r.printout.measured
      return m.hour * 3600 + m.minute * 60 + m.second
    }
    const first = Math.min(...all.map(time))
    const kept = new Map<PrintoutKind, PrintoutRead>()
    for (const r of all) {
      if (time(r) !== first) {
        left.push({
          what: r.printout.path,
          why: `it is of another scan measured on ${day}: a scan's report is keyed by its date, and the first scan of the day is read`,
        })
        continue
      }
      const had = kept.get(r.printout.kind)
      if (had && had.printout.pages > r.printout.pages) {
        left.push({
          what: r.printout.path,
          why: `another copy of the scan's ${r.printout.kind} printout is read`,
        })
        continue
      }
      if (had)
        left.push({
          what: had.printout.path,
          why: `another copy of the scan's ${had.printout.kind} printout is read`,
        })
      kept.set(r.printout.kind, r)
    }
    scans.push({
      day,
      printouts: [...kept.values()].sort(
        (a, b) => NUMBER[a.printout.kind] - NUMBER[b.printout.kind]
      ),
    })
  }
  return scans.sort((a, b) => (a.day < b.day ? -1 : a.day > b.day ? 1 : 0))
}

/** A scan's regional bone density and its total's scores: each region from
 * the first table listing it, the table listing most regions first (the
 * supplementary results page lists every region). */
function boneDensity(scan: Scan): {
  regions: { region: Region; bmd: string; page: number }[]
  total?: string
  t?: { value: string; page: number }
  z?: { value: string; page: number }
} {
  const tables = scan.printouts
    .flatMap((p) => p.printout.bmdTables)
    .map((t, i) => ({ ...t, i }))
    .sort((a, b) => b.rows.length - a.rows.length || a.i - b.i)
  const regions: { region: Region; bmd: string; page: number }[] = []
  let total: string | undefined
  let t: { value: string; page: number } | undefined
  let z: { value: string; page: number } | undefined
  const seen = new Set<Region>()
  for (const table of tables)
    for (const row of table.rows) {
      if (row.region === "total") {
        total ??= row.bmd
        if (row.t !== undefined) t ??= { value: row.t, page: table.page }
        if (row.z !== undefined) z ??= { value: row.z, page: table.page }
        continue
      }
      if (seen.has(row.region)) continue
      seen.add(row.region)
      regions.push({ region: row.region, bmd: row.bmd, page: table.page })
    }
  return { regions, total, t, z }
}

/** A figure as the merge holds it: whose table it is from, and whether
 * that is the scan's own. */
interface Figure {
  own: boolean
  scan: Day
  value: string | null
}

/** The trend merge: each scan date's whole-body figures, a figure from the
 * scan's own printout or page first, then from the latest scan's trend
 * table; of two of one rank, the later read. */
class Merge {
  private figures = new Map<Day, Map<WholeBody, Figure>>()
  private sources = new Map<
    Day,
    { source: Source; printout: boolean; scan: Day; prefer: number }
  >()

  note(
    scan: Day,
    when: Day,
    values: Partial<Record<WholeBody, string | null | undefined>>,
    source?: { source: Source; printout: boolean; prefer: number }
  ): void {
    const own = when === scan
    let figures = this.figures.get(when)
    if (!figures) {
      figures = new Map<WholeBody, Figure>()
      this.figures.set(when, figures)
    }
    for (const [k, v] of Object.entries(values) as [
      WholeBody,
      string | null | undefined,
    ][]) {
      if (v === undefined) continue
      const had = figures.get(k)
      if (
        !had ||
        (own ? 1 : 0) > (had.own ? 1 : 0) ||
        (own === had.own && scan >= had.scan)
      )
        figures.set(k, { own, scan, value: v })
    }
    if (!source) return
    const had = this.sources.get(when)
    const better =
      !had ||
      (source.printout && !had.printout) ||
      (source.printout === had.printout &&
        (scan > had.scan || (scan === had.scan && source.prefer < had.prefer)))
    if (better) this.sources.set(when, { ...source, scan })
  }

  days(): Day[] {
    return [...this.figures.keys()].sort()
  }

  values(day: Day): Partial<Record<WholeBody, string | null>> {
    const out: Partial<Record<WholeBody, string | null>> = {}
    for (const [k, f] of this.figures.get(day) ?? []) out[k] = f.value
    return out
  }

  /** The document a scan known only from trend tables is attached to: a
   * printout listing it before a photo, the latest scan's first, its trend
   * printout before its bone density printout. */
  source(day: Day): Source | undefined {
    return this.sources.get(day)?.source
  }
}

function row(
  day: Day,
  m: Measure,
  value: string,
  opts: { channel?: Region; page?: number; regional?: boolean } = {}
): LabRow {
  const code = opts.regional ? `${m.code}/${opts.channel}` : m.code
  const name = opts.regional ? `${m.name} ${opts.channel}` : m.name
  const channel = opts.channel ?? m.channel
  return {
    key: `${day}/${resultPart(m.metric, channel)}`,
    analyte: { metric: m.metric },
    coding: { system: CODING, code, display: name },
    value,
    unit: m.unit,
    method: METHOD,
    channel: channel ?? null,
    upstreamId: `${day}/${code}`,
    page: opts.page ?? null,
  }
}

/** What reading the files gave, as a plan. */
export function dxaPlan(
  read: {
    name: string
    printouts: readonly PrintoutRead[]
    transcriptions: readonly TranscriptionRead[]
    left: LeftOut[]
  },
  answers: Answers,
  ctx: Context
): Plan {
  const left = [...read.left]
  const scans = scansOf(read.printouts, left)
  const merge = new Merge()

  // Transcribed pages first, then the printouts, so a printout's figure
  // wins over a transcription's of the same rank.
  for (const t of read.transcriptions)
    for (const r of t.trend)
      merge.note(
        t.day,
        r.date,
        r.values,
        t.photo && { source: t.photo, printout: false, prefer: 0 }
      )

  const own = new Map<Day, ReturnType<typeof boneDensity>>()
  for (const scan of scans) {
    const order = [...scan.printouts].sort(
      (a, b) => MERGE_ORDER[a.printout.kind] - MERGE_ORDER[b.printout.kind]
    )
    for (const p of order) {
      const source = printoutSource(p, scan.day)
      const prefer = p.printout.kind === "trend" ? 0 : 1
      for (const r of p.printout.trend)
        merge.note(scan.day, r.date, r.values, {
          source,
          printout: true,
          prefer,
        })
      for (const r of p.printout.bmdTrend)
        merge.note(
          scan.day,
          r.date,
          { bmd_total: r.bmd },
          { source, printout: true, prefer }
        )
    }
    const bone = boneDensity(scan)
    own.set(scan.day, bone)
    if (bone.total !== undefined)
      merge.note(scan.day, scan.day, { bmd_total: bone.total })
    const comp = scan.printouts.find((p) => p.printout.composition)?.printout
      .composition
    const total = comp?.regions.find((r) => r.region === "total")?.values
    if (total)
      merge.note(scan.day, scan.day, {
        body_mass: total.mass,
        body_fat: total.regionFat,
        tissue_fat: total.tissueFat,
        fat_mass: total.fat,
        lean_mass: total.lean,
        bone_mass: total.bmc,
      })
  }

  // Every record's revision: the newest scan any printout or page is of
  // (a later scan's trend table can restate an earlier scan's figures).
  const scanDays = [
    ...scans.map((s) => s.day),
    ...read.transcriptions.map((t) => t.day),
  ].sort()
  const newest = scanDays[scanDays.length - 1]
  const revision = newest ? epoch(`${newest}T00:00:00Z`)! : 0

  const source: RecordKey = sourceKey(answers.namespace)
  const report = (
    day: Day,
    r: Omit<LabReportInput, "namespace" | "key" | "source" | "lab" | "revision">
  ): LabReportInput => ({
    namespace: answers.namespace,
    key: day,
    source,
    lab: answers.centre,
    status: "final",
    orderedAt: null,
    revision,
    review: true,
    ...r,
  })
  const at = (w: WallClock) => zoned(w, answers.zone)!

  interface Planned {
    report: LabReportInput
    rows: LabRow[]
    source?: Source
    notes: string[]
  }
  const planned = new Map<Day, Planned>()

  for (const scan of scans) {
    const first = scan.printouts[0].printout
    const analyzed = scan.printouts.find((p) => p.printout.analyzed)?.printout
      .analyzed
    const accession = scan.printouts.find((p) => p.printout.accession)?.printout
      .accession
    const doc =
      scan.printouts.find((p) => p.printout.kind === "body composition") ??
      scan.printouts[0]
    const docSource = printoutSource(doc, scan.day)
    const rows: LabRow[] = []
    const bone = own.get(scan.day)!
    if (bone.t)
      rows.push(row(scan.day, T_SCORE, bone.t.value, { page: bone.t.page }))
    if (bone.z)
      rows.push(row(scan.day, Z_SCORE, bone.z.value, { page: bone.z.page }))
    const withRatios = scan.printouts.find((p) => p.printout.ratios)?.printout
    if (withRatios?.ratios)
      withRatios.ratios.values.forEach((v, i) =>
        rows.push(
          row(scan.day, RATIOS[i], v, { page: withRatios.ratios!.page })
        )
      )
    const comp = scan.printouts.find((p) => p.printout.composition)?.printout
      .composition
    for (const { region, values } of comp?.regions ?? []) {
      if (region === "total") continue
      for (const [field, m] of REGIONAL)
        rows.push(
          row(scan.day, m, values[field], {
            channel: region,
            page: comp!.page,
            regional: true,
          })
        )
    }
    for (const r of bone.regions)
      rows.push(
        row(scan.day, REGIONAL_BMD, r.bmd, {
          channel: r.region,
          page: r.page,
          regional: true,
        })
      )
    planned.set(scan.day, {
      report: report(scan.day, {
        accession: accession ?? null,
        at: at(first.measured),
        issuedAt: analyzed ? at(analyzed) : null,
        file: null,
      }),
      rows,
      source: docSource,
      notes: [
        "the printout header's height, weight and BMI are left out: the operator types them in",
        ...(scan.printouts.some((p) => p.printout.bmdTrend.length)
          ? [
              "the BMD change against the baseline is left out: it is derived from the BMD values read",
            ]
          : []),
      ],
    })
  }

  for (const t of read.transcriptions) {
    if (planned.has(t.day)) continue
    planned.set(t.day, {
      report: report(t.day, {
        accession: t.accession,
        at: at(t.measured),
        issuedAt: at(t.analyzed),
        file: null,
      }),
      rows: [],
      source: t.photo,
      notes: [
        "the printout header's height, weight and BMI are left out: the operator types them in",
        "only the trend page was transcribed: the scan's bone density and regional composition are not in it",
      ],
    })
  }

  for (const day of merge.days()) {
    if (planned.has(day)) continue
    const [y, m, d] = day.split("-").map(Number)
    planned.set(day, {
      report: report(day, {
        accession: day,
        at: at(wallClock(y, m, d, 12)!),
        issuedAt: null,
        file: null,
      }),
      rows: [],
      source: merge.source(day),
      notes: [],
    })
  }

  // Each scan's whole-body figures from the merge, after its own rows; the
  // trend table's android and gynoid %fat are the composition table's
  // regional tissue %fat of the same regions, kept once.
  for (const [day, p] of planned) {
    const values = merge.values(day)
    for (const [k, m] of WHOLE_BODY) {
      const v = values[k]
      if (v === null || v === undefined) continue
      const r = row(day, m, v)
      const twin = p.rows.find((x) => x.key === r.key)
      if (twin) {
        if (!sameDecimal(twin.value!, v))
          left.push({
            what: `${m.name}, ${p.report.accession ?? day}`,
            why: "the trend table's figure differs from the composition table's, which is kept",
          })
        continue
      }
      p.rows.push(r)
    }
  }

  const records: PlannedRecord[] = [
    sourceRecord({
      namespace: answers.namespace,
      name: answers.centre,
      class: "lab",
    }),
  ]
  const documents = new Map<string, PlannedDocument>()
  for (const day of [...planned.keys()].sort()) {
    const p = planned.get(day)!
    let doc: PlannedDocument | undefined
    if (p.source)
      doc = plannedDocument(p.source.bytes, {
        provider: PROVIDER,
        kind: p.source.kind,
        date: p.source.day,
        mediaType: p.source.mediaType,
        ext: p.source.ext,
      })
    else
      left.push({
        what: `the scan of ${day}`,
        why: "no printout or photo of it is among the files, so its report names no document",
      })
    const report = { ...p.report, file: doc?.digest ?? null }
    const out = labRecords(report, p.rows, ctx)
    left.push(...out.left)
    if (!out.records.length) continue
    records.push(...out.records)
    if (doc && !documents.has(doc.digest)) documents.set(doc.digest, doc)
    for (const why of p.notes) left.push({ what: `the scan of ${day}`, why })
  }

  return {
    importer: "dxa",
    name: read.name,
    source,
    records,
    documents: [...documents.values()],
    left,
  }
}
