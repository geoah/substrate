// A plan as the import keeps it (docs/plans/healthspan.md, The package,
// import): the importer's `Plan` with every document named by its digest and
// none of its bytes, written as JSON to the blob the import's `plan` names.
// Any device reads it back from there to resume or undo the import.

import type {
  ImporterId,
  LeftOut,
  Plan,
  PlannedRecord,
  RecordKey,
} from "./importers/types"
import { sha256 } from "./ids"

/** A document as the plan blob names it: no bytes. */
export interface StoredDocument {
  digest: string
  name: string
  mediaType: string
}

/** The plan blob's JSON. */
export interface StoredPlan {
  /** The shape's version. */
  v: 1
  importer: ImporterId
  name: string
  source?: RecordKey
  records: PlannedRecord[]
  documents: StoredDocument[]
  left: LeftOut[]
}

/** The plan as it is kept: its documents by digest alone, each once. */
export function storedPlan(plan: Plan): StoredPlan {
  const seen = new Set<string>()
  const documents: StoredDocument[] = []
  for (const d of plan.documents) {
    if (seen.has(d.digest)) continue
    seen.add(d.digest)
    documents.push({ digest: d.digest, name: d.name, mediaType: d.mediaType })
  }
  const out: StoredPlan = {
    v: 1,
    importer: plan.importer,
    name: plan.name,
    records: plan.records,
    documents,
    left: plan.left,
  }
  if (plan.source) out.source = plan.source
  return out
}

/** The plan blob's bytes. */
export function planBytes(plan: StoredPlan): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(plan))
}

const HEX = "0123456789abcdef"

/** The blob digest of `bytes`, `blob-sha256-<hex>`, as the server derives
 * it. */
export function blobDigestOf(bytes: Uint8Array): string {
  let hex = ""
  for (const b of sha256(bytes)) hex += HEX[b >> 4] + HEX[b & 15]
  return `blob-sha256-${hex}`
}

function isKey(v: unknown): v is RecordKey {
  if (!v || typeof v !== "object") return false
  const o = v as Record<string, unknown>
  return (
    typeof o.kind === "string" &&
    typeof o.namespace === "string" &&
    typeof o.key === "string"
  )
}

/** A plan blob read back; one that is not a plan throws, naming why. */
export function parsePlan(bytes: Uint8Array): StoredPlan {
  let raw: unknown
  try {
    raw = JSON.parse(new TextDecoder().decode(bytes))
  } catch {
    throw new Error("Its plan is not JSON.")
  }
  if (!raw || typeof raw !== "object")
    throw new Error("Its plan is not an object.")
  const o = raw as Record<string, unknown>
  if (o.v !== 1) throw new Error("Its plan is of a shape this app cannot read.")
  if (!Array.isArray(o.records) || !o.records.every(isKey))
    throw new Error("Its plan's records are not records.")
  for (const r of o.records as unknown as Record<string, unknown>[])
    if (!r.properties || typeof r.properties !== "object")
      throw new Error("A record of its plan has no properties.")
  return {
    v: 1,
    importer: o.importer as ImporterId,
    name: typeof o.name === "string" ? o.name : "Import",
    source: isKey(o.source) ? o.source : undefined,
    records: o.records as PlannedRecord[],
    documents: Array.isArray(o.documents)
      ? (o.documents as StoredDocument[])
      : [],
    left: Array.isArray(o.left) ? (o.left as LeftOut[]) : [],
  }
}

/** What the importer left out, as the import's `skipped`. */
export function leftMarkdown(left: readonly LeftOut[]): string | undefined {
  if (!left.length) return undefined
  return left.map((l) => `- ${l.what}: ${l.why}`).join("\n")
}

/** A key's identity within a plan. */
export function keyOf(k: RecordKey): string {
  return `${k.kind}\u0000${k.namespace}\u0000${k.key}`
}
