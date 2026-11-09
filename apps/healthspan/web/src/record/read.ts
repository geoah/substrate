// The clinical record (docs/plans/healthspan.md, The app: Record): one
// list per clinical kind, all small, from every provider that writes them.
// A row's provider is the source of the import that created it, where an
// import did (its `origin`: an import's records have minted ids, which say
// nothing of who wrote them); else, for a provider that keeps its
// upstream's ids as `<provider>/<account>/<id>` (docs/clinical.md), the
// id's first segment. It is named by that source's name, else the service
// of its patient registration, else the care provider its rows name most.
// Nothing here names one.

import { SubstrateError, parseReference } from "@substrate/client"
import type { SubstrateClient, SubstrateRecord } from "@substrate/client"
import { patientOf, type PatientRegistration } from "../profile/owner"

export const CLINICAL = "substrate.reamde.dev/clinical"

export const RECORD_KINDS = [
  "condition",
  "medicationrequest",
  "medicationdispense",
  "allergy",
  "immunization",
  "appointment",
  "referral",
  "document",
  "communication",
  "encounter",
  "organization",
  "patient",
] as const
export type RecordKind = (typeof RECORD_KINDS)[number]

/** Every clinical kind in full, for a read's `kinds`. */
export const CLINICAL_KINDS = RECORD_KINDS.map((k) => `${CLINICAL}/${k}`)

/** Kinds read for lookups, not listed as record rows. */
type LookupKind = "organization" | "patient"
export type RowKind = Exclude<RecordKind, LookupKind>

export interface ClinicalRow {
  id: string
  kind: RowKind
  /** The id's first segment: the provider that wrote it. */
  provider: string
  /** The care provider's record id. */
  organization?: string
  /** The clinical date: when the letter was written, the visit happened. */
  at?: string
  /** When the record reached the repository: its `createdAt`, or its
   * `updatedAt` once it has been revised. What "new" is judged by. */
  arrivedAt: string
  title: string
  p: Record<string, unknown>
}

/** A stored file a record points at (a `blobref`). */
export interface StoredFile {
  digest: string
  mediaType?: string
  name?: string
  size?: number
}

/** The stored files in a `blobref` property, one or many. */
export function filesOf(v: unknown): StoredFile[] {
  const list: unknown[] = Array.isArray(v) ? v : v ? [v] : []
  return list.flatMap((f) => {
    const o = f as Partial<StoredFile> | null
    return o && typeof o.digest === "string"
      ? [
          {
            digest: o.digest,
            mediaType:
              typeof o.mediaType === "string" ? o.mediaType : undefined,
            name: typeof o.name === "string" ? o.name : undefined,
            size: typeof o.size === "number" ? o.size : undefined,
          },
        ]
      : []
  })
}

export interface Organization {
  id: string
  name: string
}

export interface ClinicalRecord {
  rows: Record<RowKind, ClinicalRow[]>
  organizations: Map<string, Organization>
  /** Patient registrations, by id order. Empty where the repository has
   * none. */
  patients: PatientRegistration[]
  /** Each patient registration's provider (`providerOf`). */
  patientProviders: ReadonlyMap<string, string>
  /** A source's id to its name, for a provider an import's source is. */
  sourceNames: ReadonlyMap<string, string>
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() ? v : undefined
}

/** The id a reference to `kind` names: `{ref}` as served, or the path. */
function refTo(v: unknown, kind: string): string | undefined {
  const path =
    typeof v === "string"
      ? v
      : v && typeof v === "object" && "ref" in v
        ? v.ref
        : undefined
  const ref = typeof path === "string" ? parseReference(path) : undefined
  return ref?.kind === kind ? ref.id : undefined
}

/** Which import wrote a record, and its source's name: what names a
 * provider whose records' ids are minted. */
export interface Provenance {
  /** An import's id to its source's id. */
  importSources: ReadonlyMap<string, string>
  /** A source's id to its name. */
  sourceNames: ReadonlyMap<string, string>
}

const IMPORT_KIND = "substrate.reamde.dev/healthspan/import"
const SOURCE_KIND = "substrate.reamde.dev/observations/source"
const NO_PROVENANCE: Provenance = {
  importSources: new Map(),
  sourceNames: new Map(),
}

/** A record's provider: `source:<id>` for one an import created, else its
 * id's first segment. */
export function providerOf(
  r: { id: string; properties?: Record<string, unknown> },
  prov: Provenance = NO_PROVENANCE
): string {
  const imported = refTo(r.properties?.origin, IMPORT_KIND)
  const source = imported && prov.importSources.get(imported)
  if (source) return `source:${source}`
  return r.id.split("/")[0]
}

export function toRow(
  kind: RowKind,
  r: SubstrateRecord,
  prov: Provenance = NO_PROVENANCE
): ClinicalRow {
  const p = r.properties
  return {
    id: r.id,
    kind,
    provider: providerOf(r, prov),
    organization: refTo(p.organization, `${CLINICAL}/organization`),
    at: str(p.at),
    arrivedAt: (r.version ?? 1) > 1 ? r.updatedAt : r.createdAt,
    // The kinds' display templates, then what the server rendered.
    title:
      str(p.name) ??
      str(p.summary) ??
      str(p.topic) ??
      str(p.medication) ??
      str(r.title) ??
      str(p.type) ??
      str(p.specialty) ??
      str(p.sender) ??
      r.id,
    p,
  }
}

export function toRecord(
  lists: Partial<Record<string, SubstrateRecord[]>>,
  prov: Provenance = NO_PROVENANCE
): ClinicalRecord {
  const rows = {} as ClinicalRecord["rows"]
  for (const k of RECORD_KINDS) {
    if (k === "organization" || k === "patient") continue
    rows[k] = (lists[k] ?? [])
      .filter((r) => !r.deletedAt)
      .map((r) => toRow(k, r, prov))
      .sort((a, b) => (b.at ?? "").localeCompare(a.at ?? ""))
  }
  const organizations = new Map(
    (lists.organization ?? []).map((r) => [
      r.id,
      { id: r.id, name: str(r.properties.name) ?? str(r.title) ?? r.id },
    ])
  )
  const live = (lists.patient ?? []).filter((r) => !r.deletedAt)
  const patients = live.map(patientOf).sort((a, b) => a.id.localeCompare(b.id))
  const patientProviders = new Map(live.map((r) => [r.id, providerOf(r, prov)]))
  return {
    rows,
    organizations,
    patients,
    patientProviders,
    sourceNames: prov.sourceNames,
  }
}

/** Every row the record lists, of every kind. */
export function allRows(rec: ClinicalRecord): ClinicalRow[] {
  return Object.values(rec.rows).flat()
}

/** Whether the record has anything to list. */
export function hasRows(rec: ClinicalRecord): boolean {
  return Object.values(rec.rows).some((rows) => rows.length > 0)
}

export interface Provider {
  id: string
  name: string
}

/** The providers the rows come from, by name: a provider's patient
 * registration's service, else the care provider its rows name most, else
 * its id. */
export function providersOf(rec: ClinicalRecord): Provider[] {
  const counts = new Map<string, Map<string, number>>()
  for (const r of allRows(rec)) {
    const named = counts.get(r.provider) ?? new Map<string, number>()
    counts.set(r.provider, named)
    const org = r.organization && rec.organizations.get(r.organization)?.name
    if (org) named.set(org, (named.get(org) ?? 0) + 1)
  }
  return [...counts.entries()]
    .map(([id, named]) => {
      const source = id.startsWith("source:")
        ? rec.sourceNames.get(id.slice("source:".length))
        : undefined
      const service = rec.patients.find(
        (p) => rec.patientProviders.get(p.id) === id && p.service
      )?.service
      const most = [...named.entries()].sort(
        (a, b) => b[1] - a[1] || a[0].localeCompare(b[0])
      )[0]?.[0]
      return { id, name: source ?? service ?? most ?? id }
    })
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id))
}

/** A row's care provider by name, else its provider's. */
export function organizationName(
  rec: ClinicalRecord,
  r: ClinicalRow,
  providers: readonly Provider[]
): string {
  return (
    (r.organization && rec.organizations.get(r.organization)?.name) ??
    providers.find((p) => p.id === r.provider)?.name ??
    r.provider
  )
}

/** A refusal of the read itself (the kind not installed, no scope for
 * it): read as none. */
function refused(e: unknown): boolean {
  return (
    e instanceof SubstrateError &&
    e.status >= 400 &&
    e.status < 500 &&
    e.status !== 401 &&
    e.status !== 429
  )
}

/** One list per kind. A repository without the clinical package reads as
 * an empty record. */
export async function readRecord(
  client: SubstrateClient,
  signal: AbortSignal
): Promise<ClinicalRecord> {
  // The clinical kinds come in one package: where its patient kind is
  // refused, none is installed, and nine more refusals would say nothing.
  try {
    await client.list({ kinds: [`${CLINICAL}/patient`], first: 1 }, signal)
  } catch (e) {
    if (refused(e)) return toRecord({})
    throw e
  }
  const [imports, sources] = await Promise.all([
    client.listAll({ kinds: [IMPORT_KIND] }, 5000, signal),
    client.listAll({ kinds: [SOURCE_KIND] }, 5000, signal),
  ])
  const prov: Provenance = {
    importSources: new Map(
      imports.records.flatMap((r) => {
        const source = refTo(r.properties.source, SOURCE_KIND)
        return source ? [[r.id, source] as const] : []
      })
    ),
    sourceNames: new Map(
      sources.records.flatMap((r) => {
        const name = str(r.properties.name)
        return name ? [[r.id, name] as const] : []
      })
    ),
  }
  const lists = await Promise.all(
    RECORD_KINDS.map(async (k) => {
      try {
        const page = await client.listAll(
          { kinds: [`${CLINICAL}/${k}`] },
          5000,
          signal
        )
        return page.records
      } catch (e) {
        if (refused(e)) return []
        throw e
      }
    })
  )
  return toRecord(
    Object.fromEntries(RECORD_KINDS.map((k, i) => [k, lists[i]])),
    prov
  )
}

/** `P18W` -> days; undefined when the duration is not one this reads. */
export function durationDays(iso: unknown): number | undefined {
  if (typeof iso !== "string") return undefined
  const m = /^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?$/.exec(iso)
  if (!m || iso === "P") return undefined
  const [, y, mo, w, d] = m.map((x) => (x ? Number(x) : 0))
  return y * 365 + mo * 30 + w * 7 + d
}

/** Opens a stored file through the blob route, in a new tab where the
 * browser allows one, else as a download. */
export async function openStoredFile(
  client: SubstrateClient,
  file: StoredFile
): Promise<void> {
  if (!/^blob-sha256-[0-9a-f]{64}$/.test(file.digest))
    throw new Error("Not a stored file.")
  const tab = window.open("", "_blank")
  try {
    const res = await client.send("GET", `api/v1/blobs/${file.digest}`)
    const bytes = await res.blob()
    const url = URL.createObjectURL(
      file.mediaType ? new Blob([bytes], { type: file.mediaType }) : bytes
    )
    if (tab) tab.location.href = url
    else {
      const a = document.createElement("a")
      a.href = url
      a.download = file.name ?? file.digest
      a.click()
    }
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
  } catch (e) {
    tab?.close()
    throw e
  }
}
