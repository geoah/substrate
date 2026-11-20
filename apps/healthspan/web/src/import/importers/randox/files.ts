// The files randoxctl writes from the Randox Health portal (pkg/rawstore's
// layout), found in an input and decoded by their schemas
// (apps/healthspan/docs/importers/02-randox.md, What it reads):
//
//   manifest.json                     every stored file's endpoint, and the runs
//   items/visit/<visit id>.json       a visit, its orders and their report data
//   items/visit-reports/<id>.json     the visit's published PDFs
//   files/report/<file name>.pdf      the PDFs themselves

import { Effect, Schema } from "effect"
import type { ImportError, Input, InputFile } from "../types"
import { decodeValue, parseJson, readText } from "../shared"

// ---- the folder ----

/** Where the export's files are in an input. */
export interface Layout {
  /** The prefix every path of the export starts with: "" for a folder or
   * zip of the export itself. */
  root: string
  /** `items/visit/*.json`, by the visit id as a number. */
  visits: InputFile[]
  manifest?: InputFile
  /** A file of the export by its path under the root. */
  file(path: string): InputFile | undefined
}

const VISIT = /^(.*?)items\/visit\/([^/]+)\.json$/

/** A visit file's id as the folder sorts it: by number where it is one,
 * after those by name. */
function stemOrder(a: string, b: string): number {
  const x = /^\d+$/.test(a) ? Number(a) : Number.NaN
  const y = /^\d+$/.test(b) ? Number(b) : Number.NaN
  if (!Number.isNaN(x) && !Number.isNaN(y)) return x - y
  if (!Number.isNaN(x)) return -1
  if (!Number.isNaN(y)) return 1
  return a < b ? -1 : a > b ? 1 : 0
}

/** The stem of a visit file: its visit id. */
export function visitStem(file: InputFile): string {
  return VISIT.exec(file.path)?.[2] ?? file.path
}

/** The export in an input: its visits, under the folder the first visit
 * file names; undefined where the input holds no `items/visit/*.json`. */
export function layoutOf(input: Input): Layout | undefined {
  const first = input.files.find((f) => VISIT.test(f.path))
  if (!first) return undefined
  const root = VISIT.exec(first.path)![1]
  const byPath = new Map(input.files.map((f) => [f.path, f]))
  const visits = input.files
    .filter((f) => {
      const m = VISIT.exec(f.path)
      return !!m && m[1] === root
    })
    .sort((a, b) => stemOrder(visitStem(a), visitStem(b)))
  return {
    root,
    visits,
    manifest: byPath.get(`${root}manifest.json`),
    file: (path) => byPath.get(`${root}${path}`),
  }
}

// ---- schemas ----

const Text = Schema.optional(Schema.NullOr(Schema.String))
const Flag = Schema.optional(Schema.NullOr(Schema.Boolean))

/** One band of a test's reference range. */
export const RandoxRange = Schema.Struct({
  sortOrder: Schema.optional(Schema.NullOr(Schema.Number)),
  description: Text,
  /** 0 between `value1` and `value2`; 1 `<`, 2 `<=`, 3 `>`, 4 `>=` `value1`. */
  operator: Schema.optional(Schema.NullOr(Schema.Number)),
  value1: Text,
  value2: Text,
  /** `G` green, `A` amber, `R` red. */
  statusColourSymbol: Text,
  /** Whether the result fell in the band. */
  matchedRange: Flag,
})
export type RandoxRange = typeof RandoxRange.Type

export const RandoxReferenceRange = Schema.Struct({
  unit: Text,
  ranges: Schema.optional(Schema.NullOr(Schema.Array(RandoxRange))),
})
export type RandoxReferenceRange = typeof RandoxReferenceRange.Type

const RandoxTest = Schema.Struct({
  name: Schema.String,
  result: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        displayValue: Schema.optional(
          Schema.NullOr(Schema.Union([Schema.String, Schema.Number]))
        ),
      })
    )
  ),
  referenceRange: Schema.optional(Schema.NullOr(RandoxReferenceRange)),
  secondaryReferenceRange: Schema.optional(Schema.Unknown),
})

/** A reported order's report data (`phpReportData`). */
export const RandoxReport = Schema.Struct({
  summaryData: Schema.Struct({
    /** UK wall-clock, no offset. */
    sampleCollectionDate: Schema.String,
    fastingDate: Text,
    resultDate: Text,
  }),
  panels: Schema.optional(
    Schema.NullOr(
      Schema.Array(
        Schema.Struct({
          name: Schema.String,
          tests: Schema.optional(Schema.NullOr(Schema.Array(RandoxTest))),
        })
      )
    )
  ),
})
export type RandoxReport = typeof RandoxReport.Type

/** A visit file (`items/visit/<id>.json`): its orders, each read in full
 * only where it is reported. */
export const RandoxVisit = Schema.Struct({
  /** The portal's own version of the visit, UK wall-clock. */
  lastUpdated: Text,
  nexusOrders: Schema.optional(
    Schema.NullOr(
      Schema.Array(
        Schema.Struct({
          id: Schema.Union([Schema.Number, Schema.String]),
          /** The sample id. */
          nexusExternalNumber: Text,
          /** The programme the order is part of. */
          nexusOrderProgrammeName: Text,
          isReported: Flag,
          phpReportData: Schema.optional(Schema.Unknown),
        })
      )
    )
  ),
})
export type RandoxVisit = typeof RandoxVisit.Type

/** A visit's published PDFs (`items/visit-reports/<id>.json`). */
export const RandoxListing = Schema.Array(
  Schema.Struct({
    fileName: Text,
    published: Flag,
    isPreliminaryReport: Flag,
  })
)
export type RandoxListing = typeof RandoxListing.Type

/** The folder's manifest: the runs the revision falls back on. */
export const RandoxManifest = Schema.Struct({
  runs: Schema.optional(
    Schema.NullOr(
      Schema.Array(
        Schema.Struct({
          started: Text,
          startedAt: Text,
        })
      )
    )
  ),
})

/** A value decoded by `schema`, the field it fails at named from the
 * file's top (`nexusOrders[0].phpReportData.summaryData`). */
export function decodeAt<A>(
  schema: Schema.Decoder<A>,
  value: unknown,
  path: string,
  at: string
): Effect.Effect<A, ImportError> {
  return decodeValue(schema, value, path).pipe(
    Effect.mapError((e) =>
      e._tag === "Unreadable"
        ? { ...e, field: e.field ? `${at}.${e.field}` : at }
        : e
    )
  )
}

// ---- the portal's marks ----

/** The endpoint randoxctl stores each visit from: the Randox Health
 * portal's API. */
export const VISITS_ENDPOINT = "v2/Visit/GetAllVisitsByUserIdWithReports"

/** A JSON file's value, or undefined where it cannot be read or is not
 * JSON: detection never fails. */
function jsonOf(file: InputFile | undefined): Effect.Effect<unknown> {
  if (!file) return Effect.succeed(undefined)
  return readText(file).pipe(
    Effect.flatMap((t) => parseJson(t, file.path)),
    Effect.orElseSucceed(() => undefined)
  )
}

const isObject = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v)

/** Whether the manifest is randoxctl's: a file map whose visits came from
 * the portal's visits endpoint. */
export function manifestIsRandox(file: InputFile | undefined) {
  return jsonOf(file).pipe(
    Effect.map((m) => {
      if (!isObject(m) || !isObject(m.files)) return false
      return Object.values(m.files).some(
        (e) =>
          isObject(e) &&
          typeof e.endpoint === "string" &&
          e.endpoint.startsWith(VISITS_ENDPOINT)
      )
    })
  )
}

/** Whether a visit file is in the portal's shape: an object with its
 * orders (`nexusOrders`). */
export function visitIsRandox(file: InputFile | undefined) {
  return jsonOf(file).pipe(
    Effect.map((v) => isObject(v) && Array.isArray(v.nexusOrders))
  )
}
