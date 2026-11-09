import { parseReference } from "@substrate/client"
import {
  defineKind,
  refProp,
  stringProp,
  type SubstrateRecord,
} from "@substrate/react"
import type { ImporterId } from "./import/importers/types"

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
  | "staging"
  | "planned"
  | "importing"
  | "stopped"
  | "imported"
  | "undoing"
  | "undone"

export type { ImporterId }

/** `import.yaml`'s `importer` values: every importer's id. */
export const IMPORTER_IDS: readonly ImporterId[] = [
  "csv",
  "manual",
  "numan",
  "randox",
  "dxa",
  "nhs",
  "mychart",
]

export interface ImportRunner {
  device: string
  until: string
}

/** What the latest run of an import did. */
export interface ImportCounts {
  created: number
  /** A raised revision alone counts here, as the scripts count it. */
  updated: number
  unchanged: number
  /** A newer export already wrote it: left alone. */
  stale: number
  /** References left out: no import has written their record. */
  left: number
}

export const NO_COUNTS: ImportCounts = {
  created: 0,
  updated: 0,
  unchanged: 0,
  stale: 0,
  left: 0,
}

/** An import as the screens and the runner read it. */
export interface ImportRecord {
  id: string
  version: number
  name: string
  importer?: ImporterId
  status: ImportStatus
  runner?: ImportRunner
  /** For a CSV or a hand entry, the lab's name, which names its source
   * where none exists yet. */
  lab?: string
  /** The source id the import names. */
  source?: string
  /** The plan blob's digest, from `planned` on. */
  plan?: string
  /** The digests of the documents named so far. */
  documents: string[]
  /** What the latest run did; absent until a run saved some. */
  counts?: ImportCounts
  problem?: string
  importedAt?: string
  skipped?: string
}

/** What a write of an import names: null clears a field. `status` moves
 * only by a patch, along a declared transition. */
export interface ImportDraft {
  name: string
  importer: ImporterId
  status: ImportStatus
  runner: ImportRunner | null
  lab: string
  source: string | null
  /** A blob digest, `blob-sha256-<hex>`. */
  plan: string | null
  documents: string[]
  counts: ImportCounts | null
  problem: string | null
  importedAt: string | null
  skipped: string | null
}

const STATUSES: readonly ImportStatus[] = [
  "staging",
  "planned",
  "importing",
  "stopped",
  "imported",
  "undoing",
  "undone",
]

function str(o: Record<string, unknown>, key: string): string | undefined {
  const v = o[key]
  return typeof v === "string" && v !== "" ? v : undefined
}

/** A blob reference as served (`{digest, name, mediaType, size, status}`,
 * or the bare `{digest}` of one whose manifest is gone) or as written (the
 * digest). */
export function blobDigest(v: unknown): string | undefined {
  if (typeof v === "string") return v || undefined
  if (v && typeof v === "object" && "digest" in v) {
    const d = v.digest
    return typeof d === "string" && d ? d : undefined
  }
  return undefined
}

function count(o: Record<string, unknown>, key: string): number {
  const v = o[key]
  return typeof v === "number" && Number.isFinite(v) && v > 0
    ? Math.floor(v)
    : 0
}

function readCounts(v: unknown): ImportCounts | undefined {
  if (!v || typeof v !== "object") return undefined
  const o = v as Record<string, unknown>
  return {
    created: count(o, "created"),
    updated: count(o, "updated"),
    unchanged: count(o, "unchanged"),
    stale: count(o, "stale"),
    left: count(o, "left"),
  }
}

export const Import = defineKind(KIND.import, {
  read: (r): ImportRecord => {
    const status = stringProp(r, "status")
    const runner = r.properties.runner
    const importer = stringProp(r, "importer")
    const documents = r.properties.documents
    return {
      id: r.id,
      version: r.version,
      name: stringProp(r, "name") ?? "Import",
      importer: IMPORTER_IDS.find((i) => i === importer),
      status: STATUSES.find((s) => s === status) ?? "staging",
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
      plan: blobDigest(r.properties.plan),
      documents: Array.isArray(documents)
        ? documents.flatMap((d) => blobDigest(d) ?? [])
        : [],
      counts: readCounts(r.properties.counts),
      problem: stringProp(r, "problem"),
      importedAt: stringProp(r, "importedAt"),
      skipped: stringProp(r, "skipped"),
    }
  },
  write: (d: Partial<ImportDraft>) => {
    const out: Record<string, unknown> = {}
    if (d.name !== undefined) out.name = d.name
    if (d.importer !== undefined) out.importer = d.importer
    if (d.status !== undefined) out.status = d.status
    if (d.runner !== undefined) out.runner = d.runner
    if (d.lab !== undefined) out.lab = d.lab
    if (d.source !== undefined)
      out.source = d.source === null ? null : `${KIND.source}/${d.source}`
    if (d.plan !== undefined) out.plan = d.plan
    if (d.documents !== undefined) out.documents = [...d.documents]
    if (d.counts !== undefined)
      out.counts =
        d.counts === null
          ? null
          : {
              created: d.counts.created,
              updated: d.counts.updated,
              unchanged: d.counts.unchanged,
              stale: d.counts.stale,
              left: d.counts.left,
            }
    if (d.problem !== undefined) out.problem = d.problem
    if (d.importedAt !== undefined) out.importedAt = d.importedAt
    if (d.skipped !== undefined) out.skipped = d.skipped
    return out
  },
})
