// One Numan test as a lab report and its results (apps/healthspan/docs/
// importers/01-numan.md, How it reads): the lab's own rows from the test's
// `results_raw`, in Randox's shape or Hurdle's, each with the lab's range
// and Numan's clinical range, and the test's own JSON as the report's file.

import { Effect, Schema } from "effect"
import {
  analyteEntry,
  dateIn,
  decodeValue,
  epoch,
  isNumber,
  jsonDocument,
  labRecords,
  parseClock,
  parseIso,
  pythonFloat,
  sourceKey,
  splitComparator,
  strip,
  unreadable,
  zoned,
  type AnalyteRef,
  type Band,
  type Comparator,
  type LabReportInput,
  type LabRow,
  type MetricContext,
} from "../shared"
import type {
  ImportError,
  LeftOut,
  PlannedDocument,
  PlannedRecord,
} from "../types"
import type { ChosenTest } from "./exports"
import {
  HurdleRows,
  RandoxOrder,
  RandoxRows,
  type HurdleRow,
  type Measurement,
  type RandoxRow,
} from "./schema"
import {
  CLINICAL_CODES,
  HOME_KIT,
  HURDLE_BIOMARKERS,
  HURDLE_FLAGS,
  HURDLE_SPECIMEN,
  METHODS,
  ORDER_PRESSURE_METHOD,
  ORDER_PRESSURES,
  RANDOX_ANALYTES,
} from "./tables"

/** Numan's namespace, and its source's. */
export const NAMESPACE = "numan"
/** Numan writes its sample times as London wall-clock time. */
export const ZONE = "Europe/London"
export const SOURCE = sourceKey(NAMESPACE)

const own = <V>(table: Readonly<Record<string, V>>, key: string) =>
  Object.hasOwn(table, key) ? table[key] : undefined

/** An analyte by its lab's code: the catalogue entry the table names, else
 * the entry its name or code finds. */
function analyteOf(
  table: Readonly<Record<string, string>>,
  code: string,
  name: string
): AnalyteRef {
  const entry =
    own(table, code) ??
    analyteEntry({ name })?.code ??
    analyteEntry({ name: code })?.code
  return entry ? { entry } : { name }
}

/** A wall-clock time Numan writes (`2026-01-02T08:30:00`, anything after
 * the seconds ignored) as an instant in London. */
export function londonTime(text: string): string | undefined {
  const wall = parseClock(text.slice(0, 19), "%Y-%m-%dT%H:%M:%S")
  return wall && zoned(wall, ZONE)
}

/** Numan's clinical range of a measurement, as a band in the measurement's
 * own unit (after the value's first space), or none where its range names
 * no bound. An upper bound of 10000 or more is an open end. Its text is
 * `normal: Numan clinical range`, and it sorts after the lab's. */
export function clinicalBand(o: Measurement): Band | undefined {
  const s = strip(o.normal_value ?? "")
  const v = o.value ?? ""
  const unit = v.includes(" ") ? v.slice(v.indexOf(" ") + 1) : null
  let low: string | null = null
  let high: string | null = null
  const range = /^([0-9.]+)\s*-\s*([0-9.]+)$/.exec(s)
  if (range) {
    low = range[1]
    high = Number(range[2]) >= 10000 ? null : range[2]
  } else if (/^<\s*([0-9.]+)$/.test(s)) high = strip(s.replace(/^<+/, ""))
  else if (/^>\s*([0-9.]+)$/.test(s)) low = strip(s.replace(/^>+/, ""))
  if (low === null && high === null) return undefined
  // Numan gives it no label: its word is `normal` (it is `normal_value`),
  // which it keeps with its detail where its bounds cannot be written (a
  // value kept as received)
  return { low, high, word: "normal", detail: "Numan clinical range", unit }
}

/** Hurdle's bands from its rule (`{"NORMAL": ["<3.0"], "HIGH":
 * [">=3.0"]}`), one per interpretation in the rule's order: a `>` or `>=`
 * sets its low bound, a `<` or `<=` its high one. Undefined for a rule in
 * any other shape. */
export function hurdleBands(rule: unknown): Band[] | undefined {
  if (rule === null || rule === undefined) return []
  if (typeof rule !== "object" || Array.isArray(rule)) return undefined
  const out: Band[] = []
  for (const [label, exprs] of Object.entries(rule)) {
    if (!Array.isArray(exprs)) return undefined
    let low: string | null = null
    let high: string | null = null
    for (const e of exprs as unknown[]) {
      if (typeof e !== "string") return undefined
      const { comparator, value } = splitComparator(e)
      if (comparator === ">" || comparator === ">=") low = value
      else if (comparator === "<" || comparator === "<=") high = value
    }
    out.push({ low, high, lab: label.toLowerCase() })
  }
  return out
}

/** A value as Python's `repr` writes it, which orders how a sample was
 * taken where a test names several ways. */
function pyRepr(v: string | null | undefined): string {
  if (v === null || v === undefined) return "None"
  const q = v.includes("'") && !v.includes('"') ? '"' : "'"
  const body = v
    .replace(/\\/g, "\\\\")
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r")
    .replace(/\t/g, "\\t")
  return q + (q === "'" ? body.replace(/'/g, "\\'") : body) + q
}

/** How a test's sample was taken, in words: of the ways its results name
 * (`collection_detail` and `collection_method`), the first in the order
 * the scripts sorted them, read through the table; a home kit otherwise. */
export function methodOf(
  results: readonly {
    collection_detail?: string | null
    collection_method?: string | null
  }[]
): string {
  const ways = results.map((r) => ({
    detail: r.collection_detail ?? null,
    method: r.collection_method ?? null,
    key: `(${pyRepr(r.collection_detail)}, ${pyRepr(r.collection_method)})`,
  }))
  if (!ways.length) return HOME_KIT
  const first = ways.reduce((a, b) => (b.key < a.key ? b : a))
  return (
    METHODS.find((m) => m.detail === first.detail && m.method === first.method)
      ?.words ?? HOME_KIT
  )
}

/** A result's text as a value: digits (with a comparator) where they are a
 * number, else words. */
function valueOf(text: string | null | undefined): {
  value: string | null
  comparator: Comparator | null
  text: string | null
} {
  const { comparator, value } = splitComparator(text ?? "")
  return isNumber(value)
    ? { value, comparator: comparator ?? null, text: null }
    : { value: null, comparator: null, text: value || null }
}

const capitalize = (s: string) =>
  s ? s.charAt(0).toUpperCase() + s.slice(1).toLowerCase() : s

/** Where a field of a test's `results_raw` is, for an error. */
function within(base: string, field: string | undefined): string {
  if (!field) return base
  return field.startsWith("[") ? `${base}${field}` : `${base}.${field}`
}

/** `results_raw` decoded by `schema`, an error naming the field inside it. */
function decodeRaw<A>(
  schema: Schema.Decoder<A>,
  value: unknown,
  path: string,
  base: string
): Effect.Effect<A, ImportError> {
  return decodeValue(schema, value, path).pipe(
    Effect.mapError((e) =>
      e._tag === "Unreadable"
        ? unreadable(e.path, e.message, within(base, e.field))
        : e
    )
  )
}

/** The lab's rows of a test, by its `results_raw`'s shape. */
type Payload =
  | { lab: "randox"; order?: RandoxOrder; rows: readonly RandoxRow[] }
  | { lab: "hurdle"; rows: readonly HurdleRow[] }

function payloadOf(
  value: unknown,
  path: string,
  base: string
): Effect.Effect<Payload, ImportError> {
  if (value && typeof value === "object" && !Array.isArray(value))
    return decodeRaw(RandoxOrder, value, path, base).pipe(
      Effect.map((order) => ({
        lab: "randox" as const,
        order,
        rows: order.reportResults,
      }))
    )
  if (Array.isArray(value)) {
    const head: unknown = value[0]
    if (head && typeof head === "object" && "analyte" in head)
      return decodeRaw(RandoxRows, value, path, base).pipe(
        Effect.map((rows) => ({ lab: "randox" as const, rows }))
      )
    return decodeRaw(HurdleRows, value, path, base).pipe(
      Effect.map((rows) => ({ lab: "hurdle" as const, rows }))
    )
  }
  return Effect.fail(
    unreadable(path, "Expected a Randox order or a list of results", base)
  )
}

/** A test as its report and results, its document, and what was left out
 * of it. */
export interface TestReading {
  records: PlannedRecord[]
  documents: PlannedDocument[]
  left: LeftOut[]
}

function same(a: RandoxRow, b: RandoxRow): boolean {
  return (
    a.result === b.result &&
    a.units === b.units &&
    (a.refLow ?? null) === (b.refLow ?? null) &&
    (a.refHigh ?? null) === (b.refHigh ?? null) &&
    (a.lowHigh ?? null) === (b.lowHigh ?? null) &&
    (a.sampleType ?? null) === (b.sampleType ?? null)
  )
}

/** A Numan test as a lab report of the lab that ran it and its results. */
export function readTest(
  chosen: ChosenTest,
  ctx: MetricContext
): Effect.Effect<TestReading, ImportError> {
  return Effect.gen(function* () {
    const { test, index } = chosen
    const path = chosen.from.file.path
    const a = test.attributes
    const sid = a.short_id
    const field = (name: string) => `data[${index}].attributes.${name}`
    const what = `test ${sid}`
    const left: LeftOut[] = []

    // when the sample was taken: London wall-clock, else when the lab
    // received it, in UTC
    let at: string | undefined
    if (a.sample_taken_date) {
      at = londonTime(a.sample_taken_date)
      if (!at)
        return yield* Effect.fail(
          unreadable(
            path,
            "Expected a time as YYYY-MM-DDTHH:MM:SS",
            field("sample_taken_date")
          )
        )
    } else if (a.sample_accessioned_date) {
      at = a.sample_accessioned_date.replace("Z", "+00:00")
      if (parseIso(at)?.offset === undefined)
        return yield* Effect.fail(
          unreadable(
            path,
            "Expected an ISO 8601 time with its offset",
            field("sample_accessioned_date")
          )
        )
    } else {
      return {
        records: [],
        documents: [],
        left: [
          { what, why: "it names no time its sample was taken or received" },
        ],
      }
    }

    const stamp = a.updated_at || a.created_at
    const revision = epoch(stamp)
    if (revision === undefined)
      return yield* Effect.fail(
        unreadable(
          path,
          "Expected an ISO 8601 time",
          field(a.updated_at ? "updated_at" : "created_at")
        )
      )

    const base = field("results_raw")
    const parsed = yield* Effect.try({
      try: () => JSON.parse(a.results_raw) as unknown,
      catch: () => unreadable(path, "the field is not JSON", base),
    })
    const payload = yield* payloadOf(parsed, path, base)
    const rowsField =
      payload.lab === "randox" && payload.order ? `${base}.reportResults` : base

    const clinical = new Map<string, Measurement>()
    for (const o of a.observations_measurements ?? [])
      clinical.set(o.identifier_short, o)
    const clinicalOf = (analyte: AnalyteRef): Band | undefined => {
      const entry = analyteEntry(analyte)?.code
      const code = entry ? own(CLINICAL_CODES, entry) : undefined
      const o = code ? clinical.get(code) : undefined
      return o && clinicalBand(o)
    }
    const method = methodOf(a.results)
    const rows: LabRow[] = []
    let issued: string | null = null
    let ordered: string | null = null

    if (payload.lab === "randox") {
      const seen = new Map<string, RandoxRow>()
      let reported: { text: string; at: number } | undefined
      for (const [j, r] of payload.rows.entries()) {
        const first = seen.get(r.analyte)
        if (first) {
          if (!same(first, r))
            left.push({
              what: `${r.displayName || r.analyte}, ${sid}`,
              why: "a second value under the same key; the first is kept",
            })
          continue
        }
        seen.set(r.analyte, r)
        if (!reported && r.dateOfReport)
          reported = { text: r.dateOfReport, at: j }
        const name = r.displayName || r.analyte
        const analyte = analyteOf(RANDOX_ANALYTES, r.analyte, name)
        const v = valueOf(r.result)
        const bands: Band[] = []
        if (r.refLow || r.refHigh)
          bands.push({
            low: r.refLow || null,
            high: r.refHigh || null,
            lab: "lab",
          })
        let flag = r.lowHigh || null
        if (!flag && bands.length && v.value !== null) flag = "N"
        const numan = clinicalOf(analyte)
        if (numan) bands.push(numan)
        const material = (r.sampleType ?? "").toLowerCase()
        rows.push({
          key: `${sid}/${r.analyte}`,
          analyte,
          coding: { system: "randox", code: r.analyte, display: name },
          ...v,
          unit: r.units,
          bands,
          flag,
          flagText: r.lowHigh || null,
          specimen: material ? { material } : null,
          method,
          upstreamId: `${test.id}/${r.analyte}`,
        })
      }
      if (reported) {
        issued = londonTime(reported.text) ?? null
        if (!issued)
          return yield* Effect.fail(
            unreadable(
              path,
              "Expected a time as YYYY-MM-DDTHH:MM:SS",
              `${rowsField}[${reported.at}].dateOfReport`
            )
          )
      }
      const order = payload.order
      if (order) {
        for (const [fieldName, entry] of ORDER_PRESSURES) {
          const p = order[fieldName]
          if (p === null || p === undefined) continue
          const text =
            typeof p === "number"
              ? Number.isInteger(p)
                ? String(p)
                : pythonFloat(p)
              : p
          rows.push({
            key: `${sid}/${fieldName}`,
            analyte: { entry },
            coding: { system: "randox", code: fieldName, display: fieldName },
            ...valueOf(text),
            unit: "mmHg",
            method: ORDER_PRESSURE_METHOD,
            upstreamId: `${test.id}/${fieldName}`,
          })
        }
        if (order.orderCreatedDate) {
          ordered = londonTime(order.orderCreatedDate) ?? null
          if (!ordered)
            return yield* Effect.fail(
              unreadable(
                path,
                "Expected a time as YYYY-MM-DDTHH:MM:SS",
                `${base}.orderCreatedDate`
              )
            )
        }
      }
    } else {
      const seen = new Map<string, HurdleRow>()
      for (const [j, r] of payload.rows.entries()) {
        const b = r.biomarker
        const name = b.name || b.code
        const first = seen.get(b.code)
        if (first) {
          if (
            first.result !== r.result ||
            first.biomarker.unit !== b.unit ||
            (first.interpretation ?? null) !== (r.interpretation ?? null)
          )
            left.push({
              what: `${name}, ${sid}`,
              why: "a second value under the same key; the first is kept",
            })
          continue
        }
        seen.set(b.code, r)
        const analyte = analyteOf(HURDLE_BIOMARKERS, b.code, name)
        const ruled = hurdleBands(r.interpretationData?.value)
        if (!ruled)
          left.push({
            what: `${name}, ${sid}`,
            why: `Hurdle's ranges for it (${base}[${j}].interpretationData.value) are not in a shape this importer reads, so they are left out`,
          })
        const bands = [...(ruled ?? [])]
        const numan = clinicalOf(analyte)
        if (numan) bands.push(numan)
        rows.push({
          key: `${sid}/${b.code}`,
          analyte,
          coding: { system: "hurdle", code: b.code, display: name },
          ...valueOf(r.result),
          unit: b.unit,
          bands,
          flag: own(HURDLE_FLAGS, r.interpretation ?? "") ?? null,
          flagText: capitalize(r.interpretation ?? "") || null,
          specimen: { material: HURDLE_SPECIMEN },
          method,
          upstreamId: r.testId ?? null,
        })
      }
    }
    issued ??= a.results_received_at ?? null

    const doc = jsonDocument(
      { test: chosen.raw, review: chosen.review ?? null },
      {
        provider: "Numan",
        kind: "test",
        date: dateIn(at, ZONE) ?? at.slice(0, 10),
      }
    )
    const report: LabReportInput = {
      namespace: NAMESPACE,
      key: sid,
      source: SOURCE,
      lab: `${capitalize(payload.lab)} (ordered through Numan)`,
      accession: sid,
      at,
      orderedAt: ordered,
      issuedAt: issued,
      status: a.status,
      file: doc.digest,
      revision,
    }
    const out = labRecords(report, rows, ctx)
    left.push(...out.left)
    if (!out.records.length) {
      left.push({
        what,
        why: "none of its values can be written, so its report is not",
      })
      return { records: [], documents: [], left }
    }
    return { records: out.records, documents: [doc], left }
  })
}
