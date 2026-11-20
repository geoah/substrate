// One GE Lunar enCORE printout read from pdf.js's positioned text
// (apps/healthspan/docs/importers/04-dxa.md, How it reads): its
// header's scan time, analysis time and scanner file name, and every table
// it prints, each found by its header and read by column.

import type { LeftOut } from "../types"
import {
  commaDecimal,
  dateText,
  parseClock,
  type PdfPage,
  type WallClock,
} from "../shared"
import {
  cellOf,
  fieldAfter,
  findTables,
  leftOf,
  pageRows,
  type ColumnSpec,
  type Row,
  type Table,
} from "./layout"
import {
  bmdRegion,
  COMPOSITION_ROWS,
  type Day,
  type Region,
  type WholeBody,
} from "./measures"

/** What a printout is, by the tables it holds: the scanner prints a scan
 * as a bone density printout, a body composition printout and a trend
 * printout. */
export type PrintoutKind = "bone density" | "body composition" | "trend"

/** The bone density table's row of a region. */
export interface BmdRow {
  region: Region
  bmd: string
  /** The T-score and Z-score, where the row prints them. */
  t?: string
  z?: string
}

/** The composition table's values of one region. */
export interface CompositionValues {
  tissueFat: string
  regionFat: string
  lean: string
  fat: string
  bmc: string
  /** The total mass, kg, printed for the whole body alone. */
  mass?: string
}

/** A row of a trend table: a scan's whole-body figures, a null where the
 * table prints `-`. */
export interface TrendRow {
  date: Day
  values: Partial<Record<WholeBody, string | null>>
}

export interface Printout {
  path: string
  pages: number
  kind: PrintoutKind
  /** When the scan was measured, on the centre's clock. */
  measured: WallClock
  analyzed?: WallClock
  /** The scanner's file name for the scan. */
  accession?: string
  bmdTables: { page: number; rows: BmdRow[] }[]
  bmdTrend: { date: Day; bmd: string }[]
  composition?: {
    page: number
    regions: { region: Region; values: CompositionValues }[]
  }
  ratios?: { page: number; values: [string, string, string] }
  /** The composition and the fat distribution trend tables' rows. */
  trend: TrendRow[]
  left: LeftOut[]
}

// ---- cells ----

const DATE = /^(\d{1,2})\/(\d{1,2})\/(\d{4})$/
const DECIMAL = /^-?\d+(,\d+)?$/
const GRAMS = /^\d+(\.\d{3})*$/
const CLOCK = /^(\d{1,2}\/\d{1,2}\/\d{4}) (\d{1,2}:\d\d:\d\d) ?(πμ|μμ)/

/** A printed `D/M/YYYY` date as `YYYY-MM-DD`, or undefined. */
export function printedDate(text: string | undefined): Day | undefined {
  const m = text ? DATE.exec(text) : null
  const w = m && parseClock(`${m[1]}/${m[2]}/${m[3]}`, "%d/%m/%Y")
  return w ? dateText(w) : undefined
}

/** A printed date and 12-hour time (`8/4/2025 11:16:26 πμ`, πμ before
 * noon, μμ after) as a wall clock, or undefined. */
export function printedClock(
  date: string,
  time: string,
  half: string
): WallClock | undefined {
  const p = half === "μμ" ? "pm" : half === "πμ" ? "am" : undefined
  if (!p) return undefined
  return parseClock(`${date} ${time} ${p}`, "%d/%m/%Y %I:%M:%S %p")
}

/** A printed number with `,` for its decimal point, as decimal digits. */
export function decimal(text: string | undefined): string | undefined {
  return text !== undefined && DECIMAL.test(text)
    ? commaDecimal(text)
    : undefined
}

/** A printed mass in grams, `.` between its thousands, as digits. */
export function grams(text: string | undefined): string | undefined {
  return text !== undefined && GRAMS.test(text) ? commaDecimal(text) : undefined
}

/** A header field's date and time, from the text after its label. */
function stamp(text: string | undefined): WallClock | undefined {
  const m = text ? CLOCK.exec(text) : null
  return m ? printedClock(m[1], m[2], m[3]) : undefined
}

// ---- the tables ----

type BmdKey = "region" | "bmd" | "t" | "z"
type BmdTrendKey = "date" | "bmd"
type CompositionKey =
  "tissueFat" | "regionFat" | "tissue" | "fat" | "lean" | "bmc" | "mass"
type TrendKey =
  | "date"
  | "age"
  | "tissueFat"
  | "centile"
  | "mass"
  | "regionFat"
  | "tissue"
  | "fat"
  | "lean"
  | "bmc"
  | "fatFree"
type DistributionKey = "date" | "age" | "android" | "gynoid" | "ratio" | "total"
type RatioKey = "trunkTotal" | "legsTotal" | "limbsTrunk"

const col = <K extends string>(
  key: K,
  labels: string | readonly string[],
  optional?: boolean
): ColumnSpec<K> => ({
  key,
  labels: typeof labels === "string" ? [labels] : labels,
  optional,
})

/** The bone density table: region, BMD (g/cm²), young adult (%), T-score,
 * age matched (%), Z-score, and on the supplementary page BMC and area. */
const BMD_TABLE = [
  col<BmdKey>("region", "Περιοχή"),
  col<BmdKey>("bmd", "(g/cm²)"),
  col<BmdKey>("t", "T-Score", true),
  col<BmdKey>("z", "Z-Score", true),
]

/** The bone density trend: measured date, age, BMD, its change against
 * the baseline. */
const BMD_TREND = [
  col<BmdTrendKey>("date", "Μετρηθείσα"),
  col<BmdTrendKey>("bmd", "BMD"),
]

/** The body composition table: region, tissue (%fat), region (%fat),
 * tissue, fat, lean (tissue without fat) and BMC in grams, total mass in
 * kg. */
const COMPOSITION = [
  col<CompositionKey>("tissueFat", "Ιστός"),
  col<CompositionKey>("regionFat", "Περιοχή"),
  col<CompositionKey>("tissue", "Ιστός"),
  col<CompositionKey>("fat", "Λίπος"),
  col<CompositionKey>("lean", "Ιστός χωρίς"),
  col<CompositionKey>("bmc", "Οστική μάζα"),
  col<CompositionKey>("mass", "Συνολική"),
]

/** The composition trend: measured date, age, tissue (%fat), centile,
 * total mass (kg), region (%fat), tissue, fat, lean, BMC and fat free in
 * grams. */
const COMPOSITION_TREND = [
  col<TrendKey>("date", "Ημερομηνία"),
  col<TrendKey>("age", "Ηλικία"),
  col<TrendKey>("tissueFat", "Ιστός"),
  col<TrendKey>("centile", "Εκατοστό"),
  col<TrendKey>("mass", "Συν. Μάζα"),
  col<TrendKey>("regionFat", "Περιοχή"),
  col<TrendKey>("tissue", "Ιστός"),
  col<TrendKey>("fat", "Παχύς"),
  col<TrendKey>("lean", ["Αδύνατο", "Αδύνατος"]),
  col<TrendKey>("bmc", "BMC"),
  col<TrendKey>("fatFree", "Fat Free"),
]

/** The fat distribution trend: measured date, age, android, gynoid (%fat),
 * their ratio, total body (%fat). */
const DISTRIBUTION_TREND = [
  col<DistributionKey>("date", "Ημερομηνία"),
  col<DistributionKey>("age", "Ηλικία"),
  col<DistributionKey>("android", "Android"),
  col<DistributionKey>("gynoid", "Gynoid"),
  col<DistributionKey>("ratio", "A/G"),
  col<DistributionKey>("total", "Total Body"),
]

/** The fat mass ratios: trunk/total, legs/total, (arms+legs)/trunk. */
const RATIOS = [
  col<RatioKey>("trunkTotal", "Κορμός/"),
  col<RatioKey>("legsTotal", "Κνήμες/"),
  col<RatioKey>("limbsTrunk", "(Βραχίονες+Κνήμες)/"),
]

const dated =
  <K extends string>(key: K) =>
  (row: Row, t: Table<K>) =>
    DATE.test(cellOf(t, row, key) ?? "")

const where = (path: string, page: number, what: string) =>
  `${path}, page ${page}, ${what}`

/** A row's cells, each read by its column's reading: the values, or the
 * first column whose cell is not what the reading takes. */
function values<K extends string>(
  table: Table<K>,
  row: Row,
  read: readonly (readonly [K, (s: string | undefined) => string | undefined])[]
): { values: Partial<Record<K, string>>; bad?: K } {
  const out: Partial<Record<K, string>> = {}
  for (const [key, as] of read) {
    const v = as(cellOf(table, row, key))
    if (v === undefined) return { values: out, bad: key }
    out[key] = v
  }
  return { values: out }
}

/** A reading that also takes a `-`, as the empty string: none. */
const orDash =
  (as: (s: string | undefined) => string | undefined) =>
  (s: string | undefined): string | undefined =>
    s === "-" ? "" : as(s)

/** A centile: digits, or a `-` as the empty string (none). */
const centile = (s: string | undefined) =>
  s === "-" ? "" : s !== undefined && /^\d+$/.test(s) ? s : undefined

const NAMES: Record<string, string> = {
  bmd: "BMD",
  t: "T-score",
  z: "Z-score",
  tissueFat: "tissue %fat",
  regionFat: "region %fat",
  tissue: "tissue",
  fat: "fat",
  lean: "lean",
  bmc: "BMC",
  mass: "total mass",
  fatFree: "fat free",
  centile: "centile",
  android: "android %fat",
  gynoid: "gynoid %fat",
  ratio: "A/G ratio",
}

/** A printout's pages read: its header and its tables, or why it is not a
 * GE Lunar printout this importer reads. */
export function readPrintout(
  path: string,
  pages: readonly PdfPage[]
): { printout: Printout } | { why: string } {
  const left: LeftOut[] = []
  const pageRowsOf = pages.map((p) => pageRows(p))
  const all = pageRowsOf.flat()
  const measuredText = fieldAfter(all, "Μετρηθείσα:")
  const measured = stamp(measuredText)
  if (!measured)
    return {
      why: measuredText
        ? "its measured time is not a date and a 12-hour time"
        : "it is not a GE Lunar printout: it names no measured time",
    }
  const analyzed = stamp(fieldAfter(all, "Analyzed:"))
  const accession =
    fieldAfter(all, "Όνομα αρχείου:")?.split(" ")[0] || undefined

  const out: Printout = {
    path,
    pages: pages.length,
    kind: "bone density",
    measured,
    analyzed,
    accession,
    bmdTables: [],
    bmdTrend: [],
    trend: [],
    left,
  }
  const holds = { bmd: false, composition: false, trend: false }

  pageRowsOf.forEach((rows, index) => {
    const page = index + 1

    for (const t of findTables(rows, page, BMD_TABLE, (row, t) => {
      return (
        !!leftOf(row, t.columns.bmd) &&
        decimal(cellOf(t, row, "bmd")) !== undefined
      )
    })) {
      if (!t.rows.length) continue
      holds.bmd = true
      const read: BmdRow[] = []
      for (const row of t.rows) {
        const label = leftOf(row, t.columns.bmd)
        const region = bmdRegion(label)
        if (!region) {
          left.push({
            what: where(path, page, `the bone density table's row ${label}`),
            why: "its region is not one this importer knows",
          })
          continue
        }
        const r: BmdRow = { region, bmd: decimal(cellOf(t, row, "bmd"))! }
        const tScore = decimal(cellOf(t, row, "t"))
        const zScore = decimal(cellOf(t, row, "z"))
        if (tScore !== undefined) r.t = tScore
        if (zScore !== undefined) r.z = zScore
        read.push(r)
      }
      out.bmdTables.push({ page, rows: read })
    }

    for (const t of findTables(rows, page, BMD_TREND, dated("date"))) {
      for (const row of t.rows) {
        const date = printedDate(cellOf(t, row, "date"))!
        const bmd = decimal(cellOf(t, row, "bmd"))
        if (!bmd) {
          left.push({
            what: where(path, page, `the bone density trend's row of ${date}`),
            why: "its BMD is not a number",
          })
          continue
        }
        holds.bmd = true
        out.bmdTrend.push({ date, bmd })
      }
    }

    for (const t of findTables(
      rows,
      page,
      COMPOSITION,
      (row, t) =>
        !!leftOf(row, t.columns.tissueFat) &&
        decimal(cellOf(t, row, "tissueFat")) !== undefined
    )) {
      if (!t.rows.length) continue
      holds.composition = true
      const what = where(path, page, "the composition table")
      if (t.rows.length !== COMPOSITION_ROWS.length) {
        left.push({
          what,
          why: `it has ${t.rows.length} rows, not the ${COMPOSITION_ROWS.length} regions it prints, so its regional values are left out`,
        })
        continue
      }
      const label = (i: number) => leftOf(t.rows[i], t.columns.tissueFat)
      if (label(11) !== "Android" || label(12) !== "Gynoid") {
        left.push({
          what,
          why: "its rows are not in the order it prints them (android and gynoid twelfth and thirteenth), so its regional values are left out",
        })
        continue
      }
      const regions: { region: Region; values: CompositionValues }[] = []
      let bad: string | undefined
      t.rows.forEach((row, i) => {
        const region = COMPOSITION_ROWS[i]
        const v = values(t, row, [
          ["tissueFat", decimal],
          ["regionFat", decimal],
          ["lean", grams],
          ["fat", grams],
          ["bmc", grams],
          ["mass", orDash(decimal)],
        ])
        if (v.bad) {
          bad ??= `the ${NAMES[v.bad]} of ${region} is not a number`
          return
        }
        const c: CompositionValues = {
          tissueFat: v.values.tissueFat!,
          regionFat: v.values.regionFat!,
          lean: v.values.lean!,
          fat: v.values.fat!,
          bmc: v.values.bmc!,
        }
        if (v.values.mass) c.mass = v.values.mass
        regions.push({ region, values: c })
      })
      if (bad) {
        left.push({ what, why: `${bad}, so its regional values are left out` })
        continue
      }
      out.composition = { page, regions }
    }

    for (const t of findTables(rows, page, RATIOS, (row, t) =>
      (["trunkTotal", "legsTotal", "limbsTrunk"] as const).every(
        (k) => decimal(cellOf(t, row, k)) !== undefined
      )
    )) {
      const row = t.rows[0]
      if (!row) continue
      holds.composition = true
      out.ratios = {
        page,
        values: [
          decimal(cellOf(t, row, "trunkTotal"))!,
          decimal(cellOf(t, row, "legsTotal"))!,
          decimal(cellOf(t, row, "limbsTrunk"))!,
        ],
      }
    }

    for (const t of findTables(rows, page, COMPOSITION_TREND, dated("date"))) {
      for (const row of t.rows) {
        const date = printedDate(cellOf(t, row, "date"))
        const v = values(t, row, [
          ["mass", decimal],
          ["regionFat", decimal],
          ["tissueFat", decimal],
          ["centile", centile],
          ["fat", grams],
          ["bmc", grams],
          ["lean", grams],
          ["fatFree", grams],
        ])
        if (!date || v.bad) {
          left.push({
            what: where(
              path,
              page,
              `the composition trend's row of ${cellOf(t, row, "date")}`
            ),
            why: date
              ? `its ${NAMES[v.bad!]} is not a number`
              : "its date is not a date",
          })
          continue
        }
        holds.trend = true
        out.trend.push({
          date,
          values: {
            body_mass: v.values.mass,
            body_fat: v.values.regionFat,
            fat_centile: v.values.centile || null,
            tissue_fat: v.values.tissueFat,
            fat_mass: v.values.fat,
            bone_mass: v.values.bmc,
            lean_mass: v.values.lean,
            fat_free_mass: v.values.fatFree,
          },
        })
      }
    }

    for (const t of findTables(rows, page, DISTRIBUTION_TREND, dated("date"))) {
      for (const row of t.rows) {
        const date = printedDate(cellOf(t, row, "date"))
        const v = values(t, row, [
          ["android", decimal],
          ["gynoid", decimal],
          ["ratio", decimal],
        ])
        if (!date || v.bad) {
          left.push({
            what: where(
              path,
              page,
              `the fat distribution trend's row of ${cellOf(t, row, "date")}`
            ),
            why: date
              ? `its ${NAMES[v.bad!]} is not a number`
              : "its date is not a date",
          })
          continue
        }
        holds.trend = true
        out.trend.push({
          date,
          values: {
            android_fat: v.values.android,
            gynoid_fat: v.values.gynoid,
            ag_ratio: v.values.ratio,
          },
        })
      }
    }
  })

  if (holds.composition) out.kind = "body composition"
  else if (holds.trend) out.kind = "trend"
  else if (holds.bmd) out.kind = "bone density"
  else
    return {
      why: "it holds none of the tables this importer reads (bone density, body composition, trend)",
    }
  return { printout: out }
}
