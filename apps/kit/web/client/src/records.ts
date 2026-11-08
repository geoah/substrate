/** A record as the REST API answers it (internal/substrate's wire shape). */
export interface SubstrateRecord {
  id: string
  kind: string
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  version: number
  createdAt: string
  updatedAt: string
  deletedAt?: string
  title?: string
}

/** One page of `GET /api/v1/records`. `head` and `generation` are the change
 * feed's handoff: a watch resumed from them sees every write after the read. */
export interface RecordPage {
  records: SubstrateRecord[]
  cursor?: string
  head?: number
  generation?: string
}

/** A list read: the kinds in `filter.kinds`, the property arms beside them. */
export interface ListQuery {
  kinds: string[]
  properties?: Record<string, unknown>
  first?: number
  after?: string
  orderBy?: string
}

/** A record's own key where several kinds share one store. */
export function recordKey(kind: string, id: string): string {
  return `${kind}/${id}`
}

export function stringProp(
  r: SubstrateRecord,
  key: string
): string | undefined {
  const v = r.properties[key]
  return typeof v === "string" && v !== "" ? v : undefined
}

export function numberProp(
  r: SubstrateRecord,
  key: string
): number | undefined {
  const v = r.properties[key]
  if (typeof v === "number") return v
  if (typeof v === "string" && v.trim() !== "" && !Number.isNaN(Number(v)))
    return Number(v)
  return undefined
}

export function booleanProp(
  r: SubstrateRecord,
  key: string
): boolean | undefined {
  const v = r.properties[key]
  return typeof v === "boolean" ? v : undefined
}

export interface RecordReference {
  kind: string
  id: string
}

/** A reference from its path, `<authority>/<package>/<kind>/<id>`. */
export function parseReference(path: string): RecordReference | undefined {
  const parts = path.split("/")
  if (parts.length < 4 || parts.slice(0, 4).some((p) => p === ""))
    return undefined
  return { kind: parts.slice(0, 3).join("/"), id: parts.slice(3).join("/") }
}

/** A reference property's value: `{ref: <path>}` as served, or the bare
 * path. A write sends the bare path. */
export function refProp(
  r: SubstrateRecord,
  key: string
): RecordReference | undefined {
  const v = r.properties[key]
  if (typeof v === "string") return parseReference(v)
  if (
    v &&
    typeof v === "object" &&
    typeof (v as { ref?: unknown }).ref === "string"
  ) {
    return parseReference((v as { ref: string }).ref)
  }
  return undefined
}

export function referencePath(kind: string, id: string): string {
  return `${kind}/${id}`
}
