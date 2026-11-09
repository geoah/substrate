import { parseReference } from "@substrate/client"
import {
  defineKind,
  numberProp,
  refProp,
  stringProp,
  type SubstrateRecord,
} from "@substrate/react"

/** Every kind the app names, in full: its own package's two, and the shared
 * ones it reads (and, for labs and sources, writes). */
export const KIND = {
  profile: "substrate.reamde.dev/healthspan/profile",
  import: "substrate.reamde.dev/healthspan/import",
  contact: "substrate.reamde.dev/core/contact",
  setting: "substrate.reamde.dev/core/setting",
  unit: "substrate.reamde.dev/core/unit",
  source: "substrate.reamde.dev/observations/source",
  sourcelink: "substrate.reamde.dev/observations/sourcelink",
  metric: "substrate.reamde.dev/observations/metric",
  observation: "substrate.reamde.dev/observations/observation",
  trace: "substrate.reamde.dev/observations/trace",
  labresult: "substrate.reamde.dev/labs/labresult",
  labreport: "substrate.reamde.dev/labs/labreport",
  analyte: "substrate.reamde.dev/labs/analyte",
  sleep: "substrate.reamde.dev/health/sleep",
  workout: "substrate.reamde.dev/health/workout",
  patient: "substrate.reamde.dev/clinical/patient",
} as const

/** The setting whose `value` is the id of the owner's own contact
 * (decision 0231). */
export const OWNER_CONTACT_SETTING = "substrate.reamde.dev/core/ownerContact"

/** The one profile's id: every device names the same row. */
export const PROFILE_ID = "owner"

export type Sex = "female" | "male"

/** The ids a repeated reference property holds, in order, for references
 * of `kind`; anything else is left out. */
export function refIds(
  r: SubstrateRecord,
  key: string,
  kind: string
): string[] {
  const v = r.properties[key]
  if (!Array.isArray(v)) return []
  const ids: string[] = []
  for (const item of v) {
    const path =
      typeof item === "string"
        ? item
        : item && typeof item === "object" && "ref" in item
          ? (item as { ref: unknown }).ref
          : undefined
    const ref = typeof path === "string" ? parseReference(path) : undefined
    if (ref?.kind === kind) ids.push(ref.id)
  }
  return ids
}

/** The profile as the screens read it. */
export interface Profile {
  id: string
  version: number
  name: string
  /** The contact it describes, by id. */
  contact?: string
  birthDate?: string
  sex?: Sex
  /** Source ids, first highest. */
  nightSources: string[]
  workoutSources: string[]
  startedAt?: string
  recordSeenAt?: string
}

/** What a write of the profile names: null clears a field. */
export interface ProfileDraft {
  name: string
  contact: string | null
  birthDate: string | null
  sex: Sex | null
  nightSources: string[]
  workoutSources: string[]
  startedAt: string | null
  recordSeenAt: string | null
}

function sexOf(v: string | undefined): Sex | undefined {
  return v === "female" || v === "male" ? v : undefined
}

const sourceRefs = (ids: string[]) => ids.map((id) => `${KIND.source}/${id}`)

export const Profile = defineKind(KIND.profile, {
  read: (r): Profile => ({
    id: r.id,
    version: r.version,
    name: stringProp(r, "name") ?? "Healthspan profile",
    contact: refProp(r, "contact")?.id,
    birthDate: stringProp(r, "birthDate"),
    sex: sexOf(stringProp(r, "sex")),
    nightSources: refIds(r, "nightSources", KIND.source),
    workoutSources: refIds(r, "workoutSources", KIND.source),
    startedAt: stringProp(r, "startedAt"),
    recordSeenAt: stringProp(r, "recordSeenAt"),
  }),
  write: (d: Partial<ProfileDraft>) => {
    const out: Record<string, unknown> = {}
    if (d.name !== undefined) out.name = d.name
    if (d.contact !== undefined)
      out.contact = d.contact === null ? null : `${KIND.contact}/${d.contact}`
    if (d.birthDate !== undefined) out.birthDate = d.birthDate
    if (d.sex !== undefined) out.sex = d.sex
    if (d.nightSources !== undefined)
      out.nightSources = sourceRefs(d.nightSources)
    if (d.workoutSources !== undefined)
      out.workoutSources = sourceRefs(d.workoutSources)
    if (d.startedAt !== undefined) out.startedAt = d.startedAt
    if (d.recordSeenAt !== undefined) out.recordSeenAt = d.recordSeenAt
    return out
  },
})

export type ImportStatus =
  "planned" | "importing" | "stopped" | "imported" | "undoing" | "undone"

/** One result the plan writes. */
export interface ImportRow {
  key: string
  /** The key of its report in the plan. */
  report: string
  name?: string
  /** The metric's id, such as `labs/ldl_cholesterol`. */
  metric: string
  takenAt: string
  value?: string
  comparator?: string
  unit?: string
  text?: string
  low?: string
  high?: string
  flag?: string
}

export interface ImportReport {
  key: string
  takenAt: string
  accession?: string
}

export interface ImportRunner {
  device: string
  until: string
}

/** An import as the screens and the runner read it. */
export interface LabImport {
  id: string
  version: number
  name: string
  format?: "csv" | "manual"
  status: ImportStatus
  runner?: ImportRunner
  /** The lab's name, which names its source where none exists yet. */
  lab?: string
  /** The lab's source id. */
  source?: string
  rows: ImportRow[]
  reports: ImportReport[]
  results: number
  problem?: string
  importedAt?: string
  skipped?: string
}

/** What a write of an import names: null clears a field. `status` moves
 * only by a patch, along a declared transition. */
export interface ImportDraft {
  name: string
  format: "csv" | "manual"
  status: ImportStatus
  runner: ImportRunner | null
  lab: string
  source: string | null
  rows: ImportRow[]
  reports: ImportReport[]
  results: number
  problem: string | null
  importedAt: string | null
  skipped: string | null
}

const STATUSES: readonly ImportStatus[] = [
  "planned",
  "importing",
  "stopped",
  "imported",
  "undoing",
  "undone",
]

function objects(v: unknown): Record<string, unknown>[] {
  return Array.isArray(v)
    ? v.filter(
        (x): x is Record<string, unknown> => !!x && typeof x === "object"
      )
    : []
}

function str(o: Record<string, unknown>, key: string): string | undefined {
  const v = o[key]
  return typeof v === "string" && v !== "" ? v : undefined
}

/** A reference field inside an object: `{ref}` as served or a bare path. */
function refId(v: unknown, kind: string): string | undefined {
  const path =
    typeof v === "string"
      ? v
      : v && typeof v === "object" && "ref" in v
        ? v.ref
        : undefined
  const ref = typeof path === "string" ? parseReference(path) : undefined
  return ref?.kind === kind ? ref.id : undefined
}

function readRow(o: Record<string, unknown>): ImportRow | undefined {
  const key = str(o, "key")
  const report = str(o, "report")
  const metric = refId(o.metric, KIND.metric)
  const takenAt = str(o, "takenAt")
  if (!key || !report || !metric || !takenAt) return undefined
  return {
    key,
    report,
    metric,
    takenAt,
    name: str(o, "name"),
    value: str(o, "value"),
    comparator: str(o, "comparator"),
    unit: str(o, "unit"),
    text: str(o, "text"),
    low: str(o, "low"),
    high: str(o, "high"),
    flag: str(o, "flag"),
  }
}

function writeRow(row: ImportRow): Record<string, unknown> {
  const out: Record<string, unknown> = {
    key: row.key,
    report: row.report,
    metric: `${KIND.metric}/${row.metric}`,
    takenAt: row.takenAt,
  }
  for (const k of [
    "name",
    "value",
    "comparator",
    "unit",
    "text",
    "low",
    "high",
    "flag",
  ] as const) {
    if (row[k] !== undefined) out[k] = row[k]
  }
  return out
}

export const Import = defineKind(KIND.import, {
  read: (r): LabImport => {
    const status = stringProp(r, "status")
    const runner = r.properties.runner
    const format = stringProp(r, "format")
    return {
      id: r.id,
      version: r.version,
      name: stringProp(r, "name") ?? "Import",
      format: format === "csv" || format === "manual" ? format : undefined,
      status: STATUSES.find((s) => s === status) ?? "planned",
      runner:
        runner && typeof runner === "object"
          ? (() => {
              const o = runner as Record<string, unknown>
              const device = str(o, "device")
              const until = str(o, "until")
              return device && until ? { device, until } : undefined
            })()
          : undefined,
      lab: stringProp(r, "lab"),
      source: refProp(r, "source")?.id,
      rows: objects(r.properties.rows)
        .map(readRow)
        .filter((x): x is ImportRow => !!x),
      reports: objects(r.properties.reports).flatMap((o) => {
        const key = str(o, "key")
        const takenAt = str(o, "takenAt")
        return key && takenAt
          ? [{ key, takenAt, accession: str(o, "accession") }]
          : []
      }),
      results: numberProp(r, "results") ?? 0,
      problem: stringProp(r, "problem"),
      importedAt: stringProp(r, "importedAt"),
      skipped: stringProp(r, "skipped"),
    }
  },
  write: (d: Partial<ImportDraft>) => {
    const out: Record<string, unknown> = {}
    if (d.name !== undefined) out.name = d.name
    if (d.format !== undefined) out.format = d.format
    if (d.status !== undefined) out.status = d.status
    if (d.runner !== undefined) out.runner = d.runner
    if (d.lab !== undefined) out.lab = d.lab
    if (d.source !== undefined)
      out.source = d.source === null ? null : `${KIND.source}/${d.source}`
    if (d.rows !== undefined) out.rows = d.rows.map(writeRow)
    if (d.reports !== undefined)
      out.reports = d.reports.map((r) =>
        r.accession === undefined
          ? { key: r.key, takenAt: r.takenAt }
          : { key: r.key, takenAt: r.takenAt, accession: r.accession }
      )
    if (d.results !== undefined) out.results = d.results
    if (d.problem !== undefined) out.problem = d.problem
    if (d.importedAt !== undefined) out.importedAt = d.importedAt
    if (d.skipped !== undefined) out.skipped = d.skipped
    return out
  },
})
